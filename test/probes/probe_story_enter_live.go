//go:build probe

// 目标: 抓取「已通关关卡重复挑战」时 enter_mission (4/1) 的原始回包，
// 判定服务端是否下发怪物编队，以及 payload 布局是否与首通一致。
//
// 背景: 2026-11-27 线上，任务 581 (火弩俑) 进度卡在 14/100；主线关卡 625
// 已通关，重复挑战时 4/1 成功但 4/2 fight_monster 一律静默丢弃。日志显示
// enter_mission 未下发怪物编队 (mCnt=0)，客户端回退静态字典的 150 个队伍 ID。
// 若服务端实际下发了编队但偏移不同，回退 ID 与真实队伍不符就会被丢弃。
//
// 运行命令:
//
//	SXD_CONFIG=configs/niuchangsheng.yaml SXD_PROBE_ROLE_INDEX=1 go run -tags=probe test/probes/probe_story_enter_live.go
//
// 只读探测：get_sections 与 enter_mission 不消耗体力，不发起战斗。会顶掉该角色
// 当前会话，运行前请确认该角色没有正在进行的玩法。
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
	slog.Info("会话已激活", "role_id", roleCfg.RoleID)

	call := func(pkt *protocol.Packet, act uint32, label string) *protocol.Packet {
		slog.Info("请求载荷", "label", label, "payload_hex", hex.EncodeToString(pkt.Payload),
			"last_ack", fmt.Sprintf("0x%08X", sess.LastAckActionID()),
			"last_business", fmt.Sprintf("0x%08X", sess.LastBusinessActionID()))
		c, ccancel := context.WithTimeout(ctx, 8*time.Second)
		defer ccancel()
		resp, err := sess.Call(c, pkt, act)
		if err != nil || resp == nil {
			slog.Warn("请求失败", "label", label, "err", err)
			return nil
		}
		slog.Info("请求成功", "label", label, "payload_len", len(resp.Payload),
			"payload_hex", hex.EncodeToString(resp.Payload))
		return resp
	}

	// 与线上推图一致：先拉章节列表建立 Mod 4 锚点，再进入副本。
	sec := call(protocol.BuildMissionGetSectionsPacket(mapID, sess.LastAckActionID()),
		protocol.ActionIDMissionGetSections, "get_sections 4/0")
	if sec == nil {
		slog.Warn("章节列表无回包，仍尝试直接进入副本")
	}
	enter := call(protocol.BuildMissionPlotEnterPacket(missionID, sess.LastAckActionID()),
		protocol.ActionIDMissionPlotEnter, "enter_mission 4/1")
	if enter == nil {
		slog.Error("进入副本无回包，无法继续")
		return
	}
	p := enter.Payload
	if len(p) < 20 {
		slog.Warn("回包过短", "len", len(p))
		return
	}
	status := p[0]
	mid := int32(binary.BigEndian.Uint32(p[1:5]))
	stam := int32(binary.BigEndian.Uint32(p[5:9]))
	b := p[9]
	i1 := int32(binary.BigEndian.Uint32(p[10:14]))
	i2 := int32(binary.BigEndian.Uint32(p[14:18]))
	mCnt := binary.BigEndian.Uint16(p[18:20])
	slog.Info("enter_mission 解析", "status", status, "mission_id", mid, "stamina", stam,
		"byte", b, "i1", i1, "i2", i2, "monster_count", mCnt, "payload_len", len(p))
	teams := make([]int32, 0, mCnt)
	for i := uint16(0); i < mCnt && 20+int(i)*4+4 <= len(p); i++ {
		teams = append(teams, int32(binary.BigEndian.Uint32(p[20+int(i)*4:])))
	}
	slog.Info("enter_mission 怪物编队", "teams", teams, "remaining_after_teams", len(p)-20-int(mCnt)*4)
	if len(p) > 20+int(mCnt)*4 {
		slog.Info("enter_mission 尾部字节", "tail_hex", hex.EncodeToString(p[20+int(mCnt)*4:]))
	}
}
