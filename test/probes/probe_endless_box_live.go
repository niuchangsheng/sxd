//go:build probe

// 目标: 只读探测无尽宝箱 (Module 690, Mod_EndlessBox_Base) 活动面板与箱子面板
//
// 运行命令: SXD_CONFIG=configs/qq.yaml TMPDIR=/home/tongchen/gotmp go run -tags=probe test/probes/probe_endless_box_live.go
//
// 行为: 690/4 activity_panel → 690/1 panel_info；只读，不开箱、不购买。
package main

import (
	"context"
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
	deadline := time.Now().Add(25 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}
	slog.Info("会话已激活", "role_id", sess.RoleID(), "ingots", sess.GetPlayerState().Ingots)

	call := func(act uint32, pkt *protocol.Packet) {
		cCtx, cCancel := context.WithTimeout(ctx, 10*time.Second)
		resp, err := sess.Call(cCtx, pkt, act)
		cCancel()
		if err != nil || resp == nil {
			slog.Warn("无尽宝箱无回包", "action", act, "err", err)
			return
		}
		slog.Info("无尽宝箱回包", "action", act, "len", len(resp.Payload), "hex", resp.Payload)
	}
	call(0x02B20004, protocol.NewPacket(0x02B20004, nil)) // 690/4 activity_panel
	call(0x02B20001, protocol.NewPacket(0x02B20001, nil)) // 690/1 panel_info
	slog.Info("探测完成")
}
