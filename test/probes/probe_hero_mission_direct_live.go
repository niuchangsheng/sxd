//go:build probe

// 目标: 验证英雄副本 35/0 列表与 35/2 start_practice 在跨城镇场景下的可达性
// 运行: SXD_CONFIG=configs/qq.yaml go run -tags=probe test/probes/probe_hero_mission_direct_live.go
// 注意: 会对城镇 54 发起一次免费英雄副本扫荡（auto_num=0，不买翻牌）。
package main

import (
	"context"
	"encoding/hex"
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

	ticketCache := platform.NewTicketCache(90 * time.Minute)
	xdAuth, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		return
	}
	xdAuth.SetCookie(acc.Cookie)
	ticketCache.RegisterAuthenticator(xdAuth)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
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
	slog.Info("会话已激活", "role_id", sess.RoleID())

	// 1. 列表查询（城镇 54）。
	listPkt := protocol.BuildHeroMissionListPacket(54)
	c1, cancel1 := context.WithTimeout(ctx, 6*time.Second)
	resp, err := sess.Call(c1, listPkt, protocol.ActionHeroMissionList)
	cancel1()
	if err != nil {
		slog.Info("35/0 列表无回包", "town", 54, "err", err)
	} else {
		slog.Info("35/0 列表回包", "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload[:min(40, len(resp.Payload))]))
	}

	// 2. 直接 start_practice（城镇 54，auto_num=0）。
	startPkt, pErr := protocol.BuildHeroPracticePacket(protocol.HeroPracticeRequest{TownID: 54, AutoNum: 0, HeroType: 0})
	if pErr != nil {
		slog.Error("构造 35/2 失败", "err", pErr)
		return
	}
	c2, cancel2 := context.WithTimeout(ctx, 8*time.Second)
	sresp, sErr := sess.Call(c2, startPkt, protocol.ActionHeroMissionStartPractice)
	cancel2()
	if sErr != nil {
		slog.Info("35/2 无回包", "town", 54, "err", sErr)
	} else {
		slog.Info("35/2 回包", "len", len(sresp.Payload), "hex", hex.EncodeToString(sresp.Payload[:min(24, len(sresp.Payload))]))
		if len(sresp.Payload) > 0 && sresp.Payload[0] == protocol.HeroMissionResultSuccess {
			quickPkt := protocol.BuildHeroQuicklyPacket()
			c3, cancel3 := context.WithTimeout(ctx, 8*time.Second)
			qresp, qErr := sess.Call(c3, quickPkt, protocol.ActionHeroMissionQuickly)
			cancel3()
			if qErr != nil {
				slog.Info("35/7 无回包", "err", qErr)
			} else {
				slog.Info("35/7 回包", "len", len(qresp.Payload), "hex", hex.EncodeToString(qresp.Payload[:min(16, len(qresp.Payload))]))
			}
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}