//go:build probe

// 目标: 验证「主服业务读保活」是否能替代常驻跨服连接：
// 每 90 秒发送一次无害的 0/6 功能列表查询，主服会话应跨过 5 分钟回收扫描。
//
// 运行命令:
//
//	SXD_CONFIG=configs/qq.yaml TMPDIR=/home/tongchen/gotmp \
//	  go run -tags=probe test/probes/probe_main_keepalive_live.go
//
// 行为: 登录 → enter_town 验证上下文 → 空闲 6.5 分钟，每 90 秒 0/6。
// 不建立跨服连接，不消耗元宝。
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

	townID := int32(sess.GetPlayerState().TownMapID)
	if townID <= 0 {
		townID = 3
	}
	eCtx, eCancel := context.WithTimeout(ctx, 10*time.Second)
	code, eErr := sess.EnterTown(eCtx, townID)
	eCancel()
	slog.Info("主服 enter_town", "map_id", townID, "code", code, "err", eErr)

	start := time.Now()
	last := time.Now()
	for time.Since(start) < 390*time.Second {
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
		if time.Since(last) >= 90*time.Second {
			last = time.Now()
			cCtx, cCancel := context.WithTimeout(ctx, 8*time.Second)
			resp, cErr := sess.Call(cCtx, protocol.BuildGetPlayerFunctionsPacket(0), protocol.ActionIDPlayerGetFunctions)
			cCancel()
			slog.Info("保活读 0/6", "ok", cErr == nil && resp != nil,
				"resp_len", func() int {
					if resp != nil {
						return len(resp.Payload)
					}
					return 0
				}(), "err", cErr)
		}
		slog.Info("空闲观察", "elapsed", time.Since(start).Round(time.Second).String(),
			"main_state", sess.State().String())
		if sess.State() != client.StateActive {
			slog.Warn("主服会话在空闲期失效", "elapsed", time.Since(start).Round(time.Second).String())
			return
		}
	}
	slog.Info("空闲 6.5 分钟主服仍在线", "state", sess.State().String())
}