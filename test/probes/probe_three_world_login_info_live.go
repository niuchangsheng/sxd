//go:build probe

// 目标: 勘察三界修炼 529/2 get_login_info 的回包结构，为 529/3 跨服登录提供凭据解析依据
// 运行: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_three_world_login_info_live.go
// 只读：仅查询 529/1、529/2、529/4 与 531/12 面板，不开始/结算修炼。
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
	slog.Info("会话已激活", "role_id", sess.RoleID())

	query := func(tag string, act uint32, payload []byte) {
		pkt := protocol.NewPacket(act, payload)
		cCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		defer cancel()
		resp, err := sess.Call(cCtx, pkt, act)
		if err != nil {
			slog.Info("无回包", "tag", tag, "module", act>>16, "act", act&0xFFFF, "err", err)
			return
		}
		slog.Info("回包", "tag", tag, "module", act>>16, "act", act&0xFFFF,
			"len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload))
	}

	query("529/1 status", 0x02110001, nil)
	query("529/4 three_world_status", 0x02110004, nil)
	for _, param := range []int32{0, 1, 2, 3, 4} {
		w := protocol.NewWriter()
		w.WriteInt32(param)
		query(fmt.Sprintf("529/2 login_info param=%d", param), 0x02110002, w.Bytes())
	}
	query("531/12 practice_info", 0x0213000C, nil)
	query("531/1 big_map_panel", 0x02130001, nil)
	query("531/27 auto_practice_panel", 0x0213001B, nil)
	query("531/29 auto_practice_info", 0x0213001D, nil)
}