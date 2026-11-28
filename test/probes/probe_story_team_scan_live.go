//go:build probe

// 目标: 扫描剧情副本重复挑战 (4/2) 可接受的怪物队伍 ID。
//
// 背景: 官方抓包 (/tmp/story_repeat.pcapng) 中任务 648/651/654 的战斗队伍
// 为 1940/1948/1957，恰好等于「任务ID+99」对应的神魔战场任务怪物，但该
// 规律套到任务 625→724(1832) 上被服务端拒绝。本探针在同一会话内依次尝试
// 候选队伍 ID，观察哪一个能收到 4/2 回包（整场战斗日志）。
//
// 运行:
//
//	SXD_CONFIG=configs/niuchangsheng.yaml SXD_PROBE_ROLE_INDEX=1 \
//	  SXD_PROBE_MISSION_ID=625 SXD_PROBE_TEAM_IDS=1512,1840,1841,1940,1948,1963 \
//	  go run -tags=probe test/probes/probe_story_team_scan_live.go
//
// 先停服；会真实进入副本并尝试战斗（成功时消耗体力）。
package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
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
	var teams []int32
	for _, s := range strings.Split(envOr("SXD_PROBE_TEAM_IDS", "1512,1840,1841,1940,1948,1963"), ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if n, e := strconv.Atoi(s); e == nil {
			teams = append(teams, int32(n))
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
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
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
	slog.Info("会话已激活", "role_id", roleCfg.RoleID, "stamina", sess.GetBaseStamina())

	// 进入副本前置序列（与官方抓包一致）。
	if _, err := sess.Call(ctxTimeout(ctx, 8*time.Second),
		protocol.BuildMissionGetSectionsPacket(mapID, sess.LastAckActionID()),
		protocol.ActionIDMissionGetSections); err != nil {
		slog.Error("get_sections 失败", "err", err)
		return
	}
	if _, err := sess.Call(ctxTimeout(ctx, 8*time.Second),
		protocol.NewPacketWithExplicitPrevAct(actionMissionPlotPrep, nil, sess.LastRxActionID()),
		actionMissionPlotPrep); err != nil {
		slog.Warn("4/25 无回包", "err", err)
	}
	enterResp, err := sess.Call(ctxTimeout(ctx, 8*time.Second),
		protocol.NewPacketWithExplicitPrevAct(protocol.ActionIDMissionPlotEnter,
			int32Bytes(missionID), sess.LastRxActionID()),
		protocol.ActionIDMissionPlotEnter)
	if err != nil || enterResp == nil || len(enterResp.Payload) == 0 || enterResp.Payload[0] != protocol.MissionResultSuccess {
		slog.Error("进入副本失败", "err", err)
		return
	}
	w := protocol.NewWriter()
	w.WriteInt32(missionID)
	w.WriteInt32(0x02F80001)
	body := w.Bytes()
	raw := make([]byte, 8+len(body))
	binary.BigEndian.PutUint32(raw[0:4], uint32(4+len(body)))
	binary.BigEndian.PutUint16(raw[4:6], uint16(actionMissionFightPrep>>16))
	binary.BigEndian.PutUint16(raw[6:8], uint16(actionMissionFightPrep&0xFFFF))
	copy(raw[8:], body)
	_ = sess.SendRaw(raw)
	time.Sleep(300 * time.Millisecond)

	// 逐个候选队伍发起战斗，观察是否收到 4/2 回包。
	for _, team := range teams {
		war := protocol.NewPacketWithExplicitPrevAct(protocol.ActionIDPlayerGetWarCdTime,
			int32Bytes(13), sess.LastBusinessActionID())
		_ = sess.Send(war)
		time.Sleep(60 * time.Millisecond)
		fight := protocol.NewPacketWithExplicitPrevAct(protocol.ActionIDMissionFightMonster,
			append(append(int32Bytes(team), 0), int32Bytes(0)...), 0x00000029)
		slog.Info("尝试队伍", "team_id", team,
			"fight_payload", hex.EncodeToString(fight.Payload),
			"last_business", fmt.Sprintf("0x%08X", sess.LastBusinessActionID()))
		_ = sess.Send(fight)
		accepted := false
		for i := 0; i < 30; i++ {
			time.Sleep(100 * time.Millisecond)
			for _, a := range sess.RecentRxActionIDs() {
				if a == protocol.ActionIDMissionFightMonster {
					accepted = true
					break
				}
			}
			if accepted {
				break
			}
		}
		if accepted {
			slog.Info("队伍被接受（收到 4/2 战斗回包）", "team_id", team)
			return
		}
		slog.Info("队伍无回包，继续下一个", "team_id", team)
		time.Sleep(300 * time.Millisecond)
	}
	slog.Info("所有候选均无回包", "recent_rx", fmt.Sprintf("%X", sess.RecentRxActionIDs()))
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func int32Bytes(v int32) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(v))
	return b[:]
}

func ctxTimeout(parent context.Context, d time.Duration) context.Context {
	c, _ := context.WithTimeout(parent, d)
	return c
}