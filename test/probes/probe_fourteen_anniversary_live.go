//go:build probe

// 目标协议: [Module 821 / 0x335] Mod_FourteenAnniversaryGift_Base 十四周年礼包
//
//	get_panel_info   Act 1 -> 0x03350001  空请求
//	  response: UByte result, Int year, Int bubble_day, Int is_get_free,
//	            [Int item_id, Int item_num] free_gift_list, Int charge_ingot,
//	            [Int item_id, Int item_num] ingot_gift_list, Int ingot_pool
//	get_free_gift    Act 2 -> 0x03350002  空请求, 回包 UByte result
//	buy_ingot_gift   Act 3 -> 0x03350003  Int gift_id (消耗元宝, 本探针不调用)
//
// 基准对齐: com.protocols.Mod_FourteenAnniversaryGift_Base / com.datas.FourteenAnniversaryGiftData
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_fourteen_anniversary_live.go [-execute]
//
// 预期表现: 默认只读打印周年礼包面板与免费礼包内容；-execute 在未领取时
//
//	调用 get_free_gift 并打印结果码。全流程不消耗任何元宝。
package main

import (
	"context"
	"encoding/hex"
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

const (
	actionFourteenAnniversaryPanel uint32 = 0x03350001
	actionFourteenAnniversaryFree  uint32 = 0x03350002
)

func main() {
	execute := flag.Bool("execute", false, "实际领取免费周年礼包")
	flag.Parse()
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
	before := sess.GetPlayerState().Ingots
	slog.Info("会话已激活", "role_id", sess.RoleID(), "player_id", sess.PlayerID(), "ingots", before)

	call := func(act uint32, name string) *protocol.Packet {
		pkt := protocol.NewPacket(act, nil)
		cCtx, cCancel := context.WithTimeout(ctx, 5*time.Second)
		resp, cErr := sess.Call(cCtx, pkt, act)
		cCancel()
		if cErr != nil {
			slog.Error("调用失败: "+name, "action", fmt.Sprintf("0x%08X", act), "err", cErr)
			return nil
		}
		slog.Info("回包: "+name,
			"action", fmt.Sprintf("0x%08X", resp.ActionID),
			"len", len(resp.Payload),
			"hex", hex.EncodeToString(resp.Payload),
		)
		return resp
	}

	resp := call(actionFourteenAnniversaryPanel, "get_panel_info")
	if resp == nil {
		return
	}
	r := protocol.NewReader(resp.Payload)
	result, e1 := r.ReadUint8()
	year, e2 := r.ReadInt32()
	bubbleDay, e3 := r.ReadInt32()
	isGetFree, e4 := r.ReadInt32()
	freeCount, e5 := r.ReadUint16()
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil {
		slog.Error("解析周年礼包面板失败", "err", fmt.Sprint(e1, e2, e3, e4, e5))
		return
	}
	type giftItem struct {
		ItemID int32
		Num    int32
	}
	freeGifts := make([]giftItem, 0, freeCount)
	for i := 0; i < int(freeCount); i++ {
		itemID, err := r.ReadInt32()
		if err != nil {
			break
		}
		num, err := r.ReadInt32()
		if err != nil {
			break
		}
		freeGifts = append(freeGifts, giftItem{ItemID: itemID, Num: num})
	}
	chargeIngot, _ := r.ReadInt32()
	ingotCount, _ := r.ReadUint16()
	ingotGifts := make([]giftItem, 0, ingotCount)
	for i := 0; i < int(ingotCount); i++ {
		itemID, err := r.ReadInt32()
		if err != nil {
			break
		}
		num, err := r.ReadInt32()
		if err != nil {
			break
		}
		ingotGifts = append(ingotGifts, giftItem{ItemID: itemID, Num: num})
	}
	ingotPool, _ := r.ReadInt32()
	slog.Info("周年礼包面板",
		"result", result,
		"year", year,
		"bubble_day", bubbleDay,
		"is_get_free", isGetFree,
		"free_gifts", freeGifts,
		"charge_ingot", chargeIngot,
		"ingot_gifts", ingotGifts,
		"ingot_pool", ingotPool,
		"remaining", r.Remaining(),
	)

	if !*execute {
		slog.Info("只读模式结束；如需领取免费周年礼包请追加 -execute")
		return
	}
	if result != 0 {
		slog.Info("周年礼包功能未开放，跳过领取", "result", result)
		return
	}
	if isGetFree == 1 {
		slog.Info("免费周年礼包今日已领取，跳过")
		return
	}
	gResp := call(actionFourteenAnniversaryFree, "get_free_gift")
	if gResp == nil || len(gResp.Payload) == 0 {
		return
	}
	slog.Info("免费周年礼包领取结果", "result", gResp.Payload[0],
		"ingots_before", before, "ingots_after", sess.GetPlayerState().Ingots)
}
