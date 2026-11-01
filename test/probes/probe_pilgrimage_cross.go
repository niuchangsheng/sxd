//go:build probe

// 目标协议: 仙界跨服西天取经 (Mod 114 / 0x0072) 登录与只读 GetInfo 验证
// 基准对齐: 对齐抓包中 9x703.sxdweb.xd.com:8480 上的 StLogin → 进入仙界城镇 → getinfo 链路
// 运行命令: go run -tags=probe test/probes/probe_pilgrimage_cross.go
// 预期表现: 建立仙界跨服会话并打印 0x00720002 的 5 使者记录，不执行刷新/启程
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

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

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	ticket, err := ticketCache.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		slog.Error("换票失败", "err", err)
		return
	}

	// 1. 主服会话：激活并拉取仙界跨服地址
	mainCfg := client.SessionConfig{
		RoleID:               fmt.Sprintf("%s-%s", role.ServerID, role.RoleName),
		RoleName:             role.RoleName,
		Username:             acc.Username,
		ServerAddr:           ticket.GatewayURL,
		ServerID:             role.ServerID,
		Platform:             acc.Platform,
		Authenticator:        client.DefaultAuthenticator,
		MaxReconnectAttempts: 1,
	}
	client.ApplyTicket(&mainCfg, ticket)
	mainSess := client.NewRoleSession(mainCfg)
	if err := mainSess.Start(ctx); err != nil {
		slog.Error("主服会话启动失败", "err", err)
		return
	}
	defer mainSess.Close()
	for i := 0; i < 60 && mainSess.State() != client.StateActive; i++ {
		time.Sleep(250 * time.Millisecond)
	}
	if mainSess.State() != client.StateActive {
		slog.Error("主服未激活", "state", mainSess.State())
		return
	}

	// 2. 拉取仙界跨服凭据
	credCtx, credCancel := context.WithTimeout(ctx, 5*time.Second)
	cred, err := mainSess.FetchCrossServerCredentials(credCtx)
	credCancel()
	if err != nil {
		slog.Error("拉取仙界凭据失败", "err", err)
		return
	}
	slog.Info("仙界跨服凭据", "addr", net.JoinHostPort(cred.Host, strconv.Itoa(int(cred.Port))),
		"server_id", cred.ServerName, "time1", cred.Time, "town_sign", cred.TownSign,
		"player_id", mainSess.PlayerID(), "origin_nick", mainSess.OriginNickName())

	// 3. 建立仙界跨服会话 (Mod 94 登录 + Mod 95 进入仙界城镇)
	crossSess, err := mainSess.OpenCrossServerSessionWithInfo(ctx, cred)
	if err != nil {
		slog.Error("仙界跨服会话建立失败", "err", err)
		return
	}
	defer crossSess.Close()
	slog.Info("仙界会话已激活", "player_id", crossSess.PlayerID(), "last_action", fmt.Sprintf("0x%08X", crossSess.LastActionID()))

	// 4. 只读 GetInfo (0x00720002，请求体为空)
	iCtx, iCancel := context.WithTimeout(ctx, 5*time.Second)
	defer iCancel()
	ir, err := crossSess.Call(iCtx, protocol.BuildStTakeBibleCrossGetInfoPacket(), protocol.ActionIDStTakeBibleCrossGetInfo)
	if err != nil {
		slog.Error("仙界取经 GetInfo 失败", "err", err)
		return
	}
	slog.Info("仙界取经 GetInfo 响应", "len", len(ir.Payload), "hex", hex.EncodeToString(ir.Payload))
	info, err := protocol.ParseStTakeBibleCrossInfoResult(ir.Payload)
	if err != nil {
		slog.Error("解析仙界取经详情失败", "err", err)
		return
	}
	slog.Info("仙界取经准备详情",
		"take_times", info.TakeBibleTimes,
		"total_times", info.TotalTakeBibleTimes,
		"refresh_price", info.RefreshPrice,
		"status", info.TakeBibleStatus,
		"protection", protocol.StTakeBibleCrossProtectionName(info.CanProtection),
		"protect_player", info.ProtectPlayer,
		"bless", info.Bless,
		"buff", info.Buff,
		"protections", len(info.ProtectionList),
		"protect_players", len(info.ProtectPlayerList),
	)
	for _, p := range info.ProtectionList {
		slog.Info("使者选项",
			"protection", protocol.StTakeBibleCrossProtectionName(p.Protection),
			"take_time", p.TakeTime,
			"award_coin", p.AwardCoin,
			"award_fame", p.AwardFame,
			"award_st_exp", p.AwardStExp,
			"npc_level", p.NpcLevel,
			"npc_expe", p.NpcExpe,
		)
	}
	for _, p := range info.ProtectPlayerList {
		slog.Info("保护好友",
			"player_id", p.PlayerID,
			"nick_name", p.NickName,
			"remain_times", p.RemainProtectTimes,
			"total_times", p.TotalProtectTimes,
			"status", p.ProtectStatus,
		)
	}
}
