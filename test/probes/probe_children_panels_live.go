//go:build probe

// 目标: 用孩子角色 ID 探测孩子学习/修行/任务/探索面板（只读）
//
//	学习信息  Mod 312 Act 28 0x0138001C  request=[Int role_id]
//	修行信息  Mod 312 Act 29 0x0138001D  request=[Int role_id]
//	学习/上课 Mod 312 Act 9  0x01380009  request=[Int role_id]
//	任务信息  Mod 315 Act 0  0x013B0000  request=[Int role_id]
//	探索信息  Mod 313 Act 0  0x01390000  request=[Int role_id]
//
// 孩子 role_id 默认 1812（来自主面板记录首字段），可用 SXD_PROBE_CHILD_ID 覆盖。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_children_panels_live.go
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	childID := int32(1812)
	if raw := os.Getenv("SXD_PROBE_CHILD_ID"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			childID = int32(v)
		}
	}

	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/s0.yaml"
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
		slog.Error("启动主服会话失败", "err", err)
		return
	}
	defer sess.Close()
	deadline := time.Now().Add(40 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("主服会话未激活", "state", sess.State())
		return
	}
	slog.Info("使用孩子角色 ID", "child_id", childID)

	type req struct {
		name   string
		action uint32
		id     int32
	}
	reqs := []req{
		{"学习信息", 0x0138001C, childID},
		{"修行信息", 0x0138001D, childID},
		{"学习面板", 0x01380009, childID},
		{"任务信息", 0x013B0000, childID},
		{"探索信息", 0x01390000, childID},
	}
	for _, rq := range reqs {
		if ctx.Err() != nil {
			break
		}
		w := protocol.NewWriter()
		w.WriteInt32(rq.id)
		w.WriteUint32(0)
		pkt := protocol.NewPacket(rq.action, w.Bytes())
		pkt.HasPrevAct = true
		cCtx, cCancel := context.WithTimeout(ctx, 6*time.Second)
		resp, cErr := sess.Call(cCtx, pkt, rq.action)
		cCancel()
		if cErr != nil || resp == nil {
			slog.Info("孩子面板无回包", "name", rq.name, "action", fmt.Sprintf("0x%08X", rq.action), "err", cErr)
			continue
		}
		head := resp.Payload
		if len(head) > 80 {
			head = head[:80]
		}
		slog.Info("孩子面板回包", "name", rq.name, "action", fmt.Sprintf("0x%08X", rq.action),
			"len", len(resp.Payload), "head", hex.EncodeToString(head))
	}
}
