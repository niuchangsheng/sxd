//go:build probe

// 目标: 依次执行三项圣域玩法例程，验证线上可用性
//
//	圣域竞技场   pvp.NewHolyArenaRoutine()      抢占空席位 (免费)
//	圣域修炼替身 daily.NewSaintPracticeRoutine() 免费修炼房入座/领经验
//	圣域取经     farming.NewPilgrimageRoutine()  圣域取经链路 (护送/打劫，免费次数)
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_saint_area_run_live.go
// 预期表现: 三条例程均打印明确的业务结果码含义；元宝 delta 必须为 0。
// 注意: 本探针会执行真实的免费游戏动作 (占席/领经验/启程/打劫)，不消耗元宝。
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
	"sxd-pie-ng/internal/routines/daily"
	"sxd-pie-ng/internal/routines/farming"
	"sxd-pie-ng/internal/routines/pvp"
	"sxd-pie-ng/internal/scheduler"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)

	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/s0.yaml"
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

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
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
		MaxReconnectAttempts: 3,
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动主服会话失败", "err", err)
		return
	}
	defer sess.Close()
	deadline := time.Now().Add(40 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("主服会话未激活", "state", sess.State())
		return
	}

	before := sess.GetPlayerState()
	slog.Info("开始圣域玩法巡检", "coins", before.Coins, "ingots", before.Ingots)

	routines := []struct {
		name string
		r    scheduler.ActivityRoutine
	}{
		{"holy_arena", pvp.NewHolyArenaRoutine()},
		{"saint_practice", daily.NewSaintPracticeRoutine()},
		{"pilgrimage", farming.NewPilgrimageRoutine(farming.DefaultPilgrimageConfig())},
	}
	for _, it := range routines {
		if ctx.Err() != nil {
			break
		}
		slog.Info("=== 执行圣域例程 ===", "routine", it.name)
		if err := it.r.Execute(ctx, sess, nil); err != nil {
			slog.Warn("例程返回错误", "routine", it.name, "err", err)
		} else {
			slog.Info("例程执行完成", "routine", it.name)
		}
		time.Sleep(2 * time.Second)
	}

	after := sess.GetPlayerState()
	slog.Info("圣域玩法巡检结束",
		"coins", after.Coins,
		"ingots", after.Ingots,
		"coin_delta", after.Coins-before.Coins,
		"ingot_delta", after.Ingots-before.Ingots,
	)
}