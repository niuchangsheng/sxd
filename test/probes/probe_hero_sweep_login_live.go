//go:build probe

// 目标: 验证登录后英雄副本多城镇扫荡 (Module 35) 在上下文未被污染前的可达性
// 运行: SXD_CONFIG=configs/qq.yaml go run -tags=probe test/probes/probe_hero_sweep_login_live.go
// 仅使用免费扫荡 auto_num=0，不购买翻牌。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/routines/dungeon"
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

	dictRepo, err := dictionary.NewRepository("")
	if err != nil {
		slog.Error("打开数据字典失败", "err", err)
		return
	}
	defer dictRepo.Close()

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
	deadline := time.Now().Add(15 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活")
		return
	}
	slog.Info("会话已激活，立即执行英雄副本多城镇扫荡", "role_id", sess.RoleID())
	dungeon.RunHeroMissionSweepOnLogin(ctx, sess, dictRepo)
}