//go:build probe

// 目标: 轮回净地挂机奖励 (Mod_EraRebirthLandAuto_Base, Module 852)
//
//	get_panel_info (Act 1, 0x03540001) request=[]
//	get_auto_profit (Act 9, 0x03540009) request=[] response=[UByte,Int,[Int,Long]]
//	one_click_quick_free_back (Act 6, 0x03540006) request=[] response=[UByte,[Int,Long]]
//	start_auto (Act 2, 0x03540002) request=[] response=[UByte]
//
// 每日活跃度任务 70「轮回净地领取挂机奖励」每次 +20。
// 本探针只调用免费一键领取，绝不调用 one_click_quick_ingot_back (元宝档)。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_era_rebirth_land_live.go
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
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
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
		return p.DailyTotal(), p.RemainingTimesFor(70), true
	}
	call := func(act uint32) *protocol.Packet {
		c, cc := context.WithTimeout(ctx, 8*time.Second)
		defer cc()
		pkt := protocol.NewPacket(act, []byte{0, 0, 0, 0})
		pkt.HasPrevAct = true
		resp, err := sess.Call(c, pkt, act)
		if err != nil {
			slog.Warn("无回包", "action", fmt.Sprintf("0x%08X", act), "err", err)
			return nil
		}
		return resp
	}
	before, remain, ok := act()
	st := sess.GetPlayerState()
	slog.Info("轮回净地前", "daily_activity", before, "task70_remain", remain, "ok", ok,
		"coins", st.Coins, "ingots", st.Ingots)
	if r := call(0x03540001); r != nil {
		head := r.Payload
		if len(head) > 80 {
			head = head[:80]
		}
		slog.Info("轮回净地面板", "len", len(r.Payload), "head", hex.EncodeToString(head))
	}
	if r := call(0x03540009); r != nil {
		slog.Info("挂机收益", "len", len(r.Payload), "hex", hex.EncodeToString(r.Payload))
	}
	if !ok || remain <= 0 {
		slog.Info("任务 70 已完成或不可读，跳过领取")
		return
	}
	if r := call(0x03540006); r != nil {
		slog.Info("免费一键领取回包", "len", len(r.Payload), "hex", hex.EncodeToString(r.Payload))
	}
	after, remain2, _ := act()
	fin := sess.GetPlayerState()
	slog.Info("轮回净地后", "daily_activity", after, "task70_remain", remain2,
		"activity_delta", after-before, "coin_delta", fin.Coins-st.Coins, "ingot_delta", fin.Ingots-st.Ingots)
}
