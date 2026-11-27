//go:build probe

// 目标: 验证「剧情副本 fight_monster (4/2) 的 PrevAct 必须取最近收到的包」假设。
//
// 背景: /tmp/story_repeat.pcapng (2026-11-27 官方客户端 s1) 显示：
//   - enter 前有 4/25，enter 后有 4/24 ([Int32 missionID][Int32 0x02F80001])；
//   - 4/2 发出后服务端不回 4/2，而是推送战斗结果 (Module 30876, ~1.8KB)；
//   - 4/2 的尾链是 0x00000029 (= 紧邻其前收到的 0/41 推送)，不是上一条请求。
//
// 线上自动化用 enter 的 LastAck 作尾链、并 Call 等待 4/2 回包，导致每次
// 战斗都被判定为静默丢包、任务进度不涨。
//
// 本探针按官方顺序发送，并用 LastRxActionID 作为 4/2 尾链，观察 3 秒内是否
// 收到战斗推送（Module 30876 或 4/x 之外的推送）。
//
// 运行命令:
//
//	SXD_CONFIG=configs/niuchangsheng.yaml SXD_PROBE_ROLE_INDEX=1 SXD_PROBE_MISSION_ID=625 \
//	  go run -tags=probe test/probes/probe_story_fight_live.go
//
// 会真实发起一场副本战斗（消耗体力），并顶掉该角色当前会话。
package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
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
	actionMissionPlotPrep  uint32 = 0x00040019 // 4/25
	actionMissionFightPrep uint32 = 0x00040018 // 4/24
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/niuchangsheng.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	idx := 0
	if v := os.Getenv("SXD_PROBE_ROLE_INDEX"); v != "" {
		if n, e := strconv.Atoi(v); e == nil && n >= 0 && n < len(acc.Roles) {
			idx = n
		}
	}
	role := acc.Roles[idx]
	mapID := int32(22)
	missionID := int32(625)
	teamID := int32(1512)
	if v := os.Getenv("SXD_PROBE_MAP_ID"); v != "" {
		if n, e := strconv.Atoi(v); e == nil {
			mapID = int32(n)
		}
	}
	if v := os.Getenv("SXD_PROBE_MISSION_ID"); v != "" {
		if n, e := strconv.Atoi(v); e == nil {
			missionID = int32(n)
		}
	}
	if v := os.Getenv("SXD_PROBE_TEAM_ID"); v != "" {
		if n, e := strconv.Atoi(v); e == nil {
			teamID = int32(n)
		}
	}

	tc := platform.NewTicketCache(90 * time.Minute)
	auth, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		return
	}
	auth.SetCookie(acc.Cookie)
	tc.RegisterAuthenticator(auth)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	tc.Invalidate(acc.Platform, acc.Username, role.ServerID)
	ticket, err := tc.GetOrFetch(ctx, acc.Platform, acc.Username, acc.Password, role.ServerID)
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
	deadline := time.Now().Add(30 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}
	slog.Info("会话已激活", "role_id", roleCfg.RoleID,
		"stamina", sess.GetBaseStamina())

	call := func(pkt *protocol.Packet, act uint32, label string) *protocol.Packet {
		slog.Info("请求", "label", label, "payload_hex", hex.EncodeToString(pkt.Payload),
			"last_rx", fmt.Sprintf("0x%08X", sess.LastRxActionID()),
			"last_ack", fmt.Sprintf("0x%08X", sess.LastAckActionID()))
		c, ccancel := context.WithTimeout(ctx, 8*time.Second)
		defer ccancel()
		resp, err := sess.Call(c, pkt, act)
		if err != nil || resp == nil {
			slog.Warn("请求失败", "label", label, "err", err)
			return nil
		}
		slog.Info("响应", "label", label, "payload_hex", hex.EncodeToString(resp.Payload))
		return resp
	}

	// 1. 4/0 get_sections
	sec := call(protocol.BuildMissionGetSectionsPacket(mapID, sess.LastAckActionID()),
		protocol.ActionIDMissionGetSections, "4/0 get_sections")
	if sec == nil {
		return
	}
	// 2. 4/25（尾链 = 最近收到的包，官方抓包为 4/0）
	call(protocol.NewPacketWithExplicitPrevAct(actionMissionPlotPrep, nil, sess.LastRxActionID()),
		actionMissionPlotPrep, "4/25 prep")
	// 3. 4/1 enter（尾链 = 最近收到的包，官方为 4/25）
	enter := call(protocol.NewPacketWithExplicitPrevAct(protocol.ActionIDMissionPlotEnter,
		int32Bytes(missionID), sess.LastRxActionID()),
		protocol.ActionIDMissionPlotEnter, "4/1 enter")
	if enter == nil || len(enter.Payload) == 0 || enter.Payload[0] != protocol.MissionResultSuccess {
		slog.Warn("进入副本未成功，终止", "payload", hex.EncodeToString(enterPayload(enter)))
		return
	}
	// 4. 4/24（8 字节：[Int32 missionID][Int32 0x02F80001]，官方无尾链）
	//    必须走 SendRaw：module 4 的 4 字节以上载荷会被会话层自动覆盖尾 4 字节。
	w := protocol.NewWriter()
	w.WriteInt32(missionID)
	w.WriteInt32(0x02F80001)
	body := w.Bytes()
	raw := make([]byte, 8+len(body))
	binary.BigEndian.PutUint32(raw[0:4], uint32(4+len(body)))
	binary.BigEndian.PutUint16(raw[4:6], uint16(actionMissionFightPrep>>16))
	binary.BigEndian.PutUint16(raw[6:8], uint16(actionMissionFightPrep&0xFFFF))
	copy(raw[8:], body)
	slog.Info("发送 4/24 fight_prep(raw)", "frame_hex", hex.EncodeToString(raw))
	if err := sess.SendRaw(raw); err != nil {
		slog.Warn("发送 4/24 失败", "err", err)
	}
	time.Sleep(300 * time.Millisecond)

	// 5. 0/41 get_player_war_cd_time [warType=13][prevAct]：官方在 fight 前 60ms 发送，
	//    fight 的尾链正是这条请求 (0x00000029)。
	warPkt := protocol.NewPacketWithExplicitPrevAct(protocol.ActionIDPlayerGetWarCdTime,
		int32Bytes(13), actionMissionFightPrep)
	slog.Info("发送 0/41 war_cd", "payload_hex", hex.EncodeToString(warPkt.Payload))
	if err := sess.Send(warPkt); err != nil {
		slog.Warn("发送 0/41 失败", "err", err)
	}
	time.Sleep(60 * time.Millisecond)

	// 6. 4/2 fight（尾链 = 0/41 请求动作 0x29）
	prev := uint32(0x00000029)
	fight := protocol.NewPacketWithExplicitPrevAct(protocol.ActionIDMissionFightMonster,
		append(append(int32Bytes(teamID), 0), int32Bytes(0)...), prev)
	slog.Info("发送 4/2 fight", "team_id", teamID, "prev_act", fmt.Sprintf("0x%08X", prev),
		"payload_hex", hex.EncodeToString(fight.Payload))
	if err := sess.Send(fight); err != nil {
		slog.Error("发送 fight 失败", "err", err)
		return
	}
	seen := map[uint32]bool{}
	for i := 0; i < 30; i++ {
		time.Sleep(100 * time.Millisecond)
		rx := sess.LastRxActionID()
		if rx != 0 && !seen[rx] {
			seen[rx] = true
			slog.Info("收到新包", "action", fmt.Sprintf("0x%08X", rx),
				"elapsed_ms", (i+1)*100)
		}
	}
	slog.Info("观察结束", "last_rx", fmt.Sprintf("0x%08X", sess.LastRxActionID()),
		"recent_rx", fmt.Sprintf("%X", sess.RecentRxActionIDs()))
}

func enterPayload(p *protocol.Packet) []byte {
	if p == nil {
		return nil
	}
	return p.Payload
}

func int32Bytes(v int32) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(v))
	return b[:]
}
