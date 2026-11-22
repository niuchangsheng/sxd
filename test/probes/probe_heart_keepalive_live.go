//go:build probe

// 目标协议: [Module 0, Action 75] Mod_Player_Base.heart 心跳载荷形态与保活实测
// 基准对齐: 对比空载荷 (0B) 与携带 4B PrevAct 两种 0/75 心跳形态的服务端回包行为及空闲保活表现
// 运行命令: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_heart_keepalive_live.go
// 预期表现: 打印两种形态的 0/75 是否收到 1 字节回包 (0x01)，以及空闲期是否保活
// 安全性: 仅只读心跳探测，不执行任何业务操作、不消耗任何资源
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
		cfgPath = "configs/niuchangsheng.yaml"
	}
	cfg, err := config.Load(cfgPath)
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

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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
		Authenticator:        client.DefaultGatewayAuthenticator,
		HeartbeatInterval:    time.Hour, // 禁用自动心跳，由探针显式控制
		MaxReconnectAttempts: 3,
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()

	deadline := time.Now().Add(45 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未能在时限内激活", "state", sess.State())
		return
	}
	slog.Info("会话已激活", "last_biz", fmt.Sprintf("0x%08X", sess.LastBusinessActionID()))

	// 实验 A: 空载荷 0/75
	{
		c, cancelA := context.WithTimeout(ctx, 3*time.Second)
		resp, err := sess.Call(c, protocol.NewPacket(protocol.ActionHeartbeat, nil), protocol.ActionHeartbeat)
		cancelA()
		if err != nil {
			slog.Info("实验 A (空载荷 0/75)", "ok", false, "err", err)
		} else {
			slog.Info("实验 A (空载荷 0/75)", "ok", true, "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload))
		}
	}

	// 实验 B: 带 4B PrevAct 的 0/75
	{
		pkt := protocol.NewPacketWithExplicitPrevAct(protocol.ActionHeartbeat, nil, sess.LastBusinessActionID())
		c, cancelB := context.WithTimeout(ctx, 3*time.Second)
		resp, err := sess.Call(c, pkt, protocol.ActionHeartbeat)
		cancelB()
		if err != nil {
			slog.Info("实验 B (4B PrevAct 0/75)", "ok", false, "err", err)
		} else {
			slog.Info("实验 B (4B PrevAct 0/75)", "ok", true, "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload))
		}
	}

	// 实验 C: 发完实验 B 后测试后续业务包 (get_player_function 0/6) 是否正常
	{
		const actGetPlayerFunction uint32 = 0x00000006
		pkt := protocol.NewPacketWithAutoPrevAct(actGetPlayerFunction, nil)
		c, cancelC := context.WithTimeout(ctx, 4*time.Second)
		resp, err := sess.Call(c, pkt, actGetPlayerFunction)
		cancelC()
		if err != nil {
			slog.Info("实验 C (心跳后业务包 0/6)", "ok", false, "err", err)
		} else {
			slog.Info("实验 C (心跳后业务包 0/6)", "ok", true, "len", len(resp.Payload))
		}
	}

	// 实验 D: 纯空闲 12 秒不发任何包，验证网关空闲踢线阈值
	slog.Info("实验 D: 开始纯空闲 12 秒不发任何包，观察是否被网关踢线...")
	for i := 1; i <= 12; i++ {
		time.Sleep(time.Second)
		if sess.State() != client.StateActive {
			slog.Info("实验 D: 空闲断线", "after_sec", i, "state", sess.State())
			return
		}
	}
	slog.Info("实验 D: 空闲 12 秒仍保持 Active", "state", sess.State())
}
