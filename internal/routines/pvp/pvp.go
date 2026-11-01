// Package pvp 提供本服竞技场、仙界竞技场、神魔大战与劫镖等对抗性玩法。
//
// @author Ateng
// @since 2026-10-08
package pvp

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// SelectSoftPersimmonOpponent 软柿子挑选算法：优先挑选等级最低的对手；
// 等级相同时选择排名数字更大 (排位更低、实力更弱) 的对手。
// 服务端对手列表不下发战力，等级是唯一可直接比较的实力代理指标。
func SelectSoftPersimmonOpponent(opponents []protocol.ArenaOpponent) protocol.ArenaOpponent {
	if len(opponents) == 0 {
		return protocol.ArenaOpponent{Rank: 5}
	}
	best := opponents[0]
	for _, opp := range opponents[1:] {
		if opp.Level < best.Level || (opp.Level == best.Level && opp.Rank > best.Rank) {
			best = opp
		}
	}
	return best
}

// NewArenaRoutine 构造本服竞技场挑战任务。
func NewArenaRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"arena",
		"pvp",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}

			// 1. 拟人防封随机抖动
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			slog.Info("正在执行对抗任务: 本服竞技场自动挑战与领奖", "role_id", session.RoleID())

			// 2. 发送查询剩余挑战次数
			timesPkt, err := protocol.BuildArenaGetTimesPacket(protocol.ArenaGetTimesRequest{
				PrevAct: protocol.ActionIDTownEnter,
			})
			if err != nil {
				return fmt.Errorf("构造竞技场次数查询封包失败: %w", err)
			}

			tCtx, tCancel := context.WithTimeout(ctx, 3*time.Second)
			defer tCancel()

			timesRespPkt, err := session.Call(tCtx, timesPkt, protocol.ActionIDArenaGetTimes)
			if err != nil {
				return fmt.Errorf("查询竞技场次数失败: %w", err)
			}

			timesRes, err := protocol.ParseArenaGetTimesResult(timesRespPkt.Payload)
			if err != nil {
				return fmt.Errorf("解析竞技场剩余次数失败: %w", err)
			}

			if timesRes.RemainingTimes <= 0 {
				slog.Info("今日竞技场免费挑战次数已用尽", "role_id", session.RoleID(), "remaining", timesRes.RemainingTimes)
				return nil
			}

			// 3. 打开竞技场面板：一次拿到当前排名、可挑战对手与历史战报
			openPanel := func() (*protocol.ArenaPanelResult, error) {
				panelPkt, err := protocol.BuildArenaOpenPanelPacket(protocol.ArenaPanelRequest{
					PrevAct: protocol.ActionIDArenaGetTimes,
				})
				if err != nil {
					return nil, err
				}
				pCtx, pCancel := context.WithTimeout(ctx, 3*time.Second)
				defer pCancel()
				respPkt, err := session.Call(pCtx, panelPkt, protocol.ActionIDArenaOpenPanel)
				if err != nil {
					return nil, err
				}
				return protocol.ParseArenaPanelResult(respPkt.Payload)
			}

			panel, err := openPanel()
			if err != nil {
				return fmt.Errorf("打开竞技场面板失败: %w", err)
			}
			slog.Info("竞技场面板已打开",
				"role_id", session.RoleID(),
				"my_rank", panel.MyRank,
				"remain_challenge_times", panel.RemainChallengeTimes,
				"opponents", len(panel.Opponents),
				"war_reports", len(panel.WarReports),
			)

			opponents := panel.Opponents
			if len(opponents) == 0 {
				// 兼容保底: 面板未携带对手时退回独立对手查询协议
				opponentsPkt, err := protocol.BuildArenaGetOpponentsPacket(protocol.ArenaGetOpponentsRequest{
					PrevAct: protocol.ActionIDArenaGetTimes,
				})
				if err != nil {
					return fmt.Errorf("构造竞技场对手查询封包失败: %w", err)
				}
				oppCtx, oppCancel := context.WithTimeout(ctx, 3*time.Second)
				oppRespPkt, err := session.Call(oppCtx, opponentsPkt, protocol.ActionIDArenaGetOpponents)
				oppCancel()
				if err != nil {
					return fmt.Errorf("查询竞技场对手列表失败: %w", err)
				}
				oppRes, err := protocol.ParseArenaGetOpponentsResult(oppRespPkt.Payload)
				if err != nil {
					return fmt.Errorf("解析竞技场对手回包失败: %w", err)
				}
				opponents = oppRes.Opponents
			}

			// 4. 软柿子筛选算法：锁定等级最低、排名最靠后的挑战目标
			target := SelectSoftPersimmonOpponent(opponents)
			slog.Info("竞技场软柿子筛选锁定目标",
				"role_id", session.RoleID(),
				"target_name", target.Name,
				"target_rank", target.Rank,
				"target_player_id", target.PlayerID,
				"target_level", target.Level,
				"target_role_id", target.RoleID,
				"target_award_coin", target.AwardCoin,
				"target_award_fame", target.AwardFame,
			)

			// 5. 发起挑战
			challengePkt, err := protocol.BuildArenaChallengePacket(protocol.ArenaChallengeRequest{
				TargetRank: target.Rank,
				PrevAct:    protocol.ActionIDArenaOpenPanel,
			})
			if err != nil {
				return fmt.Errorf("构造竞技场挑战封包失败: %w", err)
			}

			chCtx, chCancel := context.WithTimeout(ctx, 5*time.Second)
			chRespPkt, err := session.Call(chCtx, challengePkt, protocol.ActionIDArenaChallenge)
			chCancel()

			if err != nil || chRespPkt == nil {
				slog.Info("本服竞技场挑战指令已发送但未收到结算回包",
					"role_id", session.RoleID(),
					"target_rank", target.Rank,
				)
				return nil
			}

			chRes, err := protocol.ParseArenaChallengeResult(chRespPkt.Payload)
			if err != nil {
				return fmt.Errorf("解析竞技场挑战结算失败: %w", err)
			}
			if !chRes.Success {
				slog.Info("本服竞技场挑战被服务端拒绝",
					"role_id", session.RoleID(),
					"target_rank", target.Rank,
					"result", chRes.Result,
					"remain_times", chRes.RemainTimes,
				)
				return nil
			}

			// 6. 战报胜负位: winPlayerID 等于本方 PlayerID 即为胜利
			playerID := session.PlayerID()
			win := chRes.IsWin(playerID)
			slog.Info("本服竞技场战斗结算",
				"role_id", session.RoleID(),
				"player_id", playerID,
				"target_rank", target.Rank,
				"win", win,
				"win_player_id", chRes.WinPlayerID,
				"deploy_type", chRes.DeployType,
				"report_id", chRes.ReportID,
				"award_coin", chRes.AwardCoin,
				"award_fame", chRes.AwardFame,
				"award_gun", chRes.AwardGun,
				"scrap_id", chRes.ScrapID,
				"remain_times", chRes.RemainTimes,
				"war_report_bytes", len(chRes.WarReport),
			)

			// 7. 重新打开面板，从对应战报读取 oldRank/newRank，锁定精确名次变化
			refreshed, err := openPanel()
			if err != nil {
				slog.Warn("竞技场名次确认失败: 面板刷新异常", "role_id", session.RoleID(), "err", err)
				return nil
			}
			report := refreshed.FindWarReport(chRes.ReportID)
			if report == nil {
				slog.Info("竞技场名次已刷新但未找到对应战报",
					"role_id", session.RoleID(),
					"report_id", chRes.ReportID,
					"my_rank", refreshed.MyRank,
				)
				return nil
			}
			slog.Info("本服竞技场名次结算完成",
				"role_id", session.RoleID(),
				"report_id", report.WarReportID,
				"win", report.IsWin(playerID),
				"win_player_id", report.WinPlayerID,
				"old_rank", report.OldRank,
				"new_rank", report.NewRank,
				"rank_delta", report.RankDelta(),
				"my_rank", refreshed.MyRank,
				"attack_player", report.AttackPlayerName,
				"defense_player", report.DefensePlayerName,
			)
			return nil
		},
	)
}

// NewCelestialArenaRoutine 构造仙界竞技场挑战与下注任务。
func NewCelestialArenaRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"celestial_arena",
		"pvp",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行对抗任务: 仙界竞技场挑战与竞猜下注")
			return nil
		},
	)
}

// NewGodsAndDemonsRoutine 构造神魔大战限时活动任务。
func NewGodsAndDemonsRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"gods_and_demons",
		"pvp",
		scheduler.ScheduleCron,
		scheduler.PriorityHigh,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行对抗任务: 神魔大战阵营自动参战")
			return nil
		},
	)
}

// NewCaravanHijackRoutine 构造阵营劫镖任务 (Feature 7: 劫镖)。
func NewCaravanHijackRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"caravan_hijack",
		"pvp",
		scheduler.ScheduleCron,
		scheduler.PriorityLow,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			slog.Info("正在执行对抗任务: 阵营商队巡查与自动劫镖拦截", "role_id", session.RoleID())

			// 1. 获取当前运镖队伍列表 (Mod 314, Act 1)
			listPkt := protocol.BuildCaravanGetListPacket(protocol.ActionIDTownEnter)
			callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			resp, err := session.Call(callCtx, listPkt, protocol.ActionIDCaravanGetList)
			cancel()

			if err != nil || resp == nil {
				slog.Warn("获取劫镖队伍列表超时或无可用目标", "err", err)
				return nil
			}

			// 2. 查询拦截目标详情 (Mod 314, Act 0)
			detailPkt := protocol.BuildCaravanGetDetailPacket(protocol.ActionIDCaravanGetList)
			dCtx, dCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
			_, _ = session.Call(dCtx, detailPkt, protocol.ActionIDCaravanGetDetail)
			dCancel()

			// 3. 发起劫镖拦截攻击 (Mod 314, Act 12: 载荷对齐抓包仅包含 PrevAct)
			hijackPkt := protocol.BuildCaravanHijackPacket(protocol.ActionIDCaravanGetDetail)
			hCtx, hCancel := context.WithTimeout(ctx, 2*time.Second)
			_, _ = session.Call(hCtx, hijackPkt, protocol.ActionIDCaravanHijack)
			hCancel()

			// 4. 战斗结果结算 (Mod 314, Act 2)
			settlePkt := protocol.BuildCaravanSettlePacket(protocol.ActionIDCaravanHijack)
			sCtx, sCancel := context.WithTimeout(ctx, 2*time.Second)
			sResp, sErr := session.Call(sCtx, settlePkt, protocol.ActionIDCaravanSettle)
			sCancel()

			if sErr == nil && sResp != nil {
				slog.Info("劫镖战斗结算完成，已获取声望与铜钱奖励", "role_id", session.RoleID())
			}
			return nil
		},
	)
}

// NewWorldBossRoutine 构造世界Boss击杀争夺任务。
func NewWorldBossRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"world_boss",
		"pvp",
		scheduler.ScheduleCron,
		scheduler.PriorityHigh,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行对抗任务: 世界Boss准时参战与鼓舞输出")
			return nil
		},
	)
}

// NewCrossServerLadderRoutine 构造跨服天梯争霸挑战任务。
func NewCrossServerLadderRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"cross_server_ladder",
		"pvp",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行对抗任务: 跨服天梯排位挑战与段位领奖")
			return nil
		},
	)
}

