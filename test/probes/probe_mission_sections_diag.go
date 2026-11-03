//go:build probe

// 目标协议: [Module 4, Action 0] get_sections 与 [Module 3, Action 9] town_quest_show 的 PrevAct 链配合
// 基准对齐: 原版 pie.exe 网关 PrevAct 原子链 (用于定位“请求已发出但无回包”是拒绝、丢包还是回包 ActionID 漂移)
// 运行命令: SXD_CONFIG=configs/config.yaml go run -tags=probe test/probes/probe_mission_sections_diag.go [mapID]
// 预期表现: 分别在同一次登录内于「GetInfo 之后」与「town_quest_show 之后」各发一次 get_sections,
//           打印每次的原始回包 Hex、耗时，并全量观察入站 ActionID，从而区分载荷被拒与回包编号漂移。
// 安全性: 仅发送只读查询（get_sections / town_quest_show），不进入副本、不消耗体力、不改变任务状态。
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)

	mapID := int32(13)
	if len(os.Args) > 1 {
		if v, err := strconv.Atoi(os.Args[1]); err == nil {
			mapID = int32(v)
		}
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
		MaxReconnectAttempts: 2,
	}
	client.ApplyTicket(&roleCfg, ticket)

	sess := client.NewRoleSession(roleCfg)

	var mu sync.Mutex
	inbound := make([]string, 0, 32)
	sess.SetPacketObserver(func(pkt *protocol.Packet) {
		mu.Lock()
		defer mu.Unlock()
		inbound = append(inbound, fmt.Sprintf("0x%08X(%dB)", pkt.ActionID, len(pkt.Payload)))
	})
	drainInbound := func() []string {
		mu.Lock()
		defer mu.Unlock()
		got := append([]string(nil), inbound...)
		inbound = inbound[:0]
		return got
	}

	if err := sess.Start(ctx); err != nil {
		slog.Error("启动会话失败", "err", err)
		return
	}
	defer sess.Close()

	deadline := time.Now().Add(20 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未能在时限内激活", "state", sess.State())
		return
	}
	fmt.Printf("\n=== 会话已激活 role=%s level=%d stamina=%d last_action=0x%08X ===\n",
		sess.OriginNickName(), sess.GetPlayerState().Level, sess.GetStamina(), sess.LastActionID())
	drainInbound()

	callSections := func(label string) {
		prev := sess.LastActionID()
		pkt := protocol.BuildMissionGetSectionsPacket(mapID, prev)
		start := time.Now()
		callCtx, callCancel := context.WithTimeout(ctx, 6*time.Second)
		resp, callErr := sess.Call(callCtx, pkt, protocol.ActionIDMissionGetSections)
		callCancel()
		elapsed := time.Since(start).Round(time.Millisecond)
		fmt.Printf("\n=== %s ===\nprev_act=0x%08X payload=%s elapsed=%s\n", label, prev, hex.EncodeToString(pkt.Payload), elapsed)
		if callErr != nil || resp == nil {
			fmt.Printf("<<< NO MATCHED RESPONSE err=%v\n", callErr)
		} else {
			fmt.Printf("<<< response action=0x%08X payload=%s\n", resp.ActionID, hex.EncodeToString(resp.Payload))
			if sections, pErr := protocol.ParseMissionGetSectionsResponse(resp.Payload); pErr == nil {
				for _, sec := range sections {
					fmt.Printf("  mission=%d status=%d\n", sec.MissionID, sec.Status)
				}
			} else {
				fmt.Printf("  parse err: %v\n", pErr)
			}
		}
		// 排空可能延迟到达、甚至编号漂移的回包。
		time.Sleep(1500 * time.Millisecond)
		fmt.Printf("  inbound during call: %v\n", drainInbound())
	}

	// 1. 紧接登录 GetInfo 之后: 官方客户端打开章节列表时的典型时序。
	callSections("[1] get_sections 紧接 GetInfo 之后")

	// 1.5 心跳包对 PrevAct 链的影响: 线上推图在多波次战斗间隙会偶发心跳,
	// 若心跳推进服务端原子链而客户端 lastActionID 未同步, 后续 Mod 4 请求会被静默丢弃。
	idle := 1600 * time.Millisecond
	fmt.Printf("\n等待 %s 空闲后发送一次心跳包 (模拟战斗波次间隙)...\n", idle)
	time.Sleep(idle)
	hbPrev := sess.LastActionID()
	hbPayload := make([]byte, 4)
	hbPayload[0] = byte(hbPrev >> 24)
	hbPayload[1] = byte(hbPrev >> 16)
	hbPayload[2] = byte(hbPrev >> 8)
	hbPayload[3] = byte(hbPrev)
	if hbErr := sess.Send(protocol.NewPacket(protocol.ActionHeartbeat, hbPayload)); hbErr != nil {
		fmt.Printf("!!! 心跳发送失败: %v\n", hbErr)
	}
	time.Sleep(400 * time.Millisecond)
	fmt.Printf("心跳已发送 prev_act=0x%08X, inbound=%v, client_last_action=0x%08X\n",
		hbPrev, drainInbound(), sess.LastActionID())
	callSections("[1.5] get_sections 紧接心跳包之后")

	// 2. 紧接 town_quest_show 之后: 自动化 Routine 的真实时序。
	if _, qErr := sess.SyncQuests(ctx); qErr != nil {
		fmt.Printf("\n!!! SyncQuests 失败: %v\n", qErr)
	} else {
		fmt.Printf("\n=== SyncQuests 成功, last_action=0x%08X ===\n", sess.LastActionID())
	}
	time.Sleep(300 * time.Millisecond)
	drainInbound()
	callSections("[2] get_sections 紧接 town_quest_show 之后")

	fmt.Printf("\n=== 诊断结束, session_state=%s ===\n", sess.State())
}