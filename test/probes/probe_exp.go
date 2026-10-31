//go:build probe

// Package main 提供打坐经验领取与角色等级、经验、升级剩余经验校验探针。
//
// 目标基准事实: 官方 AS3 Mod_Player_Base
// 执行方式: go run -tags=probe test/probes/probe_exp.go
// 遵循规范: docs/adr/0013-recon-probe-isolation-and-test-harness.md (严禁硬编码敏感凭据)
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

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
		MaxReconnectAttempts: 5,
		RefreshCredentials: func(c context.Context, sCfg *client.SessionConfig) error {
			ticketCache.Invalidate(acc.Platform, acc.Username, role.ServerID)
			freshTicket, err := ticketCache.GetOrFetch(c, acc.Platform, acc.Username, acc.Password, role.ServerID)
			if err != nil {
				return err
			}
			client.ApplyTicket(sCfg, freshTicket)
			if freshTicket.GatewayURL != "" {
				sCfg.ServerAddr = freshTicket.GatewayURL
			}
			return nil
		},
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动长连接会话失败", "err", err)
		return
	}
	defer sess.Close()

	// 等待会话激活
	for i := 0; i < 60; i++ {
		if sess.State() == client.StateActive {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未能在时限内激活", "state", sess.State())
		return
	}

	// 查询现代离线打坐练功进度与待领取经验
	practiceInfo, err := sess.GetNewPracticeData(ctx)
	pendingExp := int64(0)
	if err == nil && practiceInfo != nil {
		pendingExp = practiceInfo.PracticeExp
	}

	beforeState := sess.GetPlayerState()
	slog.Info("准备领取城镇打坐练功累积经验",
		"pending_exp", pendingExp,
		"current_level", beforeState.Level,
		"current_exp", beforeState.Experience,
	)

	// 一键领取现代打坐经验与灵气
	collectRes, err := sess.ClaimNewPracticeExp(ctx)
	if err != nil {
		slog.Warn("领取城镇打坐练功经验失败", "err", err)
	}

	time.Sleep(1 * time.Second)

	// 拉取最新角色属性
	latest, err := sess.FetchLatestPlayerInfo(ctx)
	if err != nil {
		slog.Error("拉取最新角色数据失败", "err", err)
		return
	}

	gainExp := latest.Experience - beforeState.Experience
	if collectRes != nil && collectRes.GainExp > 0 && gainExp <= 0 {
		gainExp = collectRes.GainExp
	}
	remExp := int64(0)
	if latest.MaxExperience > latest.Experience {
		remExp = latest.MaxExperience - latest.Experience
	}

	fmt.Printf("\n------------------------------------------------------------\n")
	fmt.Printf("角色名称: %s\n", latest.RoleName)
	fmt.Printf("最新等级: %d 级\n", latest.Level)
	fmt.Printf("即将领取(待结算)打坐经验: %d\n", pendingExp)
	fmt.Printf("本次实际结算经验增加: +%d\n", gainExp)
	fmt.Printf("当前角色总经验: %d\n", latest.Experience)
	fmt.Printf("升下一级所需: %d\n", latest.MaxExperience)
	fmt.Printf("升级剩余差额: %d 经验\n", remExp)
	fmt.Printf("------------------------------------------------------------\n\n")
}
