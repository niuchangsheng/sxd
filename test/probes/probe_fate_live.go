//go:build probe

// 目标协议: 猎命 (Mod_Fate_Base, Module 22)
//
//	get_fate_npc (Act 0) response: [Byte, [Byte,Byte,Byte,Byte,Short], Int]
//	appoint_fate_npc (Act 9) request=[Byte fate_npc_id]，
//	  response=[UByte result, Short, Byte, Long]，SUCCESS=0。
//
// 每日活跃度任务 4「免费猎命」每日 1 次 +5。本探针先列猎命师，选择
// 费用为 0 的免费猎命师；若无免费项则不做任何消耗。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_fate_live.go
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

const (
	actionFateNPC     uint32 = 0x00160000
	actionFateAppoint uint32 = 0x00160009
	fateSuccess       uint8  = 0
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
	earned, task4 := 0, 0
	for _, row := range active {
		if row[0] > 83 {
			continue
		}
		earned += int(row[1] * row[3])
		if row[0] == 4 {
			task4 = int(row[2] - row[1])
		}
	}
	return earned, task4, true
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

	infoPkt := protocol.NewPacket(actionFateNPC, []byte{0, 0, 0, 0})
	infoPkt.HasPrevAct = true
	resp := call(infoPkt, actionFateNPC, 8*time.Second)
	if resp == nil {
		slog.Error("查询猎命师失败")
		return
	}
	slog.Info("猎命师原始回包", "hex", fmt.Sprintf("%x", resp.Payload))
	r := protocol.NewReader(resp.Payload)
	first, err := r.ReadUint8()
	if err != nil {
		slog.Error("解析猎命师首字段失败", "err", err)
		return
	}
	n, err := r.ReadUint16()
	if err != nil {
		slog.Error("解析猎命师数量失败", "err", err)
		return
	}
	type npc struct {
		id       uint8
		costType uint8
		cost     uint8
		flag     uint8
		extra    int16
	}
	npcs := make([]npc, 0, n)
	for i := 0; i < int(n); i++ {
		var x npc
		x.id, _ = r.ReadUint8()
		x.costType, _ = r.ReadUint8()
		x.cost, _ = r.ReadUint8()
		x.flag, _ = r.ReadUint8()
		x.extra, _ = r.ReadInt16()
		npcs = append(npcs, x)
	}
	tail, _ := r.ReadInt32()
	slog.Info("猎命师面板", "first", first, "count", len(npcs), "tail", tail)
	var free *npc
	for i := range npcs {
		x := &npcs[i]
		slog.Info("猎命师", "id", x.id, "cost_type", x.costType, "cost", x.cost, "flag", x.flag, "extra", x.extra)
		if x.cost == 0 && x.costType == 0 {
			free = x
		}
	}
	if free == nil {
		slog.Info("未发现免费猎命师，跳过 (不消耗元宝)")
		return
	}

	_, remainTask, _ := dailyActivity(sess, ctx)
	if remainTask <= 0 {
		slog.Info("今日免费猎命活跃度已完成")
		return
	}
	before := sess.GetPlayerState()
	beforeAct, _, _ := dailyActivity(sess, ctx)
	apptPkt := protocol.NewPacket(actionFateAppoint, []byte{free.id})
	apptResp := call(apptPkt, actionFateAppoint, 10*time.Second)
	if apptResp == nil || len(apptResp.Payload) == 0 {
		slog.Warn("免费猎命无回包")
		return
	}
	slog.Info("免费猎命结果", "npc_id", free.id, "result", apptResp.Payload[0],
		"hex", fmt.Sprintf("%x", apptResp.Payload))
	after := sess.GetPlayerState()
	afterAct, afterTask, _ := dailyActivity(sess, ctx)
	slog.Info("猎命完成",
		"task4_remain_after", afterTask,
		"daily_before", beforeAct,
		"daily_after", afterAct,
		"activity_delta", afterAct-beforeAct,
		"coin_delta", after.Coins-before.Coins,
		"ingot_delta", after.Ingots-before.Ingots,
	)
}