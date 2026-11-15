//go:build probe

// 目标: 隔离「3/9 任务列表同步」对 4/0 get_sections 的影响。
//
// 现象: 任务推图例程在 3/9 同步任务列表后，即使先 enter_town(裸4字节) 成功复位，
// 4/0 get_sections 仍连续超时；而单独登录(不调 3/9)+enter_town 后 4/0 正常。
// 本探针按顺序执行 3/9 → enter_town → 4/0 → (必要时再 enter_town) → 4/0，
// 并打印每步回包/耗时，用于确认毒化点与恢复方式。
//
// 运行: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_sections_after_quest_sync_live.go -role 1
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
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/niuchangsheng.yaml"
	}
	roleIdx := flag.Int("role", 1, "配置中的角色下标")
	mapID := flag.Int("map", 21, "章节地图编号")
	flag.Parse()

	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[*roleIdx]

	tc := platform.NewTicketCache(90 * time.Minute)
	xd, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		return
	}
	xd.SetCookie(acc.Cookie)
	tc.RegisterAuthenticator(xd)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	ticket, err := tc.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
	if err != nil {
		slog.Error("换票失败", "err", err)
		return
	}
	rc := client.SessionConfig{
		RoleID:               fmt.Sprintf("%s-%s", role.ServerID, role.RoleName),
		RoleName:             role.RoleName,
		Username:             acc.Username,
		ServerAddr:           ticket.GatewayURL,
		ServerID:             role.ServerID,
		Platform:             acc.Platform,
		Authenticator:        client.DefaultAuthenticator,
		MaxReconnectAttempts: 3,
	}
	client.ApplyTicket(&rc, ticket)
	sess := client.NewRoleSession(rc)
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
	slog.Info("登录完成", "town_map_id", sess.GetPlayerState().TownMapID,
		"last_action", fmt.Sprintf("0x%08X", sess.LastActionID()),
		"last_ack", fmt.Sprintf("0x%08X", sess.LastAckActionID()))

	call := func(pkt *protocol.Packet, act uint32, name string, timeout time.Duration) bool {
		cCtx, cCancel := context.WithTimeout(ctx, timeout)
		start := time.Now()
		resp, cErr := sess.Call(cCtx, pkt, act)
		cCancel()
		took := time.Since(start).Truncate(time.Millisecond)
		if cErr != nil || resp == nil {
			slog.Warn("无回包", "call", name, "took", took, "err", cErr,
				"last_action", fmt.Sprintf("0x%08X", sess.LastActionID()),
				"last_ack", fmt.Sprintf("0x%08X", sess.LastAckActionID()))
			return false
		}
		limit := len(resp.Payload)
		if limit > 40 {
			limit = 40
		}
		slog.Info("有回包", "call", name, "took", took, "len", len(resp.Payload),
			"hex", hex.EncodeToString(resp.Payload[:limit]))
		return true
	}

	time.Sleep(700 * time.Millisecond)
	// 1. 任务列表同步 (3/9)
	syncPkt := protocol.BuildGetPlayerQuestListPacket(sess.LastActionID())
	call(syncPkt, protocol.ActionIDQuestList, "quest_list_3_9", 8*time.Second)

	// 1.1 同步后直接 4/0（不 enter_town），判断是否 3/9 本身毒化
	time.Sleep(700 * time.Millisecond)
	call(protocol.BuildMissionGetSectionsPacket(int32(*mapID), sess.LastAckActionID()),
		protocol.ActionIDMissionGetSections, "get_sections_direct_after_sync", 6*time.Second)

	// 1.2 同步后 8 字节 enter_town（PrevAct=3/9 动作号）再 4/0
	syncPrev := sess.LastActionID()
	wSync := protocol.NewWriter()
	wSync.WriteInt32(int32(sess.GetPlayerState().TownMapID))
	wSync.WriteUint32(syncPrev)
	call(protocol.NewPacket(protocol.ActionIDTownEnter, wSync.Bytes()), protocol.ActionIDTownEnter,
		"enter_town_prev_quest9", 6*time.Second)
	time.Sleep(700 * time.Millisecond)
	call(protocol.BuildMissionGetSectionsPacket(int32(*mapID), sess.LastAckActionID()),
		protocol.ActionIDMissionGetSections, "get_sections_after_prev_quest9", 6*time.Second)

	// 2. enter_town(裸 4 字节)
	if _, tErr := sess.EnterTown(ctx, int32(sess.GetPlayerState().TownMapID)); tErr != nil {
		slog.Warn("enter_town 失败", "err", tErr)
	} else {
		slog.Info("enter_town 成功", "last_action", fmt.Sprintf("0x%08X", sess.LastActionID()),
			"last_ack", fmt.Sprintf("0x%08X", sess.LastAckActionID()))
	}

	// 3. get_sections
	time.Sleep(700 * time.Millisecond)
	call(protocol.BuildMissionGetSectionsPacket(int32(*mapID), sess.LastAckActionID()),
		protocol.ActionIDMissionGetSections, "get_sections_after_sync", 6*time.Second)

	// 4. 再次 enter_town 后再试
	if _, tErr := sess.EnterTown(ctx, int32(sess.GetPlayerState().TownMapID)); tErr != nil {
		slog.Warn("二次 enter_town 失败", "err", tErr)
	}
	time.Sleep(700 * time.Millisecond)
	call(protocol.BuildMissionGetSectionsPacket(int32(*mapID), sess.LastAckActionID()),
		protocol.ActionIDMissionGetSections, "get_sections_after_reenter", 6*time.Second)

	// 5. 8 字节 [Int townID, Uint32 PrevAct=enter_town] 形态
	townID := int32(sess.GetPlayerState().TownMapID)
	w := protocol.NewWriter()
	w.WriteInt32(townID)
	w.WriteUint32(protocol.ActionIDTownEnter)
	call(protocol.NewPacket(protocol.ActionIDTownEnter, w.Bytes()), protocol.ActionIDTownEnter,
		"enter_town_prev_self", 6*time.Second)
	time.Sleep(700 * time.Millisecond)
	call(protocol.BuildMissionGetSectionsPacket(int32(*mapID), sess.LastAckActionID()),
		protocol.ActionIDMissionGetSections, "get_sections_after_prev_self", 6*time.Second)

	// 6. 8 字节 [Int townID, Uint32 PrevAct=LastAction]
	w2 := protocol.NewWriter()
	w2.WriteInt32(townID)
	w2.WriteUint32(sess.LastActionID())
	call(protocol.NewPacket(protocol.ActionIDTownEnter, w2.Bytes()), protocol.ActionIDTownEnter,
		"enter_town_prev_last", 6*time.Second)
	time.Sleep(700 * time.Millisecond)
	call(protocol.BuildMissionGetSectionsPacket(int32(*mapID), sess.LastAckActionID()),
		protocol.ActionIDMissionGetSections, "get_sections_after_prev_last", 6*time.Second)
	slog.Info("探针结束")
}