//go:build probe

// 目标: 诊断无量劫海 853/4 挑战被静默丢弃时服务端期望的 now_level。
//
// 运行命令: SXD_CONFIG=configs/qq.yaml TMPDIR=/home/tongchen/gotmp go run -tags=probe test/probes/probe_wuliang_diag_live.go
//
// 行为: 按官方抓包顺序发送 853/0 → 853/1 → 853/2(章节) → 854/39 → 854/1 →
// 853/3 → 853/4。853/4 依次尝试 mission_level+1、mission_level、
// 章节 level_min 三个候选关卡，并打印每个变体的入站动作。
//
// 安全性: 挑战免费、不消耗元宝；只读取面板与发起挑战。
package main

import (
	"context"
	"encoding/hex"
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

func wuliangChapter(level int32) int32 {
	switch {
	case level <= 10:
		return 1
	case level <= 30:
		return 2
	case level <= 60:
		return 3
	default:
		return 4
	}
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
		rx = append(rx, fmt.Sprintf("%d/%d(%dB)", p.ActionID>>16, p.ActionID&0xFFFF, len(p.Payload)))
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
	slog.Info("会话已激活", "role_id", sess.RoleID())

	call := func(label string, pkt *protocol.Packet, respAct uint32, timeout time.Duration) *protocol.Packet {
		mu.Lock()
		rx = rx[:0]
		mu.Unlock()
		c, ccancel := context.WithTimeout(ctx, timeout)
		resp, err := sess.Call(c, pkt, respAct)
		ccancel()
		mu.Lock()
		got := append([]string(nil), rx...)
		mu.Unlock()
		ok := err == nil && resp != nil
		payload := ""
		if resp != nil {
			payload = hex.EncodeToString(resp.Payload)
			if len(payload) > 160 {
				payload = payload[:160] + "..."
			}
		}
		slog.Info("无量诊断", "label", label, "ok", ok, "err", err,
			"resp_len", len(payload), "resp", payload, "rx", got)
		return resp
	}
	send := func(label string, pkt *protocol.Packet) {
		if err := sess.Send(pkt); err != nil {
			slog.Warn("发送失败", "label", label, "err", err)
		}
	}

	_ = call("853/0 season", protocol.BuildEraInfiniteSeasonPacket(),
		protocol.ActionIDEraInfiniteSeason, 5*time.Second)
	panelResp := call("853/1 panel", protocol.BuildEraInfinitePanelPacket(),
		protocol.ActionIDEraInfinitePanel, 5*time.Second)
	missionLevel := int32(0)
	if panelResp != nil {
		if panel, pErr := protocol.ParseEraInfinitePanel(panelResp.Payload); pErr == nil {
			missionLevel = panel.MissionLevel
			slog.Info("无量面板", "mission_level", missionLevel,
				"era_level", panel.EraLevel, "fight_end_ts", panel.FightEndTS)
		}
	}
	if missionLevel <= 0 {
		missionLevel = 15
	}
	target := missionLevel + 1
	chapter := wuliangChapter(target)

	_ = call(fmt.Sprintf("853/2 chapter=%d", chapter),
		protocol.BuildEraInfiniteChapterPacket(chapter),
		protocol.ActionIDEraInfiniteChapter, 8*time.Second)

	deployID := int32(0)
	if resp := call("854/39 deploy seq", protocol.BuildEraInfiniteDeployTypeSeqPacket(),
		protocol.ActionIDEraInfiniteDeployTypeSeq, 5*time.Second); resp != nil {
		if ids, dErr := protocol.ParseEraInfiniteDeployTypeSeq(resp.Payload); dErr == nil && len(ids) > 0 {
			deployID = ids[0]
		}
	}
	slog.Info("布阵", "deploy_id", deployID)
	if deployID != 0 {
		send("854/1", protocol.BuildEraInfiniteSuperDeployListPacket(deployID))
		time.Sleep(300 * time.Millisecond)
	}
	send(fmt.Sprintf("853/3 level=%d", target), protocol.BuildEraInfiniteMonsterTeamPacket(target, 1))
	time.Sleep(300 * time.Millisecond)

	for _, level := range []int32{target, missionLevel, 11} {
		_ = call(fmt.Sprintf("853/4 level=%d", level),
			protocol.BuildEraInfiniteFightPacket(level),
			protocol.ActionIDEraInfiniteFight, 15*time.Second)
	}
}