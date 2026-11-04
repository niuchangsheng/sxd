//go:build probe

// 目标协议: 仙界练功房生产 Routine 端到端验证 (Mod 96 → 94 → 95 → 117)
// 基准对齐: internal/routines/dungeon/training_room.go 与官方 Mod_StPracticeRoom_Base
// 运行命令: go run -tags=probe test/probes/probe_training_room_routine_live.go
// 预期表现: 主服登录后直接调用生产 NewTrainingRoomRoutine,
//
//	跨服完成 enter_room → get_rooms → 领取经验 → (必要时) 换房入座。
//
// 安全说明: 该探针会真实领取当前累积经验并可能调整练功席位, 默认不消耗元宝。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/routines/dungeon"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)

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

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
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
		slog.Error("主服会话启动失败", "err", err)
		return
	}
	defer sess.Close()

	deadline := time.Now().Add(20 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("主服未激活", "state", sess.State())
		return
	}
	slog.Info("主服已激活", "role", sess.OriginNickName(), "level", sess.GetPlayerState().Level)

	rt := dungeon.NewTrainingRoomRoutine(dungeon.DefaultTrainingRoomConfig())
	if err := rt.Execute(ctx, sess, nil); err != nil {
		slog.Error("仙界练功房生产 Routine 执行失败", "err", err)
		return
	}
	slog.Info("仙界练功房生产 Routine 执行完成", "role", sess.OriginNickName())
}
