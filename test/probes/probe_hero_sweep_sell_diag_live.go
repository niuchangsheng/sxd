//go:build probe

// 目标协议: 精英副本扫荡后出售扫荡物失败诊断
//
//	[Module 35, Act 2] start_practice 0x00230002 [u16 town_id, u8 auto_num, u8 hero_type]
//	[Module 35, Act 7] quickly        0x00230007 []
//	[Module 35, Act 3] stop_practice  0x00230003 []
//	[Module 2, Act 41] get_player_pack_item_list 0x00020029
//	[Module 2, Act 73] player_sell_item          0x00020049 [Short grid_id]
//
// 目的: 复现「扫荡完成后立即出售被服务端静默丢弃」的问题，并验证先发送 stop_practice 是否恢复。
//
// 运行命令: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_hero_sweep_sell_diag_live.go
// 预期表现: 打印扫荡后出售 A（未 stop）与 stop_practice 后出售 B 的结果对比。
// 注意: 会真实扫荡一个城镇并出售最多 2 件可出售物品。
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

const probeTownID = 4

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
	slog.Info("会话已激活", "role_id", sess.RoleID(), "player_id", sess.PlayerID())

	// 1. 查询城镇副本列表 (builder 已携带 PrevAct 尾部)。
	listPkt := protocol.BuildHeroMissionListPacket(probeTownID)
	lCtx, lCancel := context.WithTimeout(ctx, 8*time.Second)
	lResp, lErr := sess.Call(lCtx, listPkt, protocol.ActionHeroMissionList)
	lCancel()
	if lErr != nil || lResp == nil {
		slog.Error("查询英雄副本列表失败", "town_id", probeTownID, "err", lErr)
		return
	}
	list, pErr := protocol.ParseHeroMissionListResponse(lResp.Payload)
	if pErr != nil {
		slog.Error("解析英雄副本列表失败", "err", pErr)
		return
	}
	slog.Info("英雄副本列表", "town_id", probeTownID, "missions", len(list.Missions))

	// 2. 开始扫荡并秒完成 (builder 已携带 PrevAct 尾部)。
	startPkt, _ := protocol.BuildHeroPracticePacket(protocol.HeroPracticeRequest{TownID: probeTownID, AutoNum: 0, HeroType: 0})
	sCtx, sCancel := context.WithTimeout(ctx, 8*time.Second)
	sResp, sErr := sess.Call(sCtx, startPkt, protocol.ActionHeroMissionStartPractice)
	sCancel()
	if sErr != nil || sResp == nil {
		slog.Error("开始扫荡失败", "err", sErr)
		return
	}
	startRes, _ := protocol.ParseHeroPracticeResult(sResp.Payload)
	slog.Info("开始扫荡回包", "result", startRes.Result, "practice_time", startRes.PracticeTime)

	quickPkt := protocol.BuildHeroQuicklyPacket()
	qCtx, qCancel := context.WithTimeout(ctx, 8*time.Second)
	qResp, qErr := sess.Call(qCtx, quickPkt, protocol.ActionHeroMissionQuickly)
	qCancel()
	if qErr != nil || qResp == nil {
		slog.Error("秒完成扫荡失败", "err", qErr)
		return
	}
	quickRes, _ := protocol.ParseHeroQuicklyResponse(qResp.Payload)
	slog.Info("秒完成回包", "result", quickRes.Result)

	// 等待服务端结算掉落。
	time.Sleep(2 * time.Second)

	// 3. 未 stop_practice 时出售。
	targetA := findSellable(ctx, sess, nil)
	if targetA == nil {
		slog.Error("扫荡后未找到可出售物品 A")
		return
	}
	slog.Info("出售 A (未 stop_practice)", "grid_id", targetA.GridID, "item_id", targetA.ItemID)
	okA, codeA, errA := sellOnce(ctx, sess, targetA.GridID)
	slog.Info("出售 A 结果", "ok", okA, "code", codeA, "err", errA)

	// 4. 发送 stop_practice 后再出售。
	slog.Info("发送 stop_practice (带 PrevAct) 并等待 1 秒")
	_ = sess.Send(protocol.BuildHeroStopPracticePacket())
	time.Sleep(1 * time.Second)

	targetB := findSellable(ctx, sess, &targetA.GridID)
	if targetB == nil {
		slog.Error("stop_practice 后未找到可出售物品 B")
		return
	}
	slog.Info("出售 B (stop_practice 后)", "grid_id", targetB.GridID, "item_id", targetB.ItemID)
	okB, codeB, errB := sellOnce(ctx, sess, targetB.GridID)
	slog.Info("出售 B 结果", "ok", okB, "code", codeB, "err", errB)

	slog.Info("诊断完成", "without_stop", okA, "after_stop", okB)
}

func findSellable(ctx context.Context, sess *client.RoleSession, excludeGrid *int32) *protocol.ItemRecord {
	cCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	resp, err := sess.Call(cCtx, protocol.BuildGetPackItemListPacket(), protocol.ActionIDItemGetPackList)
	if err != nil || resp == nil {
		slog.Error("查询背包失败", "err", err)
		return nil
	}
	pack, pErr := protocol.ParsePackItemListResponse(resp.Payload)
	if pErr != nil || pack == nil {
		slog.Error("解析背包失败", "err", pErr)
		return nil
	}
	for i := range pack.PackItems {
		it := pack.PackItems[i]
		if it.ItemID < 900 || it.ItemID > 1000 || it.Field4 <= 0 || it.Field5 <= 0 {
			continue
		}
		if excludeGrid != nil && it.GridID == *excludeGrid {
			continue
		}
		return &pack.PackItems[i]
	}
	return nil
}

func sellOnce(ctx context.Context, sess *client.RoleSession, gridID int32) (bool, uint8, error) {
	pkt, err := protocol.BuildSellItemPacket(int16(gridID))
	if err != nil {
		return false, 0, err
	}
	sCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	resp, err := sess.Call(sCtx, pkt, protocol.ActionIDItemSell)
	if err != nil {
		return false, 0, err
	}
	if resp == nil {
		return false, 0, fmt.Errorf("空回包")
	}
	return protocol.ParseSellItemResponse(resp.Payload)
}
