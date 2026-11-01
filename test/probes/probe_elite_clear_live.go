//go:build probe

// 目标协议: [Module 4] 精英副本首通闭环 (get_sections -> plot_enter -> fight_monster -> rank -> open_box -> pickup)
// 基准对齐: 原版 pie.exe 精英副本任务 (Quests.Type=4 -> Missions.MissionType=1) 与剧情副本共用 Mod_Mission_Base 状态机
// 运行命令: SXD_CONFIG=configs/s1.yaml go run -tags=probe test/probes/probe_elite_clear_live.go [missionID]
// 预期表现: 打印进入/战斗/结算每步回包 Hex 与任务 336/337 状态变化, 用于确认精英任务应走 Mod 4 首通链路。
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

// eliteMissionMonsters 精英关卡 -> 怪物编队 (Pieb.db Missions.monsters 静态配置)。
var eliteMissionMonsters = map[int32]int32{
	455: 1040,
	456: 1041,
	457: 1042,
}

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)

	missionID := int32(456)
	if len(os.Args) > 1 {
		if v, err := strconv.Atoi(os.Args[1]); err == nil {
			missionID = int32(v)
		}
	}
	monsterTeam, ok := eliteMissionMonsters[missionID]
	if !ok {
		slog.Error("探针未配置该精英关卡的怪物编队", "mission_id", missionID)
		return
	}

	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/config.yaml"
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
	printQuests(ctx, sess, "登录后")

	call := func(label string, pkt *protocol.Packet, respAction uint32, timeout time.Duration) *protocol.Packet {
		c, cc := context.WithTimeout(ctx, timeout)
		resp, cErr := sess.Call(c, pkt, respAction)
		cc()
		if cErr != nil || resp == nil {
			fmt.Printf("[%s] no response: %v (session_state=%s)\n", label, cErr, sess.State())
			return nil
		}
		fmt.Printf("[%s] req=%s resp(%dB)=%s\n", label, hex.EncodeToString(pkt.Payload), len(resp.Payload), hex.EncodeToString(resp.Payload))
		return resp
	}

	// 0. 建立章节上下文
	call("get_sections", protocol.BuildMissionGetSectionsPacket(13, sess.LastActionID()), protocol.ActionIDMissionGetSections, 5*time.Second)

	// 1. 进入精英副本
	enterResp := call("plot_enter", protocol.BuildMissionPlotEnterPacket(missionID, sess.LastActionID()), protocol.ActionIDMissionPlotEnter, 5*time.Second)
	if enterResp == nil || len(enterResp.Payload) == 0 {
		return
	}
	if enterResp.Payload[0] != protocol.MissionResultSuccess {
		fmt.Printf("plot_enter status=0x%02X, abort\n", enterResp.Payload[0])
		return
	}

	// 2. 挑战怪物编队
	time.Sleep(500 * time.Millisecond)
	fightResp := call("fight_monster", protocol.BuildMissionFightMonsterPacket(monsterTeam, 0, 0, sess.LastActionID()), protocol.ActionIDMissionFightMonster, 8*time.Second)
	if fightResp == nil || len(fightResp.Payload) == 0 {
		return
	}
	fmt.Printf("fight result=0x%02X\n", fightResp.Payload[len(fightResp.Payload)-1])

	// 3. 结算与领奖
	time.Sleep(700 * time.Millisecond)
	call("rank", protocol.BuildMissionRankPacket(sess.LastActionID()), protocol.ActionIDMissionRank, 4*time.Second)
	time.Sleep(500 * time.Millisecond)
	call("open_box", protocol.BuildMissionOpenBoxPacket(sess.LastActionID()), protocol.ActionIDMissionOpenBox, 4*time.Second)
	time.Sleep(500 * time.Millisecond)
	call("pickup", protocol.BuildMissionPickupAwardPacket(sess.LastActionID()), protocol.ActionIDMissionPickupAward, 4*time.Second)

	printQuests(ctx, sess, "通关后")
}

func printQuests(ctx context.Context, sess *client.RoleSession, label string) {
	items, err := sess.SyncQuests(ctx)
	if err != nil {
		slog.Warn("同步任务列表失败", "label", label, "err", err)
		return
	}
	fmt.Printf("--- quests %s ---\n", label)
	for _, it := range items {
		if it.QuestID == 336 || it.QuestID == 337 {
			fmt.Printf("quest=%d state=%d current=%d max=%d\n", it.QuestID, it.State, it.CurrentCount, it.MaxCount)
		}
	}
}
