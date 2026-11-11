//go:build probe

// 目标: 验证药园旧版收获 harvest (13/6) 能否收获新版面板中卡住的地块。
//
// 背景: qq 药园 land_id=161476 状态为成熟(state=3)，但 harvest_new (13/25)
// 固定返回不可采摘；该地块 seed_id=2，疑似旧版摇钱树/仙灵树，需要旧协议。
//
// 本探针只读取土地列表并尝试收获该成熟地块，不购买种子、不消耗元宝。
//
// 运行: SXD_CONFIG=configs/qq.yaml go run -tags=probe test/probes/probe_farm_legacy_harvest_live.go
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
			slog.Warn("无回包", "call", name, "err", cErr)
			return nil
		}
		limit := len(resp.Payload)
		if limit > 60 {
			limit = 60
		}
		slog.Info("有回包", "call", name, "len", len(resp.Payload),
			"hex", hex.EncodeToString(resp.Payload[:limit]))
		return resp
	}

	infoPkt, _ := protocol.BuildFarmGetInfoPacket(protocol.FarmGetInfoRequest{PrevAct: sess.LastActionID()})
	infoResp := call(infoPkt, protocol.ActionIDFarmGetInfo, "farm_info")
	if infoResp == nil {
		return
	}
	fields, pErr := protocol.ParseFarmGetInfoResult(infoResp.Payload)
	if pErr != nil {
		slog.Warn("解析土地列表失败", "err", pErr)
		return
	}
	for _, f := range fields.Fields {
		slog.Info("土地状态", "land_id", f.LandID, "state", f.State, "seed_or_role", f.SeedOrRoleID)
	}

	const legacyHarvest = 0x000D0006 // Mod_Farm harvest
	landID := int32(161476)
	prev := sess.LastActionID()

	build := func(kind byte, withPrev bool) []byte {
		out := []byte{
			byte(landID >> 24), byte(landID >> 16), byte(landID >> 8), byte(landID),
			kind,
		}
		if withPrev {
			out = append(out, byte(prev>>24), byte(prev>>16), byte(prev>>8), byte(prev))
		}
		return out
	}
	for _, tc := range []struct {
		name     string
		kind     byte
		withPrev bool
	}{
		{"legacy_bare_kind0", 0, false},
		{"legacy_prev_kind0", 0, true},
		{"legacy_bare_kind1", 1, false},
		{"legacy_prev_kind1", 1, true},
	} {
		resp := call(protocol.NewPacket(legacyHarvest, build(tc.kind, tc.withPrev)), legacyHarvest, tc.name)
		if resp != nil {
			slog.Info("旧版收获有回包", "case", tc.name, "first_byte", resp.Payload[0])
			break
		}
	}
	slog.Info("探针结束")
}