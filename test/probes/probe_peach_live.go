//go:build probe

// 目标协议: 摘仙桃召唤 (Mod_GetPeach_Base, Module 40)
//
//	peach_info (Act 3) response: [Byte, Byte, Byte, Byte]
//	player_info(Act 4) response: [Byte, Byte, Byte, Byte, Byte, Int call_coin_times, Int call_need_ingot]
//	call_peach (Act 6) 无请求，SUCCESS=0。
//
// 官方客户端仅当 call_coin_times==0 时提示消耗元宝；call_coin_times>0 时为免费/铜钱次数。
// 本探针只在 call_coin_times>0 时召唤，绝不触发元宝档。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml SXD_PROBE_PEACH_CALLS=2 go run -tags=probe test/probes/probe_peach_live.go
package main

import (
	"context"
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

const (
	actionPeachInfo      uint32 = 0x00280003
	actionPeachPlayer    uint32 = 0x00280004
	actionPeachCall      uint32 = 0x00280006
	peachSuccess         uint8  = 0
)

func dailyActivity(sess *client.RoleSession, ctx context.Context) (int, int, bool) {
	cCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	resp, err := sess.Call(cCtx, protocol.NewPacket(0x0000002A, nil), 0x0000002A)
	if err != nil || resp == nil {
		return 0, 0, false
	}
	r := protocol.NewReader(resp.Payload)
	for i := 0; i < 33; i++ {
		if i == 1 || i == 3 {
			_, _ = r.ReadInt64()
		} else {
			_, _ = r.ReadInt32()
		}
	}
	readList := func() [][4]int32 {
		n, _ := r.ReadUint16()
		out := make([][4]int32, 0, n)
		for i := 0; i < int(n); i++ {
			var row [4]int32
			for j := 0; j < 4; j++ {
				row[j], _ = r.ReadInt32()
			}
			out = append(out, row)
		}
		return out
	}
	_ = readList()
	active := readList()
	earned, task64 := 0, 0
	for _, row := range active {
		if row[0] > 83 {
			continue
		}
		earned += int(row[1] * row[3])
		if row[0] == 64 {
			task64 = int(row[2] - row[1])
		}
	}
	return earned, task64, true
}

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	want := 2
	if raw := os.Getenv("SXD_PROBE_PEACH_CALLS"); raw != "" {
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
		slog.Error("加载配置失败", "path", cfgPath, "err", err)
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
		MaxReconnectAttempts: 3,
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动主服会话失败", "err", err)
		return
	}
	defer sess.Close()
	deadline := time.Now().Add(40 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("主服会话未激活", "state", sess.State())
		return
	}

	call := func(pkt *protocol.Packet, act uint32, timeout time.Duration) *protocol.Packet {
		cCtx, cCancel := context.WithTimeout(ctx, timeout)
		defer cCancel()
		resp, cErr := sess.Call(cCtx, pkt, act)
		if cErr != nil {
			slog.Warn("调用无回包", "action", fmt.Sprintf("0x%08X", act), "err", cErr)
			return nil
		}
		return resp
	}

	infoPkt := protocol.NewPacket(actionPeachInfo, []byte{0, 0, 0, 0})
	infoPkt.HasPrevAct = true
	if r := call(infoPkt, actionPeachInfo, 6*time.Second); r != nil {
		slog.Info("摘仙桃面板", "hex", fmt.Sprintf("%x", r.Payload))
	}
	playerPkt := protocol.NewPacket(actionPeachPlayer, []byte{0, 0, 0, 0})
	playerPkt.HasPrevAct = true
	pr := call(playerPkt, actionPeachPlayer, 6*time.Second)
	if pr == nil {
		slog.Error("查询仙桃玩家信息失败")
		return
	}
	rr := protocol.NewReader(pr.Payload)
	vals := make([]uint8, 5)
	for i := range vals {
		vals[i], _ = rr.ReadUint8()
	}
	callCoinTimes, err1 := rr.ReadInt32()
	callNeedIngot, err2 := rr.ReadInt32()
	if err1 != nil || err2 != nil {
		slog.Error("解析仙桃玩家信息失败", "hex", fmt.Sprintf("%x", pr.Payload))
		return
	}
	slog.Info("仙桃玩家信息", "bytes", vals,
		"call_coin_times", callCoinTimes, "call_need_ingot", callNeedIngot)
	if callCoinTimes <= 0 {
		slog.Info("无免费/铜钱召唤次数，跳过 (不使用元宝)")
		return
	}

	_, remainTask, _ := dailyActivity(sess, ctx)
	if want > remainTask {
		want = remainTask
	}
	if int(callCoinTimes) < want {
		want = int(callCoinTimes)
	}
	before := sess.GetPlayerState()
	beforeAct, _, _ := dailyActivity(sess, ctx)
	done := 0
	for done < want {
		if ctx.Err() != nil {
			break
		}
		resp := call(protocol.NewPacket(actionPeachCall, nil), actionPeachCall, 8*time.Second)
		if resp == nil || len(resp.Payload) == 0 {
			break
		}
		code := resp.Payload[0]
		slog.Info("召唤仙桃", "result", code)
		if code != peachSuccess {
			break
		}
		done++
		time.Sleep(150 * time.Millisecond)
	}
	after := sess.GetPlayerState()
	afterAct, afterTask, _ := dailyActivity(sess, ctx)
	slog.Info("召唤仙桃完成",
		"calls", done,
		"task64_remain_after", afterTask,
		"daily_before", beforeAct,
		"daily_after", afterAct,
		"activity_delta", afterAct-beforeAct,
		"coin_delta", after.Coins-before.Coins,
		"ingot_delta", after.Ingots-before.Ingots,
	)

	// 已召唤的仙桃需要先摘取 (get_peach 战斗) 才能再次召唤。
	if afterTask > 0 {
		slog.Info("先摘取已召唤仙桃，再尝试第二次召唤")
		warPkt := protocol.NewPacket(0x00280001, []byte{0, 0, 0, 0})
		warPkt.HasPrevAct = true
		warResp := call(warPkt, 0x00280001, 15*time.Second)
		if warResp == nil || len(warResp.Payload) == 0 {
			slog.Warn("摘取仙桃无回包")
			return
		}
		slog.Info("摘取仙桃战斗结果", "result", warResp.Payload[0], "war_bytes", len(warResp.Payload))
		if warResp.Payload[0] != peachSuccess {
			return
		}
		callResp := call(protocol.NewPacket(actionPeachCall, nil), actionPeachCall, 8*time.Second)
		if callResp == nil || len(callResp.Payload) == 0 {
			slog.Warn("第二次召唤无回包")
			return
		}
		slog.Info("第二次召唤仙桃", "result", callResp.Payload[0])
		finalAct, finalTask, _ := dailyActivity(sess, ctx)
		final := sess.GetPlayerState()
		slog.Info("第二次召唤完成",
			"task64_remain", finalTask,
			"daily_activity", finalAct,
			"activity_delta_total", finalAct-beforeAct,
			"coin_delta", final.Coins-before.Coins,
			"ingot_delta", final.Ingots-before.Ingots,
		)
	}
}