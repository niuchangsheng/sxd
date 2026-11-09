//go:build probe

// 目标协议: 精英副本城镇切换 + 英雄副本列表 (Module 1/0 enter_town, Module 35/0 list)
// 基准对齐: Mod_Town_Base.enter_town / Mod_HeroMission_Base.get_hero_mission_list
// 运行命令: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_elite_sweep_live.go
// 预期表现: 进入精英城镇 4 后，携带 enter_town(0x00010000) 作为 PrevAct 查询英雄副本列表。
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
		cfgPath = "configs/niuchangsheng.yaml"
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
	slog.Info("会话已激活", "role_id", sess.RoleID(), "player_id", sess.PlayerID())

	// 1. 跳过进城：直接走 Module 25 扫荡。
	// 2. 对前几个精英副本逐个执行 status + start，打印结果。
	for _, dungeonID := range []int32{169, 166, 167, 168, 170} {
		sw := protocol.NewWriter()
		sw.WriteInt32(dungeonID)
		sw.WriteUint32(sess.LastActionID())
		sCtx, sCancel := context.WithTimeout(ctx, 8*time.Second)
		stResp, stErr := sess.Call(sCtx, protocol.NewPacket(protocol.ActionIDPracticeGetStatus, sw.Bytes()), protocol.ActionIDPracticeGetStatus)
		sCancel()
		if stErr != nil || stResp == nil {
			slog.Warn("练习状态无回包", "dungeon", dungeonID, "err", stErr)
			continue
		}
		st, perr := protocol.ParsePracticeStatusResult(stResp.Payload)
		if perr != nil || st == nil || len(st.Missions) == 0 {
			slog.Warn("练习状态解析失败", "dungeon", dungeonID, "len", len(stResp.Payload), "err", perr)
			continue
		}
		best := st.Missions[0]
		for _, m := range st.Missions[1:] {
			if m.Level > best.Level || (m.Level == best.Level && m.MissionID > best.MissionID) {
				best = m
			}
		}
		slog.Info("练习状态", "dungeon", dungeonID, "missions", len(st.Missions), "best", best.MissionID, "level", best.Level, "type", best.Type)
		pw := protocol.NewWriter()
		pw.WriteInt32(best.MissionID)
		pw.WriteInt16(1)
		pw.WriteInt8(1)
		pw.WriteUint32(protocol.ActionIDPracticeGetStatus)
		pCtx, pCancel := context.WithTimeout(ctx, 10*time.Second)
		startResp, pErr := sess.Call(pCtx, protocol.NewPacket(protocol.ActionIDPracticeStart, pw.Bytes()), protocol.ActionIDPracticeStart)
		pCancel()
		if pErr != nil || startResp == nil {
			slog.Warn("扫荡启动无回包", "dungeon", dungeonID, "mission", best.MissionID, "err", pErr)
			continue
		}
		slog.Info("扫荡启动回包", "dungeon", dungeonID, "mission", best.MissionID, "len", len(startResp.Payload), "hex", hex.EncodeToString(startResp.Payload))
	}

	// 3. 用显式 PrevAct=0x00010000 查询英雄副本列表。
	for _, town := range []uint16{4} {
		lw := protocol.NewWriter()
		lw.WriteUint16(town)
		lw.WriteUint32(protocol.ActionIDTownEnter)
		pkt := protocol.NewPacket(protocol.ActionHeroMissionList, lw.Bytes())
		lCtx, lCancel := context.WithTimeout(ctx, 10*time.Second)
		resp, lErr := sess.Call(lCtx, pkt, protocol.ActionHeroMissionList)
		lCancel()
		if lErr != nil || resp == nil {
			slog.Warn("英雄副本列表无回包", "town", town, "err", lErr)
			continue
		}
		limit := len(resp.Payload)
		if limit > 64 {
			limit = 64
		}
		slog.Info("英雄副本列表", "town", town, "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload[:limit]))
		if list, pErr := protocol.ParseHeroMissionListResponse(resp.Payload); pErr == nil && list != nil {
			slog.Info("解析", "town", town, "missions", len(list.Missions))
		} else {
			slog.Warn("解析英雄副本列表失败", "err", pErr)
		}
	}
}
