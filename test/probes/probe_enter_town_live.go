//go:build probe

// 目标: 定位 enter_town (Module 1, Action 0) 的正确载荷形态。
//
// 任务推图前需要按官方 gotoTownAtId 复位城镇场景以清除魔王试炼残留的
// 战场上下文；线上当前 8 字节（Int + PrevAct）载荷无回包。
// 本探针依次尝试裸 4 字节与不同 PrevAct，随后再请求 get_sections(21) 对照。
//
// 运行: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_enter_town_live.go -role 1
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
	roleIdx := flag.Int("role", 1, "配置中的角色下标 (0=s0, 1=s1)")
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
	mapID := int32(sess.GetPlayerState().TownMapID)
	townID := int32(sess.GetPlayerState().TownID)
	slog.Info("城镇标识", "map_id", mapID, "town_id", townID)

	call := func(payload []byte, act uint32, name string) bool {
		cCtx, cCancel := context.WithTimeout(ctx, 6*time.Second)
		resp, cErr := sess.Call(cCtx, protocol.NewPacket(act, payload), act)
		cCancel()
		if cErr != nil || resp == nil {
			slog.Warn("无回包", "call", name, "err", cErr)
			return false
		}
		limit := len(resp.Payload)
		if limit > 40 {
			limit = 40
		}
		slog.Info("有回包", "call", name, "len", len(resp.Payload),
			"hex", hex.EncodeToString(resp.Payload[:limit]))
		return true
	}

	variants := []struct {
		name    string
		payload []byte
	}{
		{"bare_map", protocol.NewWriter().WriteInt32(mapID).Bytes()},
		{"bare_town", protocol.NewWriter().WriteInt32(townID).Bytes()},
		{"prev_map", protocol.NewWriter().WriteInt32(mapID).WriteUint32(sess.LastActionID()).Bytes()},
		{"prev_town_enter", protocol.NewWriter().WriteInt32(mapID).WriteUint32(protocol.ActionIDTownEnter).Bytes()},
	}
	for _, v := range variants {
		time.Sleep(600 * time.Millisecond)
		call(v.payload, protocol.ActionIDTownEnter, v.name)
	}

	// 复位后对照：get_sections(21) 是否恢复。
	time.Sleep(600 * time.Millisecond)
	call(protocol.BuildMissionGetSectionsPacket(21, sess.LastAckActionID()).Payload,
		protocol.ActionIDMissionGetSections, "get_sections_21")
	slog.Info("探针结束")
}
