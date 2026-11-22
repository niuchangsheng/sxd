//go:build probe

// 目标: 判定心跳 (0/75, 0x0000004B) 之后各业务模块要求的前序动作 (PrevAct) 锚点。
//
// 运行命令: SXD_CONFIG=configs/qq.yaml TMPDIR=/home/tongchen/gotmp go run -tags=probe test/probes/probe_prevact_anchor_live.go
//
// 背景: 2026-11-22 实测 357/0 在心跳之后携带 0x4B 尾链会被静默丢弃，
// 而携带最后一条业务动作可正常回包；历史 Mod 4 get_sections 的旧实验
// 使用的是 0x17（实为 get_buy_power_data，不是心跳）。本探针同时验证
// Mod 4 与 Module 357，确认「心跳不计入业务 PrevAct 链」是否对所有模块成立。
//
// 行为: 先取基线 → 空闲 2s 触发自动心跳 → 分别用 0x4B 与最后业务动作作尾链
// 重发。只读查询，不进入副本、不消耗元宝。
package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"os"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

const anchorProbeMapID int32 = 13

func rawPacket(actionID uint32, args []byte, prevAct uint32) *protocol.Packet {
	payload := append([]byte(nil), args...)
	var tail [4]byte
	binary.BigEndian.PutUint32(tail[:], prevAct)
	payload = append(payload, tail[:]...)
	return protocol.NewPacket(actionID, payload)
}

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

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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
		HeartbeatInterval:    1500 * time.Millisecond,
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
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

	call := func(label string, pkt *protocol.Packet, respActionID uint32) bool {
		c, ccancel := context.WithTimeout(ctx, 3*time.Second)
		defer ccancel()
		resp, err := sess.Call(c, pkt, respActionID)
		ok := err == nil && resp != nil
		slog.Info("锚点实验", "label", label,
			"payload", fmt.Sprintf("%X", pkt.Payload),
			"last_sent", fmt.Sprintf("0x%08X", sess.LastActionID()),
			"last_rx", fmt.Sprintf("0x%08X", sess.LastRxActionID()),
			"ok", ok, "err", err)
		return ok
	}

	// 基线：用最后业务动作作尾链。
	business := sess.LastActionID()
	if !call("357/0 baseline", rawPacket(protocol.ActionIDLevitateInfo, []byte{0}, business),
		protocol.ActionIDLevitateInfo) {
		slog.Warn("基线 357/0 失败，实验可能受外部登录干扰")
	}

	// 空闲 2s，确保自动心跳已发出。
	time.Sleep(2 * time.Second)
	slog.Info("空闲后状态", "last_sent", fmt.Sprintf("0x%08X", sess.LastActionID()),
		"heartbeat", fmt.Sprintf("0x%08X", protocol.ActionHeartbeat))
	if got := sess.LastActionID(); got != protocol.ActionHeartbeat {
		slog.Warn("空闲后 last_sent 不是心跳，实验结果可能无效", "got", fmt.Sprintf("0x%08X", got))
	}
	hb := sess.LastActionID()

	call("357/0 tail=heartbeat", rawPacket(protocol.ActionIDLevitateInfo, []byte{0}, hb),
		protocol.ActionIDLevitateInfo)
	call("357/0 tail=business", rawPacket(protocol.ActionIDLevitateInfo, []byte{0}, business),
		protocol.ActionIDLevitateInfo)

	// Mod 4 get_sections：同样比较心跳与业务锚点。
	business = sess.LastActionID()
	call("Mod4 baseline", rawPacket(protocol.ActionIDMissionGetSections,
		int32Bytes(anchorProbeMapID), business), protocol.ActionIDMissionGetSections)

	time.Sleep(2 * time.Second)
	hb = sess.LastActionID()
	call("Mod4 tail=heartbeat", rawPacket(protocol.ActionIDMissionGetSections,
		int32Bytes(anchorProbeMapID), hb), protocol.ActionIDMissionGetSections)
	call("Mod4 tail=business", rawPacket(protocol.ActionIDMissionGetSections,
		int32Bytes(anchorProbeMapID), business), protocol.ActionIDMissionGetSections)
}

func int32Bytes(v int32) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(v))
	return b[:]
}