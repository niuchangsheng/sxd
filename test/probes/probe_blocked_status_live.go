//go:build probe

// 目标协议: 时段类玩法阻塞状态一站式巡检
//
// 一次登录顺序查询以下只读状态，输出紧凑状态表，供 18:00/19:37 定时复查使用：
//
//	全网幻魔塔 521/0 get_open_status (Int)
//	奇缘宝藏   808/0 is_open (Int)
//	阶位赛     349/0 get_status
//	宠物仙域   714/1 main_panel
//	锁妖塔     705/8 base_info (赛季开放状态)
//	节日在线   218/31 get_online_award_info
//
// 运行: SXD_CONFIG=configs/qq.yaml go run -tags=probe test/probes/probe_blocked_status_live.go
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
		cfgPath = "configs/qq.yaml"
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

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
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
	slog.Info("会话已激活", "role_id", sess.RoleID())

	call := func(label string, pkt *protocol.Packet, act uint32) []byte {
		cCtx, cCancel := context.WithTimeout(ctx, 8*time.Second)
		defer cCancel()
		resp, cErr := sess.Call(cCtx, pkt, act)
		if cErr != nil {
			slog.Warn("无回包", "check", label, "action", fmt.Sprintf("0x%08X", act), "err", cErr)
			return nil
		}
		limit := len(resp.Payload)
		if limit > 24 {
			limit = 24
		}
		slog.Info("状态", "check", label, "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload[:limit]))
		return resp.Payload
	}
	intAt := func(b []byte, off int) int32 {
		if off+4 > len(b) {
			return -1
		}
		return int32(uint32(b[off])<<24 | uint32(b[off+1])<<16 | uint32(b[off+2])<<8 | uint32(b[off+3]))
	}

	if b := call("tower_open", protocol.NewPacket(0x02090000, nil), 0x02090000); b != nil {
		slog.Info("解析", "tower_open", intAt(b, 0))
	}
	if b := call("fortune_open", protocol.NewPacket(0x03280000, nil), 0x03280000); b != nil {
		slog.Info("解析", "fortune_open", intAt(b, 0))
	}
	call("ladder_status", protocol.NewPacket(0x015D0000, nil), 0x015D0000)
	call("fairyland_panel", protocol.NewPacket(0x02CA0001, nil), 0x02CA0001)
	call("demon_tower_base", protocol.NewPacket(0x02C10008, nil), 0x02C10008)
	call("festival_online_info", protocol.NewPacket(0x00DA001F, nil), 0x00DA001F)
}
