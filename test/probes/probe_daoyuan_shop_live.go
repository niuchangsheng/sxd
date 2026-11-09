//go:build probe

// 目标协议: 精品杂货/道缘 VIP 商店 132 在圣域连接上的可达性验证
//
// 覆盖:
//
//	132/0 daoyuan_shop_item_list
//	132/3 daoyuan_vip_shop_item_list [UByte VIP]
//
// 背景: 主服连接上 132 全动作静默丢弃；尝试在 Mod 293 圣域独立会话上调用。
//
// 运行: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_daoyuan_shop_live.go
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

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/niuchangsheng.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
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
	slog.Info("主服会话已激活", "role_id", sess.RoleID())

	call := func(s *client.RoleSession, pkt *protocol.Packet, act uint32, name string) {
		cCtx, cCancel := context.WithTimeout(ctx, 8*time.Second)
		defer cCancel()
		resp, cErr := s.Call(cCtx, pkt, act)
		if cErr != nil {
			slog.Warn("无回包", "call", name, "action", fmt.Sprintf("0x%08X", act), "err", cErr)
			return
		}
		limit := len(resp.Payload)
		if limit > 80 {
			limit = 80
		}
		slog.Info("回包", "call", name, "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload[:limit]))
	}

	call(sess, protocol.NewPacket(0x00840000, nil), 0x00840000, "main_132_0")
	call(sess, protocol.NewPacket(0x00840003, []byte{5}), 0x00840003, "main_132_3_vip5")

	sa, err := sess.OpenSaintAreaSession(ctx)
	if err != nil {
		slog.Error("建立圣域会话失败", "err", err)
		return
	}
	defer sa.Close()
	slog.Info("圣域会话已建立", "player_id", sa.PlayerID())

	call(sa, protocol.NewPacket(0x00840000, nil), 0x00840000, "sa_132_0")
	for _, v := range []byte{0, 1, 2, 3, 4, 6, 7} {
		call(sa, protocol.NewPacket(0x00840003, []byte{v}), 0x00840003, fmt.Sprintf("sa_132_3_byte%d", v))
	}
	call(sa, protocol.BuildDaoyuanVipShopListPacket(5), 0x00840003, "sa_132_3_builder5")
	call(sa, protocol.BuildDaoyuanVipShopListPacket(8), 0x00840003, "sa_132_3_builder8")
	call(sa, protocol.NewPacket(0x00840002, nil), 0x00840002, "sa_132_2_nil")
	call(sa, protocol.NewPacket(0x00840002, []byte{5}), 0x00840002, "sa_132_2_5")
	call(sa, protocol.NewPacket(0x00840002, []byte{8}), 0x00840002, "sa_132_2_8")
	call(sa, protocol.BuildSaUnionWorshipPanelPacketWithPrev(sa.LastActionID()), 0x01A30001, "sa_419_panel")
	call(sa, protocol.BuildSaUnionWorshipPacket(0), 0x01A30002, "sa_419_worship")
}
