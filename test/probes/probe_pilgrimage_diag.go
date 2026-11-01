//go:build probe

// 目标协议: 西天取经真实链路定位 (Mod 114 / 0x0072)
// 基准对齐: 对齐抓包中主服与仙界跨服连接上的取经请求，仅做只读 GetInfo 探测
// 运行命令: go run -tags=probe test/probes/probe_pilgrimage_diag.go
// 预期表现: 分别打印主服与仙界跨服上的 0x00720002 回包，不执行刷新/启程
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

func callAndLog(sess *client.RoleSession, act uint32, name string) {
	p := protocol.NewPacket(act, nil)
	tCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resp, err := sess.Call(tCtx, p, act)
	if err != nil {
		slog.Warn("主服请求失败: "+name, "err", err)
		return
	}
	slog.Info("主服响应: "+name,
		"action", fmt.Sprintf("0x%08X", resp.ActionID),
		"len", len(resp.Payload),
		"hex", hex.EncodeToString(resp.Payload),
	)
}

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
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
	slog.Info("主服网关", "addr", ticket.GatewayURL)

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
	for i := 0; i < 60 && sess.State() != client.StateActive; i++ {
		time.Sleep(250 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("主服未激活", "state", sess.State())
		return
	}
	slog.Info("主服已激活", "role_id", sess.RoleID(), "player_id", sess.PlayerID())

	// 1. 主服上直接尝试 Mod114 GetInfo
	callAndLog(sess, 0x00720002, "主服 Mod114 GetInfo")

	// 2. 拉取仙界跨服登录凭据
	tCtx, tCancel := context.WithTimeout(ctx, 3*time.Second)
	resp, err := sess.Call(tCtx, protocol.NewPacket(0x00600000, nil), 0x00600000)
	tCancel()
	if err != nil {
		slog.Error("拉取仙界凭据失败", "err", err)
		return
	}
	r := protocol.NewReader(resp.Payload)
	stHost, _ := r.ReadString()
	stPort, _ := r.ReadUint16()
	stServerID, _ := r.ReadString()
	stTime1, _ := r.ReadInt32()
	stHash1, _ := r.ReadString()
	stTownSign, _ := r.ReadString()
	slog.Info("仙界跨服凭据", "host", stHost, "port", stPort, "server_id", stServerID, "time", stTime1, "hash", stHash1, "town_sign", stTownSign)

	stAddr := fmt.Sprintf("%s:%d", stHost, stPort)
	conn, err := net.DialTimeout("tcp", stAddr, 5*time.Second)
	if err != nil {
		slog.Error("连接仙界跨服失败", "addr", stAddr, "err", err)
		return
	}
	defer conn.Close()
	slog.Info("已连接仙界跨服", "addr", stAddr)

	// 3. 进入仙界城镇 (town_id=41)
	w := protocol.NewWriter()
	w.WriteInt32(41)
	if err := protocol.WritePacket(conn, protocol.NewPacket(0x005F0000, w.Bytes())); err != nil {
		slog.Error("发送进入仙界城镇失败", "err", err)
		return
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	respTown, err := protocol.ReadPacket(conn)
	if err != nil {
		slog.Error("仙界城镇无应答", "err", err)
		return
	}
	slog.Info("仙界城镇响应", "action", fmt.Sprintf("0x%08X", respTown.ActionID), "len", len(respTown.Payload), "hex", hex.EncodeToString(respTown.Payload))

	// 4. 仙界跨服连接上只读 GetInfo
	sendSt := func(act uint32, payload []byte, name string) {
		if err := protocol.WritePacket(conn, protocol.NewPacket(act, payload)); err != nil {
			slog.Warn("仙界发送失败: "+name, "err", err)
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		p, rErr := protocol.ReadPacket(conn)
		if rErr != nil {
			slog.Warn("仙界无应答: "+name, "err", rErr)
			return
		}
		slog.Info("仙界响应: "+name, "action", fmt.Sprintf("0x%08X", p.ActionID), "len", len(p.Payload), "hex", hex.EncodeToString(p.Payload))
	}

	// 首次请求按抓包惯例回填自身 ActionID 作为 PrevAct
	prev := protocol.NewWriter()
	prev.WriteUint32(0x00720002)
	sendSt(0x00720002, prev.Bytes(), "Mod114 GetInfo")
}