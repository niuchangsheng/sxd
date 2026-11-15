//go:build probe

// 目标: 只读探测无量劫海各境 chapter_id (Module 853)，确认极境编号
//
// 运行命令: SXD_CONFIG=configs/qq.yaml TMPDIR=/home/tongchen/gotmp go run -tags=probe test/probes/probe_wuliang_chapters_live.go
//
// 行为: 853/0/1 → 853/2 章节面板(now_level) → 853/3 怪物队伍 → 853/4 挑战，
// 逐关推进，失败/冷却即停止；不消耗元宝。
package main

import (
	"context"
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
		MaxReconnectAttempts: 3,
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
	slog.Info("会话已激活", "role_id", sess.RoleID(), "ingots", sess.GetPlayerState().Ingots)

	for chapter := int32(1); chapter <= 6; chapter++ {
		cCtx, cCancel := context.WithTimeout(ctx, 10*time.Second)
		resp, cErr := sess.Call(cCtx, protocol.BuildEraInfiniteChapterPacket(chapter), protocol.ActionIDEraInfiniteChapter)
		cCancel()
		if cErr != nil || resp == nil {
			slog.Info("章节探测无回包", "chapter", chapter, "err", cErr)
			continue
		}
		ch, pErr := protocol.ParseEraInfiniteChapter(resp.Payload)
		if pErr != nil {
			slog.Info("章节探测解析失败", "chapter", chapter, "err", pErr, "hex", resp.Payload[:min(len(resp.Payload), 24)])
			continue
		}
		slog.Info("章节探测", "chapter", chapter, "result", ch.Result, "now_level", ch.NowLevel)
	}
}
