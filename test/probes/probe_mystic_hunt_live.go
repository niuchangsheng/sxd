//go:build probe

// 目标: 仙幽猎境 (Mod_MysticCapture_Base, Module 762) 每日进入
//
//	is_open_hunt_entrance (Act 5, 0x02FA0005) request=[] -> Int
//	click_enter_hunt      (Act 0, 0x02FA0000) request=[] -> Int × 13
//	enter_hunt            (Act 1, 0x02FA0001) request=[] -> 大面板
//	capture_soul          (Act 2, 0x02FA0002) request=[Int,Int]  (消耗猎魂次数/道具, 本探针不调用)
//
// 本探针默认只读; -enter 时调用 enter_hunt 一次并核对元宝/体力。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_mystic_hunt_live.go
package main

import (
	"context"
	"encoding/hex"
	"flag"
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
	enter := flag.Bool("enter", false, "调用 enter_hunt 进入一次")
	afterActivity := flag.Bool("after-activity", false, "先调用 0x2A 活跃度面板, 检查会话链污染")
	flag.Parse()
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
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
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
	call := func(action uint32, payload []byte, timeout time.Duration, withPrev bool) *protocol.Packet {
		pkt := protocol.NewPacket(action, payload)
		pkt.HasPrevAct = withPrev
		c, cc := context.WithTimeout(ctx, timeout)
		defer cc()
		resp, err := sess.Call(c, pkt, action)
		if err != nil {
			slog.Warn("无回包", "action", fmt.Sprintf("0x%08X", action), "with_prev", withPrev, "err", err)
			return nil
		}
		return resp
	}
	dump := func(name string, r *protocol.Packet) {
		if r != nil {
			slog.Info(name, "len", len(r.Payload), "hex", hex.EncodeToString(r.Payload))
		}
	}
	if *afterActivity {
		call(0x0000002A, nil, 8*time.Second, false)
	}
	dump("猎境入口开放", call(0x02FA0005, nil, 8*time.Second, false))
	dump("点击进入猎境", call(0x02FA0000, nil, 8*time.Second, false))
	if !*enter {
		return
	}
	st := sess.GetPlayerState()
	dump("进入猎境回包", call(0x02FA0001, nil, 15*time.Second, false))
	fin := sess.GetPlayerState()
	slog.Info("进入猎境后", "coin_delta", fin.Coins-st.Coins, "ingot_delta", fin.Ingots-st.Ingots,
		"stamina_delta", fin.Stamina-st.Stamina)
}
