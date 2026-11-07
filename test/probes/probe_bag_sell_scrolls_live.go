//go:build probe

// 目标: 出售背包中的装备制作卷，腾出扫荡空间
//
//	Mod_Item_Base.get_player_pack_item_list (0x00020029)
//	Mod_Item_Base.player_sell_item (0x00020049, request=[Short grid])
//
// 只出售名称包含「制作卷」且无元宝价值(ingot=0)的物品；不触碰其他道具。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_bag_sell_scrolls_live.go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/dictionary"
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
		slog.Error("加载配置失败", "err", err)
		return
	}
	dictRepo, err := dictionary.NewRepository("")
	if err != nil {
		slog.Error("加载数据字典失败", "err", err)
		return
	}
	defer dictRepo.Close()
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
	call := func(pkt *protocol.Packet, act uint32, timeout time.Duration) *protocol.Packet {
		c, cc := context.WithTimeout(ctx, timeout)
		defer cc()
		resp, err := sess.Call(c, pkt, act)
		if err != nil {
			slog.Warn("无回包", "action", fmt.Sprintf("0x%08X", act), "err", err)
			return nil
		}
		return resp
	}
	resp := call(protocol.BuildGetPackItemListPacket(), protocol.ActionIDItemGetPackList, 8*time.Second)
	if resp == nil {
		slog.Error("查询背包失败")
		return
	}
	pack, perr := protocol.ParsePackItemListResponse(resp.Payload)
	if perr != nil || pack == nil {
		slog.Error("解析背包失败", "err", perr)
		return
	}
	st := sess.GetPlayerState()
	slog.Info("背包状态", "capacity", pack.PackCapacity, "empty_grids", pack.PackEmptyGrids, "items", len(pack.PackItems),
		"coins", st.Coins, "ingots", st.Ingots)
	sold, skipped := 0, 0
	for _, it := range pack.PackItems {
		name := ""
		ingot := 0
		if dItem, dErr := dictRepo.GetItem(int(it.ItemID)); dErr == nil && dItem != nil {
			name = dItem.Name
			ingot = dItem.Ingot
		}
		if !strings.Contains(name, "制作卷") {
			continue
		}
		if ingot > 0 {
			slog.Info("跳过元宝物品", "grid", it.GridID, "item", it.ItemID, "name", name)
			skipped++
			continue
		}
		pkt, err := protocol.BuildSellItemPacket(int16(it.GridID))
		if err != nil {
			continue
		}
		sellResp := call(pkt, protocol.ActionIDItemSell, 8*time.Second)
		if sellResp == nil {
			continue
		}
		ok, code, pErr := protocol.ParseSellItemResponse(sellResp.Payload)
		if pErr != nil {
			continue
		}
		slog.Info("出售制作卷", "grid", it.GridID, "item", it.ItemID, "name", name, "result", code, "ok", ok)
		if ok {
			sold++
		}
		time.Sleep(120 * time.Millisecond)
	}
	// 复查背包空格
	if r2 := call(protocol.BuildGetPackItemListPacket(), protocol.ActionIDItemGetPackList, 8*time.Second); r2 != nil {
		if p2, e2 := protocol.ParsePackItemListResponse(r2.Payload); e2 == nil && p2 != nil {
			fin := sess.GetPlayerState()
			slog.Info("出售完成", "sold", sold, "skipped_ingot", skipped,
				"empty_grids", p2.PackEmptyGrids, "items", len(p2.PackItems),
				"coin_delta", fin.Coins-st.Coins, "ingot_delta", fin.Ingots-st.Ingots)
		}
	}
}
