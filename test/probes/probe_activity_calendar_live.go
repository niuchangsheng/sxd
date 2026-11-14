//go:build probe

// 目标: 采集运营活动时间表 (Module 0, Action 67 operation_activity_time_list)。
//
// 客户端 LuckyDayCalendarData 解析为 [type, time_list[[start,end]], scale_value, call_flag]，
// 活动类型来自 LuckyDayCalendarForm (LINK_FATE_BOX=14, FIVE_BLESSINGS=13, FARM=23 等)。
// 本探针只读，不消耗任何资源。
//
// 运行: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_activity_calendar_live.go -role 1
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

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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

	for _, payload := range [][]byte{nil, {0, 0, 0, 0}} {
		cCtx, cCancel := context.WithTimeout(ctx, 6*time.Second)
		resp, cErr := sess.Call(cCtx, protocol.NewPacket(protocol.ActionIDActivityTimeList, payload),
			protocol.ActionIDActivityTimeList)
		cCancel()
		if cErr != nil || resp == nil {
			slog.Warn("无回包", "payload_len", len(payload), "err", cErr)
			continue
		}
		limit := len(resp.Payload)
		if limit > 300 {
			limit = 300
		}
		slog.Info("有回包", "payload_len", len(payload), "len", len(resp.Payload),
			"hex", hex.EncodeToString(resp.Payload[:limit]))
		if wErr := os.WriteFile("/tmp/activity_time.bin", resp.Payload, 0o644); wErr == nil {
			slog.Info("已写入 /tmp/activity_time.bin")
		}
		break
	}
}
