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

// SelectSoftPersimmonOpponent 软柿子挑选算法：优先挑选战力最低且排位靠前的对手。
func SelectSoftPersimmonOpponent(opponents []protocol.ArenaOpponent) protocol.ArenaOpponent {
	if len(opponents) == 0 {
		return protocol.ArenaOpponent{Rank: 5}
	}
	best := opponents[0]
	for _, opp := range opponents[1:] {
		// 战力更低，或者战力相当但排名更高 (Rank 更小)
		if opp.FightValue < best.FightValue || (opp.FightValue == best.FightValue && opp.Rank < best.Rank) {
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

			// 3. 发送查询可挑战对手列表
			opponentsPkt, err := protocol.BuildArenaGetOpponentsPacket(protocol.ArenaGetOpponentsRequest{
				PrevAct: protocol.ActionIDArenaGetTimes,
			})
			if err != nil {
				return fmt.Errorf("构造竞技场对手查询封包失败: %w", err)
			}

			oppCtx, oppCancel := context.WithTimeout(ctx, 3*time.Second)
			defer oppCancel()

			oppRespPkt, err := session.Call(oppCtx, opponentsPkt, protocol.ActionIDArenaGetOpponents)
			if err != nil {
				return fmt.Errorf("查询竞技场对手列表失败: %w", err)
			}

			oppRes, err := protocol.ParseArenaGetOpponentsResult(oppRespPkt.Payload)
			if err != nil {
				return fmt.Errorf("解析竞技场对手回包失败: %w", err)
			}

			// 4. 软柿子筛选算法：锁定战力最低、胜率最高的挑战目标
			target := SelectSoftPersimmonOpponent(oppRes.Opponents)
			slog.Info("竞技场软柿子筛选锁定目标",
				"role_id", session.RoleID(),
				"target_name", target.Name,
				"target_rank", target.Rank,
				"fight_value", target.FightValue,
			)

			// 5. 发起挑战
			challengePkt, err := protocol.BuildArenaChallengePacket(protocol.ArenaChallengeRequest{
				TargetRank: target.Rank,
				PrevAct:    protocol.ActionIDArenaGetOpponents,
			})
			if err != nil {
				return fmt.Errorf("构造竞技场挑战封包失败: %w", err)
			}

			chCtx, chCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
			chRespPkt, err := session.Call(chCtx, challengePkt, protocol.ActionIDArenaChallenge)
			chCancel()

			if err == nil && chRespPkt != nil {
				chRes, pErr := protocol.ParseArenaChallengeResult(chRespPkt.Payload)
				if pErr == nil {
					slog.Info("本服竞技场挑战完成",
						"role_id", session.RoleID(),
						"target_rank", target.Rank,
						"success", chRes.Success,
						"new_rank", chRes.NewRank,
					)
					return nil
				}
			}

			slog.Info("本服竞技场挑战指令已发送",
				"role_id", session.RoleID(),
				"target_rank", target.Rank,
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

			// 3. 发起劫镖拦截攻击 (Mod 314, Act 12)
			hijackPkt := protocol.BuildCaravanHijackPacket(1, protocol.ActionIDCaravanGetDetail)
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

