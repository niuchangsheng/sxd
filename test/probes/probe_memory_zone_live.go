//go:build probe

// 目标: 回忆之境主面板/回溯面板与免费回溯 (Mod_MemoryZone_Base, Module 526)
//
//	main_panel (Act 1, 0x020E0001) request=[] response=[UByte,[Int,Int,Int,Int],Int]
//	back_panel (Act 2, 0x020E0002) request=[Int type] response=[UByte,[Int,Int,Int],Int,Int,Int,Int]
//	backtrack  (Act 4, 0x020E0004) request=[Int role_id] response=[UByte,[Int,Int],[Int,Int,Int],Int,Int]
//
// 仅当 back_panel 返回 daily_free_times>0 时才执行一次免费回溯，绝不使用元宝档。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_memory_zone_live.go
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
	call := func(action uint32, payload []byte) *protocol.Packet {
		pkt := protocol.NewPacket(action, payload)
		pkt.HasPrevAct = true
		c, cc := context.WithTimeout(ctx, 7*time.Second)
		defer cc()
		resp, err := sess.Call(c, pkt, action)
		if err != nil {
			slog.Warn("无回包", "action", fmt.Sprintf("0x%08X", action), "err", err)
			return nil
		}
		return resp
	}
	zero := []byte{0, 0, 0, 0}
	mainResp := call(0x020E0001, zero)
	if mainResp == nil {
		slog.Error("回忆之境主面板无回包")
		return
	}
	slog.Info("主面板", "hex", hex.EncodeToString(mainResp.Payload))
	// 解析 back_list types
	mr := protocol.NewReader(mainResp.Payload)
	res, _ := mr.ReadUint8()
	n, _ := mr.ReadUint16()
	types := make([]int32, 0, n)
	for i := 0; i < int(n); i++ {
		t, _ := mr.ReadInt32()
		_, _ = mr.ReadInt32()
		_, _ = mr.ReadInt32()
		_, _ = mr.ReadInt32()
		types = append(types, t)
	}
	daoyuan, _ := mr.ReadInt32()
	slog.Info("主面板解析", "result", res, "types", types, "award_daoyuan", daoyuan)
	if len(types) == 0 {
		return
	}

	typeW := protocol.NewWriter()
	typeW.WriteInt32(types[0])
	typeW.WriteUint32(0)
	backResp := call(0x020E0002, typeW.Bytes())
	if backResp == nil {
		slog.Error("回溯面板无回包")
		return
	}
	slog.Info("回溯面板", "hex", hex.EncodeToString(backResp.Payload))
	br := protocol.NewReader(backResp.Payload)
	bres, _ := br.ReadUint8()
	bn, _ := br.ReadUint16()
	roleIDs := make([]int32, 0, bn)
	for i := 0; i < int(bn); i++ {
		rid, _ := br.ReadInt32()
		have, _ := br.ReadInt32()
		need, _ := br.ReadInt32()
		roleIDs = append(roleIDs, rid)
		slog.Info("伙伴", "role_id", rid, "have_scrap", have, "need_scrap", need)
	}
	todayBack, _ := br.ReadInt32()
	freeTimes, _ := br.ReadInt32()
	ingotTimes, _ := br.ReadInt32()
	costIngot, _ := br.ReadInt32()
	slog.Info("回溯面板解析", "result", bres, "today_back", todayBack,
		"daily_free_times", freeTimes, "daily_ingot_times", ingotTimes, "cost_ingot", costIngot)
	// 先尝试领取绑定奖励 (Act 3)，主面板 get_award=1 表示有可领取。
	for _, t := range types {
		gw := protocol.NewWriter()
		gw.WriteInt32(t)
		gw.WriteUint32(0)
		if gResp := call(0x020E0003, gw.Bytes()); gResp != nil {
			slog.Info("绑定奖励回包", "type", t, "hex", hex.EncodeToString(gResp.Payload))
			break
		}
	}
	if freeTimes <= 0 || len(roleIDs) == 0 {
		slog.Info("无免费回溯次数或无伙伴，跳过")
		return
	}
	st := sess.GetPlayerState()
	rw := protocol.NewWriter()
	rw.WriteInt32(roleIDs[0])
	rw.WriteUint32(0)
	btResp := call(0x020E0004, rw.Bytes())
	if btResp == nil {
		return
	}
	slog.Info("回溯回包", "hex", hex.EncodeToString(btResp.Payload))
	time.Sleep(300 * time.Millisecond)
	fin := sess.GetPlayerState()
	slog.Info("回溯后", "coin_delta", fin.Coins-st.Coins, "ingot_delta", fin.Ingots-st.Ingots)
}
