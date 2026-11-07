//go:build probe

// 目标: 兵法浇灌与活跃度任务 54 的计数关系
//
// 已实测 draw_tactics_exp(type=1 黄金圣水, arg=2) 返回 SUCCEED(7)。
// 本探针连续浇灌数次并逐次检查 task 54 剩余次数与总活跃度。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_tactics_loop_live.go
package main

import (
	"context"
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
	call := func(action uint32, payload []byte) *protocol.Packet {
		pkt := protocol.NewPacket(action, payload)
		c, cc := context.WithTimeout(ctx, 7*time.Second)
		defer cc()
		resp, err := sess.Call(c, pkt, action)
		if err != nil {
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
		return p.DailyTotal(), p.RemainingTimesFor(54)
	}
	before, remain := act()
	slog.Info("浇灌前", "daily_total", before, "task54_remain", remain)
	st := sess.GetPlayerState()
	for i := 0; i < 5; i++ {
		if ctx.Err() != nil {
			break
		}
		w := protocol.NewWriter()
		w.WriteUint8(1)
		w.WriteInt32(2)
		resp := call(0x012B0001, w.Bytes())
		if resp == nil || len(resp.Payload) == 0 {
			slog.Warn("浇灌无回包", "i", i)
			break
		}
		code := resp.Payload[0]
		time.Sleep(150 * time.Millisecond)
		cur, rem := act()
		slog.Info("浇灌", "i", i, "result", code, "daily_total", cur, "task54_remain", rem)
		if code != 7 {
			break
		}
	}
	fin := sess.GetPlayerState()
	slog.Info("浇灌汇总", "coin_delta", fin.Coins-st.Coins, "ingot_delta", fin.Ingots-st.Ingots)
}
