//go:build probe

// 目标协议: [Module 2] Mod_Item_Base.player_sell_item (0x00020049)
// 用途: 一次性清理上一轮精英扫荡中断时遗留的掉落物 (按 PlayerItemID 下限筛选)。
// 运行命令:
//   SXD_CONFIG=configs/s0.yaml SXD_MIN_PLAYER_ITEM_ID=271639000 \
//     go run -tags=probe test/probes/probe_sell_recent_sweep_drops_live.go
// 预期表现: 只卖出 PlayerItemID >= 指定阈值且价格 > 0 的背包物品，用于释放背包格位。
// 注意: 该探针会真实出售物品，属于写操作；仅在确认这些是扫荡遗留物时运行。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)

	minRaw := os.Getenv("SXD_MIN_PLAYER_ITEM_ID")
	minID, err := strconv.ParseInt(minRaw, 10, 32)
	if err != nil || minID <= 0 {
		fmt.Println("请通过 SXD_MIN_PLAYER_ITEM_ID 指定 PlayerItemID 下限 (例如 271639000)")
		return
	}

	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/config.yaml"
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

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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
		MaxReconnectAttempts: 1,
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()

	deadline := time.Now().Add(15 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未能在时限内激活", "state", sess.State())
		return
	}

	c, cc := context.WithTimeout(ctx, 5*time.Second)
	resp, callErr := sess.Call(c, protocol.BuildGetPackItemListPacket(), protocol.ActionIDItemGetPackList)
	cc()
	if callErr != nil || resp == nil {
		slog.Error("查询背包失败", "err", callErr)
		return
	}
	pack, pErr := protocol.ParsePackItemListResponse(resp.Payload)
	if pErr != nil || pack == nil {
		slog.Error("解析背包失败", "err", pErr)
		return
	}

	sold := 0
	for _, it := range pack.PackItems {
		if int64(it.PlayerItemID) < minID || it.Field4 <= 0 {
			continue
		}
		pkt, bErr := protocol.BuildSellItemPacket(int16(it.GridID))
		if bErr != nil {
			continue
		}
		sc, scCancel := context.WithTimeout(ctx, 8*time.Second)
		sResp, sErr := sess.Call(sc, pkt, protocol.ActionIDItemSell)
		scCancel()
		if sErr != nil || sResp == nil {
			fmt.Printf("sell grid=%d item=%d failed: %v\n", it.GridID, it.ItemID, sErr)
			continue
		}
		ok, code, rErr := protocol.ParseSellItemResponse(sResp.Payload)
		if rErr != nil || !ok {
			fmt.Printf("sell grid=%d item=%d rejected: code=%d err=%v\n", it.GridID, it.ItemID, code, rErr)
			continue
		}
		sold++
		fmt.Printf("sold grid=%d item=%d qty=%d price=%d\n", it.GridID, it.ItemID, it.Field5, it.Field4)
		time.Sleep(50 * time.Millisecond)
	}
	fmt.Printf("cleanup done: sold=%d (threshold=%d)\n", sold, minID)
}