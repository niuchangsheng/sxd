//go:build probe

// 目标: 确认灵域猎妖 challenge_panel/start_challenge 的正确关卡号。
//
// 线上 s0 主面板 fields[0]=25，按 1..N 挑战时 start_challenge(1) 返回
// CANT_CHALLENGE；本探针 dump 主面板与 challenge_panel(1/24/25/26) 的原始回包，
// 用于定位 cur_step/is_can_challenge 字段。
//
// 运行: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_soulhunt_step_live.go
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

const (
	actionMainPanel      uint32 = 0x029D0001
	actionChallengePanel uint32 = 0x029D0003
	actionStartChallenge uint32 = 0x029D0005
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

	call := func(pkt *protocol.Packet, act uint32, name string) *protocol.Packet {
		c, cc := context.WithTimeout(ctx, 8*time.Second)
		defer cc()
		resp, err := sess.Call(c, pkt, act)
		if err != nil {
			slog.Warn("无回包", "call", name, "err", err)
			return nil
		}
		slog.Info("回包", "call", name, "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload))
		return resp
	}

	call(protocol.NewPacket(actionMainPanel, nil), actionMainPanel, "669_main_panel")
	for _, step := range []int32{1, 24, 25, 26} {
		w := protocol.NewWriter().WriteInt32(step)
		call(protocol.NewPacket(actionChallengePanel, w.Bytes()), actionChallengePanel,
			fmt.Sprintf("669_challenge_panel_%d", step))
	}
	w := protocol.NewWriter().WriteInt32(25)
	call(protocol.NewPacket(actionStartChallenge, w.Bytes()), actionStartChallenge, "669_start_25")
}