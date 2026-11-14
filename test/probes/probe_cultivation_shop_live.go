//go:build probe

// 目标: 对比 s0/s1 卡包商城 shop_info (859/7) 条目，定位 buy_shop (859/8) 在 s1 静默丢包的原因。
//
// 只发送只读的 shop_info，不发购买请求，不会消耗任何资源。
//
// 运行: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_cultivation_shop_live.go -role 1
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
	resp, cErr := sess.Call(cCtx, protocol.BuildCultivationCardShopInfoPacket(), protocol.ActionIDCultivationShopInfo)
	cCancel()
	if cErr != nil || resp == nil {
		slog.Warn("shop_info 无回包", "err", cErr)
		return
	}
	slog.Info("shop_info 原始载荷", "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload))
	items, extra, pErr := protocol.ParseCultivationCardShop(resp.Payload)
	if pErr != nil {
		slog.Warn("解析失败", "err", pErr)
		return
	}
	slog.Info("shop_info 解析", "count", len(items), "extra", extra)
	for i, it := range items {
		slog.Info("shop 条目", "index", i, "shop_id", it.ShopID, "left_number", it.LeftNumber,
			"cost_item_id", func() int32 {
				if g := protocol.GetCultivationCardShopGood(it.ShopID); g != nil {
					return g.CostItemID
				}
				return 0
			}())
	}
}
