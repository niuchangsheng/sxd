//go:build probe

// 目标: 诊断飞升 (Module 357) 357/0、357/1 被服务端静默丢弃的原因。
//
// 运行命令: SXD_CONFIG=configs/qq.yaml TMPDIR=/home/tongchen/gotmp go run -tags=probe test/probes/probe_levitate_diag_live.go
//
// 行为: 登录后依次用四种 PrevAct 锚点发送 357/0(type=0/1)：
//   - 自动尾链（最后发送动作，含心跳）；
//   - 显式使用服务端最后回包（LastAckActionID）；
//   - 显式使用心跳 0x4B；
//   - 显式 0。
//
// 打印每个变体是否收到 357/0 回包，用于判断静默丢弃是否由 PrevAct 锚点
// 或心跳导致。不消耗元宝。
package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func rawLevitateInfoPacket(typ uint8, prevAct uint32) *protocol.Packet {
	payload := make([]byte, 5)
	payload[0] = typ
	binary.BigEndian.PutUint32(payload[1:], prevAct)
	// 不使用 PrevActFromLastSent / HasPrevAct：保持显式锚点不被会话层覆盖。
	return protocol.NewPacket(protocol.ActionIDLevitateInfo, payload)
}

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/qq.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "path", cfgPath, "err", err)
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

	ticketCache.Invalidate(acc.Platform, acc.Username, role.ServerID)
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
		MaxReconnectAttempts: 1,
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)

	var mu sync.Mutex
	var rx []string
	sess.SetPacketObserver(func(p *protocol.Packet) {
		mu.Lock()
		rx = append(rx, fmt.Sprintf("%d/%d len=%d", p.ActionID>>16, p.ActionID&0xFFFF, len(p.Payload)))
		mu.Unlock()
	})

	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()
	deadline := time.Now().Add(25 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}
	slog.Info("会话已激活", "role_id", sess.RoleID(), "last_action", fmt.Sprintf("0x%08X", sess.LastActionID()))

	cctx, ccancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer ccancel()

	try := func(name string, pkt *protocol.Packet, timeout time.Duration) {
		mu.Lock()
		rx = rx[:0]
		mu.Unlock()
		before := sess.LastActionID()
		c1, cancel1 := context.WithTimeout(cctx, timeout)
		resp, err := sess.Call(c1, pkt, protocol.ActionIDLevitateInfo)
		cancel1()
		mu.Lock()
		got := append([]string(nil), rx...)
		mu.Unlock()
		respLen := 0
		if resp != nil {
			respLen = len(resp.Payload)
		}
		slog.Info("357/0 变体", "name", name,
			"payload", fmt.Sprintf("%X", pkt.Payload),
			"last_sent_before", fmt.Sprintf("0x%08X", before),
			"last_rx", fmt.Sprintf("0x%08X", sess.LastRxActionID()),
			"last_ack", fmt.Sprintf("0x%08X", sess.LastAckActionID()),
			"ok", err == nil, "err", err, "resp_len", respLen,
			"rx_count", len(got), "rx", got)
	}

	// A. 自动尾链（会话层按最后发送动作填充；期间若有心跳会变成 0x4B）。
	try("A auto type=0", protocol.BuildLevitateInfoPacket(protocol.LevitateTypeMainRole), 1200*time.Millisecond)

	// B. 显式 LastAck（最后一条业务回包）。
	anchor := sess.LastAckActionID()
	if anchor == 0 {
		anchor = sess.LastActionID()
	}
	try(fmt.Sprintf("B last_ack(0x%08X) type=1", anchor), rawLevitateInfoPacket(protocol.LevitateTypeRole, anchor), 1200*time.Millisecond)

	// C. 强制心跳锚点 0x4B。
	try("C heartbeat(0x4B) type=0", rawLevitateInfoPacket(protocol.LevitateTypeMainRole, protocol.ActionHeartbeat), 1200*time.Millisecond)

	// D. 显式 0。
	try("D zero type=1", rawLevitateInfoPacket(protocol.LevitateTypeRole, 0), 1200*time.Millisecond)

	// E. 再次自动尾链，确认链路仍可用。
	try("E auto type=0", protocol.BuildLevitateInfoPacket(protocol.LevitateTypeMainRole), 3000*time.Millisecond)
}