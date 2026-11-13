//go:build probe

// 目标: 定位 s1 英雄副本列表 35/0 全城镇静默丢包的根因。
//
// 背景: niuchangsheng s1 (等级 94) 的多城镇英雄副本扫荡对 54/43/40... 全部
// 8 秒超时；同账号 s0 (等级 300) 首个城镇可正常回包。本探针在 s1 上对比：
//
//	35/8 特殊英雄副本列表 (空请求)
//	35/0 当前城镇/目标城镇 [Uint16 town + PrevAct] (现行构造)
//	35/0 纯 [Uint16 town] (无 PrevAct)
//	35/0 [Uint16 town + 固定 PrevAct=42]
//	35/0 [Uint16 town + 最后一次真实回包动作]
//
// 运行: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_hero_mission_list_live.go -role 1
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
	roleIdx := flag.Int("role", 1, "配置中的角色下标 (0=s0, 1=s1)")
	flag.Parse()

	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	if *roleIdx < 0 || *roleIdx >= len(acc.Roles) {
		slog.Error("角色下标越界", "role", *roleIdx, "roles", len(acc.Roles))
		return
	}
	role := acc.Roles[*roleIdx]

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
	slog.Info("会话已激活", "role_id", sess.RoleID(), "level", sess.GetPlayerState().Level,
		"town_map_id", sess.GetPlayerState().TownMapID,
		"last_action", fmt.Sprintf("0x%08X", sess.LastActionID()),
		"last_ack", fmt.Sprintf("0x%08X", sess.LastAckActionID()))

	call := func(pkt *protocol.Packet, act uint32, name string) *protocol.Packet {
		cCtx, cCancel := context.WithTimeout(ctx, 6*time.Second)
		defer cCancel()
		resp, cErr := sess.Call(cCtx, pkt, act)
		if cErr != nil {
			slog.Warn("无回包", "call", name, "err", cErr)
			return nil
		}
		limit := len(resp.Payload)
		if limit > 50 {
			limit = 50
		}
		slog.Info("有回包", "call", name, "len", len(resp.Payload),
			"hex", hex.EncodeToString(resp.Payload[:limit]))
		return resp
	}

	// 1. 特殊英雄副本列表 (35/8，空请求)。
	call(protocol.NewPacket(protocol.ActionHeroMissionSpecialList, nil),
		protocol.ActionHeroMissionSpecialList, "special_list")

	town := func(id uint16, prev uint32, withPrevFlag bool) *protocol.Packet {
		w := protocol.NewWriter()
		w.WriteUint16(id)
		if prev != 0 {
			w.WriteUint32(prev)
		}
		pkt := protocol.NewPacket(protocol.ActionHeroMissionList, w.Bytes())
		pkt.HasPrevAct = withPrevFlag
		return pkt
	}

	cur := uint16(sess.GetPlayerState().TownMapID)
	for _, tc := range []struct {
		name string
		town uint16
		pkt  *protocol.Packet
	}{
		{"cur_town_builder", cur, protocol.BuildHeroMissionListPacket(cur)},
		{"town54_builder", 54, protocol.BuildHeroMissionListPacket(54)},
		{"town54_bare", 54, town(54, 0, false)},
		{"town54_prev42", 54, town(54, 42, false)},
		{"town54_prev_ack", 54, town(54, sess.LastAckActionID(), false)},
	} {
		call(tc.pkt, protocol.ActionHeroMissionList, tc.name)
		time.Sleep(time.Second)
	}

	// 2. Module 25 get_status 载荷对比：裸 [Int32 mission] vs [mission + PrevAct]。
	// 会话层会对 mod==25 且长度>=4 的载荷自动覆盖末尾 4 字节，
	// 这里先把 lastActionID 置 0 以保留原始载荷。
	const missionID int32 = 166
	statusPkt := func(payload []byte) *protocol.Packet {
		return protocol.NewPacket(protocol.ActionIDPracticeGetStatus, payload)
	}
	missionBytes := []byte{
		byte(missionID >> 24), byte(missionID >> 16), byte(missionID >> 8), byte(missionID),
	}
	for _, tc := range []struct {
		name    string
		payload []byte
	}{
		{"status_bare_mission", missionBytes},
		{"status_mission_prev42", append(append([]byte{}, missionBytes...), 0, 0, 0, 42)},
		{"status_mission_prev0", append(append([]byte{}, missionBytes...), 0, 0, 0, 0)},
		// 官方抓包 c2s_m25_a0.bin 使用的 PrevAct=0x00040019 (Module 4 Act 25)。
		{"status_mission_prev_sections", append(append([]byte{}, missionBytes...), 0x00, 0x04, 0x00, 0x19)},
		{"status_sections_anchor", append(append([]byte{}, missionBytes...), 0x00, 0x04, 0x00, 0x19)},
	} {
		sess.SetLastActionID(0) // 禁止会话层改写末尾 4 字节
		if tc.name == "status_sections_anchor" {
			// 模拟现行代码：lastActionID=0x00040019 时由会话层覆盖末尾 4 字节。
			sess.SetLastActionID(0x00040019)
		}
		call(statusPkt(tc.payload), protocol.ActionIDPracticeGetStatus, tc.name)
		time.Sleep(time.Second)
	}

	// 2b. Module 25 start_practice 载荷对比（裸 7 字节 vs 带 PrevAct 11 字节）。
	// 先用裸状态查询拿到一个真实可扫荡 mission。
	if stResp := call(statusPkt(missionBytes), protocol.ActionIDPracticeGetStatus, "status_for_start"); stResp != nil {
		if st, stErr := protocol.ParsePracticeStatusResult(stResp.Payload); stErr == nil && len(st.Missions) > 0 {
			best := st.Missions[0]
			startBytes := func(prev uint32) []byte {
				out := []byte{
					byte(best.MissionID >> 24), byte(best.MissionID >> 16), byte(best.MissionID >> 8), byte(best.MissionID),
					0, 1, // count=1
					1, // auto_sale
				}
				if prev != 0 {
					out = append(out, byte(prev>>24), byte(prev>>16), byte(prev>>8), byte(prev))
				}
				return out
			}
			startOK := false
			for _, tc := range []struct {
				name string
				prev uint32
			}{
				{"start25_bare", 0},
				{"start25_prev_status", protocol.ActionIDPracticeGetStatus},
				{"start25_prev42", 42},
			} {
				sess.SetLastActionID(0)
				if call(protocol.NewPacket(protocol.ActionIDPracticeStart, startBytes(tc.prev)),
					protocol.ActionIDPracticeStart, tc.name) != nil {
					startOK = true
					break
				}
				time.Sleep(time.Second)
			}
			if startOK {
				quickBytes := func(prev uint32) []byte {
					out := []byte{0, 0} // count=0
					if prev != 0 {
						out = append(out, byte(prev>>24), byte(prev>>16), byte(prev>>8), byte(prev))
					}
					return out
				}
				for _, tc := range []struct {
					name string
					prev uint32
				}{
					{"quickly25_bare", 0},
					{"quickly25_prev_start", protocol.ActionIDPracticeStart},
					{"quickly25_prev_status", protocol.ActionIDPracticeGetStatus},
				} {
					sess.SetLastActionID(0)
					if call(protocol.NewPacket(protocol.ActionIDPracticeQuickly, quickBytes(tc.prev)),
						protocol.ActionIDPracticeQuickly, tc.name) != nil {
						break
					}
					time.Sleep(time.Second)
				}
			}
		}
	}

	// 3. Module 35 start_practice / quickly 载荷对比。
	// start 是写操作：只在首个有回包的形态上继续 quickly，避免重复开扫荡。
	startPkt := func(town uint16, auto, typ byte, prev uint32) *protocol.Packet {
		w := protocol.NewWriter()
		w.WriteUint16(town)
		w.WriteUint8(auto)
		w.WriteUint8(typ)
		if prev != 0 {
			w.WriteUint32(prev)
		}
		pkt := protocol.NewPacket(protocol.ActionHeroMissionStartPractice, w.Bytes())
		pkt.HasPrevAct = false
		return pkt
	}
	started := false
	for _, tc := range []struct {
		name string
		prev uint32
	}{
		{"start_bare", 0},
		{"start_prev_list", protocol.ActionHeroMissionList},
		{"start_prev42", 42},
	} {
		sess.SetLastActionID(0)
		if call(startPkt(54, 0, 0, tc.prev), protocol.ActionHeroMissionStartPractice, tc.name) != nil {
			started = true
			break
		}
		time.Sleep(time.Second)
	}
	if started {
		quickPkt := func(prev uint32) *protocol.Packet {
			w := protocol.NewWriter()
			w.WriteInt16(0)
			if prev != 0 {
				w.WriteUint32(prev)
			}
			pkt := protocol.NewPacket(protocol.ActionHeroMissionQuickly, w.Bytes())
			pkt.HasPrevAct = false
			return pkt
		}
		for _, tc := range []struct {
			name string
			prev uint32
		}{
			{"quickly_bare", 0},
			{"quickly_prev_start", protocol.ActionHeroMissionStartPractice},
		} {
			sess.SetLastActionID(0)
			if call(quickPkt(tc.prev), protocol.ActionHeroMissionQuickly, tc.name) != nil {
				break
			}
			time.Sleep(time.Second)
		}
	}
	slog.Info("探针结束")
}
