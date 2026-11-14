//go:build probe

// 目标: 只读探测魔界远征 (Module 429, Mod_DevilExpedition_Base) 面板字段，
// 用于确认 open_match_panel(429/2) 中三类伙伴材料（魔灵/阴阳石/隐仙石）的
// 本周计数与上限字段位置。
//
// 仅发送 activity_status(429/1) 与 open_match_panel(429/2)，不开始任何远征、
// 不消耗屠魔令、不消耗元宝。
//
// 运行命令: SXD_CONFIG=configs/qq.yaml TMPDIR=/home/tongchen/gotmp go run -tags=probe test/probes/probe_devil_expedition_panel_live.go
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
	actDevilActivityStatus uint32 = 0x01AD0001
	actDevilMatchPanel     uint32 = 0x01AD0002
)

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
	if len(cfg.Accounts) == 0 || len(cfg.Accounts[0].Roles) == 0 {
		slog.Error("配置缺少账号或角色")
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

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
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
	slog.Info("会话已激活", "role_id", sess.RoleID(), "player_id", sess.PlayerID())

	call := func(pkt *protocol.Packet, act uint32, timeout time.Duration) *protocol.Packet {
		cCtx, cCancel := context.WithTimeout(ctx, timeout)
		defer cCancel()
		resp, cErr := sess.Call(cCtx, pkt, act)
		if cErr != nil {
			slog.Warn("无回包", "act", fmt.Sprintf("0x%08X", act), "err", cErr)
			return nil
		}
		return resp
	}
	withPrev := func(act uint32, prefix ...byte) *protocol.Packet {
		payload := append(append([]byte{}, prefix...), 0, 0, 0, 0)
		pkt := protocol.NewPacket(act, payload)
		pkt.HasPrevAct = true
		return pkt
	}

	if resp := call(withPrev(actDevilActivityStatus), actDevilActivityStatus, 8*time.Second); resp != nil {
		slog.Info("魔界远征活动状态", "hex", hex.EncodeToString(resp.Payload))
	}
	resp := call(withPrev(actDevilMatchPanel), actDevilMatchPanel, 10*time.Second)
	if resp == nil {
		slog.Error("匹配面板无回包")
		return
	}
	slog.Info("魔界远征匹配面板", "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload))

	p := protocol.DecompressPayloadIfNeeded(resp.Payload)
	r := protocol.NewReader(p)
	readInt := func(label string) int32 {
		v, e := r.ReadInt32()
		if e != nil {
			slog.Warn("字段读取失败", "field", label, "err", e)
		}
		return v
	}
	readLong := func(label string) int64 {
		v, e := r.ReadInt64()
		if e != nil {
			slog.Warn("字段读取失败", "field", label, "err", e)
		}
		return v
	}
	result, e := r.ReadUint8()
	if e != nil {
		slog.Error("解析面板 result 失败", "err", e)
		return
	}
	reconnect, _ := r.ReadUint8()
	slog.Info("面板头部", "result", result, "reconnect_status", reconnect)
	slog.Info("面板标量1", "i", readInt("i1"), "l1", readLong("l1"), "l2", readLong("l2"))
	slog.Info("面板标量2", "i", readInt("i2"), "l3", readLong("l3"), "l4", readLong("l4"))
	for i := 0; i < 5; i++ {
		slog.Info("面板标量3", "idx", i, "v", readInt("i3"), "off", len(p)-r.Remaining())
	}
	if n, e := r.ReadUint16(); e == nil {
		slog.Info("四元组列表", "count", n, "off", len(p)-r.Remaining())
		for i := 0; i < int(n); i++ {
			a := [4]int32{}
			for j := 0; j < 4; j++ {
				a[j] = readInt("quad")
			}
			slog.Info("四元组", "idx", i, "vals", a, "off", len(p)-r.Remaining())
		}
	}
	// 记录剩余 hex，便于人工确认后续字段。
	if rem, rErr := r.ReadBytes(r.Remaining()); rErr == nil {
		slog.Info("面板剩余", "len", len(rem), "hex", hex.EncodeToString(rem))
	}
}
