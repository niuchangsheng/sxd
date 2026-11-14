//go:build probe

// 目标: 线上验证炼宝炉例程 (farming.NewRefinePotRoutine) 的完整链路。
//
// 验证点:
//  1. open_panel -> select_air(道具 ID) -> start_refine(裸 4 字节) 全程有回包；
//  2. 铜钱不足时服务端返回业务码 4 (LESS_COIN)，例程按半量回退并最终进入冷却；
//  3. 全程元宝数量不变。
//
// 运行: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_furnace_routine_live.go -role 0
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/routines/farming"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/niuchangsheng.yaml"
	}
	roleIdx := flag.Int("role", 0, "配置中的角色下标 (0=s0, 1=s1)")
	flag.Parse()

	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[*roleIdx]

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
		MaxReconnectAttempts: 3,
	}
	client.ApplyTicket(&roleCfg, ticket)
	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()
	deadline := time.Now().Add(40 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}

	ingotsBefore := sess.GetPlayerState().Ingots
	coinsBefore := sess.GetPlayerState().Coins
	slog.Info("炼制前资产", "role_id", role.RoleName, "coins", coinsBefore, "ingots", ingotsBefore)
	err = farming.NewRefinePotRoutine().Execute(ctx, sess, nil)
	slog.Info("炼宝炉例程结束", "err", err)
	ingotsAfter := sess.GetPlayerState().Ingots
	if ingotsAfter != ingotsBefore {
		slog.Error("炼宝炉例程导致元宝变化，应立即熔断", "before", ingotsBefore, "after", ingotsAfter)
	}
	slog.Info("炼制后资产", "coins", sess.GetPlayerState().Coins, "ingots", ingotsAfter)
}
