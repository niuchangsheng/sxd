//go:build probe

// 目标: 实机执行法相 Routine（造物佛手 / 守护魔兽, Module 518）
//
// 基准对齐: /tmp/faxiang3.pcapng 官方客户端抓包（2026-11-21）
//
// 运行命令:
//
//	SXD_CONFIG=configs/qq.yaml TMPDIR=/home/tongchen/gotmp \
//	  go run -tags=probe test/probes/probe_faxiang_routine_live.go
//
// 预期表现: 打印法相主面板/佛手/魔兽状态、领奖或炼化分支、守护魔兽挑战结果，
// 以及执行前后元宝 delta（必须为 0）。例程内部单次自动炼化最多消耗 1000 道缘。
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/routines/minigame"
	"sxd-pie-ng/internal/scheduler"
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

	before := sess.GetPlayerState().Ingots
	slog.Info("会话已激活", "role_id", sess.RoleID(), "ingots_before", before)

	err = minigame.NewFaxiangRoutine().Execute(ctx, sess, nil)
	var cd *scheduler.CooldownError
	switch {
	case err == nil:
		slog.Info("Routine 执行完成")
	case errors.As(err, &cd):
		slog.Info("Routine 返回冷却", "until", cd.Until.Format(time.RFC3339), "reason", cd.Reason)
	default:
		slog.Warn("Routine 执行返回错误", "err", err)
	}
	after := sess.GetPlayerState().Ingots
	slog.Info("元宝结算", "ingots_before", before, "ingots_after", after, "delta", after-before)
}
