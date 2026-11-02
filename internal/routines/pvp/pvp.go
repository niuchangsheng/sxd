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

const (
	// arenaFailCooldown 挑战失败 (战败) 且服务端未下发冷却计时的兜底等待时长。
	arenaFailCooldown = 30 * time.Second
	// arenaCooldownMaxWait 单次冷却等待上限，防止异常 cdTimer 长时间阻塞调度。
	arenaCooldownMaxWait = 60 * time.Second
	// arenaCooldownMaxRetries 被服务端以"冷却中"拒绝时的最大重试次数。
	arenaCooldownMaxRetries = 3
)

// arenaCooldownDuration 将竞技场 cdTimer 归一化为剩余等待时长。
//
// 字段语义为"冷却结束时间点"，线上可能下发 Unix 绝对秒或剩余秒数：
// 大于 1e9 视为绝对时间戳，否则视为剩余秒数。<=0 或已过期返回 0。
func arenaCooldownDuration(cdTimer int32) time.Duration {
	if cdTimer <= 0 {
		return 0
	}
	var wait time.Duration
	if int64(cdTimer) > 1_000_000_000 {
		wait = time.Until(time.Unix(int64(cdTimer), 0))
	} else {
		wait = time.Duration(cdTimer) * time.Second
	}
	if wait <= 0 {
		return 0
	}
	if wait > arenaCooldownMaxWait {
		wait = arenaCooldownMaxWait
	}
	return wait
}

// sleepWithContext 可被 context 提前中断的等待，返回 false 表示等待被取消。
func sleepWithContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
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
			timesRespPkt, err := session.Call(tCtx, timesPkt, protocol.ActionIDArenaGetTimes)
			tCancel()
			if err != nil {
				return fmt.Errorf("查询竞技场次数失败: %w", err)
			}

			timesRes, err := protocol.ParseArenaGetTimesResult(timesRespPkt.Payload)
			if err != nil {
				return fmt.Errorf("解析竞技场剩余次数失败: %w", err)
			}

			remaining := timesRes.RemainingTimes
			if remaining <= 0 {
				slog.Info("今日竞技场免费挑战次数已用尽", "role_id", session.RoleID(), "remaining", remaining)
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

			// 拉取本轮可挑战对手，面板未携带时退回独立对手查询协议
			loadOpponents := func(panel *protocol.ArenaPanelResult) ([]protocol.ArenaOpponent, error) {
				if len(panel.Opponents) > 0 {
					return panel.Opponents, nil
				}
				opponentsPkt, err := protocol.BuildArenaGetOpponentsPacket(protocol.ArenaGetOpponentsRequest{
					PrevAct: protocol.ActionIDArenaGetTimes,
				})
				if err != nil {
					return nil, fmt.Errorf("构造竞技场对手查询封包失败: %w", err)
				}
				oppCtx, oppCancel := context.WithTimeout(ctx, 3*time.Second)
				oppRespPkt, err := session.Call(oppCtx, opponentsPkt, protocol.ActionIDArenaGetOpponents)
				oppCancel()
				if err != nil {
					return nil, fmt.Errorf("查询竞技场对手列表失败: %w", err)
				}
				oppRes, err := protocol.ParseArenaGetOpponentsResult(oppRespPkt.Payload)
				if err != nil {
					return nil, fmt.Errorf("解析竞技场对手回包失败: %w", err)
				}
				return oppRes.Opponents, nil
			}

			panel, err := openPanel()
			if err != nil {
				return fmt.Errorf("打开竞技场面板失败: %w", err)
			}
			slog.Info("竞技场面板已打开",
				"role_id", session.RoleID(),
				"my_rank", panel.MyRank,
				"remain_challenge_times", panel.RemainChallengeTimes,
				"cd_timer", panel.CdTimer,
				"opponents", len(panel.Opponents),
				"war_reports", len(panel.WarReports),
			)

			playerID := session.PlayerID()
			// 冷却状态: 开局取面板值；每场战斗后由结算回包/刷新面板回填。
			// 战败固定触发 30s 冷却，服务端未下发时按兜底时长等待。
			cooldownCd := panel.CdTimer
			cooldownRetries := 0
			// 4. 循环消耗挑战次数：每轮打完立即刷新面板，既确认名次又拿到下一轮对手
			for battle := 1; remaining > 0; battle++ {
				if err := ctx.Err(); err != nil {
					slog.Info("竞技场挑战已取消，提前退出", "role_id", session.RoleID(), "err", err)
					return nil
				}

				// 冷却未结束时挑战必被拒绝，先等待再出手
				if wait := arenaCooldownDuration(cooldownCd); wait > 0 {
					slog.Info("竞技场挑战冷却中，等待结束后继续",
						"role_id", session.RoleID(),
						"battle", battle,
						"cd_timer", cooldownCd,
						"wait", wait,
					)
					if !sleepWithContext(ctx, wait) {
						slog.Info("竞技场冷却等待已取消，提前退出", "role_id", session.RoleID())
						return nil
					}
				}

				opponents, err := loadOpponents(panel)
				if err != nil {
					if battle == 1 {
						return err
					}
					slog.Warn("竞技场对手列表刷新失败，结束本轮挑战", "role_id", session.RoleID(), "err", err)
					return nil
				}
				if len(opponents) == 0 {
					slog.Info("竞技场暂无可挑战对手，结束本轮挑战", "role_id", session.RoleID())
					return nil
				}

				// 软柿子筛选算法：锁定等级最低、排名最靠后的挑战目标
				target := SelectSoftPersimmonOpponent(opponents)
				slog.Info("竞技场软柿子筛选锁定目标",
					"role_id", session.RoleID(),
					"battle", battle,
					"target_name", target.Name,
					"target_rank", target.Rank,
					"target_player_id", target.PlayerID,
					"target_level", target.Level,
					"target_role_id", target.RoleID,
					"target_award_coin", target.AwardCoin,
					"target_award_fame", target.AwardFame,
				)

				// 发起挑战
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
					// 冷却拦截: 等待服务端下发的冷却时长后重试 (上限 3 次)
					if chRes.Result == protocol.ArenaResultCoolingDown && cooldownRetries < arenaCooldownMaxRetries {
						cooldownRetries++
						wait := arenaCooldownDuration(chRes.CdTimer)
						if wait == 0 {
							wait = arenaFailCooldown
						}
						slog.Info("本服竞技场挑战被冷却拦截，等待后重试",
							"role_id", session.RoleID(),
							"battle", battle,
							"target_rank", target.Rank,
							"cd_timer", chRes.CdTimer,
							"wait", wait,
							"retry", cooldownRetries,
						)
						if !sleepWithContext(ctx, wait) {
							slog.Info("竞技场冷却等待已取消，提前退出", "role_id", session.RoleID())
							return nil
						}
						cooldownCd = 0
						continue
					}
					slog.Info("本服竞技场挑战被服务端拒绝",
						"role_id", session.RoleID(),
						"target_rank", target.Rank,
						"result", chRes.Result,
						"remain_times", chRes.RemainTimes,
						"cd_timer", chRes.CdTimer,
					)
					return nil
				}
				cooldownRetries = 0

				// 战报胜负位: winPlayerID 等于本方 PlayerID 即为胜利
				win := chRes.IsWin(playerID)
				slog.Info("本服竞技场战斗结算",
					"role_id", session.RoleID(),
					"battle", battle,
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
					"cd_timer", chRes.CdTimer,
					"war_report_bytes", len(chRes.WarReport),
				)

				// 重新打开面板，从对应战报读取 oldRank/newRank，锁定精确名次变化
				refreshed, err := openPanel()
				if err != nil {
					slog.Warn("竞技场名次确认失败: 面板刷新异常", "role_id", session.RoleID(), "err", err)
					return nil
				}
				if report := refreshed.FindWarReport(chRes.ReportID); report != nil {
					slog.Info("本服竞技场名次结算完成",
						"role_id", session.RoleID(),
						"battle", battle,
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
				} else {
					slog.Info("竞技场名次已刷新但未找到对应战报",
						"role_id", session.RoleID(),
						"report_id", chRes.ReportID,
						"my_rank", refreshed.MyRank,
					)
				}
				panel = refreshed

				// 回填冷却: 优先取结算回包，其次取刷新面板；战败但两者均未下发时按 30s 兜底
				cooldownCd = chRes.CdTimer
				if cooldownCd == 0 {
					cooldownCd = refreshed.CdTimer
				}
				if !win && cooldownCd == 0 {
					cooldownCd = int32(arenaFailCooldown / time.Second)
				}
				if cooldownCd != 0 {
					slog.Info("竞技场挑战冷却已生效",
						"role_id", session.RoleID(),
						"battle", battle,
						"win", win,
						"cd_timer", cooldownCd,
					)
				}

				// 熔断保护: 服务端未扣减挑战次数时立即退出，避免死循环刷榜
				if int32(chRes.RemainTimes) >= remaining {
					slog.Warn("竞技场挑战次数未扣减，疑似服务端拒绝扣次，熔断退出",
						"role_id", session.RoleID(),
						"battle", battle,
						"before_times", remaining,
						"after_times", chRes.RemainTimes,
					)
					return nil
				}
				remaining = int32(chRes.RemainTimes)

				if remaining > 0 && jitter != nil {
					_ = jitter.Wait(ctx)
				}
			}

			slog.Info("本服竞技场挑战次数已用尽", "role_id", session.RoleID(), "remaining", remaining)
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
