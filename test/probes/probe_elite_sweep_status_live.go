//go:build probe

// 目标: 对比可扫荡与不可扫荡精英副本的 25/0 状态字段，寻找“已扫荡/不可扫荡”的判据。
//
// 线上现象：全局精英扫荡对 166/167/169 可成功，对 168/170/176/310 等副本的
// 25/1 start_practice 静默无回包。若状态回包已带剩余次数/类型标志，
// 就能直接跳过而无需试探性发 25/1。
//
// 运行: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_elite_sweep_status_live.go -role 1
package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
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
	dungeons := flag.String("dungeons", "166,167,168,169,170,176,310", "待对比的副本 ID，逗号分隔")
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

	for _, part := range strings.Split(*dungeons, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var id int32
		if _, err := fmt.Sscanf(part, "%d", &id); err != nil {
			continue
		}
		time.Sleep(400 * time.Millisecond)
		cCtx, cCancel := context.WithTimeout(ctx, 6*time.Second)
		resp, cErr := sess.Call(cCtx, protocol.BuildPracticeGetStatusBarePacket(id),
			protocol.ActionIDPracticeGetStatus)
		cCancel()
		if cErr != nil || resp == nil {
			slog.Warn("状态无回包", "dungeon_id", id, "err", cErr)
			continue
		}
		limit := len(resp.Payload)
		if limit > 80 {
			limit = 80
		}
		status, pErr := protocol.ParsePracticeStatusResult(resp.Payload)
		if pErr != nil {
			slog.Warn("解析状态失败", "dungeon_id", id, "err", pErr,
				"hex", hex.EncodeToString(resp.Payload[:limit]))
			continue
		}
		slog.Info("副本状态", "dungeon_id", id,
			"tag", status.HeaderTag, "missions", status.Missions,
			"is_practicing", status.IsPracticing,
			"remaining", status.Remaining, "total_time", status.TotalTime,
			"len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload[:limit]))
	}
	// 对照：已解锁地图 (13/15/21) 与疑似未解锁地图 (22) 的 get_sections 结果。
	for _, mapID := range []int32{13, 15, 21, 22} {
		time.Sleep(400 * time.Millisecond)
		cCtx, cCancel := context.WithTimeout(ctx, 6*time.Second)
		resp, cErr := sess.Call(cCtx, protocol.BuildMissionGetSectionsPacket(mapID, sess.LastAckActionID()),
			protocol.ActionIDMissionGetSections)
		cCancel()
		if cErr != nil || resp == nil {
			slog.Warn("章节列表无回包", "map_id", mapID, "err", cErr)
			continue
		}
		secs, pErr := protocol.ParseMissionGetSectionsResponse(resp.Payload)
		if pErr != nil {
			slog.Warn("章节列表解析失败", "map_id", mapID, "err", pErr, "len", len(resp.Payload))
			continue
		}
		cleared := 0
		for _, sec := range secs {
			if sec.Status == 1 {
				cleared++
			}
		}
		slog.Info("章节列表", "map_id", mapID, "count", len(secs), "cleared", cleared)
	}
	slog.Info("探针结束")
}
