//go:build probe

// 目标协议: 奇门遁甲修炼 (Mod_MiracleFighters_Base, Module 251)
//
//	get_miracle_fighters_info (Act 0, 0x00FB0000) response=[UByte result, Int, Byte, [Int x6]]
//	practice_miracle_fighters (Act 1, 0x00FB0001) request=[Int id, UByte type]
//	  type: NORMAL_PRACTICE=3 (铜钱), INGOT_PRACTICE=4 (元宝，禁用)
//
// 本探针只执行 type=3 的普通修炼，绝不使用元宝档；用于验证每日活跃度任务 50。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_qimen_practice_live.go
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

func activityTask(sess *client.RoleSession, ctx context.Context, taskID int32) (int, int, bool) {
	cCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	resp, err := sess.Call(cCtx, protocol.BuildGameAssistantInfoPacket(), protocol.ActionIDGameAssistantInfo)
	if err != nil || resp == nil {
		return 0, 0, false
	}
	panel, err := protocol.ParseGameAssistantInfo(resp.Payload)
	if err != nil {
		return 0, 0, false
	}
	return panel.DailyTotal(), panel.RemainingTimesFor(taskID), true
}

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

	before, beforeTask, _ := activityTask(sess, ctx, 50)
	st := sess.GetPlayerState()
	slog.Info("修炼前", "daily_activity", before, "task50_remain", beforeTask,
		"coins", st.Coins, "ingots", st.Ingots)

	iCtx, iCancel := context.WithTimeout(ctx, 8*time.Second)
	infoPkt := protocol.NewPacket(0x00FB0000, []byte{0, 0, 0, 0})
	infoPkt.HasPrevAct = true
	infoResp, iErr := sess.Call(iCtx, infoPkt, 0x00FB0000)
	iCancel()
	if iErr != nil || infoResp == nil {
		slog.Error("查询奇门信息失败", "err", iErr)
		return
	}
	r := protocol.NewReader(infoResp.Payload)
	result, _ := r.ReadUint8()
	_, _ = r.ReadInt32()
	level, _ := r.ReadUint8()
	n, _ := r.ReadUint16()
	slog.Info("奇门信息", "result", result, "level", level, "entries", n)
	ids := make([]int32, 0, n)
	for i := 0; i < int(n); i++ {
		var first int32
		for j := 0; j < 6; j++ {
			v, _ := r.ReadInt32()
			if j == 0 {
				first = v
			}
		}
		ids = append(ids, first)
	}
	slog.Info("奇门条目", "ids", ids)
	if beforeTask <= 0 {
		slog.Info("任务 50 已完成，跳过")
		return
	}
	if len(ids) == 0 {
		slog.Info("无可用奇门条目")
		return
	}

	var resp *protocol.Packet
	for _, id := range ids {
		w := protocol.NewWriter()
		w.WriteInt32(id)
		w.WriteUint8(3) // NORMAL_PRACTICE
		pkt := protocol.NewPacket(0x00FB0001, w.Bytes())
		pCtx, pCancel := context.WithTimeout(ctx, 5*time.Second)
		r, pErr := sess.Call(pCtx, pkt, 0x00FB0001)
		pCancel()
		if pErr != nil || r == nil {
			slog.Warn("奇门修炼无回包", "id", id, "err", pErr)
			continue
		}
		slog.Info("奇门修炼回包", "id", id, "len", len(r.Payload), "hex", fmt.Sprintf("%x", r.Payload))
		if len(r.Payload) > 0 && r.Payload[0] == 0 {
			resp = r
			break
		}
		continue
	}
	if resp == nil {
		return
	}

	time.Sleep(300 * time.Millisecond)
	after, afterTask, _ := activityTask(sess, ctx, 50)
	fin := sess.GetPlayerState()
	slog.Info("修炼后", "daily_activity", after, "task50_remain", afterTask,
		"activity_delta", after-before,
		"coin_delta", fin.Coins-st.Coins,
		"ingot_delta", fin.Ingots-st.Ingots)
}
