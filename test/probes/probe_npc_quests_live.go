//go:build probe

// 目标协议: [Module 3, Action 1] get_npc_quests (查询指定 NPC 可接/可交任务)
// 基准对齐: 对齐原版 pie.exe NPCQuest 对话框任务列表
// 运行命令: go run -tags=probe test/probes/probe_npc_quests_live.go
// 预期表现: 登录后依次查询主线程当前 NPC，输出每个 NPC 返回的任务状态 (1=可接取 0=不可接取 2=可交付)
package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelDebug)

	cfg, err := config.Load("configs/config.yaml")
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

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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
		MaxReconnectAttempts: 1,
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)
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

	// 当前主线任务 NPC (Quest 251 BeginNPCId=82)，以及相邻任务 NPC 与城镇常驻任务 NPC。
	npcIDs := []int32{82, 83, 81, 67, 21, 38, 48, 125, 9, 13, 14}

	for _, npcID := range npcIDs {
		pkt, _ := protocol.BuildGetNPCQuestsPacket(protocol.GetNPCQuestsRequest{
			NPCID:   npcID,
			PrevAct: sess.LastActionID(),
		})
		cCtx, cCancel := context.WithTimeout(ctx, 4*time.Second)
		resp, callErr := sess.Call(cCtx, pkt, protocol.ActionIDQuestGetNPCQuests)
		cCancel()
		if callErr != nil {
			slog.Warn("查询 NPC 任务失败", "npc_id", npcID, "err", callErr)
			continue
		}
		if resp == nil {
			slog.Warn("查询 NPC 任务回包为空", "npc_id", npcID)
			continue
		}
		items, pErr := protocol.ParseNPCQuestsResponse(resp.Payload)
		if pErr != nil {
			slog.Warn("解析 NPC 任务回包失败", "npc_id", npcID, "err", pErr, "hex", fmt.Sprintf("%x", resp.Payload))
			continue
		}
		slog.Info("NPC 任务列表", "npc_id", npcID, "count", len(items), "raw_len", len(resp.Payload))
		for _, it := range items {
			slog.Info("  NPC 任务", "npc_id", npcID, "quest_id", it.QuestID, "state", it.State)
		}
	}
}
