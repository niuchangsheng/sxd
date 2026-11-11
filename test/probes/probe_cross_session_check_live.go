//go:build probe

// 目标: 对比幻境修炼(306) / 三界修炼(529,531) 在主服与仙界跨服会话上的可达性
// 运行: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_cross_session_check_live.go
// 只读：仅查询面板。
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

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
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
	slog.Info("主服会话已激活", "role_id", sess.RoleID())

	query := func(tag string, s *client.RoleSession, act uint32, payload []byte) {
		pkt := protocol.NewPacket(act, payload)
		cCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		defer cancel()
		resp, err := s.Call(cCtx, pkt, act)
		if err != nil {
			slog.Info("无回包", "session", tag, "module", act>>16, "act", act&0xFFFF, "err", err)
			return
		}
		slog.Info("回包", "session", tag, "module", act>>16, "act", act&0xFFFF,
			"len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload))
	}

	// 主服基线
	query("main", sess, 0x01320000, nil) // 306/0
	query("main", sess, 0x0213000C, nil) // 531/12
	query("main", sess, 0x02110001, nil) // 529/1

	// 仙界跨服会话
	cross, cErr := sess.OpenCrossServerSession(ctx)
	if cErr != nil {
		slog.Warn("打开仙界跨服会话失败", "err", cErr)
		return
	}
	defer cross.Close()
	slog.Info("仙界跨服会话已建立", "role_id", cross.RoleID())
	query("cross", cross, 0x01320000, nil)
	query("cross", cross, 0x0213000C, nil)
	query("cross", cross, 0x02110001, nil)

	// 圣域会话
	if sa, sErr := sess.OpenSaintAreaSession(ctx); sErr == nil {
		defer sa.Close()
		slog.Info("圣域会话已建立", "role_id", sa.RoleID())
		query("sa", sa, 0x01320000, nil)
		query("sa", sa, 0x0213000C, nil)
		query("sa", sa, 0x02110001, nil)
	} else {
		slog.Warn("打开圣域会话失败", "err", sErr)
	}
}