//go:build probe

// 目标: 检查 s1 背包/仓库是否存有药园种子 (169~176)，解释 seed_*_owned=0。
//
// 若仓库中存在种子，可迁移到背包后走 13/24 新农场播种。
//
// 运行: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_pack_seeds_live.go -role 1
package main

import (
	"context"
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

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/niuchangsheng.yaml"
	}
	roleIdx := flag.Int("role", 1, "配置中的角色下标 (0=s0, 1=s1)")
	flag.Parse()

	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[*roleIdx]

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
		MaxReconnectAttempts: 3,
	}
	client.ApplyTicket(&roleCfg, ticket)
	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()
	deadline := time.Now().Add(40 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}

	cCtx, cCancel := context.WithTimeout(ctx, 8*time.Second)
	resp, cErr := sess.Call(cCtx, protocol.BuildGetPackItemListPacket(), protocol.ActionIDItemGetPackList)
	cCancel()
	if cErr != nil || resp == nil {
		slog.Error("背包查询无回包", "err", cErr)
		return
	}
	pack, pErr := protocol.ParsePackItemListResponse(resp.Payload)
	if pErr != nil {
		slog.Error("解析背包失败", "err", pErr)
		return
	}
	slog.Info("背包概览", "role_id", sess.RoleID(),
		"pack_items", len(pack.PackItems), "warehouse_items", len(pack.WarehouseItems))

	printSeeds := func(label string, items []protocol.ItemRecord) {
		for _, it := range items {
			if it.ItemID >= 160 && it.ItemID <= 185 {
				slog.Info("候选种子/道具", "where", label, "item_id", it.ItemID,
					"grid_id", it.GridID, "player_item_id", it.PlayerItemID)
			}
		}
	}
	printSeeds("pack", pack.PackItems)
	printSeeds("warehouse", pack.WarehouseItems)
	slog.Info("探针结束")
}
