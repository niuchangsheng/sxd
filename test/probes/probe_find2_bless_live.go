//go:build probe

// 目标: 五福临门 (FindImmortal2, Module 68 Act 10-13) 直接尝试开启与祈福
//
//	is_five_blessings_open (14) 判定在例程中已返回 false，本探针跳过该判定，
//	直接调用 open_five_blessings (10) 与 start_bless (11)，用结果码确认真实状态。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_find2_bless_live.go
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
	ctx, cancel := context.WithTimeout(context.Background(), 160*time.Second)
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
		c, cc := context.WithTimeout(ctx, 8*time.Second)
		defer cc()
		resp, err := sess.Call(c, pkt, act)
		if err != nil {
			slog.Warn("无回包", "action", fmt.Sprintf("0x%08X", act), "err", err)
			return nil
		}
		return resp
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
	before, remain := act()
	st := sess.GetPlayerState()
	slog.Info("五福前", "daily_total", before, "task46_remain", remain, "coins", st.Coins, "ingots", st.Ingots)

	if r := call(protocol.BuildFindImmortalIsFiveBlessingsOpenPacket(0), protocol.ActionIDFindImmortalIsFiveBlessingsOpen); r != nil {
		slog.Info("开启判定", "hex", hex.EncodeToString(r.Payload))
	}
	info := call(protocol.BuildFindImmortalOpenFiveBlessingsPacket(protocol.ActionIDFindImmortalIsFiveBlessingsOpen), protocol.ActionIDFindImmortalOpenFiveBlessings)
	if info == nil {
		slog.Info("五福面板无回包")
		return
	}
	slog.Info("五福面板", "hex", hex.EncodeToString(info.Payload))
	if p, perr := protocol.ParseFiveBlessingsInfoResult(info.Payload); perr == nil {
		slog.Info("五福面板解析", "bless_number", p.BlessNumber, "stage", p.Stage,
			"total_count", p.TotalCount, "pending_awards", len(p.Awards))
	}
	start := call(protocol.BuildFindImmortalStartBlessPacket(protocol.ActionIDFindImmortalOpenFiveBlessings), protocol.ActionIDFindImmortalStartBless)
	if start != nil {
		slog.Info("开始祈福", "hex", hex.EncodeToString(start.Payload))
		if r, perr := protocol.ParseFiveBlessingsActionResult(start.Payload); perr == nil {
			slog.Info("开始祈福结果", "result", r.Result)
		}
	}
	time.Sleep(200 * time.Millisecond)
	after, remain2 := act()
	fin := sess.GetPlayerState()
	slog.Info("五福后", "daily_total", after, "task46_remain", remain2,
		"activity_delta", after-before, "coin_delta", fin.Coins-st.Coins, "ingot_delta", fin.Ingots-st.Ingots)
}
