//go:build probe

// 目标: 只读解析劫镖面板 (Mod_RobMoney_Base)，确认搜索/劫镖次数与状态字段
//
//	open_panel (Act 12) 打开面板
//	get_rob_money_info (Act 0) response=[UByte result, Int, Int, Int, Int, [Int,UByte,Int,Int], String, Int, Int]
//
// 本探针不调用 search/rob，避免触发可能消耗元宝的重搜。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_caravan_info_live.go
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
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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
	call := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
		c, cc := context.WithTimeout(ctx, 6*time.Second)
		defer cc()
		resp, err := sess.Call(c, pkt, act)
		if err != nil {
			slog.Warn("无回包", "action", fmt.Sprintf("0x%08X", act), "err", err)
			return nil
		}
		return resp
	}
	if r := call(protocol.BuildCaravanOpenPanelPacket(sess.LastActionID()), protocol.ActionIDCaravanOpenPanel); r != nil {
		slog.Info("打开面板回包", "len", len(r.Payload), "hex", hex.EncodeToString(r.Payload))
	}
	if r := call(protocol.BuildCaravanGetDetailPacket(protocol.ActionIDCaravanOpenPanel), protocol.ActionIDCaravanGetDetail); r != nil {
		slog.Info("劫镖信息", "len", len(r.Payload), "hex", hex.EncodeToString(r.Payload))
	}
}
