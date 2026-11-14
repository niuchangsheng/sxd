//go:build probe

// 目标: 定位登录钩子中哪个请求会污染会话上下文，导致 get_sections 被静默丢弃。
//
// 线上现象: 服务器启动后（尚未发送任何 Mod 57 请求）任务推图 get_sections 即无回包；
// 而裸登录会话先 SyncQuests 再 get_sections 可稳定回包。
// 登录钩子依次执行：渡业台 852 -> 淬炼池 282 -> 英雄副本 35。
//
// 运行:
//
//	SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_quest_context_hook_live.go -role 1 -hook none
//	SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_quest_context_hook_live.go -role 1 -hook hero
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
	"sxd-pie-ng/internal/protocol"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/niuchangsheng.yaml"
	}
	roleIdx := flag.Int("role", 1, "配置中的角色下标 (0=s0, 1=s1)")
	hook := flag.String("hook", "none", "先执行的钩子: none|hero|spirit|altar")
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

	call := func(pkt *protocol.Packet, act uint32, name string) {
		cCtx, cCancel := context.WithTimeout(ctx, 6*time.Second)
		defer cCancel()
		resp, cErr := sess.Call(cCtx, pkt, act)
		if cErr != nil || resp == nil {
			slog.Warn("无回包", "call", name, "err", cErr)
			return
		}
		slog.Info("有回包", "call", name, "len", len(resp.Payload))
	}

	switch *hook {
	case "hero":
		// 登录钩子会对多个城镇发送 35/0 英雄副本列表。
		for _, town := range []uint16{1, 3, 4} {
			time.Sleep(300 * time.Millisecond)
			call(protocol.BuildHeroMissionListPacket(town), protocol.ActionHeroMissionList,
				fmt.Sprintf("hero_list_%d", town))
		}
	case "spirit":
		time.Sleep(300 * time.Millisecond)
		call(protocol.BuildRefinePoolOpenPacket(0), protocol.ActionIDRefinePoolOpen,
			"spirit_panel")
	case "altar":
		time.Sleep(300 * time.Millisecond)
		call(protocol.BuildEraRebirthLandPoolPanelPacket(), protocol.ActionIDEraRebirthLandPoolPanel,
			"altar_panel")
	}

	time.Sleep(500 * time.Millisecond)
	if _, sErr := sess.SyncQuests(ctx); sErr != nil {
		slog.Warn("同步任务列表失败", "err", sErr)
	}
	time.Sleep(500 * time.Millisecond)
	call(protocol.BuildMissionGetSectionsPacket(21, sess.LastAckActionID()), protocol.ActionIDMissionGetSections, "get_sections_21")

	// 反向验证：先 get_sections 再发 35/0，英雄副本列表是否仍可回包。
	if *hook == "none" {
		time.Sleep(500 * time.Millisecond)
		call(protocol.BuildHeroMissionListPacket(3), protocol.ActionHeroMissionList, "hero_list_after_sections")
	}
	slog.Info("探针结束", "hook", *hook)
}
