//go:build probe

// 目标协议: [Module 111] 英雄副本列表/扫荡 & [Module 35] 英雄关卡列表 & [Module 4] get_sections & [Module 3] 任务列表
// 基准对齐: 对齐原版 pie.exe 精英副本任务闭环 (Quests.Type=4, Missions.MissionType=1)
// 运行命令: SXD_CONFIG=configs/s1.yaml go run -tags=probe test/probes/probe_elite_quest_live.go
// 预期表现: 打印精英任务 336/337 状态、Module 111 英雄副本列表、Module 35 关卡列表与 map=13/29 章节状态，
//
//	用于判定精英任务应通过英雄副本扫荡还是章节首通协议推进。
package main

import (
	"context"
	"encoding/hex"
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
	st := sess.GetPlayerState()
	slog.Info("会话已激活", "role", sess.OriginNickName(), "level", st.Level, "stamina", sess.GetStamina())

	// 1. 任务列表：确认精英任务状态
	items, err := sess.SyncQuests(ctx)
	if err != nil {
		slog.Warn("同步任务列表失败", "err", err)
	}
	for _, it := range items {
		fmt.Printf("quest=%d state=%d current=%d max=%d\n", it.QuestID, it.State, it.CurrentCount, it.MaxCount)
	}

	// 2. Module 35 英雄副本列表 (Mod_HeroMission_Base.get_hero_mission_list)
	fmt.Println("\n=== Mod 35 (0x00230000) hero mission list town=4 ===")
	listCtx, listCancel := context.WithTimeout(ctx, 5*time.Second)
	listResp, listErr := sess.Call(listCtx, protocol.BuildHeroMissionListPacket(4), protocol.ActionHeroMissionList)
	listCancel()
	if listErr != nil || listResp == nil {
		slog.Warn("Mod 35 列表请求失败", "err", listErr)
	} else {
		fmt.Printf("resp (%dB): %s\n", len(listResp.Payload), hex.EncodeToString(listResp.Payload))
		if hl, pErr := protocol.ParseHeroMissionListResponse(listResp.Payload); pErr == nil {
			fmt.Printf("missions=%d all_buy_times=%d remain_buy_times=%d buy_price=%d finish_num=%d\n",
				len(hl.Missions), hl.AllBuyTimes, hl.RemainBuyTimes, hl.BuyPrice, hl.FinishMissionNum)
			for _, h := range hl.Missions {
				fmt.Printf("  mission=%d can_challenge=%d rank=%d finished=%d\n",
					h.MissionID, h.CanChallenge, h.Rank, h.Finished)
			}
		} else {
			fmt.Printf("parse err: %v\n", pErr)
		}
	}

	// 3. Module 35 英雄副本列表 (另一个城镇，验证多城镇精英副本覆盖)
	for _, townID := range []uint16{13, 29} {
		fmt.Printf("\n=== Mod 35 (0x00230000) hero mission list town=%d ===\n", townID)
		pkt := protocol.BuildHeroMissionListPacket(townID)
		c, cc := context.WithTimeout(ctx, 4*time.Second)
		resp, cErr := sess.Call(c, pkt, protocol.ActionHeroMissionList)
		cc()
		if cErr != nil || resp == nil {
			fmt.Printf("no response: %v\n", cErr)
			continue
		}
		fmt.Printf("resp (%dB): %s\n", len(resp.Payload), hex.EncodeToString(resp.Payload))
	}

	// 4. map=13 与 map=29 章节状态，确认 456/457 是否在章节列表中
	for _, mapID := range []int32{13, 29} {
		fmt.Printf("\n=== Mod 4 get_sections map=%d ===\n", mapID)
		secPkt := protocol.BuildMissionGetSectionsPacket(mapID, sess.LastActionID())
		c, cc := context.WithTimeout(ctx, 4*time.Second)
		resp, cErr := sess.Call(c, secPkt, protocol.ActionIDMissionGetSections)
		cc()
		if cErr != nil || resp == nil {
			fmt.Printf("no response: %v\n", cErr)
			continue
		}
		fmt.Printf("resp (%dB): %s\n", len(resp.Payload), hex.EncodeToString(resp.Payload))
		if secs, pErr := protocol.ParseMissionGetSectionsResponse(resp.Payload); pErr == nil {
			for _, sec := range secs {
				if sec.MissionID >= 440 || mapID == 29 {
					fmt.Printf("  mission=%d status=%d is_boss=%d count=%d\n", sec.MissionID, sec.Status, sec.IsBoss, sec.CompleteCount)
				}
			}
		}
	}
}

func uint32Payload(v uint32) []byte {
	w := protocol.NewWriter()
	w.WriteUint32(v)
	return w.Bytes()
}
