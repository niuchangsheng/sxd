//go:build probe

// 目标协议: [Module 4, Action 0] get_sections & [Module 4, Action 1] enter_mission
// 基准对齐: 对齐原版 pie.exe MissionMapView 章节列表与副本进入回包布局
// 运行命令: go run -tags=probe test/probes/probe_mission_enter_live.go [missionID] [mapID]
// 预期表现: 打印章节关卡状态与 enter_mission 原始回包 Hex/逐字节布局，用于核对怪物波次偏移
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
	mapID := int32(13)
	if len(os.Args) > 1 {
		if v, err := strconv.Atoi(os.Args[1]); err == nil {
			missionID = int32(v)
		}
	}
	if len(os.Args) > 2 {
		if v, err := strconv.Atoi(os.Args[2]); err == nil {
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
	secResp, secErr := sess.Call(secCtx, secPkt, protocol.ActionIDMissionGetSections)
	secCancel()
	if secErr != nil || secResp == nil {
		slog.Error("get_sections 失败", "err", secErr)
		return
	}
	fmt.Printf("\n=== get_sections map=%d (%dB) ===\n%s\n", mapID, len(secResp.Payload), hex.EncodeToString(secResp.Payload))
	if sections, pErr := protocol.ParseMissionGetSectionsResponse(secResp.Payload); pErr == nil {
		for _, sec := range sections {
			mark := ""
			if int(sec.MissionID) == int(missionID) {
				mark = "  <== target"
			}
			fmt.Printf("  mission=%d status=%d%s\n", sec.MissionID, sec.Status, mark)
		}
	} else {
		fmt.Printf("  parse err: %v\n", pErr)
	}

	// 2. enter_mission
	enterPkt := protocol.BuildMissionPlotEnterPacket(missionID, sess.LastActionID())
	fmt.Printf("\n=== enter_mission %d request ===\n%s\n", missionID, hex.EncodeToString(enterPkt.Payload))
	enterCtx, enterCancel := context.WithTimeout(ctx, 6*time.Second)
	enterResp, enterErr := sess.Call(enterCtx, enterPkt, protocol.ActionIDMissionPlotEnter)
	enterCancel()
	if enterErr != nil || enterResp == nil {
		slog.Error("enter_mission 失败", "err", enterErr)
		return
	}
	p := enterResp.Payload
	fmt.Printf("\n=== enter_mission %d response (%dB) ===\n%s\n", missionID, len(p), hex.EncodeToString(p))
	for i := 0; i < len(p); i++ {
		fmt.Printf("%02d:%02x ", i, p[i])
		if (i+1)%12 == 0 {
			fmt.Println()
		}
	}
	fmt.Println()
}
