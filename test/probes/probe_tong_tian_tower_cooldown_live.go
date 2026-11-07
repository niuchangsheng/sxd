//go:build probe

// 目标协议: 通天塔挑战冷却 (Mod_TongTianTower_Base, Module 648)
//
//	[Module 648, Act 0] panel_info 0x02880000  request=[]  -> 当前层/剩余次数
//	[Module 648, Act 1] challenge  0x02880001  request=[[UByte team_type, Int monster_team_id]]
//	  结果码 6=CD_TIME 冷却中，其余见 TongTianTowerResult*
//
// 目的: 测量连续挑战触发 CD_TIME 后，需要等待多久才能再次挑战成功，
// 为通天塔例程的每日 15 次自动挑战提供冷却退避依据。
//
// 运行命令: SXD_CONFIG=configs/niuchangsheng.yaml go run -tags=probe test/probes/probe_tong_tian_tower_cooldown_live.go
// 预期表现: 打印每次挑战结果与 CD 后重试的等待时长；最多消耗 5 次成功挑战。
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

const probeMaxSuccess = 5

func main() {
	slog.SetLogLoggerLevel(slog.LevelInfo)
	cfgPath := os.Getenv("SXD_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/s0.yaml"
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

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
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
	slog.Info("会话已激活", "role_id", sess.RoleID(), "player_id", sess.PlayerID())

	waitSteps := []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second, 20 * time.Second, 30 * time.Second}
	successes := 0
	var cdWait time.Duration
	cdCount := 0

	for successes < probeMaxSuccess && ctx.Err() == nil {
		panel, err := fetchPanel(ctx, sess)
		if err != nil {
			slog.Error("查询面板失败", "err", err)
			return
		}
		if panel.ChallengeTimes <= 0 {
			slog.Info("挑战次数已用尽", "challenge_times", panel.ChallengeTimes)
			return
		}
		teamID := protocol.TongTianTowerMonsterTeamID(panel.Heaven, panel.Floor)
		if teamID == 0 {
			slog.Error("缺少怪物队伍静态数据", "heaven", panel.Heaven, "floor", panel.Floor)
			return
		}

		start := time.Now()
		result, err := challenge(ctx, sess, teamID)
		if err != nil {
			slog.Error("挑战请求失败", "err", err)
			return
		}
		slog.Info("挑战结果",
			"heaven", panel.Heaven, "floor", panel.Floor,
			"team_id", teamID, "result", result,
			"challenge_times_before", panel.ChallengeTimes,
			"elapsed_ms", time.Since(start).Milliseconds(),
			"successes", successes,
		)

		switch result {
		case protocol.TongTianTowerResultSuccess:
			successes++
			cdWait = 0
		case protocol.TongTianTowerResultCDTime:
			if cdCount >= len(waitSteps) {
				slog.Warn("CD 重试次数达到上限", "cd_count", cdCount, "accumulated_wait", cdWait)
				return
			}
			step := waitSteps[cdCount]
			cdCount++
			cdWait += step
			slog.Info("挑战冷却中，等待后重试", "wait", step, "accumulated_wait", cdWait)
			select {
			case <-ctx.Done():
				return
			case <-time.After(step):
			}
		case protocol.TongTianTowerResultChallengeTimesLimit:
			slog.Info("挑战次数已用尽 (服务端结果码)", "successes", successes)
			return
		default:
			slog.Info("其他结果码，停止探针", "result", result, "successes", successes)
			return
		}
	}

	slog.Info("探针结束", "successes", successes, "cd_retries", cdCount, "cd_wait_total", cdWait)
}

func fetchPanel(ctx context.Context, sess *client.RoleSession) (*protocol.TongTianTowerPanel, error) {
	cCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	resp, err := sess.Call(cCtx, protocol.BuildTongTianTowerPanelPacket(), protocol.ActionIDTongTianTowerPanel)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("面板无回包")
	}
	return protocol.ParseTongTianTowerPanelResult(resp.Payload)
}

func challenge(ctx context.Context, sess *client.RoleSession, teamID int32) (uint8, error) {
	teams := []protocol.TongTianTowerTeam{{
		TeamType:      protocol.TongTianTowerMainTeam,
		MonsterTeamID: teamID,
	}}
	cCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	resp, err := sess.Call(cCtx, protocol.BuildTongTianTowerChallengePacket(teams), protocol.ActionIDTongTianTowerChallenge)
	if err != nil {
		return 0, err
	}
	if resp == nil {
		return 0, fmt.Errorf("挑战无回包")
	}
	res, err := protocol.ParseTongTianTowerChallengeResult(resp.Payload)
	if err != nil {
		return 0, err
	}
	return res.Result, nil
}
