//go:build probe

// 目标协议: 多玩法面板可用性与回包布局探测
//
// 本探针仅发送只读的面板查询/信息查询请求，不执行任何消耗资源或改变状态的操作。
// 用于确认以下模块在当前线上服务端是否可用以及回包字段布局:
//
//	通天塔      Mod_TongTianTower_Base.panel_info        Module 648 Action 0
//	幻兽        Mod_Eudemons_Base.panel_info             Module 693 Action 0
//	幻兽宝石    Mod_EudemonsStone_Base.is_func_open      Module 880 Action 0
//	飞剑        Mod_DestinySword_Base.panel_info         Module 842 Action 1
//	灵结        Mod_PartnerLink_Base.get_info            Module 290 Action 0
//	多宝山      Mod_MysticalTreasure_Base.get_panel_info Module 740 Action 1
//	寻找异兽    Mod_SpaceFind_Base.open_space_find       Module 123 Action 0
//	神仙收藏册  Mod_CollectionBooklets_Base.sect_info    Module 815 Action 0
//	NPC结交     Mod_NpcFriendship_Base.get_info          Module 436 Action 1
//	圣域竞技场  Mod_SaArena_Base.get_info                Module 298 Action 0
//	圣域修炼房  Mod_SaPracticeRoom_Base.get_rooms        Module 407 Action 0
//	圣域取经    Mod_SaTakeBible_Base.get_info?           见 Actions 列表
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_missing_features_live.go
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

type panelProbe struct {
	name   string
	action uint32
}

var probes = []panelProbe{
	{"tongtian_panel", 0x02880000},
	{"eudemons_panel", 0x02B50000},
	{"eudemons_stone_open", 0x03700000},
	{"destiny_sword_panel", 0x034A0001},
	{"partner_link_info", 0x01220000},
	{"mystical_treasure_panel", 0x02E40001},
	{"space_find_open", 0x007B0000},
	{"collection_booklets_sect", 0x032F0000},
	{"npc_friendship_info", 0x01B40001},
	{"sa_arena_info", 0x012A0000},
	{"sa_practice_rooms", 0x01970000},
	{"super_deploy_list", 0x002E0000},
	{"soul_deploy_list", 0x02660001},
}

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
	slog.Info("会话已激活", "role_id", sess.RoleID(), "player_id", sess.PlayerID())

	for _, p := range probes {
		pkt := protocol.NewPacket(p.action, nil)
		cCtx, cCancel := context.WithTimeout(ctx, 4*time.Second)
		resp, cErr := sess.Call(cCtx, pkt, p.action)
		cCancel()
		if cErr != nil {
			slog.Warn("无回包", "name", p.name, "action", fmt.Sprintf("0x%08X", p.action), "err", cErr)
			continue
		}
		slog.Info("回包",
			"name", p.name,
			"action", fmt.Sprintf("0x%08X", resp.ActionID),
			"len", len(resp.Payload),
			"hex", hex.EncodeToString(resp.Payload),
		)
		time.Sleep(300 * time.Millisecond)
	}
	slog.Info("只读探测结束")
}