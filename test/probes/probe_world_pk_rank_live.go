//go:build probe

// 目标协议: 阶位赛 (Mod_WorldPkRanking_Base, Module 349 / 0x015D)
//
// 每日活跃度任务 72「每日阶位赛胜利」每次胜利 +50，上限 2 次（合计 +100）。
//
//	get_status   (Act 0, 0x015D0000) response=[UByte status, Short, Int, Int, Int]
//	open_panel   (Act 2, 0x015D0002) response 首字节为结果码
//	join_pk      (Act 3, 0x015D0003) request=[] response=[UByte result, Int, Int]
//	notify_war_report (Act 5, 0x015D0005) 服务端异步推送战报
//
// 本探针只发送不消耗元宝的查询/参赛请求，并在推送战报或活跃度变化后立即退出。
//
// 运行命令: SXD_CONFIG=configs/s0.yaml go run -tags=probe test/probes/probe_world_pk_rank_live.go
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
	actionWorldPkStatus uint32 = 0x015D0000
	actionWorldPkPanel  uint32 = 0x015D0002
	actionWorldPkJoin   uint32 = 0x015D0003
	actionWorldPkReport uint32 = 0x015D0005
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
	earned, task72 := 0, 0
	for _, row := range active {
		if row[0] > 83 {
			continue
		}
		earned += int(row[1] * row[3])
		if row[0] == 72 {
			task72 = int(row[2] - row[1])
		}
	}
	return earned, task72, true
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

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Second)
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

	reportCh := make(chan *protocol.Packet, 4)
	sess.RegisterHandler(actionWorldPkReport, func(p *protocol.Packet) {
		select {
		case reportCh <- p:
		default:
		}
	})
	sess.SetPacketObserver(func(p *protocol.Packet) {
		if p.ActionID>>16 != 349 {
			return
		}
		head := p.Payload
		if len(head) > 32 {
			head = head[:32]
		}
		slog.Info("阶位赛推送", "action", fmt.Sprintf("0x%08X", p.ActionID),
			"len", len(p.Payload), "head", hex.EncodeToString(head))
	})

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

	before := sess.GetPlayerState()
	beforeAct, beforeTask, _ := dailyActivity(sess, ctx)
	slog.Info("阶位赛前", "daily_activity", beforeAct, "task72_remain", beforeTask,
		"ingots", before.Ingots, "coins", before.Coins)

	statusPkt := protocol.NewPacket(actionWorldPkStatus, nil)
	statusPkt.HasPrevAct = true
	if resp := call(statusPkt, actionWorldPkStatus, 8*time.Second); resp != nil {
		slog.Info("阶位赛状态", "hex", hex.EncodeToString(resp.Payload))
		if len(resp.Payload) > 0 && resp.Payload[0] != 0 {
			slog.Info("阶位赛未开放，停止", "status", resp.Payload[0])
			return
		}
	}

	panelPkt := protocol.NewPacket(actionWorldPkPanel, nil)
	panelPkt.HasPrevAct = true
	if resp := call(panelPkt, actionWorldPkPanel, 10*time.Second); resp != nil {
		head := resp.Payload
		if len(head) > 24 {
			head = head[:24]
		}
		slog.Info("阶位赛面板", "len", len(resp.Payload), "head", hex.EncodeToString(head))
	}

	joinPkt := protocol.NewPacket(actionWorldPkJoin, nil)
	joinPkt.HasPrevAct = true
	joinResp := call(joinPkt, actionWorldPkJoin, 10*time.Second)
	if joinResp == nil {
		slog.Error("阶位赛参赛无回包")
		return
	}
	slog.Info("阶位赛参赛", "hex", hex.EncodeToString(joinResp.Payload))
	if len(joinResp.Payload) > 0 && joinResp.Payload[0] != 3 {
		slog.Info("阶位赛参赛未成功，按结果码退出", "result", joinResp.Payload[0])
		return
	}

	// 等待服务端异步下发战报，或活跃度增加后立即结束。
	waitDeadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(waitDeadline) && ctx.Err() == nil {
		select {
		case p := <-reportCh:
			head := p.Payload
			if len(head) > 48 {
				head = head[:48]
			}
			slog.Info("收到阶位赛战报", "len", len(p.Payload), "head", hex.EncodeToString(head))
		case <-time.After(5 * time.Second):
		}
		cur, curTask, ok := dailyActivity(sess, ctx)
		if ok && cur != beforeAct {
			slog.Info("活跃度增加，结束等待", "daily_activity", cur)
			break
		}
		if ok && curTask == 0 {
			slog.Info("阶位赛任务已完成")
			break
		}
	}

	after := sess.GetPlayerState()
	afterAct, afterTask, _ := dailyActivity(sess, ctx)
	slog.Info("阶位赛完成",
		"task72_remain_after", afterTask,
		"daily_before", beforeAct,
		"daily_after", afterAct,
		"activity_delta", afterAct-beforeAct,
		"coin_delta", after.Coins-before.Coins,
		"ingot_delta", after.Ingots-before.Ingots,
	)
}
