//go:build probe

// 目标协议: [Module 2] Mod_Item_Base.get_player_pack_item_list (0x00020029)
// 基准对齐: 原版客户端 PackView.get_player_pack_item_list
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_pack_list_live.go
// 预期表现: 只读打印主背包物品、数量、价格与数据字典名称，用于分析背包占用来源。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
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
		cfgPath = "configs/config.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "path", cfgPath, "err", err)
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

	st := sess.GetPlayerState()
	fmt.Printf("bag_capacity=%d empty_grids=%d items=%d warehouse_capacity=%d warehouse_items=%d\n",
		pack.PackCapacity, pack.PackEmptyGrids, len(pack.PackItems), pack.WarehouseCapacity, len(pack.WarehouseItems))
	for _, it := range pack.PackItems {
		name, dictPrice := "?", 0
		if dItem, dErr := dictRepo.GetItem(int(it.ItemID)); dErr == nil && dItem != nil {
			name = dItem.Name
			dictPrice = dItem.Price
		}
		fmt.Printf("  grid=%-3d player_item=%-12d item=%-6d qty=%-4d price=%-8d dict_price=%-8d name=%s\n",
			it.GridID, it.PlayerItemID, it.ItemID, it.Field5, it.Field4, dictPrice, name)
	}
	_ = st
}