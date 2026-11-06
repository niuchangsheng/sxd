//go:build probe

// 目标协议: 仙幽猎境 (Mod_MysticCapture_Base, Module 762 / 0x02FA)
//
// 每日活跃度任务 73「每日进入仙幽猎境」进入一次 +20。
//
//	click_enter_hunt (Act 0, 0x02FA0000) request=[] response=[Int x13]，入口提示面板
//	enter_hunt       (Act 1, 0x02FA0001) request=[] response=[UByte result, [Int x5], [Int x2], Int x7]
//	                  SUCCESS=0，其余: MAX_SOULS=1, NO_TIMES=2, FAILED=3...
//	exit_hunt        (Act 3, 0x02FA0003) request=[] 离开猎境
//
// 进入猎境本身不消耗元宝；本探针进入后立即离开，并打印活跃度变化。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_mystic_capture_live.go
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

const (
	actionMysticClickEnter uint32 = 0x02FA0000
	actionMysticEnterHunt  uint32 = 0x02FA0001
	actionMysticExitHunt   uint32 = 0x02FA0003
	mysticSuccess          uint8  = 0
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
	earned, task73 := 0, 0
	for _, row := range active {
		if row[0] > 83 {
			continue
		}
		earned += int(row[1] * row[3])
		if row[0] == 73 {
			task73 = int(row[2] - row[1])
		}
	}
	return earned, task73, true
}

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
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

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
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

	state := sess.GetPlayerState()
	beforeAct, beforeTask, _ := dailyActivity(sess, ctx)
	slog.Info("进入猎境前", "town_id", state.TownID, "town_map_id", state.TownMapID,
		"daily_activity", beforeAct, "task73_remain", beforeTask,
		"ingots", state.Ingots, "coins", state.Coins)

	clickPkt := protocol.NewPacket(actionMysticClickEnter, nil)
	clickPkt.HasPrevAct = true
	if resp := call(clickPkt, actionMysticClickEnter, 8*time.Second); resp != nil {
		head := resp.Payload
		if len(head) > 52 {
			head = head[:52]
		}
		slog.Info("猎境入口", "len", len(resp.Payload), "head", hex.EncodeToString(head))
	}

	enterPkt := protocol.NewPacket(actionMysticEnterHunt, nil)
	enterPkt.HasPrevAct = true
	enterResp := call(enterPkt, actionMysticEnterHunt, 10*time.Second)
	if enterResp == nil || len(enterResp.Payload) == 0 {
		slog.Error("进入仙幽猎境无回包")
		return
	}
	result := enterResp.Payload[0]
	slog.Info("进入仙幽猎境", "result", result, "len", len(enterResp.Payload))
	if result != mysticSuccess {
		slog.Info("进入仙幽猎境未成功，按结果码退出", "result", result)
		return
	}

	time.Sleep(400 * time.Millisecond)
	afterEnter := sess.GetPlayerState()
	midAct, midTask, _ := dailyActivity(sess, ctx)
	slog.Info("进入猎境后", "town_id", afterEnter.TownID, "town_map_id", afterEnter.TownMapID,
		"daily_activity", midAct, "task73_remain", midTask, "activity_delta", midAct-beforeAct)

	// 离开猎境，恢复原地图状态。
	exitPkt := protocol.NewPacket(actionMysticExitHunt, nil)
	exitPkt.HasPrevAct = true
	if resp := call(exitPkt, actionMysticExitHunt, 8*time.Second); resp != nil && len(resp.Payload) > 0 {
		slog.Info("离开仙幽猎境", "result", resp.Payload[0])
	}
	time.Sleep(300 * time.Millisecond)
	final := sess.GetPlayerState()
	finalAct, finalTask, _ := dailyActivity(sess, ctx)
	slog.Info("仙幽猎境完成",
		"task73_remain_after", finalTask,
		"daily_before", beforeAct,
		"daily_after", finalAct,
		"activity_delta", finalAct-beforeAct,
		"coin_delta", final.Coins-state.Coins,
		"ingot_delta", final.Ingots-state.Ingots,
		"town_map_id", final.TownMapID,
	)
}
