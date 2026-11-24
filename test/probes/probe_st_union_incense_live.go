//go:build probe

// 目标: 定位仙盟上香 (179/11) 在面板显示今日未上香时仍被静默丢弃的原因，
// 对比主服 / 仙界跨服 / 仙盟城镇三种会话与不同前置序列。
//
// 运行命令:
//
//	SXD_CONFIG=configs/qq.yaml TMPDIR=/home/tongchen/gotmp \
//	  go run -tags=probe test/probes/probe_st_union_incense_live.go
//
// 行为: 登录 → 返回城镇 → 依次在三个会话上执行
//
//	179/0 许愿树 → 179/10 神像 → 179/11 上香(offer_id=1，仅铜钱档)
//
// 打印 179/0 与 179/10 的完整 hex、179/11 是否回包与结果码。
// 只使用 offer_id=1，绝不使用元宝档 (2/3)。
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/qq.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "path", cfgPath, "err", err)
		return
	}
	if len(cfg.Accounts) == 0 || len(cfg.Accounts[0].Roles) == 0 {
		slog.Error("配置缺少账号或角色")
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[0]

	ticketCache := platform.NewTicketCache(90 * time.Minute)
	xdAuth, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		return
	}
	xdAuth.SetCookie(acc.Cookie)
	ticketCache.RegisterAuthenticator(xdAuth)

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	ticketCache.Invalidate(acc.Platform, acc.Username, role.ServerID)
	ticket, err := ticketCache.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		slog.Error("换票失败", "err", err)
		return
	}
	roleCfg := client.SessionConfig{
		RoleID:               fmt.Sprintf("%s-%s", role.ServerID, role.RoleName),
		RoleName:             role.RoleName,
		Username:             acc.Username,
		ServerAddr:           ticket.GatewayURL,
		ServerID:             role.ServerID,
		Platform:             acc.Platform,
		Authenticator:        client.DefaultAuthenticator,
		MaxReconnectAttempts: 1,
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()
	deadline := time.Now().Add(90 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}
	slog.Info("会话已激活", "role_id", sess.RoleID(), "player_id", sess.PlayerID())
	if sess.ReturnToTown(ctx, 10*time.Second) {
		slog.Info("已返回城镇")
	}

	call := func(s *client.RoleSession, pkt *protocol.Packet, act uint32, name string, timeout time.Duration) *protocol.Packet {
		c, ccancel := context.WithTimeout(ctx, timeout)
		resp, err := s.Call(c, pkt, act)
		ccancel()
		if err != nil {
			slog.Info("请求无回包", "scope", name, "action", fmt.Sprintf("0x%08X", act), "err", err)
			return nil
		}
		head := resp.Payload
		if len(head) > 400 {
			head = head[:400]
		}
		slog.Info("回包", "scope", name, "action", fmt.Sprintf("0x%08X", act),
			"len", len(resp.Payload), "hex", hex.EncodeToString(head))
		return resp
	}

	attempt := func(label string, s *client.RoleSession) {
		slog.Info("=== 上香尝试 ===", "scope", label)
		call(s, protocol.BuildStUnionTreeInfoPacket(), protocol.ActionIDStUnionTreeInfo, label+"/179/0", 8*time.Second)
		god := call(s, protocol.BuildStUnionGodInfoPacket(), protocol.ActionIDStUnionGodInfo, label+"/179/10", 8*time.Second)
		if god != nil {
			if info, pErr := protocol.ParseStUnionGodInfo(god.Payload); pErr == nil {
				slog.Info("面板解析", "scope", label, "flag", info.Flag, "exp", info.Exp,
					"c", info.C, "d", info.D, "records", len(info.Records))
				for _, r := range info.Records {
					slog.Info("记录", "scope", label, "id", r.PlayerID, "name", r.Name,
						"server", r.Server, "time", time.Unix(r.Time, 0).Format(time.DateTime))
				}
			}
		}
		resp := call(s, protocol.BuildStUnionIncensePacket(1), protocol.ActionIDStUnionGodIncense, label+"/179/11", 10*time.Second)
		if resp != nil {
			if res, rErr := protocol.ParseStUnionIncenseResult(resp.Payload); rErr == nil {
				slog.Info("上香结果", "scope", label, "result", res.Result,
					"text", protocol.StUnionIncenseResultText(res.Result), "a", res.A, "b", res.B, "c", res.C)
			} else {
				slog.Info("上香回包解析失败", "scope", label, "err", rErr)
			}
		}
	}

	attempt("main", sess)

	if cross, cErr := sess.OpenCrossServerSession(ctx); cErr != nil {
		slog.Info("建立仙界跨服会话失败", "err", cErr)
	} else {
		attempt("cross", cross)
		if cross != sess {
			cross.Close()
		}
	}

	if town, tErr := sess.OpenSectTownSession(ctx); tErr != nil {
		slog.Info("建立仙盟城镇会话失败", "err", tErr)
	} else {
		attempt("sect_town", town)
		if town != sess {
			town.Close()
		}
	}
}