//go:build probe

// 目标协议: [Module 57] Mod_BeelzebubTrials_Base 战后离场 × [Module 4, Action 0] Mod_Mission_Base.get_sections
// 基准对齐: 魔王试炼结束后剧情副本协议 (0x00040000) 被服务端静默丢弃的根因定位
// 运行命令: VARIANT=A go run -tags=probe test/probes/probe_beelzebub_mod4.go
//   VARIANT=A: create_team -> fight -> war_result -> leave_war(0) -> exit_team -> get_sections
//   VARIANT=B: create_team -> fight -> war_result -> get_sections (不离场)
//   VARIANT=C: create_team -> exit_team -> get_sections (不战斗)
//   VARIANT=N: 不进行魔王试炼，仅基线 get_sections
// 预期表现: 定位导致 Mod 4 失效的步骤，并依次尝试恢复手段 (exit_team 重试 / leave_war(bossID))
package main

import (
	"context"
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
	slog.SetLogLoggerLevel(slog.LevelDebug)
	variant := os.Getenv("VARIANT")
	if variant == "" {
		variant = "A"
	}

	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[0]

	xdAuth, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		return
	}
	xdAuth.SetCookie(acc.Cookie)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	ticket, err := xdAuth.Login(ctx, acc.Username, acc.Password, role.ServerID)
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
		fmt.Printf("会话未就绪: %v\n", sess.State())
		return
	}

	getSections := func(tag string) bool {
		pkt := protocol.BuildMissionGetSectionsPacket(9, sess.LastActionID())
		cCtx, cCancel := context.WithTimeout(ctx, 4*time.Second)
		defer cCancel()
		resp, err := sess.Call(cCtx, pkt, protocol.ActionIDMissionGetSections)
		ok := err == nil && resp != nil
		n := 0
		if resp != nil {
			n = len(resp.Payload)
		}
		fmt.Printf(">>> [%s] get_sections ok=%v len=%d err=%v\n", tag, ok, n, err)
		return ok
	}

	fmt.Printf("=== VARIANT %s ===\n", variant)
	var bossID int32
	if variant != "N" {
		canList, err := sess.BeelzebubTrialsGetCanChallengeList(ctx)
		if err != nil || len(canList) == 0 {
			fmt.Printf("get_can_challenge_list err=%v len=%d\n", err, len(canList))
			return
		}
		for _, id := range canList {
			if bossID == 0 || protocol.BeelzebubTrialsBossLevel(id) < protocol.BeelzebubTrialsBossLevel(bossID) {
				bossID = id
			}
		}
		cRes, cErr := sess.BeelzebubTrialsCreateTeam(ctx, bossID, protocol.BeelzebubTrialsNotLimit)
		fmt.Printf("create_team boss=%d res=%d err=%v\n", bossID, cRes, cErr)

		switch variant {
		case "A", "B":
			fRes, fErr := sess.BeelzebubTrialsFight(ctx)
			fmt.Printf("fight res=%d err=%v\n", fRes, fErr)
			time.Sleep(500 * time.Millisecond)
			wr, wErr := sess.BeelzebubTrialsGetWarResult(ctx)
			fmt.Printf("war_result=%+v err=%v\n", wr, wErr)
			if variant == "A" {
				_ = sess.BeelzebubTrialsLeaveWar(ctx, 0)
				_ = sess.BeelzebubTrialsExitTeam(ctx)
			}
		case "C":
			_ = sess.BeelzebubTrialsExitTeam(ctx)
		}
	}

	if getSections("after-flow") {
		return
	}

	// 恢复手段 1: 重试 exit_team
	_ = sess.BeelzebubTrialsExitTeam(ctx)
	if getSections("after-exit_team-retry") {
		return
	}
	// 恢复手段 2: leave_war(bossID)
	_ = sess.BeelzebubTrialsLeaveWar(ctx, bossID)
	time.Sleep(300 * time.Millisecond)
	if getSections("after-leave_war-boss") {
		return
	}
	// 恢复手段 3: 关闭魔王试炼 (Module 57, Action 12)
	_ = sess.Send(protocol.NewPacket(protocol.ActionIDBeelzebubTrialsClose, nil))
	time.Sleep(300 * time.Millisecond)
	getSections("after-close-panel")
}
