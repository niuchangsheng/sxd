//go:build probe

// 目标: 喜从天降/五福临门「吹龙鱼」(Mod 68 Act 3) 与龙鱼令兑换 (Act 6)
//
//	open_find_immortal (1) 面板
//	get_xun_xian_ling_number (5) 龙鱼令数量
//	exchange_xian_ling (6, [Int count]) 消耗声望兑换龙鱼令
//	start_find_immortal (2) 召唤龙鱼
//	goad (3) 吹龙鱼 -> 每次 +5 活跃度 (任务 46)
//	pickup_award (4) 收取奖励
//
// 只使用声望兑换与免费/声望道具，不涉及元宝。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_find_immortal_goad_live.go
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
		cfgPath = "configs/s0.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[0]
	tc := platform.NewTicketCache(90 * time.Minute)
	xd, _ := platform.NewXindongAuthenticator()
	xd.SetCookie(acc.Cookie)
	tc.RegisterAuthenticator(xd)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Second)
	defer cancel()
	ticket, err := tc.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		slog.Error("换票失败", "err", err)
		return
	}
	rc := client.SessionConfig{RoleID: fmt.Sprintf("%s-%s", role.ServerID, role.RoleName), RoleName: role.RoleName,
		Username: acc.Username, ServerAddr: ticket.GatewayURL, ServerID: role.ServerID,
		Platform: acc.Platform, Authenticator: client.DefaultAuthenticator, MaxReconnectAttempts: 3}
	client.ApplyTicket(&rc, ticket)
	sess := client.NewRoleSession(rc)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()
	for sess.State() != client.StateActive && ctx.Err() == nil {
		time.Sleep(100 * time.Millisecond)
	}
	call := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
		c, cc := context.WithTimeout(ctx, 7*time.Second)
		defer cc()
		resp, err := sess.Call(c, pkt, act)
		if err != nil {
			slog.Warn("无回包", "action", fmt.Sprintf("0x%08X", act), "err", err)
			return nil
		}
		return resp
	}
	tokens := func(prev uint32) int32 {
		resp := call(protocol.BuildFindImmortalGetXunXianLingPacket(prev), protocol.ActionIDFindImmortalGetXunXianLing)
		if resp == nil {
			return -1
		}
		t, err := protocol.ParseFindImmortalXunXianLingResult(resp.Payload)
		if err != nil {
			slog.Warn("解析龙鱼令失败", "err", err, "hex", hex.EncodeToString(resp.Payload))
			return -1
		}
		return int32(t)
	}
	act := func() (int, int) {
		c, cc := context.WithTimeout(ctx, 8*time.Second)
		defer cc()
		resp, err := sess.Call(c, protocol.BuildGameAssistantInfoPacket(), protocol.ActionIDGameAssistantInfo)
		if err != nil || resp == nil {
			return 0, -1
		}
		p, err := protocol.ParseGameAssistantInfo(resp.Payload)
		if err != nil {
			return 0, -1
		}
		return p.DailyTotal(), p.RemainingTimesFor(46)
	}
	before, remain46 := act()
	slog.Info("五福临门前", "daily_total", before, "task46_remain", remain46)
	st := sess.GetPlayerState()

	if r := call(protocol.BuildFindImmortalOpenPacket(0), protocol.ActionIDFindImmortalOpen); r != nil {
		slog.Info("喜从天降面板", "hex", hex.EncodeToString(r.Payload))
	}
	slog.Info("龙鱼令(兑换前)", "tokens", tokens(protocol.ActionIDFindImmortalOpen))
	if r := call(protocol.BuildFindImmortalExchangeXianLingPacket(2, protocol.ActionIDFindImmortalGetXunXianLing), protocol.ActionIDFindImmortalExchangeXianLing); r != nil {
		slog.Info("兑换龙鱼令回包", "hex", hex.EncodeToString(r.Payload))
	} else {
		slog.Info("兑换龙鱼令无回包")
	}
	tk := tokens(protocol.ActionIDFindImmortalExchangeXianLing)
	slog.Info("龙鱼令(兑换后)", "tokens", tk)
	if tk <= 0 {
		fin := sess.GetPlayerState()
		slog.Info("无龙鱼令，结束", "coin_delta", fin.Coins-st.Coins, "ingot_delta", fin.Ingots-st.Ingots)
		return
	}

	start := call(protocol.BuildFindImmortalStartPacket(protocol.ActionIDFindImmortalGetXunXianLing), protocol.ActionIDFindImmortalStart)
	if start != nil {
		slog.Info("召唤龙鱼回包", "hex", hex.EncodeToString(start.Payload))
	}
	goaded := 0
	prev := protocol.ActionIDFindImmortalStart
	for i := 0; i < 10; i++ {
		resp := call(protocol.BuildFindImmortalGoadPacket(prev), protocol.ActionIDFindImmortalGoad)
		if resp == nil {
			break
		}
		prev = protocol.ActionIDFindImmortalGoad
		res, perr := protocol.ParseFindImmortalGoadResult(resp.Payload)
		if perr != nil {
			slog.Warn("解析吹龙鱼失败", "err", perr, "hex", hex.EncodeToString(resp.Payload))
			break
		}
		slog.Info("吹龙鱼", "i", i, "result", res.Result)
		if res.Result != protocol.FindImmortalResultSuccess {
			break
		}
		goaded++
		time.Sleep(120 * time.Millisecond)
	}
	if r := call(protocol.BuildFindImmortalPickupAwardPacket(protocol.ActionIDFindImmortalGoad), protocol.ActionIDFindImmortalPickupAward); r != nil {
		slog.Info("收取龙鱼奖励回包", "len", len(r.Payload))
	}
	after, remain2 := act()
	fin := sess.GetPlayerState()
	slog.Info("五福临门后", "goaded", goaded, "daily_total", after, "task46_remain", remain2,
		"activity_delta", after-before, "coin_delta", fin.Coins-st.Coins, "ingot_delta", fin.Ingots-st.Ingots)
}
