//go:build probe

// 目标: 实机验证日常福利三个 Routine (签到 / VIP 礼包 / 在线奖励)
//
//	签到     [Module 89]  get_sign_in_status -> ... (活动未开放时直接跳过)
//	VIP礼包  [Module 18]  get_player_vip_info -> get_send_gift (仅免费且未领档位)
//	在线奖励 [Module 712] main_panel -> one_key_get (活动时间窗过期时跳过)
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_daily_welfare_live.go
//
// 预期表现: 三个 Routine 均不消耗元宝；当前 s0 环境下签到活动未开放、
// VIP 免费档今日已领、在线奖励时间窗已过期，因此应全部安全跳过/无领取。
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
	"sxd-pie-ng/internal/scheduler"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/config.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "path", cfgPath, "err", err)
		return
	}
	if len(cfg.Accounts) == 0 || len(cfg.Accounts[0].Roles) == 0 {
		slog.Error("配置文件缺少账号或角色")
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

	deadline := time.Now().Add(20 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}
	before := sess.GetPlayerState().Ingots
	slog.Info("会话已激活", "role_id", sess.RoleID(), "player_id", sess.PlayerID(), "ingots", before)

	routines := []struct {
		name string
		rt   scheduler.ActivityRoutine
	}{
		{"每日签到", daily.NewSignInRoutine()},
		{"VIP每日免费礼包", daily.NewVIPWelfareRoutine()},
		{"在线奖励", daily.NewOnlineRewardRoutine()},
	}
	for _, r := range routines {
		if err := r.rt.Execute(ctx, sess, nil); err != nil {
			slog.Warn("Routine 执行返回错误", "name", r.name, "err", err)
			continue
		}
		slog.Info("Routine 执行完成", "name", r.name)
	}

	after := sess.GetPlayerState().Ingots
	slog.Info("资产变化", "ingots_before", before, "ingots_after", after, "delta", after-before)
}
