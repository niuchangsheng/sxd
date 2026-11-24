//go:build probe

// 目标: 诊断万古神墟膜拜 (840/1) 在面板显示可膜拜时仍被静默丢弃的原因，
// 确认请求载荷是否需要携带赛季/阶段字段。
//
// 运行命令:
//
//	SXD_CONFIG=configs/qq.yaml TMPDIR=/home/tongchen/gotmp \
//	  go run -tags=probe test/probes/probe_wangu_worship_live.go
//
// 行为: 登录 → 返回城镇 → 840/0 打印每个大神像的 type/season/step/
// player_id/is_worship → 对首个可膜拜对象依次尝试三种 840/1 载荷:
//
//	A: [Int type]                （当前例程实现）
//	B: [Int type][Int season][Int step]
//	C: [Int type][Int step]
//
// 每种载荷都带 PrevAct 尾链，超时 8 秒；任一变体成功即停止。
// 膜拜免费，不消耗元宝。只读+免费膜拜，不调用 840/2 领奖。
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

func probeCall(ctx context.Context, sess *client.RoleSession, pkt *protocol.Packet, act uint32, name string, timeout time.Duration) *protocol.Packet {
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resp, err := sess.Call(cCtx, pkt, act)
	if err != nil {
		slog.Info("请求无回包", "name", name, "action", fmt.Sprintf("0x%08X", act), "err", err)
		return nil
	}
	return resp
}

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

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
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
	slog.Info("会话已激活", "role_id", sess.RoleID())

	if sess.ReturnToTown(ctx, 10*time.Second) {
		slog.Info("已返回城镇")
	} else {
		slog.Info("返回城镇失败，继续")
	}

	panelResp := probeCall(ctx, sess, protocol.BuildWanguShenxuMainInfoPacket(),
		protocol.ActionIDWanguShenxuMainInfo, "840/0 主面板", 10*time.Second)
	if panelResp == nil {
		slog.Error("主面板无回包，无法继续")
		return
	}
	head := panelResp.Payload
	if len(head) > 400 {
		head = head[:400]
	}
	slog.Info("840/0 原始回包", "len", len(panelResp.Payload), "hex", hex.EncodeToString(head))
	panel, err := protocol.ParseWanguShenxuPanel(panelResp.Payload)
	if err != nil {
		slog.Error("面板解析失败", "err", err)
		return
	}
	slog.Info("面板", "result", panel.Result, "can_get_coin", panel.CanGetCoin, "players", len(panel.Players))
	var target *protocol.WanguShenxuPlayer
	for i := range panel.Players {
		p := panel.Players[i]
		slog.Info("大神像", "type", p.Type, "season", p.Season, "step", p.Step,
			"player_id", p.PlayerID, "server", p.ServerName, "nickname", p.Nickname,
			"role_id", p.RoleID, "is_worship", p.IsWorship, "worship_num", p.WorshipNum,
			"seasons", len(p.Seasons))
		if target == nil && p.PlayerID != 0 && !p.IsWorship {
			cp := p
			target = &cp
		}
	}
	if target == nil {
		slog.Info("没有可膜拜对象，结束")
		return
	}

	buildVariant := func(variant string) *protocol.Packet {
		w := protocol.NewWriter()
		w.WriteInt32(target.Type)
		switch variant {
		case "B":
			w.WriteInt32(target.Season)
			w.WriteInt32(target.Step)
		case "C":
			w.WriteInt32(target.Step)
		}
		w.WriteUint32(0)
		pkt := protocol.NewPacket(protocol.ActionIDWanguShenxuWorship, w.Bytes())
		pkt.HasPrevAct = true
		return pkt
	}

	for _, variant := range []string{"A", "B", "C"} {
		pkt := buildVariant(variant)
		slog.Info("尝试膜拜变体", "variant", variant, "payload_hex", hex.EncodeToString(pkt.Payload))
		resp := probeCall(ctx, sess, pkt, protocol.ActionIDWanguShenxuWorship,
			"840/1 膜拜变体"+variant, 8*time.Second)
		if resp == nil {
			continue
		}
		got, pErr := protocol.ParseWanguShenxuWorshipResult(resp.Payload)
		if pErr != nil {
			slog.Info("回包解析失败", "variant", variant, "err", pErr,
				"hex", hex.EncodeToString(resp.Payload))
			continue
		}
		slog.Info("膜拜有回包", "variant", variant, "result", got.Result,
			"result_text", protocol.WanguShenxuResultText(got.Result), "ingot", got.Ingot,
			"hex", hex.EncodeToString(resp.Payload))
		break
	}
}