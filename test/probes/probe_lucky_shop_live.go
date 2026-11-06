//go:build probe

// 目标协议: 琳琅阁/神秘商人 (Mod_Mobile_Base.lucky_shop_item_list, Module 100 Action 80)
//
// 官方 response: [[Int item_id, Int coin_price], Int refresh_hour, Int refresh_seconds, Int refresh_ingot]
// 商品单价字段命中 LuckyShopData.getItemList 的 coin = price，即琳琅阁商品以铜钱结算。
//
// 本探针默认只读；设置 SXD_PROBE_BUY=3 时购买指定数量的最低价商品，
// 用于验证每日活跃度任务 74「琳琅阁购买商品」(+5/次，上限 3)。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml SXD_PROBE_BUY=3 go run -tags=probe test/probes/probe_lucky_shop_live.go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

const (
	actionLuckyShopList uint32 = 0x0002004C
	actionLuckyShopBuy  uint32 = 0x0002004E

	// 神秘商人 NPC ID。当前位置为山海九界 (town 153) 时该城镇没有 NPC 38，
	// 服务端接受 0 作为全局琳琅阁入口。
	luckyShopNPC int32 = 0
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	buyCount := 0
	if raw := os.Getenv("SXD_PROBE_BUY"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			buyCount = v
		}
	}

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
		MaxReconnectAttempts: 3,
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动主服会话失败", "err", err)
		return
	}
	defer sess.Close()
	deadline := time.Now().Add(40 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("主服会话未激活", "state", sess.State())
		return
	}

	call := func(pkt *protocol.Packet, act uint32, timeout time.Duration) *protocol.Packet {
		cCtx, cCancel := context.WithTimeout(ctx, timeout)
		defer cCancel()
		resp, cErr := sess.Call(cCtx, pkt, act)
		if cErr != nil {
			slog.Warn("调用无回包", "action", fmt.Sprintf("0x%08X", act), "err", cErr)
			return nil
		}
		return resp
	}

	state := sess.GetPlayerState()
	slog.Info("角色地图状态", "town_id", state.TownID, "town_map_id", state.TownMapID)

	listW := protocol.NewWriter()
	listW.WriteInt32(luckyShopNPC)
	listW.WriteUint32(0)
	listPkt := protocol.NewPacket(actionLuckyShopList, listW.Bytes())
	listPkt.HasPrevAct = true
	resp := call(listPkt, actionLuckyShopList, 8*time.Second)
	if resp == nil {
		slog.Error("查询琳琅阁商品失败")
		return
	}
	r := protocol.NewReader(resp.Payload)
	n, err := r.ReadUint16()
	if err != nil {
		slog.Error("解析琳琅阁商品数量失败", "err", err, "hex", fmt.Sprintf("%x", resp.Payload))
		return
	}
	type shopItem struct {
		id    int32
		price int32
	}
	items := make([]shopItem, 0, n)
	for i := 0; i < int(n); i++ {
		var it shopItem
		it.id, _ = r.ReadInt32()
		it.price, _ = r.ReadInt32()
		items = append(items, it)
	}
	refreshHour, _ := r.ReadInt32()
	refreshSec, _ := r.ReadInt32()
	refreshIngot, _ := r.ReadInt32()
	slog.Info("琳琅阁商品", "count", len(items),
		"refresh_hour", refreshHour, "refresh_seconds", refreshSec, "refresh_ingot", refreshIngot)
	for _, it := range items {
		slog.Info("商品", "item_id", it.id, "coin_price", it.price)
	}

	if buyCount <= 0 || len(items) == 0 {
		return
	}
	sorted := append([]shopItem(nil), items...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].price < sorted[j].price })
	if buyCount > len(sorted) {
		buyCount = len(sorted)
	}

	before := sess.GetPlayerState()
	bought := 0
	for i := 0; i < buyCount; i++ {
		it := sorted[i]
		buyPkt := protocol.NewPacket(actionLuckyShopBuy, nil)
		w := protocol.NewWriter()
		w.WriteInt32(luckyShopNPC)
		w.WriteInt32(it.id)
		w.WriteUint32(0)
		buyPkt = protocol.NewPacket(actionLuckyShopBuy, w.Bytes())
		buyPkt.HasPrevAct = true
		br := call(buyPkt, actionLuckyShopBuy, 10*time.Second)
		if br == nil {
			break
		}
		if len(br.Payload) == 0 {
			break
		}
		result := br.Payload[0]
		slog.Info("购买结果", "item_id", it.id, "coin_price", it.price, "result", result)
		if result != 0 {
			break
		}
		bought++
		time.Sleep(150 * time.Millisecond)
	}
	after := sess.GetPlayerState()
	slog.Info("琳琅阁购买完成", "bought", bought,
		"coin_delta", after.Coins-before.Coins,
		"ingot_delta", after.Ingots-before.Ingots)
}