//go:build probe

// 目标: 在干净会话上复现灵脉采集 (104/1)，区分「长会话上下文失效」与
// 「功能未开放/已终结」。
//
// 2026-11-28 线上：104/0 面板正常（nimbus=94, remain=15, gain=5），随后
// 104/1 请求字节与官方抓包 (/tmp/lingmai.pcapng) 完全一致 (0000680000,
// 锚点 0x00680000)，但服务端无任何回包。
//
// 运行命令:
//
//	SXD_CONFIG=configs/niuchangsheng.yaml SXD_PROBE_ROLE_INDEX=1 \
//	  go run -tags=probe test/probes/probe_nimbus_live.go
//
// 注意: 会消耗一个木葫芦并顶掉该角色当前会话；运行前请先停服。
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strconv"
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
	idx := 0
	if v := os.Getenv("SXD_PROBE_ROLE_INDEX"); v != "" {
		if n, e := strconv.Atoi(v); e == nil && n >= 0 && n < len(acc.Roles) {
			idx = n
		}
	}
	role := acc.Roles[idx]

	tc := platform.NewTicketCache(90 * time.Minute)
	auth, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		return
	}
	auth.SetCookie(acc.Cookie)
	tc.RegisterAuthenticator(auth)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	tc.Invalidate(acc.Platform, acc.Username, role.ServerID)
	ticket, err := tc.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
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
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()
	deadline := time.Now().Add(30 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}
	slog.Info("会话已激活", "role_id", roleCfg.RoleID)

	call := func(pkt *protocol.Packet, act uint32, label string) *protocol.Packet {
		slog.Info("请求", "label", label, "payload_hex", hex.EncodeToString(pkt.Payload),
			"last_rx", fmt.Sprintf("0x%08X", sess.LastRxActionID()),
			"last_ack", fmt.Sprintf("0x%08X", sess.LastAckActionID()))
		c, ccancel := context.WithTimeout(ctx, 8*time.Second)
		defer ccancel()
		resp, err := sess.Call(c, pkt, act)
		if err != nil || resp == nil {
			slog.Warn("请求失败", "label", label, "err", err)
			return nil
		}
		slog.Info("响应", "label", label, "payload_hex", hex.EncodeToString(resp.Payload))
		return resp
	}

	// 1. 面板
	info := call(protocol.BuildNimbusInfoPacket(), protocol.ActionIDNimbusInfo, "104/0 info")
	if info != nil {
		if parsed, pErr := protocol.ParseNimbusInfo(info.Payload); pErr == nil {
			slog.Info("面板解析", "nimbus", parsed.Nimbus, "remain", parsed.Remain, "gain", parsed.Gain)
		}
	}

	// 2. 采集：尾链取最近收到的包（与官方一致）
	time.Sleep(200 * time.Millisecond)
	prev := sess.LastRxActionID()
	gather := protocol.NewPacketWithExplicitPrevAct(protocol.ActionIDNimbusGather,
		[]byte{protocol.NimbusGourdWood}, prev)
	call(gather, protocol.ActionIDNimbusGather, "104/1 gather(lastRx)")

	// 3. 对照：自动尾链（最后发送的业务动作）
	time.Sleep(500 * time.Millisecond)
	auto := protocol.NewPacketWithAutoPrevAct(protocol.ActionIDNimbusGather,
		[]byte{protocol.NimbusGourdWood})
	call(auto, protocol.ActionIDNimbusGather, "104/1 gather(auto)")
}