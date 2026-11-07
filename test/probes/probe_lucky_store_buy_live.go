//go:build probe

// 目标: 验证琳琅阁购买商品 (Mod_Mobile_Base, Module 100 Act 124)
//
//	get_lucky_store_item_list (Act 123) response=[[Int item_id, Int store_id, UByte buy_flag, UByte is_fresh], Short]
//	buy_lucky_store_item     (Act 124) request=[Int, Int] 或 [Int, Int, Int] (客户端版本差异)
//
// 客户端按道具基础价用铜钱结算 (ingot 显式置 0)；本探针购买 1 件最低价商品，
// 并在购买前后校验元宝不变。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_lucky_store_buy_live.go
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

type storeItem struct {
	id      int32
	storeID int32
	buyFlag uint8
	fresh   uint8
}

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
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
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
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
	call := func(action uint32, payload []byte, prev bool) *protocol.Packet {
		pkt := protocol.NewPacket(action, payload)
		pkt.HasPrevAct = prev
		c, cc := context.WithTimeout(ctx, 7*time.Second)
		defer cc()
		resp, err := sess.Call(c, pkt, action)
		if err != nil {
			slog.Warn("无回包", "action", fmt.Sprintf("0x%08X", action), "err", err)
			return nil
		}
		return resp
	}
	listPkt := []byte{0, 0, 0, 0}
	resp := call(0x0064007B, listPkt, true)
	if resp == nil {
		slog.Error("琳琅阁列表无回包")
		return
	}
	slog.Info("琳琅阁列表", "hex", hex.EncodeToString(resp.Payload))
	r := protocol.NewReader(resp.Payload)
	n, _ := r.ReadUint16()
	items := make([]storeItem, 0, n)
	for i := 0; i < int(n); i++ {
		var it storeItem
		it.id, _ = r.ReadInt32()
		it.storeID, _ = r.ReadInt32()
		it.buyFlag, _ = r.ReadUint8()
		it.fresh, _ = r.ReadUint8()
		items = append(items, it)
	}
	cd, _ := r.ReadInt16()
	slog.Info("琳琅阁商品", "count", len(items), "cd", cd)
	for _, it := range items {
		slog.Info("商品", "item_id", it.id, "store_id", it.storeID, "buy_flag", it.buyFlag, "fresh", it.fresh)
	}
	if len(items) == 0 {
		return
	}
	st := sess.GetPlayerState()
	it := items[0]
	// 2 参请求不带 PrevAct，避免会话层用 PrevAct 覆盖第二个参数。
	buy := func(arg0, arg1 int32) *protocol.Packet {
		w := protocol.NewWriter()
		w.WriteInt32(arg0)
		w.WriteInt32(arg1)
		pkt := protocol.NewPacket(0x0064007C, w.Bytes())
		c, cc := context.WithTimeout(ctx, 7*time.Second)
		defer cc()
		resp, err := sess.Call(c, pkt, 0x0064007C)
		if err != nil {
			slog.Warn("购买无回包", "arg0", arg0, "arg1", arg1, "err", err)
			return nil
		}
		return resp
	}
	candidates := [][2]int32{
		{38, it.id}, {0, it.id}, {38, it.storeID}, {0, it.storeID},
	}
	for _, cd := range candidates {
		if ctx.Err() != nil {
			break
		}
		if resp := buy(cd[0], cd[1]); resp != nil {
			slog.Info("购买回包", "arg0", cd[0], "arg1", cd[1], "hex", hex.EncodeToString(resp.Payload))
			if len(resp.Payload) > 0 && resp.Payload[0] == 175 {
				break
			}
		}
	}
	time.Sleep(300 * time.Millisecond)
	fin := sess.GetPlayerState()
	slog.Info("购买后", "coin_delta", fin.Coins-st.Coins, "ingot_delta", fin.Ingots-st.Ingots)
}
