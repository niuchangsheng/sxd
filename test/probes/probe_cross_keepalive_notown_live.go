//go:build probe

// 目标: 验证「仅登录、不进入仙界城镇」的常驻跨服连接能否同时满足：
//  1) 不破坏主服上下文（主服 enter_town 仍成功）；
//  2) 让主服会话跨过 5 分钟回收扫描（空闲 6.5 分钟不掉线）。
//
// 运行命令:
//
//	SXD_CONFIG=configs/qq.yaml TMPDIR=/home/tongchen/gotmp \
//	  go run -tags=probe test/probes/probe_cross_keepalive_notown_live.go
//
// 行为: 登录主服 → EnsureCrossSessionAlive（仅登录跨服）→ 主服 enter_town
// → 空闲 6.5 分钟，每 30 秒打印主服/跨服状态。不发送业务请求、不消耗元宝。
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

	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
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
	slog.Info("主服会话已激活", "role_id", sess.RoleID())

	if err := sess.EnsureCrossSessionAlive(ctx); err != nil {
		slog.Error("建立常驻跨服会话失败", "err", err)
		return
	}
	slog.Info("常驻跨服连接已建立(仅登录)", "has_cross", sess.HasCrossSession())

	// 回归验证：常驻跨服连接存在时，主服 enter_town 仍应成功。
	townID := int32(sess.GetPlayerState().TownMapID)
	if townID <= 0 {
		townID = 3
	}
	eCtx, eCancel := context.WithTimeout(ctx, 10*time.Second)
	code, eErr := sess.EnterTown(eCtx, townID)
	eCancel()
	slog.Info("主服 enter_town", "map_id", townID, "code", code, "err", eErr)

	// 空闲观察 6.5 分钟，覆盖一个 5 分钟扫描点。
	start := time.Now()
	for time.Since(start) < 390*time.Second {
		select {
		case <-ctx.Done():
			slog.Info("上下文结束", "state", sess.State())
			return
		case <-time.After(30 * time.Second):
		}
		slog.Info("空闲观察", "elapsed", time.Since(start).Round(time.Second).String(),
			"main_state", sess.State().String(), "has_cross", sess.HasCrossSession(),
			"last_rx", fmt.Sprintf("0x%08X", sess.LastRxActionID()))
		if sess.State() != client.StateActive {
			slog.Warn("主服会话在空闲期失效", "elapsed", time.Since(start).Round(time.Second).String(),
				"state", sess.State().String())
			return
		}
	}
	slog.Info("空闲 6.5 分钟主服仍在线", "state", sess.State().String(),
		"has_cross", sess.HasCrossSession())
	_ = protocol.ActionIDPlayerHeartbeat
}