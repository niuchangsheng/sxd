//go:build probe

// 目标协议: [Module 669, Action 1] 灵域猎妖主面板 + [Action 3] 关卡面板
// 运行命令: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_soul_hunt_live.go
//
// 目的: 对比 challenge_panel 的参数语义（1 起始序号 vs 面板 Levels 中的关卡 ID）。
// 默认只读，不调用 start_challenge / buy_challenge_times。
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
	slog.SetLogLoggerLevel(slog.LevelDebug)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/niuchangsheng.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "path", cfgPath, "err", err)
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

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

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

	deadline := time.Now().Add(15 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未能在时限内激活", "state", sess.State())
		return
	}
	slog.Info("会话已激活", "role_id", sess.RoleID())

	call := func(pkt *protocol.Packet, d time.Duration) *protocol.Packet {
		cCtx, cancel := context.WithTimeout(ctx, d)
		defer cancel()
		resp, err := sess.Call(cCtx, pkt, pkt.ActionID)
		if err != nil {
			slog.Warn("请求无应答", "action", fmt.Sprintf("0x%08X", pkt.ActionID),
				"payload_hex", hex.EncodeToString(pkt.Payload), "err", err)
			return nil
		}
		return resp
	}

	stateResp := call(protocol.NewPacket(protocol.ActionIDSoulHuntState, nil), 8*time.Second)
	if stateResp != nil {
		slog.Info("活动状态回包", "payload_hex", hex.EncodeToString(stateResp.Payload))
	}

	panelResp := call(protocol.NewPacket(protocol.ActionIDSoulHuntMainPanel, nil), 8*time.Second)
	var panel *protocol.SoulHuntMainPanel
	if panelResp != nil {
		slog.Info("主面板回包", "payload_hex", hex.EncodeToString(panelResp.Payload))
		if p, e := protocol.ParseSoulHuntMainPanel(panelResp.Payload); e == nil {
			panel = p
			slog.Info("主面板解析", "result", p.Result, "ints", p.Ints, "levels", p.Levels, "times", p.Times)
		} else {
			slog.Warn("主面板解析失败", "err", e)
		}
	}

	candidates := []int32{1, 2}
	if panel != nil {
		candidates = append(candidates, panel.Levels...)
	}
	if panel != nil && len(panel.Ints) > 2 {
		candidates = append(candidates, panel.Ints[2])
	}
	if panel != nil && len(panel.Ints) > 1 {
		candidates = append(candidates, panel.Ints[1])
	}
	if raw := os.Getenv("SOULHUNT_START"); raw != "" {
		var id int32
		fmt.Sscanf(raw, "%d", &id)
		var pkt *protocol.Packet
		if extra := os.Getenv("SOULHUNT_START_EXTRA"); extra != "" {
			var e2 int32
			fmt.Sscanf(extra, "%d", &e2)
			w := protocol.NewWriter()
			w.WriteInt32(id)
			w.WriteInt32(e2)
			pkt = protocol.NewPacket(protocol.ActionIDSoulHuntStartChallenge, w.Bytes())
		} else {
			pkt = protocol.BuildSoulHuntStartChallengePacket(id)
		}
		cCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		resp, err := sess.Call(cCtx, pkt, protocol.ActionIDSoulHuntStartChallenge)
		cancel()
		if err != nil {
			slog.Warn("start_challenge 无回包", "id", id, "err", err)
		} else {
			code, e := protocol.ParseSoulHuntChallengeResult(resp.Payload)
			if e != nil {
				slog.Warn("start_challenge 解析失败", "id", id, "err", e)
			} else {
				slog.Info("start_challenge 结果", "id", id, "result", code,
					"result_text", protocol.SoulHuntResultText(code),
					"payload_hex", hex.EncodeToString(resp.Payload))
			}
		}
	}
	seen := make(map[int32]bool)
	for _, id := range candidates {
		if seen[id] {
			continue
		}
		seen[id] = true
		resp := call(protocol.BuildSoulHuntChallengePanelPacket(id), 8*time.Second)
		if resp == nil || len(resp.Payload) == 0 {
			slog.Info("关卡面板无结果", "id", id)
			continue
		}
		code, e := protocol.ParseSoulHuntChallengeResult(resp.Payload)
		if e != nil {
			slog.Warn("关卡面板解析失败", "id", id, "err", e)
			continue
		}
		slog.Info("关卡面板", "id", id, "result", code,
			"result_text", protocol.SoulHuntResultText(code), "payload_hex", hex.EncodeToString(resp.Payload))
	}
}
