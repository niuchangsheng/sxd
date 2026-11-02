//go:build probe

// 目标协议: [Module 69, Action 1] open_lucky_wheel + [Module 69, Action 2] start_lucky_wheel
// 基准对齐: 对齐原版 Mod_LuckyWheel_Base 帮派转盘闭环，验证请求载荷与回包字段
// 运行命令: go run -tags=probe test/probes/probe_faction_lucky_wheel_live.go
// 运行命令 (消耗一枚青龙令): go run -tags=probe test/probes/probe_faction_lucky_wheel_live.go -spin
// 预期表现: 只读查询转盘状态、今日帮派贡献与剩余青龙令；-spin 时额外转动一次并打印奖励。
package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func main() {
	spin := flag.Bool("spin", false, "是否实际转动一次转盘 (会消耗一枚青龙令)")
	configPath := flag.String("config", "configs/config.yaml", "本地配置路径")
	flag.Parse()

	slog.SetLogLoggerLevel(slog.LevelDebug)
	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
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

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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

	for i := 0; i < 60; i++ {
		if sess.State() == client.StateActive {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未能在时限内激活", "state", sess.State())
		return
	}
	slog.Info("=== 会话已激活，开始帮派转盘协议探针 ===", "role_id", sess.RoleID(), "player_id", sess.PlayerID())

	openPkt := protocol.BuildLuckyWheelOpenPacket(sess.LastActionID())
	openResp, err := call(ctx, sess, openPkt, protocol.ActionIDLuckyWheelOpen, 5*time.Second)
	if err != nil {
		slog.Error("查询帮派转盘失败", "err", err)
		return
	}
	slog.Info("打开转盘回包", "payload_hex", hex.EncodeToString(openResp.Payload))
	info, err := protocol.ParseLuckyWheelOpenResult(openResp.Payload)
	if err != nil {
		slog.Error("解析打开转盘回包失败", "err", err)
		return
	}
	slog.Info("转盘状态",
		"state", info.State,
		"faction_today_contribution", info.FactionTodayContribution,
		"rosefinch_number", info.RosefinchNumber,
	)

	if !*spin {
		slog.Info("dry-run: 未转动转盘，使用 -spin 实际消耗一枚青龙令")
		return
	}
	if info.State != protocol.LuckyWheelResultOK {
		slog.Warn("转盘当前不可转动，跳过", "state", info.State)
		return
	}
	if info.RosefinchNumber <= 0 {
		slog.Warn("青龙令不足，跳过")
		return
	}

	startPkt := protocol.BuildLuckyWheelStartPacket(protocol.ActionIDLuckyWheelOpen)
	startResp, err := call(ctx, sess, startPkt, protocol.ActionIDLuckyWheelStart, 5*time.Second)
	if err != nil {
		slog.Error("转动帮派转盘失败", "err", err)
		return
	}
	slog.Info("转盘抽奖回包", "payload_hex", hex.EncodeToString(startResp.Payload))
	result, err := protocol.ParseLuckyWheelRunResult(startResp.Payload)
	if err != nil {
		slog.Error("解析转盘抽奖回包失败", "err", err)
		return
	}
	slog.Info("转盘抽奖结果",
		"run_state", result.RunState,
		"type", result.Type,
		"rosefinch_number", result.RosefinchNumber,
		"item_id", result.ItemID,
	)
}

func call(ctx context.Context, sess *client.RoleSession, pkt *protocol.Packet, respActionID uint32, timeout time.Duration) (*protocol.Packet, error) {
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return sess.Call(cCtx, pkt, respActionID)
}
