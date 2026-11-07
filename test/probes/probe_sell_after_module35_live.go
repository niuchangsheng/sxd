//go:build probe

// 目标: 隔离验证 Module 35 请求是否会阻断后续 Module 2 出售。
//
//	[Module 35, Act 0] get_hero_mission_list 0x00230000 [u16 town_id]
//	[Module 35, Act 3] stop_practice         0x00230003 []
//	[Module 2, Act 41] get_player_pack_item_list 0x00020029
//	[Module 2, Act 73] player_sell_item          0x00020049 [Short grid_id]
//
// 运行命令:
//
//	SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_sell_after_module35_live.go
//	SXD_CONFIG=configs/niuchangsheng.yaml SXD_PROBE_HERO_LIST=1 go run -tags=probe test/probes/probe_sell_after_module35_live.go
//	SXD_CONFIG=configs/niuchangsheng.yaml SXD_PROBE_STOP_PRACTICE=1 go run -tags=probe test/probes/probe_sell_after_module35_live.go
//
// 注意: 每次运行会真实出售 1 件可出售物品。
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
	"sxd-pie-ng/internal/protocol"
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
		MaxReconnectAttempts: 2,
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
	slog.Info("会话已激活", "role_id", sess.RoleID())

	if os.Getenv("SXD_PROBE_HERO_LIST") == "1" {
		cCtx, cCancel := context.WithTimeout(ctx, 8*time.Second)
		resp, cErr := sess.Call(cCtx, protocol.BuildHeroMissionListPacket(4), protocol.ActionHeroMissionList)
		cCancel()
		slog.Info("已发送 Module 35 get_hero_mission_list", "err", cErr, "resp_len", payloadLen(resp))
	}
	if os.Getenv("SXD_PROBE_HERO_LIST_PREV") == "1" {
		pkt := protocol.NewPacket(protocol.ActionHeroMissionList,
			protocol.NewWriter().WriteUint16(4).WriteUint32(sess.LastActionID()).Bytes())
		pkt.HasPrevAct = true
		cCtx, cCancel := context.WithTimeout(ctx, 8*time.Second)
		resp, cErr := sess.Call(cCtx, pkt, protocol.ActionHeroMissionList)
		cCancel()
		slog.Info("已发送 Module 35 get_hero_mission_list (带 PrevAct)", "err", cErr, "resp_len", payloadLen(resp))
	}
	if os.Getenv("SXD_PROBE_STOP_PRACTICE") == "1" {
		_ = sess.Send(protocol.BuildHeroStopPracticePacket())
		slog.Info("已发送 Module 35 stop_practice (空载荷)")
		time.Sleep(500 * time.Millisecond)
	}
	if os.Getenv("SXD_PROBE_STOP_WITH_PREV") == "1" {
		pkt := protocol.NewPacket(protocol.ActionHeroMissionStopPractice,
			protocol.NewWriter().WriteUint32(sess.LastActionID()).Bytes())
		pkt.HasPrevAct = true
		_ = sess.Send(pkt)
		slog.Info("已发送 Module 35 stop_practice (带 PrevAct)", "prev_act", fmt.Sprintf("0x%08X", sess.LastActionID()))
		time.Sleep(500 * time.Millisecond)
	}

	if os.Getenv("SXD_PROBE_ENTER_TOWN") == "1" {
		townID := int32(sess.GetPlayerState().TownID)
		code, eErr := sess.EnterTown(ctx, townID)
		slog.Info("已重新进入城镇", "town_id", townID, "result", code, "err", eErr)
		time.Sleep(500 * time.Millisecond)
	}

	// 查询背包（Module 2 业务读）并出售一个制作卷。
	pCtx, pCancel := context.WithTimeout(ctx, 8*time.Second)
	pResp, pErr := sess.Call(pCtx, protocol.BuildGetPackItemListPacket(), protocol.ActionIDItemGetPackList)
	pCancel()
	if pErr != nil || pResp == nil {
		slog.Error("查询背包失败", "err", pErr)
		return
	}
	pack, parseErr := protocol.ParsePackItemListResponse(pResp.Payload)
	if parseErr != nil || pack == nil {
		slog.Error("解析背包失败", "err", parseErr)
		return
	}
	var target *protocol.ItemRecord
	for i := range pack.PackItems {
		it := pack.PackItems[i]
		if it.ItemID >= 900 && it.ItemID <= 1000 && it.Field4 > 0 && it.Field5 > 0 {
			target = &pack.PackItems[i]
			break
		}
	}
	if target == nil {
		slog.Error("未找到可出售制作卷")
		return
	}
	slog.Info("出售目标", "grid_id", target.GridID, "item_id", target.ItemID, "price", target.Field4)

	sellPkt, _ := protocol.BuildSellItemPacket(int16(target.GridID))
	sCtx, sCancel := context.WithTimeout(ctx, 8*time.Second)
	sResp, sErr := sess.Call(sCtx, sellPkt, protocol.ActionIDItemSell)
	sCancel()
	if sErr != nil || sResp == nil {
		slog.Error("出售失败", "err", sErr)
		return
	}
	ok, code, rErr := protocol.ParseSellItemResponse(sResp.Payload)
	slog.Info("出售结果", "ok", ok, "code", code, "parse_err", rErr)
}

func payloadLen(p *protocol.Packet) int {
	if p == nil {
		return 0
	}
	return len(p.Payload)
}
