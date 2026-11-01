//go:build probe

// 目标协议: 本服竞技场对手列表 (Mod 28, Act 1) 只读诊断
// 基准对齐: 核对 0x001C0001 回包的字段布局、字符串长度前缀与战力偏移
// 运行命令: go run -tags=probe test/probes/probe_arena_opponents.go
// 预期表现: 登录后仅查询剩余次数与对手列表并打印原始 hex，不发起任何挑战、不消耗次数
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
	slog.SetLogLoggerLevel(slog.LevelWarn)
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

	for i := 0; i < 60; i++ {
		if sess.State() == client.StateActive {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未能在时限内激活", "state", sess.State())
		return
	}

	tCtx, tCancel := context.WithTimeout(ctx, 5*time.Second)
	defer tCancel()
	timesPkt, _ := protocol.BuildArenaGetTimesPacket(protocol.ArenaGetTimesRequest{PrevAct: protocol.ActionIDTownEnter})
	timesResp, err := sess.Call(tCtx, timesPkt, protocol.ActionIDArenaGetTimes)
	if err != nil {
		slog.Error("查询次数失败", "err", err)
		return
	}
	fmt.Printf("TIMES raw=%s\n", hex.EncodeToString(timesResp.Payload))

	oCtx, oCancel := context.WithTimeout(ctx, 5*time.Second)
	defer oCancel()
	oppPkt, _ := protocol.BuildArenaGetOpponentsPacket(protocol.ArenaGetOpponentsRequest{PrevAct: protocol.ActionIDArenaGetTimes})
	oppResp, err := sess.Call(oCtx, oppPkt, protocol.ActionIDArenaGetOpponents)
	if err != nil {
		slog.Error("查询对手失败", "err", err)
		return
	}
	fmt.Printf("OPPONENTS len=%d raw=%s\n", len(oppResp.Payload), hex.EncodeToString(oppResp.Payload))

	res, err := protocol.ParseArenaGetOpponentsResult(oppResp.Payload)
	if err != nil {
		fmt.Printf("PARSE ERR: %v\n", err)
		return
	}
	for i, o := range res.Opponents {
		fmt.Printf("  [%d] rank=%d playerID=%d roleID=%d name=%q lv=%d coin=%d fame=%d\n", i, o.Rank, o.PlayerID, o.RoleID, o.Name, o.Level, o.AwardCoin, o.AwardFame)
	}

	// 追加只读探测: 文档声称的 0x001C0000 列表接口与 0x001C000C 排名接口
	rCtx, rCancel := context.WithTimeout(ctx, 5*time.Second)
	defer rCancel()
	rankPkt := protocol.NewPacket(protocol.ActionIDArenaGetRanking, []byte{0x00, 0x1C, 0x00, 0x0D})
	if rankResp, err := sess.Call(rCtx, rankPkt, protocol.ActionIDArenaGetRanking); err == nil {
		fmt.Printf("RANKING raw=%s\n", hex.EncodeToString(rankResp.Payload))
	} else {
		fmt.Printf("RANKING ERR: %v\n", err)
	}

	lCtx, lCancel := context.WithTimeout(ctx, 5*time.Second)
	defer lCancel()
	legacyPkt := protocol.NewPacket(0x001C0000, []byte{0x00, 0x1C, 0x00, 0x0C})
	if legacyResp, err := sess.Call(lCtx, legacyPkt, 0x001C0000); err == nil {
		fmt.Printf("LIST(0x1C0000) len=%d raw=%s\n", len(legacyResp.Payload), hex.EncodeToString(legacyResp.Payload))
	} else {
		fmt.Printf("LIST(0x1C0000) ERR: %v\n", err)
	}
}