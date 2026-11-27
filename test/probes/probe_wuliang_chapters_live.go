//go:build probe

// 目标: 扫描无量劫海 853/2 章节面板 1..8，确认当前账号可进入的章节 ID，
// 以及 853/1 面板 now_level 与章节映射是否一致。
//
// 运行命令:
//
//	SXD_CONFIG=configs/qq.yaml TMPDIR=/home/tongchen/gotmp \
//	  go run -tags=probe test/probes/probe_wuliang_chapters_live.go
//
// 行为: 登录 → 853/0 赛季 → 853/1 面板（打印 now_level）→ 依次查询
// 853/2 chapter_id=1..8（每个 5 秒），打印结果码/now_level。
// 只读查询，不布阵、不挑战、不消耗元宝。
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

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
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
	deadline := time.Now().Add(90 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}
	slog.Info("会话已激活", "role_id", sess.RoleID())

	call := func(pkt *protocol.Packet, act uint32, name string, timeout time.Duration) *protocol.Packet {
		c, ccancel := context.WithTimeout(ctx, timeout)
		resp, err := sess.Call(c, pkt, act)
		ccancel()
		if err != nil {
			slog.Info("无回包", "name", name, "action", fmt.Sprintf("0x%08X", act), "err", err)
			return nil
		}
		head := resp.Payload
		if len(head) > 120 {
			head = head[:120]
		}
		slog.Info("回包", "name", name, "action", fmt.Sprintf("0x%08X", act),
			"len", len(resp.Payload), "hex", hex.EncodeToString(head))
		return resp
	}

	call(protocol.BuildEraInfiniteSeasonPacket(), protocol.ActionIDEraInfiniteSeason, "853/0 赛季", 8*time.Second)
	panel := call(protocol.BuildEraInfinitePanelPacket(), protocol.ActionIDEraInfinitePanel, "853/1 面板", 8*time.Second)
	if panel != nil {
		if p, e := protocol.ParseEraInfinitePanel(panel.Payload); e == nil {
			slog.Info("853/1 解析", "era_level", p.EraLevel,
				"mission_level", p.MissionLevel, "rank", p.Rank, "era_zone", p.EraZone)
		} else {
			slog.Info("853/1 解析失败", "err", e)
		}
	}

	for chapter := int32(1); chapter <= 8; chapter++ {
		resp := call(protocol.BuildEraInfiniteChapterPacket(chapter),
			protocol.ActionIDEraInfiniteChapter, fmt.Sprintf("853/2 chapter=%d", chapter), 5*time.Second)
		if resp == nil {
			continue
		}
		if ch, e := protocol.ParseEraInfiniteChapter(resp.Payload); e == nil {
			slog.Info("章节解析", "chapter_id", chapter, "result", ch.Result,
				"result_text", protocol.EraInfiniteResultText(ch.Result), "now_level", ch.NowLevel)
		} else {
			slog.Info("章节解析失败", "chapter_id", chapter, "err", e)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
