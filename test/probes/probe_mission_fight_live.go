//go:build probe

// 目标协议: [Module 4] get_sections -> enter_mission -> fight_monster 首通链路
// 基准对齐: 对齐原版 pie.exe MissionMapView 当前章节首通挑战
// 运行命令: go run -tags=probe test/probes/probe_mission_fight_live.go [missionID] [monsterTeamID] [mapID]
// 预期表现: 打印章节状态、enter_mission 回包与 fight_monster 战斗结果码，验证 mCnt=0 时是否应回退字典怪物编队
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
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)

	missionID := int32(352)
	monsterTeamID := int32(954)
	mapID := int32(13)
	if len(os.Args) > 1 {
		if v, err := strconv.Atoi(os.Args[1]); err == nil {
			missionID = int32(v)
		}
	}
	if len(os.Args) > 2 {
		if v, err := strconv.Atoi(os.Args[2]); err == nil {
			monsterTeamID = int32(v)
		}
	}
	if len(os.Args) > 3 {
		if v, err := strconv.Atoi(os.Args[3]); err == nil {
			mapID = int32(v)
		}
	}

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

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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
	slog.Info("会话已激活", "role", sess.OriginNickName(), "level", sess.GetPlayerState().Level, "stamina", sess.GetStamina())

	// 1. get_sections
	secPkt := protocol.BuildMissionGetSectionsPacket(mapID, sess.LastActionID())
	secCtx, secCancel := context.WithTimeout(ctx, 5*time.Second)
	_, _ = sess.Call(secCtx, secPkt, protocol.ActionIDMissionGetSections)
	secCancel()

	// 2. enter_mission
	enterPkt := protocol.BuildMissionPlotEnterPacket(missionID, sess.LastActionID())
	enterCtx, enterCancel := context.WithTimeout(ctx, 6*time.Second)
	enterResp, enterErr := sess.Call(enterCtx, enterPkt, protocol.ActionIDMissionPlotEnter)
	enterCancel()
	if enterErr != nil || enterResp == nil {
		slog.Error("enter_mission 失败", "err", enterErr)
		return
	}
	fmt.Printf("\nenter %d resp (%dB): %s\n", missionID, len(enterResp.Payload), hex.EncodeToString(enterResp.Payload))

	// 3. fight_monster (2026 网关 13B 格式: [Int32 mt][Int8 attackType][Int32 hireRoleID][Uint32 prevAct])
	fw := protocol.NewWriter()
	fw.WriteInt32(monsterTeamID)
	fw.WriteInt8(0)
	fw.WriteInt32(0)
	fw.WriteUint32(sess.LastActionID())
	fightPkt := protocol.NewPacket(protocol.ActionIDMissionFightMonster, fw.Bytes())
	fmt.Printf("fight request: %s\n", hex.EncodeToString(fightPkt.Payload))
	fightCtx, fightCancel := context.WithTimeout(ctx, 15*time.Second)
	fightResp, fightErr := sess.Call(fightCtx, fightPkt, protocol.ActionIDMissionFightMonster)
	fightCancel()
	if fightErr != nil || fightResp == nil {
		slog.Error("fight_monster 失败", "err", fightErr)
		return
	}
	p := fightResp.Payload
	last := p[len(p)-1]
	fmt.Printf("\nfight team=%d resp (%dB) last=0x%02X\n%s\n", monsterTeamID, len(p), last, hex.EncodeToString(p))
}
