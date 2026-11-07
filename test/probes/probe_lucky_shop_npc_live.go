//go:build probe

// 目标: 定位琳琅阁 NPC id 并验证 lucky_shop_item_list (Mod_Item_Base, Module 100, Act 80)
//
// 官方客户端 LuckyShopView: 请求参数为当前城镇 NPC id
//
//	npcID = TownType.getTownNPCIdByFunc(player.mapId, NPCType.LuckyShopNPC)
//
// 本探针先从 NPC 结交面板 (436/1) 读取全部 TownNpcID, 再逐个以
// [Int32 npcID] + PrevAct 调用 lucky_shop_item_list, 找到真正回包的 NPC。
// 只读, 不购买任何商品。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_lucky_shop_npc_live.go
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
	actionLuckyShopItemList uint32 = 0x00640050 // 100/80
	actionLuckyShopBuy      uint32 = 0x00640051 // 100/81
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	timeout := flag.Duration("timeout", 3*time.Second, "单个 NPC 的等待上限")
	flag.Parse()
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/s0.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[0]
	tc := platform.NewTicketCache(90 * time.Minute)
	xd, _ := platform.NewXindongAuthenticator()
	xd.SetCookie(acc.Cookie)
	tc.RegisterAuthenticator(xd)
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	ticket, err := tc.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		slog.Error("换票失败", "err", err)
		return
	}
	rc := client.SessionConfig{RoleID: fmt.Sprintf("%s-%s", role.ServerID, role.RoleName), RoleName: role.RoleName,
		Username: acc.Username, ServerAddr: ticket.GatewayURL, ServerID: role.ServerID,
		Platform: acc.Platform, Authenticator: client.DefaultAuthenticator, MaxReconnectAttempts: 3}
	client.ApplyTicket(&rc, ticket)
	sess := client.NewRoleSession(rc)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()
	for sess.State() != client.StateActive && ctx.Err() == nil {
		time.Sleep(100 * time.Millisecond)
	}

	infoPkt := protocol.BuildNpcFriendshipInfoPacket()
	infoPkt.HasPrevAct = true
	infoPkt.Payload = []byte{0, 0, 0, 0}
	c, cc := context.WithTimeout(ctx, 8*time.Second)
	infoResp, err := sess.Call(c, infoPkt, protocol.ActionIDNpcFriendshipInfo)
	cc()
	if err != nil || infoResp == nil {
		slog.Error("查询 NPC 结交信息失败", "err", err)
		return
	}
	snap, err := protocol.ParseNpcFriendshipInfoResult(infoResp.Payload)
	if err != nil {
		slog.Error("解析 NPC 结交信息失败", "err", err)
		return
	}
	ids := make([]int32, 0, len(snap.Npcs))
	for _, n := range snap.Npcs {
		if n.TownNpcID > 0 {
			ids = append(ids, n.TownNpcID)
		}
	}
	slog.Info("城镇 NPC 列表", "count", len(ids), "town_npc_ids", ids)

	for _, npcID := range ids {
		w := protocol.NewWriter()
		w.WriteInt32(npcID)
		pkt := protocol.NewPacket(actionLuckyShopItemList, w.Bytes())
		pkt.HasPrevAct = true
		c, cc := context.WithTimeout(ctx, *timeout)
		resp, err := sess.Call(c, pkt, actionLuckyShopItemList)
		cc()
		if err != nil {
			continue
		}
		slog.Info("琳琅阁回包命中", "npc_id", npcID, "len", len(resp.Payload),
			"hex", hex.EncodeToString(resp.Payload))
		return
	}
	slog.Warn("未找到可响应的琳琅阁 NPC", "tried", len(ids))
}
