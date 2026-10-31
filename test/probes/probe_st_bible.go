//go:build probe

package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
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

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
		MaxReconnectAttempts: 1,
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动主服会话失败", "err", err)
		return
	}
	defer sess.Close()

	for i := 0; i < 60; i++ {
		if sess.State() == client.StateActive {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("主服未能在时限内激活", "state", sess.State())
		return
	}

	slog.Info("=== 主服激活成功，开始获取仙界登录凭据 (Mod 96, Act 0) ===")

	tCtx, tCancel := context.WithTimeout(ctx, 3*time.Second)
	defer tCancel()
	loginInfoPkt := protocol.NewPacket(0x00600000, nil)
	loginInfoResp, err := sess.Call(tCtx, loginInfoPkt, 0x00600000)
	if err != nil {
		slog.Error("拉取仙界凭据失败", "err", err)
		return
	}

	// 解析 Mod 96 Act 0 回包:
	// Host (String), Port (Short), ServerID (String), Time1 (Int32), Hash1 (String), TownSign (String)
	r := protocol.NewReader(loginInfoResp.Payload)
	stHost, _ := r.ReadString()
	stPort, _ := r.ReadUint16()
	stServerID, _ := r.ReadString()
	stTime1, _ := r.ReadInt32()
	stHash1, _ := r.ReadString()
	stTownSign, _ := r.ReadString()

	slog.Info("=== 成功获取仙界跨服网关凭据 ===",
		"host", stHost,
		"port", stPort,
		"server_id", stServerID,
		"time1", stTime1,
		"hash1", stHash1,
		"town_sign", stTownSign,
	)

	// 2. 建立仙界跨服 TCP 连接
	stAddr := fmt.Sprintf("%s:%d", stHost, stPort)
	slog.Info("正在连接仙界跨服服务器...", "addr", stAddr)
	conn, err := net.DialTimeout("tcp", stAddr, 5*time.Second)
	if err != nil {
		slog.Error("连接仙界跨服服务器失败", "err", err)
		return
	}
	defer conn.Close()
	slog.Info("已成功建立仙界跨服长连接！准备进入仙界城镇 (Mod 95, Act 0)...")

	// 3. 直接发送进入仙界城镇 (Module 95, Action 0 -> 0x005F0000)
	// town_id = 41 (0x29) 代表仙界城镇
	wEnter := protocol.NewWriter()
	wEnter.WriteInt32(41)
	enterTownPkt := protocol.NewPacket(0x005F0000, wEnter.Bytes())
	rawEnter, err := enterTownPkt.Marshal()
	if err != nil {
		slog.Error("进入仙界城镇封包序列化失败", "err", err)
		return
	}
	slog.Info("发送进入仙界城镇封包", "hex", hex.EncodeToString(rawEnter))

	if _, err := conn.Write(rawEnter); err != nil {
		slog.Error("发送进入仙界城镇失败", "err", err)
		return
	}

	// 接收进入城镇回包
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	respTown, err := protocol.ReadPacket(conn)
	if err != nil {
		slog.Error("接收进入仙界城镇回包失败", "err", err)
		return
	}
	slog.Info(">>> 成功收到仙界城镇响应！ <<<",
		"action_id", fmt.Sprintf("0x%08X", respTown.ActionID),
		"len", len(respTown.Payload),
		"hex", hex.EncodeToString(respTown.Payload),
	)

	// 4. 在仙界连接上测试取经！(Module 114)
	slog.Info("=== 在仙界长连接上测试西天取经 (Mod 114) ===")
	sendStBible := func(actID uint32, payload []byte, name string) *protocol.Packet {
		pkt := protocol.NewPacket(actID, payload)
		raw, _ := pkt.Marshal()
		_, _ = conn.Write(raw)
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		p, rErr := protocol.ReadPacket(conn)
		if rErr != nil {
			slog.Warn("仙界取经读取超时: "+name, "err", rErr)
			return nil
		}
		slog.Info("仙界取经响应: "+name,
			"action_id", fmt.Sprintf("0x%08X", p.ActionID),
			"len", len(p.Payload),
			"hex", hex.EncodeToString(p.Payload),
		)
		return p
	}

	// Mod 114, Act 0: open_take_bible
	sendStBible(0x00720000, nil, "Mod 114 Act 0 (open_take_bible)")
	// Mod 114, Act 2: get_take_bible_info
	sendStBible(0x00720002, nil, "Mod 114 Act 2 (get_take_bible_info)")
	// Mod 114, Act 16: get_rob_take_bible_cd_time
	sendStBible(0x00720010, nil, "Mod 114 Act 16 (get_rob_cd)")

	slog.Info("探针验证顺利完成！")
	return

	// Mod 114, Act 0: open_take_bible
	sendStBible(0x00720000, nil, "Mod 114 Act 0 (open_take_bible)")
	// Mod 114, Act 2: get_take_bible_info
	sendStBible(0x00720002, nil, "Mod 114 Act 2 (get_take_bible_info)")
	// Mod 114, Act 16: get_rob_take_bible_cd_time
	sendStBible(0x00720010, nil, "Mod 114 Act 16 (get_rob_cd)")

	slog.Info("探针验证顺利完成！")
}
