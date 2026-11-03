//go:build probe

// 目标协议: [Module 3] Mod_Quest_Base 任务接取链路
//   - Action 1  npc_info_by_npcid  打开 NPC 对话并拉取该 NPC 的任务列表
//   - Action 3  accept_quest       接受任务
//   - Action 9  town_quest_show    拉取当前城镇任务状态
//
// 基准对齐: 官方客户端 Main.swf NPCDialogView.npc_info_by_npcid -> QuestView.accept_quest
// 以及 DataBase.netCall 在请求末尾追加 4B PrevAct (上一请求的 Module+Action) 的原子链规则。
//
// 运行命令: go run -tags=probe test/probes/probe_quest_accept_live.go [questId]
// 预期表现:
//  1. 打印 town_quest_show 中目标任务的原始状态 (0=Acceptable, 1=Accepted, 2=Completed);
//  2. 先直接发送 accept_quest，若超时则复现「缺少 NPC 对话前置导致网关静默丢弃」;
//  3. 再按官方流程先发 npc_info_by_npcid 建立对话，随后发送 accept_quest，观察是否收到结果码。
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
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelDebug)

	targetQuest := int32(0)
	if len(os.Args) > 1 {
		if v, err := strconv.Atoi(os.Args[1]); err == nil {
			targetQuest = int32(v)
		}
	}

	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[0]

	dictRepo, err := dictionary.NewRepository("")
	if err != nil {
		slog.Error("挂载 Pieb.db 数据字典失败", "err", err)
		return
	}
	defer dictRepo.Close()

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

	// 强制换取新票，避免复用登录期失效的旧凭据导致 Action 72 握手超时。
	ticketCache.Invalidate(acc.Platform, acc.Username, role.ServerID)
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
		MaxReconnectAttempts: 1,
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
	sess.SetPacketObserver(func(pkt *protocol.Packet) {
		payload := hex.EncodeToString(pkt.Payload)
		if len(payload) > 96 {
			payload = payload[:96] + "..."
		}
		fmt.Printf("  <- 0x%08X (%dB) %s\n", pkt.ActionID, len(pkt.Payload), payload)
	})
	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()

	deadline := time.Now().Add(15 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未能在时限内激活", "state", sess.State())
		return
	}
	slog.Info("会话已激活", "role", sess.OriginNickName(), "level", sess.GetPlayerState().Level)

	// 1. town_quest_show：定位目标任务并确认其状态。
	items, err := sess.SyncQuests(ctx)
	if err != nil {
		slog.Error("town_quest_show 失败", "err", err)
		return
	}
	var target *protocol.PlayerQuestItem
	for i := range items {
		it := items[i]
		slog.Info("城镇任务条目", "quest_id", it.QuestID, "state", it.State,
			"need_type", it.NeedType, "current", it.CurrentCount, "max", it.MaxCount)
		if it.State == 0 && (targetQuest == 0 || it.QuestID == targetQuest) && target == nil {
			target = &items[i]
		}
	}
	if target == nil {
		slog.Info("未找到可接取任务 (state=0)，退出探针", "target_quest", targetQuest)
		return
	}

	q, _ := dictRepo.GetQuest(int(target.QuestID))
	beginNPC := int32(0)
	if q != nil {
		beginNPC = int32(q.BeginNPCID)
		slog.Info("目标任务字典映射", "quest_id", q.ID, "title", q.Title,
			"type", q.Type, "level", q.Level, "begin_npc_id", q.BeginNPCID,
			"end_npc_id", q.EndNPCID, "mission_id", q.MissionID)
	}

	accept := func(label string) (*protocol.Packet, error) {
		pkt, _ := protocol.BuildAcceptQuestPacket(protocol.AcceptQuestRequest{
			QuestID: target.QuestID,
			PrevAct: sess.LastActionID(),
		})
		cCtx, cCancel := context.WithTimeout(ctx, 4*time.Second)
		defer cCancel()
		fmt.Printf("== %s: 发送 accept_quest quest=%d prevAct=0x%08X\n", label, target.QuestID, sess.LastActionID())
		resp, callErr := sess.Call(cCtx, pkt, protocol.ActionIDQuestAccept)
		if callErr != nil {
			fmt.Printf("== %s: 失败 err=%v\n", label, callErr)
			return nil, callErr
		}
		fmt.Printf("== %s: 回包 (%dB) %s\n", label, len(resp.Payload), hex.EncodeToString(resp.Payload))
		return resp, nil
	}

	// 2. 直接 accept_quest：复现当前 Routine 的调用方式。
	if _, err := accept("直接接取"); err == nil {
		fmt.Println("直接接取已成功，无需 NPC 对话前置")
		return
	}

	// 3. 官方流程：先 npc_info_by_npcid 打开 NPC 对话，再 accept_quest。
	if beginNPC <= 0 {
		slog.Warn("任务缺少 BeginNPCId，无法建立 NPC 对话前置", "quest_id", target.QuestID)
		return
	}
	npcPkt, _ := protocol.BuildGetNPCQuestsPacket(protocol.GetNPCQuestsRequest{
		NPCID:   beginNPC,
		PrevAct: sess.LastActionID(),
	})
	npcCtx, npcCancel := context.WithTimeout(ctx, 4*time.Second)
	fmt.Printf("== NPC 对话: 发送 npc_info_by_npcid npc=%d prevAct=0x%08X\n", beginNPC, sess.LastActionID())
	npcResp, npcErr := sess.Call(npcCtx, npcPkt, protocol.ActionIDQuestGetNPCQuests)
	npcCancel()
	if npcErr != nil {
		slog.Error("npc_info_by_npcid 失败", "npc_id", beginNPC, "err", npcErr)
		return
	}
	fmt.Printf("== NPC 对话: 回包 (%dB) %s\n", len(npcResp.Payload), hex.EncodeToString(npcResp.Payload))
	if npcItems, pErr := protocol.ParseNPCQuestsResponse(npcResp.Payload); pErr == nil {
		for _, it := range npcItems {
			slog.Info("NPC 任务条目", "npc_id", beginNPC, "quest_id", it.QuestID, "state", it.State)
		}
	} else {
		slog.Warn("解析 NPC 任务列表失败", "err", pErr)
	}

	accept("NPC 对话后接取")
}
