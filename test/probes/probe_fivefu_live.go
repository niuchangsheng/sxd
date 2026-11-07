//go:build probe

// 目标: 五福临门活动入口 (Mod_FiveFu_Base, Module 400 / 0x0190)
//
//	get_info (Act 0, 0x01900000) request=[] response=[Int x7,[Int,Int],Int x4]
//	open_pack (Act 1, 0x01900001)
//	give_fu (Act 2, 0x01900002)
//	compose_fu (Act 3, 0x01900003)
//	jackpot (Act 4, 0x01900004)
//
// 只读探测: 仅调用 get_info。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_fivefu_live.go
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
	pkt := protocol.NewPacket(0x01900000, []byte{0, 0, 0, 0})
	pkt.HasPrevAct = true
	c, cc := context.WithTimeout(ctx, 8*time.Second)
	resp, err := sess.Call(c, pkt, 0x01900000)
	cc()
	if err != nil || resp == nil {
		slog.Warn("五福临门 get_info 无回包", "err", err)
		return
	}
	slog.Info("五福临门信息", "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload))
}
