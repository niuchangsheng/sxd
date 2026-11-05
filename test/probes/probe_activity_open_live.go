//go:build probe

// 目标协议: 查询服务端当前开放的活动列表
//
//	[Module 225, Act 0]  Mod_Memoirs_Base.is_open_new   0x00E10000
//	  response: [ [ UByte activity_id, UByte ?, Int start_time, Int end_time,
//	                Int activity_mod ], UByte ? ]
//	[Module 225, Act 10] Mod_Memoirs_Base.is_open       0x00E1000A
//	  response: [ [UByte,UByte], Int, Int ]
//	[Module 809, Act 13] Mod_GodDaily_Base.activity_calendar 0x0329000D
//	  response: [ [ Int, Int, Int, Int, Int, Int ] ]
//
// 基准对齐: com.protocols.Mod_Memoirs_Base / com.protocols.Mod_GodDaily_Base
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_activity_open_live.go
//
// 预期表现: 打印三个只读查询的原始回包，用于人工解析当前真正开放的活动，
//
//	不再依赖红点（红点可能由活动大厅常驻推送而并非活动已开放）。
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

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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
	slog.Info("会话已激活", "role_id", sess.RoleID(), "player_id", sess.PlayerID())

	for _, q := range []struct {
		act     uint32
		name    string
		payload []byte
	}{
		{0x00E10000, "memoirs.is_open_new", nil},
		{0x00E1000A, "memoirs.is_open", nil},
		{0x0329000D, "god_daily.activity_calendar", nil},
		{0x03310000, "fun_seven_days.is_activity_open", nil},
		{0x03300000, "welcome_gift.is_activity_open", nil},
		{0x032C0001, "line_awards.panel_info", nil},
		{0x02C80000, "online_reward.main_panel", nil},
		{0x02C10000, "repress_demon_tower.panel_info", nil},
		{0x019B0000, "demon_invasion.get_state", nil},
		{0x03760000, "era_repress_demon_tower.panel_info", nil},
		{0x00180000, "world_boss.get_world_boss_list(0)", []byte{0}},
		{0x02BA0001, "summer_themed_gift.panel_info", nil},
		{0x033A0000, "island_fishing.panel_info", nil},
		{0x01F70000, "ask_fairy.get_recall_info", nil},
		{0x031E0001, "wealthy_fortune.panel_info", nil},
		{0x03480000, "eternal_games_present.main_info", nil},
		{0x02960001, "search_treasure_box.panel_info", nil},
		{0x016C0000, "reward_task.info", nil},
		{0x01890000, "ice_cave.activity_info", nil},
		{0x024F0001, "sect_bonus.open_panel(0)", []byte{0, 0, 0, 0}},
		{0x023C0001, "sect_utility.sect_main_info(0)", []byte{0, 0, 0, 0}},
		{0x023F000A, "sect_login.get_player_sect_info", nil},
		{0x023C0001, "sect_utility.sect_main_info(1)", []byte{0, 0, 0, 1}},
		{0x024F0001, "sect_bonus.open_panel(1)", []byte{0, 0, 0, 1}},
		{0x023C000D, "sect_worship(1)", []byte{0, 0, 0, 1}},
		{0x023C000D, "sect_utility.sect_worship(0)", []byte{0, 0, 0, 0}},
	} {
		cCtx, cCancel := context.WithTimeout(ctx, 5*time.Second)
		resp, cErr := sess.Call(cCtx, protocol.NewPacket(q.act, q.payload), q.act)
		cCancel()
		if cErr != nil {
			slog.Error("查询失败: "+q.name, "action", fmt.Sprintf("0x%08X", q.act), "err", cErr)
			continue
		}
		slog.Info("查询回包: "+q.name,
			"action", fmt.Sprintf("0x%08X", resp.ActionID),
			"len", len(resp.Payload),
			"hex", hex.EncodeToString(resp.Payload),
		)
	}
}
