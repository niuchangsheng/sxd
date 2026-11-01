//go:build probe

// 目标协议: [Module 584, Action 1] get_practice_info (查询离线打坐待领取经验，只读不领取)
// 基准对齐: 对齐原版 Mod_NewPractice_Base.get_practice_info
// 运行命令: go run -tags=probe test/probes/probe_practice_info_live.go
// 预期表现: 输出当前等级、总经验、升级所需经验与离线打坐待结算经验，不产生任何写操作
package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
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

	deadline := time.Now().Add(15 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未能在时限内激活", "state", sess.State())
		return
	}

	st := sess.GetPlayerState()
	info, err := sess.GetNewPracticeData(ctx)
	if err != nil {
		slog.Error("查询打坐信息失败", "err", err)
		return
	}
	if info == nil {
		slog.Warn("打坐信息为空")
		return
	}

	fmt.Printf("\n------------------------------------------------------------\n")
	fmt.Printf("角色: %s (Lv.%d)\n", sess.OriginNickName(), st.Level)
	fmt.Printf("当前经验: %d / %d (升级还需 %d)\n", st.Experience, st.MaxExperience, st.MaxExperience-st.Experience)
	fmt.Printf("打坐待结算经验: %d\n", info.PracticeExp)
	fmt.Printf("------------------------------------------------------------\n\n")
}
