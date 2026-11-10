//go:build probe

// 目标: 定位“奇缘宝藏”实际模块（808 FortuneBox / 780 FortuneGift / 811 TimeFortune）
// 运行: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_fortune_modules_live.go
// 只读：仅查询各模块面板/开放状态，不执行购买或抽取。
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
	deadline := time.Now().Add(15 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活")
		return
	}
	slog.Info("会话已激活", "role_id", sess.RoleID())

	probe := func(act uint32, payload []byte) {
		pkt := protocol.NewPacket(act, payload)
		cCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		resp, err := sess.Call(cCtx, pkt, act)
		if err != nil {
			slog.Info("无回包", "module", act>>16, "act", act&0xFFFF, "err", err)
			return
		}
		slog.Info("回包", "module", act>>16, "act", act&0xFFFF,
			"len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload))
	}

	// 808 FortuneBox is_open
	probe(0x03280000, nil)
	// 808 panel_info
	probe(0x03280001, nil)
	// 780 FortuneGift panel_info (empty)
	probe(0x030C0001, nil)
	// 811 TimeFortune panel_info (empty)
	probe(0x032B0000, nil)
}