//go:build probe

// 目标: 兵法「购买圣水」(Mod_Tactics_Base.buy_item, 299/2)
//
// 每日活跃度任务 54「兵法」= 每次购买圣水 +5，上限 25 次。
// 湛蓝圣水 (type=0) 用铜钱购买，自动化安全；黄金圣水 (type=1) 消耗元宝，禁用。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml SXD_PROBE_BUY_TIMES=25 go run -tags=probe test/probes/probe_tactics_buy_water_live.go
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	want := 25
	if raw := os.Getenv("SXD_PROBE_BUY_TIMES"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			want = v
		}
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
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
	act := func() (int, int, bool) {
		c, cc := context.WithTimeout(ctx, 8*time.Second)
		defer cc()
		resp, err := sess.Call(c, protocol.BuildGameAssistantInfoPacket(), protocol.ActionIDGameAssistantInfo)
		if err != nil || resp == nil {
			return 0, 0, false
		}
		p, err := protocol.ParseGameAssistantInfo(resp.Payload)
		if err != nil {
			return 0, 0, false
		}
		return p.DailyTotal(), p.RemainingTimesFor(54), true
	}
	before, remain, ok := act()
	st := sess.GetPlayerState()
	slog.Info("购买前", "daily_total", before, "task54_remain", remain, "ok", ok,
		"coins", st.Coins, "ingots", st.Ingots)
	if ok && remain > 0 && want > remain {
		want = remain
	}
	bought := 0
	for i := 0; i < want; i++ {
		if ctx.Err() != nil {
			break
		}
		resp := call(0x012B0002, []byte{0}) // type=0 湛蓝圣水
		if resp == nil || len(resp.Payload) == 0 {
			slog.Warn("购买圣水无回包", "i", i)
			break
		}
		code := resp.Payload[0]
		if i == 0 {
			slog.Info("购买回包样例", "hex", hex.EncodeToString(resp.Payload))
		}
		if code != 7 {
			slog.Info("购买圣水未成功", "i", i, "result", code)
			break
		}
		bought++
		time.Sleep(120 * time.Millisecond)
	}
	after, remain2, _ := act()
	fin := sess.GetPlayerState()
	slog.Info("购买汇总", "bought", bought, "daily_total", after, "task54_remain", remain2,
		"activity_delta", after-before, "coin_delta", fin.Coins-st.Coins, "ingot_delta", fin.Ingots-st.Ingots)
}
