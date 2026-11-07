//go:build probe

// 目标: 确认 Mod_NpcFriendship_Base.answer_question (436/8) 的参数语义。
//
// 官方客户端 NpcFriendShipView.onSendAnswer 会发送 [param1, param2, ANSWER_x]，
// 但资产回调的参数顺序未在反编译源码中体现。本探针按候选顺序逐一尝试，
// 成功即停止，避免消耗当天问答。
//
// 运行命令: SXD_CONFIG=configs/s1.yaml go run -tags=probe test/probes/probe_npc_answer_live.go
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
		cfgPath = "configs/s1.yaml"
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

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
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

	fetch := func() *protocol.NpcFriendshipSnapshot {
		resp := call(protocol.BuildNpcFriendshipInfoPacket(), protocol.ActionIDNpcFriendshipInfo, 6*time.Second)
		if resp == nil {
			return nil
		}
		snap, err := protocol.ParseNpcFriendshipInfoResult(resp.Payload)
		if err != nil {
			slog.Warn("解析失败", "err", err)
			return nil
		}
		for _, n := range snap.Npcs {
			slog.Info("NPC",
				"npc_id", n.NpcID, "exp", n.CurExp, "friend", n.IsFriend,
				"mission_type", n.MissionType, "taken", n.HasTakeMission,
				"complete", n.IsComplete, "receive", n.IsReceive,
				"question_id", n.QuestionID, "town_npc_id", n.TownNpcID,
				"mission_item_id", n.MissionItemID)
		}
		return snap
	}

	snap := fetch()
	if snap == nil {
		return
	}
	var target protocol.NpcFriendshipInfo
	found := false
	for _, n := range snap.Npcs {
		if n.QuestionID > 0 {
			target = n
			found = true
			break
		}
	}
	if !found {
		slog.Info("当前没有待回答的 NPC 问答")
		return
	}
	best, ok := protocol.NpcFriendshipBestAnswer(target.QuestionID)
	if !ok {
		slog.Info("未知问题编号", "question_id", target.QuestionID)
		return
	}
	slog.Info("目标 NPC", "npc_id", target.NpcID, "question_id", target.QuestionID,
		"mission_type", target.MissionType, "best_answer", best.Answer, "award", best.Award)

	send := func(label string, args ...int32) uint8 {
		w := protocol.NewWriter()
		for i, v := range args {
			if i == 2 {
				// 协议第三字段为 UByte (ANSWER_A/B/C 或 0/1/2)。
				w.WriteUint8(uint8(v))
			} else {
				w.WriteInt32(v)
			}
		}
		resp := call(protocol.NewPacket(protocol.ActionIDNpcFriendshipAnswerQuestion, w.Bytes()),
			protocol.ActionIDNpcFriendshipAnswerQuestion, 6*time.Second)
		if resp == nil {
			slog.Info("问答无回包", "variant", label)
			return 255
		}
		if len(resp.Payload) == 0 {
			return 255
		}
		slog.Info("问答结果", "variant", label, "args", args,
			"result", resp.Payload[0], "hex", hex.EncodeToString(resp.Payload))
		return resp.Payload[0]
	}

	// 候选 1: [npc_id, question_id, ANSWER_x]
	if send("npc,question,answer_code", target.NpcID, target.QuestionID, int32(best.Answer)) == 0 {
		return
	}
	// 重新读取，确认问题仍在。
	snap = fetch()
	if snap != nil {
		still := false
		for _, n := range snap.Npcs {
			if n.NpcID == target.NpcID && n.QuestionID > 0 {
				target.QuestionID = n.QuestionID
				still = true
				break
			}
		}
		if !still {
			slog.Info("问答已不再存在，停止")
			return
		}
	}

	// 候选 2: [question_id, npc_id, ANSWER_x]
	if send("question,npc,answer_code", target.QuestionID, target.NpcID, int32(best.Answer)) == 0 {
		return
	}
	// 候选 3: [npc_id, question_id, answer_index(0/1/2)]
	idx := int32(0)
	switch best.Answer {
	case protocol.NpcFriendshipResultAnswerB:
		idx = 1
	case protocol.NpcFriendshipResultAnswerC:
		idx = 2
	}
	if send("npc,q,answer_index", target.NpcID, target.QuestionID, idx) == 0 {
		return
	}
	// 候选 4: [npc_id, mission_type, ANSWER_x]
	if send("npc,mission,answer_code", target.NpcID, target.MissionType, int32(best.Answer)) == 0 {
		return
	}
	slog.Info("所有候选参数顺序均未成功", "npc_id", target.NpcID, "question_id", target.QuestionID)
}
