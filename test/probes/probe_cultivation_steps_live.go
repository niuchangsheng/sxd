//go:build probe

// 目标: 确认神仙卡牌 (Module 859) 任务分组/step 的合法取值。
//
// 运行命令: SXD_CONFIG=configs/qq.yaml TMPDIR=/home/tongchen/gotmp go run -tags=probe test/probes/probe_cultivation_steps_live.go
//
// 行为: 登录后读取 859/0 面板（打印完整 hex），再依次请求 859/5 step=1..5，
// 记录哪些 step 有回包。只读查询，不领取/不购买/不消耗元宝。
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
	deadline := time.Now().Add(90 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}
	slog.Info("会话已激活", "role_id", sess.RoleID())

	call := func(label string, pkt *protocol.Packet, respAct uint32, timeout time.Duration) *protocol.Packet {
		c, ccancel := context.WithTimeout(ctx, timeout)
		resp, err := sess.Call(c, pkt, respAct)
		ccancel()
		ok := err == nil && resp != nil
		head := ""
		if resp != nil {
			head = hex.EncodeToString(resp.Payload)
		}
		slog.Info("神仙卡牌诊断", "label", label, "ok", ok, "err", err,
			"resp_len", len(head)/2, "resp_hex", head)
		return resp
	}

	call("859/0 panel", protocol.BuildCultivationCardPanelPacket(),
		protocol.ActionIDCultivationCardPanel, 8*time.Second)
	for step := int32(1); step <= 5; step++ {
		call(fmt.Sprintf("859/5 step=%d", step),
			protocol.BuildCultivationCardTaskInfoPacket(step),
			protocol.ActionIDCultivationTaskInfo, 5*time.Second)
		time.Sleep(300 * time.Millisecond)
	}
}