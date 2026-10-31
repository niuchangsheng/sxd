//go:build probe

package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	cfg, err := config.Load("configs/config.yaml")
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ticket, err := ticketCache.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		slog.Error("换票失败", "err", err)
		return
	}

	slog.Info("换票成功",
		"gateway", ticket.GatewayURL,
		"player_name", ticket.RoleName,
		"code", ticket.MainServer.Code,
		"time", ticket.MainServer.Time,
		"hash", ticket.MainServer.Hash,
	)

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

	for i := 0; i < 30; i++ {
		if sess.State() == client.StateActive {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("未能在时限内激活", "state", sess.State())
		return
	}

	slog.Info("会话已激活，开始西天取经与仙界探针测试")

	callPkt := func(pkt *protocol.Packet, expectedAct uint32, name string) {
		tCtx, tCancel := context.WithTimeout(ctx, 3*time.Second)
		defer tCancel()
		resp, cErr := sess.Call(tCtx, pkt, expectedAct)
		if cErr != nil {
			slog.Warn("Call 失败: "+name, "err", cErr)
			return
		}
		slog.Info("Call 成功: "+name,
			"action_id", fmt.Sprintf("0x%08X", resp.ActionID),
			"len", len(resp.Payload),
			"hex", hex.EncodeToString(resp.Payload),
		)
	}

	// 1. 测试 Module 456 (0x01C80002)
	slog.Info("--- 1. 测试 Module 456 取经详情 ---")
	stInfoPkt := protocol.BuildStTakeBibleGetInfoPacket(sess.LastActionID())
	callPkt(stInfoPkt, protocol.ActionIDStTakeBibleGetInfo, "StTakeBibleGetInfo(Mod 456, Act 2)")

	// 2. 测试 Module 456 启程 (0x01C80006)
	stStartPkt := protocol.BuildStTakeBibleStartPacket(sess.LastActionID())
	callPkt(stStartPkt, protocol.ActionIDStTakeBibleStart, "StTakeBibleStart(Mod 456, Act 6)")

	// 3. 测试 Module 30 (0x001E0000 / 0x001E0002)
	slog.Info("--- 2. 测试 Module 30 传统取经 ---")
	openPkt := protocol.BuildTakeBibleOpenPacket(sess.LastActionID())
	callPkt(openPkt, protocol.ActionIDTakeBibleOpen, "TakeBibleOpen(Mod 30, Act 0)")

	infoPkt := protocol.BuildTakeBibleGetInfoPacket(sess.LastActionID())
	callPkt(infoPkt, protocol.ActionIDTakeBibleGetInfo, "TakeBibleGetInfo(Mod 30, Act 2)")

	// 3. 测试 Module 115 (仙界取经 Mod_StTakeBible_Base)
	slog.Info("--- 测试 Module 115 (0x0073) 仙界西天取经 ---")
	callPkt(protocol.NewPacket(0x00730000, nil), 0x00730000, "Mod 115 Act 0 (open_take_bible)")
	callPkt(protocol.NewPacket(0x00730002, nil), 0x00730002, "Mod 115 Act 2 (get_take_bible_info)")
	callPkt(protocol.NewPacket(0x00730010, nil), 0x00730010, "Mod 115 Act 16 (get_rob_cd)")


	// 测试 Module 96 Action 3 (get_st_server_host)
	slog.Info("--- 探测仙界跨服网关地址 (Module 96, Action 3) ---")
	callPkt(protocol.NewPacket(0x00600003, nil), 0x00600003, "Mod 96 Act 3 (get_st_server_host)")
	callPkt(protocol.NewPacket(0x00600000, nil), 0x00600000, "Mod 96 Act 0 (get_login_info)")
	callPkt(protocol.NewPacket(0x00600001, nil), 0x00600001, "Mod 96 Act 1 (get_status)")


	slog.Info("探针执行结束")
}
