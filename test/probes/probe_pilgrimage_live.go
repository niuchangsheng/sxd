//go:build probe

// 目标协议: 西天取经 (Mod 456 双轨网关 / Mod 30 传统取经)
// 基准对齐: 对齐原版 pie.exe 取经巡检、使者刷新与启程护送闭环
// 运行命令: go run -tags=probe test/probes/probe_pilgrimage_live.go
// 预期表现: 登录激活后执行一次 Pilgrimage Routine，输出使者状态与启程结果；会消耗当日 1 次护送机会
package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/routines/farming"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	cfg, err := config.Load("configs/config.yaml")
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
	slog.Info("=== 会话已激活，开始执行 Pilgrimage Routine ===",
		"role_id", sess.RoleID(),
		"player_id", sess.PlayerID(),
	)

	execCtx, execCancel := context.WithTimeout(ctx, 45*time.Second)
	defer execCancel()

	routine := farming.NewPilgrimageRoutine()
	if err := routine.Execute(execCtx, sess, nil); err != nil {
		slog.Error("Pilgrimage Routine 执行失败", "err", err)
		return
	}

	slog.Info("=== Pilgrimage Routine 执行完成 ===",
		"state", sess.State().String(),
		"last_action", fmt.Sprintf("0x%08X", sess.LastActionID()),
	)
}