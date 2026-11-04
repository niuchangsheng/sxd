//go:build probe

// 目标协议: Module 0 登录初始化链路 (Act 0/3/72/99/165)
// 基准对齐: 对比 s0 / s1 双角色登录期服务端下发的全部属性推送与场景同步包，
//
//	定位是否存在帮派归属等角色状态字段差异。
//
// 运行命令: go run -tags=probe test/probes/probe_player_state_dump.go -config configs/s0.yaml
package main

import (
	"context"
	"encoding/hex"
	"flag"
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

	cfgPath := flag.String("config", "configs/s0.yaml", "配置文件路径")
	collect := flag.Duration("collect", 6*time.Second, "激活后继续采集服务端推送的时长")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
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

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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
		RefreshCredentials: func(c context.Context, sCfg *client.SessionConfig) error {
			ticketCache.Invalidate(acc.Platform, acc.Username, role.ServerID)
			freshTicket, err := ticketCache.GetOrFetch(c, acc.Platform, acc.Username, acc.Password, role.ServerID)
			if err != nil {
				return err
			}
			client.ApplyTicket(sCfg, freshTicket)
			if freshTicket.GatewayURL != "" {
				sCfg.ServerAddr = freshTicket.GatewayURL
			}
			return nil
		},
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
	sess.SetPacketObserver(func(pkt *protocol.Packet) {
		if pkt == nil {
			return
		}
		switch pkt.ActionID {
		case protocol.ActionHeartbeat, 0x022E0002:
			return
		}
		slog.Info("入站报文",
			"action_id", fmt.Sprintf("0x%08X (Mod %d, Act %d)", pkt.ActionID, pkt.ActionID>>16, pkt.ActionID&0xFFFF),
			"len", len(pkt.Payload), "hex", hex.EncodeToString(pkt.Payload))
	})

	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()

	deadline := time.Now().Add(15 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未在时限内激活", "state", sess.State())
		return
	}

	slog.Info("会话已激活，继续采集服务端推送", "duration", collect.String())
	time.Sleep(*collect)
	slog.Info("采集结束")
}
