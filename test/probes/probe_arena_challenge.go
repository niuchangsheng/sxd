//go:build probe

// 目标协议: 本服竞技场挑战 (Mod 28, Act 2 -> 0x001C0002) 与排名变更通知 (0x001C000B)
// 基准对齐: 确认挑战包前序字段与目标排名取值方式，验证服务端是否异步下发战报及扣减次数
// 运行命令: go run -tags=probe test/probes/probe_arena_challenge.go
// 预期表现: 登录后查询对手并挑战第 1 名对手，长等待收集所有回包，最后复查剩余次数对比是否扣减
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

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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

	queryTimes := func(tag string) int32 {
		c, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		pkt, _ := protocol.BuildArenaGetTimesPacket(protocol.ArenaGetTimesRequest{PrevAct: protocol.ActionIDTownEnter})
		resp, err := sess.Call(c, pkt, protocol.ActionIDArenaGetTimes)
		if err != nil {
			fmt.Printf("%s TIMES ERR: %v\n", tag, err)
			return -1
		}
		res, _ := protocol.ParseArenaGetTimesResult(resp.Payload)
		fmt.Printf("%s TIMES raw=%s remaining=%d\n", tag, hex.EncodeToString(resp.Payload), res.RemainingTimes)
		return res.RemainingTimes
	}

	queryTimes("BEFORE")

	// 竞技场面板进场: 0x001C0000 (携带前序 0x001C000D)，返回个人排名/对手/战报
	eCtx, eCancel := context.WithTimeout(ctx, 5*time.Second)
	enterPkt := protocol.NewPacket(0x001C0000, []byte{0x00, 0x1C, 0x00, 0x0D})
	enterResp, err := sess.Call(eCtx, enterPkt, 0x001C0000)
	eCancel()
	if err != nil {
		fmt.Printf("ARENA ENTER ERR: %v\n", err)
	} else {
		fmt.Printf("ARENA ENTER len=%d header=%s\n", len(enterResp.Payload), hex.EncodeToString(enterResp.Payload[:12]))
	}

	c, cancel2 := context.WithTimeout(ctx, 5*time.Second)
	oppPkt, _ := protocol.BuildArenaGetOpponentsPacket(protocol.ArenaGetOpponentsRequest{PrevAct: protocol.ActionIDArenaGetTimes})
	oppResp, err := sess.Call(c, oppPkt, protocol.ActionIDArenaGetOpponents)
	cancel2()
	if err != nil {
		fmt.Printf("OPPONENTS ERR: %v\n", err)
		return
	}
	res, _ := protocol.ParseArenaGetOpponentsResult(oppResp.Payload)
	if len(res.Opponents) == 0 {
		fmt.Println("NO OPPONENT")
		return
	}
	target := res.Opponents[0]
	fmt.Printf("TARGET rank=%d playerID=%d roleID=%d name=%q lv=%d coin=%d fame=%d\n", target.Rank, target.PlayerID, target.RoleID, target.Name, target.Level, target.AwardCoin, target.AwardFame)

	// 注册所有可能回包的处理器，观察服务端异步通知
	for _, aid := range []uint32{protocol.ActionIDArenaChallenge, protocol.ActionIDArenaNotify, protocol.ActionIDArenaGetOpponents} {
		act := aid
		sess.RegisterHandler(act, func(p *protocol.Packet) {
			fmt.Printf("ASYNC action=0x%08X payload_len=%d raw=%s\n", act, len(p.Payload), hex.EncodeToString(p.Payload))
		})
	}

	chPkt, _ := protocol.BuildArenaChallengePacket(protocol.ArenaChallengeRequest{
		TargetRank: target.Rank,
		PrevAct:    protocol.ActionIDArenaGetOpponents,
	})
	fmt.Printf("CHALLENGE send payload=%s\n", hex.EncodeToString(chPkt.Payload))
	chCtx, chCancel := context.WithTimeout(ctx, 15*time.Second)
	chResp, err := sess.Call(chCtx, chPkt, protocol.ActionIDArenaChallenge)
	chCancel()
	if err != nil {
		fmt.Printf("CHALLENGE no sync resp: %v\n", err)
	} else {
		fmt.Printf("CHALLENGE resp raw=%s\n", hex.EncodeToString(chResp.Payload))
	}

	// 再给异步战报 5 秒窗口
	time.Sleep(5 * time.Second)
	queryTimes("AFTER")
}