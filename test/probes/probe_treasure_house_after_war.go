//go:build probe

// 目标协议: [Module 57] 魔王试炼战斗离场 × [Module 594] 天帝宝库 set_use_free_flag
// 基准对齐: 排查「完整服务器一轮日常后，天帝宝库 Act 4 被服务端静默丢弃，
// 而独立新会话同一载荷可正常返回」的会话状态差异。
//
// 运行命令: go run -tags=probe test/probes/probe_treasure_house_after_war.go -config configs/s1.yaml
// 观察点:
//  1. 先执行一轮魔王试炼建队->战斗->离场->退队，模拟日常流程中的战场状态
//  2. 随后执行生产 TreasureHouse Routine，观察 Act 4 是否仍能收到回包
//
// 注意: 会真实发起一场魔王试炼战斗（不消耗元宝）并执行一次宝库自选（不抽奖）。
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/config"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/platform"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/minigame"
)

func main() {
	cfgPath := flag.String("config", "configs/s1.yaml", "配置文件路径")
	bossID := flag.Int("boss", 7, "用于模拟战斗的魔王编号")
	step := flag.String("step", "full", "魔王试炼步骤: none|create|war|full")
	closePanel := flag.Bool("close-panel", false, "魔王试炼流程结束后补发 close_beelzebub_trials (Act 12)")
	settle := flag.Duration("settle", 300*time.Millisecond, "魔王试炼流程结束到执行宝库之间的等待时长")
	enterTown := flag.Bool("enter-town", false, "魔王试炼流程结束后发送 enter_town 复位城镇")
	leaveTown := flag.Bool("leave-town", false, "enter_town 之前先发送 leave_town (Mod 1 Act 1)")
	townID := flag.Int("town", 0, "enter_town 使用的城镇编号 (0 表示使用角色 townMapId)")
	prevOverride := flag.String("prevact", "", "enter_town 前强制设置 LastActionID (十六进制)，为空表示不覆盖")
	sendHeartbeat := flag.Bool("heartbeat", false, "enter_town 前先发送一次心跳，验证心跳能否重锚定 PrevAct 链")
	reanchor := flag.Bool("reanchor", false, "enter_town 前先拉取一次 get_player_info (Mod 0 Act 2) 重锚定 PrevAct 链")
	flag.Parse()

	slog.SetLogLoggerLevel(slog.LevelDebug)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		return
	}
	acc := cfg.Accounts[0]
	role := acc.Roles[0]

	dictRepo, err := dictionary.NewRepository("")
	if err != nil {
		slog.Error("挂载数据字典失败", "err", err)
		return
	}
	defer dictRepo.Close()

	ticketCache := platform.NewTicketCache(90 * time.Minute)
	xdAuth, err := platform.NewXindongAuthenticator()
	if err != nil {
		slog.Error("创建认证器失败", "err", err)
		return
	}
	xdAuth.SetCookie(acc.Cookie)
	ticketCache.RegisterAuthenticator(xdAuth)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
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

	deadline := time.Now().Add(20 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		slog.Error("会话未激活", "state", sess.State())
		return
	}

	// 1. 执行一轮魔王试炼战斗并完整离场，模拟日常流程。
	//    -step 用于二分定位哪一步导致后续 Act 4 被静默丢弃。
	if *step == "check" {
		ok, cErr := sess.BeelzebubTrialsCheckCanChallenge(ctx)
		slog.Info("魔王试炼资格查询", "can_challenge", ok, "err", cErr)
		if *enterTown && *reanchor {
			if info, rErr := sess.FetchLatestPlayerInfo(ctx); rErr != nil {
				slog.Warn("重锚定 get_player_info 失败", "err", rErr)
			} else {
				slog.Info("已拉取 get_player_info 重锚定 PrevAct 链", "level", info.Level,
					"last_action", fmt.Sprintf("0x%08X", sess.LastActionID()))
			}
		}
		if *enterTown && *sendHeartbeat {
			if hbErr := sess.Send(protocol.NewPacket(protocol.ActionHeartbeat, make([]byte, 4))); hbErr != nil {
				slog.Warn("发送心跳失败", "err", hbErr)
			} else {
				slog.Info("已发送心跳重锚定 PrevAct 链", "last_action", fmt.Sprintf("0x%08X", sess.LastActionID()))
			}
			select {
			case <-ctx.Done():
			case <-time.After(200 * time.Millisecond):
			}
		}
		if *enterTown {
			if *prevOverride != "" {
				var v uint64
				_, _ = fmt.Sscanf(*prevOverride, "%x", &v)
				sess.SetLastActionID(uint32(v))
				slog.Info("强制覆盖 LastActionID", "action", fmt.Sprintf("0x%08X", uint32(v)))
			}
			tid := int32(*townID)
			if tid == 0 {
				tid = int32(sess.GetPlayerState().TownMapID)
			}
			res, eErr := sess.EnterTown(ctx, tid)
			slog.Info("enter_town 结果", "town_id", tid, "result", res, "err", eErr)
		}
	}
	if *step != "none" && *step != "check" {
		if res, cErr := sess.BeelzebubTrialsCreateTeam(ctx, int32(*bossID), protocol.BeelzebubTrialsNotLimit); cErr != nil {
			slog.Warn("魔王试炼建队失败，继续宝库测试", "result", res, "err", cErr)
		} else if *step == "create" {
			_ = sess.BeelzebubTrialsExitTeam(ctx)
		} else {
			_, _ = sess.BeelzebubTrialsFight(ctx)
			select {
			case <-ctx.Done():
			case <-time.After(800 * time.Millisecond):
			}
			war, wErr := sess.BeelzebubTrialsGetWarResult(ctx)
			slog.Info("魔王试炼战斗结算", "war", fmt.Sprintf("%+v", war), "err", wErr, "step", *step)
			if *step == "full" {
				_ = sess.BeelzebubTrialsLeaveWar(ctx, 0)
			}
			_ = sess.BeelzebubTrialsExitTeam(ctx)
			select {
			case <-ctx.Done():
			case <-time.After(500 * time.Millisecond):
			}
		}
		if *step != "create" {
			select {
			case <-ctx.Done():
			case <-time.After(300 * time.Millisecond):
			}
		}
		if *closePanel {
			_ = sess.Send(protocol.NewPacket(protocol.ActionIDBeelzebubTrialsClose, nil))
			select {
			case <-ctx.Done():
			case <-time.After(300 * time.Millisecond):
			}
		}
		if *settle > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(*settle):
			}
		}
		if tid, _, teamErr := sess.BeelzebubTrialsGetPlayerTeamInfo(ctx); teamErr != nil {
			slog.Info("魔王试炼队伍状态查询", "err", teamErr)
		} else {
			slog.Info("魔王试炼队伍状态查询", "team_id", tid)
		}
		if *leaveTown {
			lCtx, lCancel := context.WithTimeout(ctx, 3*time.Second)
			resp, lErr := sess.Call(lCtx, protocol.NewPacket(protocol.ActionIDTownLeave, nil), protocol.ActionIDTownLeave)
			lCancel()
			if lErr != nil {
				slog.Info("leave_town 结果", "err", lErr)
			} else if resp != nil {
				slog.Info("leave_town 结果", "payload", fmt.Sprintf("%x", resp.Payload))
			}
			select {
			case <-ctx.Done():
			case <-time.After(300 * time.Millisecond):
			}
		}
		if *enterTown && *reanchor {
			if info, rErr := sess.FetchLatestPlayerInfo(ctx); rErr != nil {
				slog.Warn("重锚定 get_player_info 失败", "err", rErr)
			} else {
				slog.Info("已拉取 get_player_info 重锚定 PrevAct 链", "level", info.Level,
					"last_action", fmt.Sprintf("0x%08X", sess.LastActionID()))
			}
		}
		if *enterTown && *sendHeartbeat {
			if hbErr := sess.Send(protocol.NewPacket(protocol.ActionHeartbeat, make([]byte, 4))); hbErr != nil {
				slog.Warn("发送心跳失败", "err", hbErr)
			} else {
				slog.Info("已发送心跳重锚定 PrevAct 链", "last_action", fmt.Sprintf("0x%08X", sess.LastActionID()))
			}
			select {
			case <-ctx.Done():
			case <-time.After(200 * time.Millisecond):
			}
		}
		if *enterTown {
			if *prevOverride != "" {
				var v uint64
				_, _ = fmt.Sscanf(*prevOverride, "%x", &v)
				sess.SetLastActionID(uint32(v))
				slog.Info("强制覆盖 LastActionID", "action", fmt.Sprintf("0x%08X", uint32(v)))
			}
			tid := int32(*townID)
			if tid == 0 {
				tid = int32(sess.GetPlayerState().TownMapID)
			}
			res, eErr := sess.EnterTown(ctx, tid)
			slog.Info("enter_town 结果", "town_id", tid, "result", res, "err", eErr)
			select {
			case <-ctx.Done():
			case <-time.After(500 * time.Millisecond):
			}
		}
	}

	// 2. 宝库 Routine：观察 Act 4 是否收到回包。
	thCfg := minigame.TreasureHouseConfig{
		DictRepo:     dictRepo,
		Awards:       cfg.Scheduler.TreasureHouse.ResolveSelections(),
		MaxDrawTimes: 0, // 不抽奖
	}
	if err := minigame.NewTreasureHouseRoutine(thCfg).Execute(ctx, sess, nil); err != nil {
		slog.Error("TreasureHouse Routine 执行失败", "err", err)
	}
}
