//go:build probe

// 目标: 测量同一会话中服务端开始「静默丢弃」业务请求的时间/请求量阈值。
//
// 运行命令: SXD_CONFIG=configs/qq.yaml TMPDIR=/home/tongchen/gotmp go run -tags=probe test/probes/probe_session_decay_live.go
//
// 行为: 登录后每 20 秒发送一次 357/0（已知可回包的低成本查询），持续 10 分钟，
// 记录每次是否成功与耗时。用于判断静默丢弃是「会话时长」还是「请求量」触发。
// 只读查询，不进入战斗、不消耗元宝。
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

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
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
		MaxReconnectAttempts: 5,
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()
	deadline := time.Now().Add(120 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}
	start := time.Now()
	slog.Info("会话已激活，开始漂移测量", "role_id", sess.RoleID())

	okCount, failCount := 0, 0
	for i := 0; i < 30; i++ {
		if sess.State() != client.StateActive {
			slog.Warn("会话已断开，试验结束", "elapsed", time.Since(start).Round(time.Second),
				"ok", okCount, "fail", failCount, "state", sess.State())
			return
		}
		c, ccancel := context.WithTimeout(ctx, 5*time.Second)
		t0 := time.Now()
		resp, callErr := sess.Call(c, protocol.BuildLevitateInfoPacket(protocol.LevitateTypeMainRole),
			protocol.ActionIDLevitateInfo)
		ccancel()
		elapsed := time.Since(t0).Round(time.Millisecond)
		if callErr == nil && resp != nil {
			okCount++
			slog.Info("探针请求", "i", i, "elapsed", elapsed, "elapsed_total", time.Since(start).Round(time.Second),
				"last_business", fmt.Sprintf("0x%08X", sess.LastBusinessActionID()),
				"last_rx", fmt.Sprintf("0x%08X", sess.LastRxActionID()), "ok", true)
		} else {
			failCount++
			slog.Warn("探针请求失败", "i", i, "elapsed", elapsed, "elapsed_total", time.Since(start).Round(time.Second),
				"err", callErr, "state", sess.State(),
				"last_business", fmt.Sprintf("0x%08X", sess.LastBusinessActionID()),
				"last_rx", fmt.Sprintf("0x%08X", sess.LastRxActionID()))
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(15 * time.Second):
		}
	}
	slog.Info("漂移测量结束", "ok", okCount, "fail", failCount)
}
