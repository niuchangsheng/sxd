//go:build probe

// 目标: 宠物仙域 714 主面板/任务面板/礼包面板只读探测
// 运行: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_fairyland_live.go
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

	probe(0x02CA0001, nil) // main_panel

	// 圣域 / 仙界跨服会话对照
	if sa, err := sess.OpenSaintAreaSession(ctx); err != nil {
		slog.Warn("打开圣域会话失败", "err", err)
	} else {
		defer sa.Close()
		slog.Info("圣域会话已建立", "role_id", sa.RoleID())
		p := protocol.NewPacket(0x02CA0001, nil)
		cCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		resp, e := sa.Call(cCtx, p, p.ActionID)
		cancel()
		if e != nil {
			slog.Info("圣域会话 714/1 无回包", "err", e)
		} else {
			slog.Info("圣域会话 714/1 回包", "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload))
		}
	}
	if cross, err := sess.OpenCrossServerSession(ctx); err != nil {
		slog.Warn("打开仙界跨服会话失败", "err", err)
	} else {
		defer cross.Close()
		slog.Info("仙界跨服会话已建立", "role_id", cross.RoleID())
		p := protocol.NewPacket(0x02CA0001, nil)
		cCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		resp, e := cross.Call(cCtx, p, p.ActionID)
		cancel()
		if e != nil {
			slog.Info("跨服会话 714/1 无回包", "err", e)
		} else {
			slog.Info("跨服会话 714/1 回包", "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload))
		}
	}
	probe(0x02CA0002, nil)                // task_panel
	probe(0x02CA0003, nil)                // gift_panel
	probe(0x02CA0004, []byte{0, 0, 0, 1}) // handbook_panel(1)
}
