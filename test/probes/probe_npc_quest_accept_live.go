//go:build probe

// 目标: 验证任务 478 的接取链路，确认 npc_info_by_npcid 的参数应为城镇 NPC 行号而非静态 NPC 编号。
//
// 背景: 自动化在 s1 直接 accept_quest(478) 与 npc_info_by_npcid(107) 均无回包。
// NPCposition 中静态编号 107 (阴阳王) 的行号是 275，而行号 107 是另一名 NPC。
// 官方 NPCDialogView 传给 npc_info_by_npcid 的是 townNPCId (NPCposition.id)。
//
// 验证步骤:
//  1. 同步任务列表，确认 478 状态；
//  2. npc_info_by_npcid(275) 是否回包并列出 478；
//  3. 以 npc_info 动作号为 PrevAct 发送 accept_quest(478)，观察结果码。
//
// 运行: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_npc_quest_accept_live.go -role 1
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
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/niuchangsheng.yaml"
	}
	roleIdx := flag.Int("role", 1, "配置中的角色下标 (0=s0, 1=s1)")
	questID := flag.Int("quest", 478, "待验证的任务 ID")
	flag.Parse()

	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[*roleIdx]

	ticketCache := platform.NewTicketCache(90 * time.Minute)
	xdAuth, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		return
	}
	xdAuth.SetCookie(acc.Cookie)
	ticketCache.RegisterAuthenticator(xdAuth)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
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
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()
	deadline := time.Now().Add(40 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}

	repo, rErr := dictionary.NewRepository("")
	if rErr != nil {
		slog.Error("打开数据字典失败", "err", rErr)
		return
	}
	defer repo.Close()
	q, qErr := repo.GetQuest(*questID)
	if qErr != nil {
		slog.Error("查询任务字典失败", "quest", *questID, "err", qErr)
		return
	}
	townNPCID, tErr := repo.GetTownNPCIDByNPCID(q.BeginNPCID)
	if tErr != nil {
		slog.Error("解析城镇 NPC 失败", "npc_id", q.BeginNPCID, "err", tErr)
		return
	}
	slog.Info("任务与 NPC 映射", "quest", q.ID, "title", q.Title,
		"begin_npc_id", q.BeginNPCID, "town_npc_id", townNPCID)

	items, sErr := sess.SyncQuests(ctx)
	if sErr != nil {
		slog.Error("同步任务列表失败", "err", sErr)
		return
	}
	for _, it := range items {
		if int32(it.QuestID) == int32(*questID) {
			slog.Info("任务当前状态", "quest", it.QuestID, "state", it.State)
		}
	}

	call := func(pkt *protocol.Packet, act uint32, name string) *protocol.Packet {
		cCtx, cCancel := context.WithTimeout(ctx, 8*time.Second)
		defer cCancel()
		resp, cErr := sess.Call(cCtx, pkt, act)
		if cErr != nil {
			slog.Warn("无回包", "call", name, "action", fmt.Sprintf("0x%08X", act), "err", cErr)
			return nil
		}
		limit := len(resp.Payload)
		if limit > 80 {
			limit = 80
		}
		slog.Info("有回包", "call", name, "len", len(resp.Payload), "hex", hex.EncodeToString(resp.Payload[:limit]))
		return resp
	}

	npcPkt, _ := protocol.BuildGetNPCQuestsPacket(protocol.GetNPCQuestsRequest{
		NPCID:   int32(townNPCID),
		PrevAct: sess.LastActionID(),
	})
	npcResp := call(npcPkt, protocol.ActionIDQuestGetNPCQuests, "npc_info")
	if npcResp == nil {
		slog.Info("探针结束: 城镇 NPC 对话仍无回包")
		return
	}
	if list, pErr := protocol.ParseNPCQuestsResponse(npcResp.Payload); pErr == nil {
		slog.Info("NPC 任务列表", "count", len(list), "items", list)
	}

	accPkt, _ := protocol.BuildAcceptQuestPacket(protocol.AcceptQuestRequest{
		QuestID: int32(*questID),
		PrevAct: sess.LastActionID(),
	})
	if resp := call(accPkt, protocol.ActionIDQuestAccept, "accept_quest"); resp != nil && len(resp.Payload) > 0 {
		slog.Info("accept_quest 结果码", "result", resp.Payload[0])
	}
	slog.Info("探针结束")
}
