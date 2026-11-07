//go:build probe

// 目标: 通天道场/通天塔雇佣 (Mod_TongTianTowerHire_Base, Module 702) 只读探测
//
//	open_panel (1) request=[Int] response 角色列表
//	get_income (6) request=[] response=[UByte,Int]
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_tower_hire_live.go
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
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
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
	call := func(action uint32, payload []byte) *protocol.Packet {
		pkt := protocol.NewPacket(action, payload)
		pkt.HasPrevAct = true
		c, cc := context.WithTimeout(ctx, 7*time.Second)
		defer cc()
		resp, err := sess.Call(c, pkt, action)
		if err != nil {
			slog.Warn("无回包", "action", fmt.Sprintf("0x%08X", action), "err", err)
			return nil
		}
		return resp
	}
	zero4 := []byte{0, 0, 0, 0}
	if r := call(0x02BE0001, zero4); r != nil {
		slog.Info("雇佣面板", "len", len(r.Payload), "hex", hex.EncodeToString(r.Payload[:min(64, len(r.Payload))]))
	}
	if r := call(0x02BE0006, zero4); r != nil {
		slog.Info("道场收益", "hex", hex.EncodeToString(r.Payload))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
