//go:build probe

// 目标: 定位轮回净地渡业台抽取 852/16 在 ncs 静默丢包的 PrevAct 形态。
//
// 登录钩子与常驻例程都在面板 (852/12) 之后立即抽取，但 ncs s0 全部 8 秒超时。
// 本探针在面板后依次尝试不同 PrevAct / 裸载荷，成功即停止。
//
// 运行: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_altar_draw_live.go -role 0
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
	roleIdx := flag.Int("role", 0, "配置中的角色下标 (0=s0, 1=s1)")
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

	call := func(pkt *protocol.Packet, act uint32, name string) *protocol.Packet {
		cCtx, cCancel := context.WithTimeout(ctx, 6*time.Second)
		defer cCancel()
		resp, cErr := sess.Call(cCtx, pkt, act)
		if cErr != nil {
			slog.Warn("无回包", "call", name, "action", fmt.Sprintf("0x%08X", act), "err", cErr)
			return nil
		}
		limit := len(resp.Payload)
		if limit > 40 {
			limit = 40
		}
		slog.Info("有回包", "call", name, "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload[:limit]))
		return resp
	}

	// 面板
	panelPkt := protocol.BuildEraRebirthLandPoolPanelPrevActPacket(sess.LastActionID())
	if call(panelPkt, protocol.ActionIDEraRebirthLandPoolPanel, "panel") == nil {
		return
	}
	time.Sleep(500 * time.Millisecond)

	prev := sess.LastAckActionID()
	build := func(p []byte) *protocol.Packet {
		return protocol.NewPacket(protocol.ActionIDEraRebirthLandDraw, p)
	}
	type variant struct {
		name    string
		payload []byte
	}
	mk := func(prevAct uint32, withPrev bool) []byte {
		out := []byte{0, 0, 0, 0}
		if withPrev {
			out = append(out, byte(prevAct>>24), byte(prevAct>>16), byte(prevAct>>8), byte(prevAct))
		}
		return out
	}
	variants := []variant{
		{"draw_prev_panel", mk(protocol.ActionIDEraRebirthLandPoolPanel, true)},
		{"draw_prev_init", mk(0x00000002, true)},
		{"draw_bare", mk(0, false)},
		{"draw_prev_ack", mk(prev, true)},
	}
	for _, v := range variants {
		sess.SetLastActionID(0)
		if call(build(v.payload), protocol.ActionIDEraRebirthLandDraw, v.name) != nil {
			break
		}
		time.Sleep(time.Second)
	}
	slog.Info("探针结束")
}
