//go:build probe

// 目标: 批量只读探测「非元宝日常功能」的可用性与面板布局
//
//	孩子主面板      Mod 312 Act 0  0x01380000
//	孩子学习信息    Mod 312 Act 28 0x0138001C
//	孩子修行信息    Mod 312 Act 29 0x0138001D
//	孩子任务        Mod 315 Act 0  0x013B0000
//	孩子探索        Mod 313 Act 0  0x01390000
//	回忆之境主面板  Mod 526 Act 1  0x020E0001
//	奇门遁甲信息    Mod 251 Act 0  0x00FB0000
//	仙盟成员信息    Mod 178 Act 24 0x00B20018
//	仙盟神像信息    Mod 179 Act 10 0x00B3000A
//
// 所有请求均为只读，不改变任何状态；用于决定下一步实现哪个免费功能。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_free_features_recon_live.go
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

type probeCall struct {
	name    string
	action  uint32
	payload []byte
}

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
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

	zero4 := []byte{0, 0, 0, 0}
	calls := []probeCall{
		{"孩子主面板", 0x01380000, zero4},
		{"孩子学习信息", 0x0138001C, zero4},
		{"孩子修行信息", 0x0138001D, zero4},
		{"孩子任务", 0x013B0000, zero4},
		{"孩子探索", 0x01390000, zero4},
		{"回忆之境主面板", 0x020E0001, zero4},
		{"奇门遁甲信息", 0x00FB0000, zero4},
		{"仙盟成员信息", 0x00B20018, zero4},
		{"仙盟神像信息", 0x00B3000A, zero4},
	}

	for _, pc := range calls {
		if ctx.Err() != nil {
			break
		}
		pkt := protocol.NewPacket(pc.action, pc.payload)
		pkt.HasPrevAct = true
		cCtx, cCancel := context.WithTimeout(ctx, 6*time.Second)
		resp, cErr := sess.Call(cCtx, pkt, pc.action)
		cCancel()
		if cErr != nil || resp == nil {
			slog.Info("探测无回包", "name", pc.name, "action", fmt.Sprintf("0x%08X", pc.action), "err", cErr)
			continue
		}
		head := resp.Payload
		if len(head) > 64 {
			head = head[:64]
		}
		slog.Info("探测回包", "name", pc.name, "action", fmt.Sprintf("0x%08X", pc.action),
			"len", len(resp.Payload), "head", hex.EncodeToString(head))
	}
}
