//go:build probe

// 目标: 结缘宝箱 232/34 是否接受 box_index 而非 item_id。
//
// 232/30 列表按顺序返回 3937..3942；官方 open_link_fate_box 的 param1 可能
// 是列表索引。本探针对 232/34 依次发送 [Int 1]、[Int 1, Byte 0]、
// [Int 1, Int 0]，任意有回包即停止。
//
// 运行: SXD_CONFIG=configs/s1.yaml go run -tags=probe test/probes/probe_jieyuan_index_live.go
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

const actionOpen uint32 = 0x00E80022

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/s1.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[0]
	tc := platform.NewTicketCache(90 * time.Minute)
	xd, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		return
	}
	xd.SetCookie(acc.Cookie)
	tc.RegisterAuthenticator(xd)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	ticket, err := tc.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		slog.Error("换票失败", "err", err)
		return
	}
	rc := client.SessionConfig{
		RoleID:               fmt.Sprintf("%s-%s", role.ServerID, role.RoleName),
		RoleName:             role.RoleName,
		Username:             acc.Username,
		ServerAddr:           ticket.GatewayURL,
		ServerID:             role.ServerID,
		Platform:             acc.Platform,
		Authenticator:        client.DefaultAuthenticator,
		MaxReconnectAttempts: 3,
	}
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

	call := func(payload []byte, name string) bool {
		c, cc := context.WithTimeout(ctx, 8*time.Second)
		defer cc()
		resp, cErr := sess.Call(c, protocol.NewPacket(actionOpen, payload), actionOpen)
		if cErr != nil {
			slog.Warn("无回包", "form", name, "err", cErr)
			return false
		}
		limit := len(resp.Payload)
		if limit > 40 {
			limit = 40
		}
		slog.Info("有回包", "form", name, "len", len(resp.Payload),
			"hex", hex.EncodeToString(resp.Payload[:limit]))
		return true
	}

	if call(protocol.NewWriter().WriteInt32(1).Bytes(), "A_index1") {
		return
	}
	if call(protocol.NewWriter().WriteInt32(1).WriteUint8(0).Bytes(), "B_index1_byte") {
		return
	}
	if call(protocol.NewWriter().WriteInt32(1).WriteInt32(0).Bytes(), "C_index1_int") {
		return
	}
	slog.Info("232/34 index 三种形态均无回包")
}