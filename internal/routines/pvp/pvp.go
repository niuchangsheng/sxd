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

// 仙界竞技场自动化常量。
var (
	// stArenaCallTimeout 单次 Mod 214 协议调用超时。
	// 面板含 50 条排行榜记录，跨服链路较慢时 3s 会偏紧。
	stArenaCallTimeout = 8 * time.Second
	// stArenaChallengeTimeout 发起挑战后等待完整战报结算的超时。
	// challenge 回包携带完整战斗回放 (实机约 13KB)，跨服节点拥塞时留足预算。
	stArenaChallengeTimeout = 15 * time.Second
)

const (
	// stArenaMaxBattles 单轮调度的最大挑战场次，防止服务端异常导致死循环。
	stArenaMaxBattles = 30
	// stArenaMaxStaleRetries 目标已不在列表时的最大重拉次数。
	stArenaMaxStaleRetries = 2
)

// stArenaCallFunc 仙界跨服协议调用函数签名。
type stArenaCallFunc func(pkt *protocol.Packet, expectedAct uint32, d time.Duration) (*protocol.Packet, error)

// NewCelestialArenaRoutine 构造仙界竞技场积分赛自动挑战任务。
//
// 官方客户端流程为：Mod 96 取跨服凭据 → Mod 94 登录跨服节点 → Mod 95 进入仙界城镇
// → Mod 214 open_st_arena → (可选 refresh_player_list) → challenge。
// 该任务每次调度会循环消耗当日剩余挑战次数；不主动消耗元宝清冷却或购买次数。
func NewCelestialArenaRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"celestial_arena",
		"pvp",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			slog.Info("正在执行对抗任务: 仙界竞技场自动积分赛挑战", "role_id", session.RoleID())
			runStArenaChallenge(ctx, session, jitter)
			return nil
		},
	)
}

// runStArenaChallenge 在仙界跨服连接上完成一轮竞技场积分赛挑战。
//
// 跨服凭据缺失或节点登录失败时仅记录日志并跳过，不阻断主服日常任务批次。
func runStArenaChallenge(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) {
	// 单轮最多消耗 30 次挑战，叠加拟人抖动后可能持续数分钟，预留充足预算；
	// 外层调度 ctx 若先到期会提前取消。
	crossCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// 1. 申请一次性跨服凭据：单机/测试环境无此模块时快速跳过。
	credCtx, credCancel := context.WithTimeout(crossCtx, 2*time.Second)
	cred, err := session.FetchCrossServerCredentials(credCtx)
	credCancel()
	if err != nil {
		slog.Debug("仙界竞技场不可用，跳过", "role_id", session.RoleID(), "error", err)
		return
	}

	// 2. 建立仙界跨服会话并调用 Mod 214。
	cross, err := session.OpenCrossServerSessionWithInfo(crossCtx, cred)
	if err != nil {
		slog.Warn("仙界竞技场跨服会话建立失败", "role_id", session.RoleID(), "error", err)
		return
	}
	defer cross.Close()

	call := func(pkt *protocol.Packet, expectedAct uint32, d time.Duration) (*protocol.Packet, error) {
		cCtx, cCancel := context.WithTimeout(crossCtx, d)
		defer cCancel()
		return cross.Call(cCtx, pkt, expectedAct)
	}

	panel, err := loadStArenaPanel(call)
	if err != nil {
		slog.Warn("打开仙界竞技场面板失败", "role_id", session.RoleID(), "error", err)
		return
	}
	slog.Info("仙界竞技场面板已打开",
		"role_id", session.RoleID(),
		"integral", panel.Integral,
		"remain_challenge_times", panel.ReduceChallengeCount,
		"remain_refresh_times", panel.ReduceRefreshTime,
		"opponents", len(panel.ChallengeList),
		"war_reports", len(panel.ReportList),
		"rank_entries", len(panel.IntegralRankList),
	)

	if panel.ReduceChallengeCount <= 0 {
		slog.Info("仙界竞技场今日挑战次数已用尽", "role_id", session.RoleID())
		return
	}

	// 3. 对手列表为空且仍有刷新次数时，主动刷新一次。
	if len(panel.ChallengeList) == 0 {
		if panel.ReduceRefreshTime <= 0 {
			slog.Info("仙界竞技场暂无可挑战对手且刷新次数已用尽", "role_id", session.RoleID())
			return
		}
		panel, err = refreshStArenaOpponents(call, panel)
		if err != nil {
			slog.Warn("刷新仙界竞技场对手列表失败", "role_id", session.RoleID(), "error", err)
			return
		}
		if len(panel.ChallengeList) == 0 {
			slog.Info("仙界竞技场刷新后仍无可挑战对手", "role_id", session.RoleID())
			return
		}
	}

	playerID := cross.PlayerID()
	remaining := panel.ReduceChallengeCount
	staleRetries := 0

	// 4. 循环消耗挑战次数：每场结束后重新打开面板确认积分、剩余次数与战报。
	for battle := 1; remaining > 0 && battle <= stArenaMaxBattles; battle++ {
		if err := ctx.Err(); err != nil {
			slog.Info("仙界竞技场挑战已取消，提前退出", "role_id", session.RoleID(), "error", err)
			return
		}

		target, ok := SelectStArenaOpponent(panel.ChallengeList)
		if !ok {
			slog.Info("仙界竞技场暂无可挑战对手", "role_id", session.RoleID())
			return
		}
		slog.Info("仙界竞技场锁定最弱对手",
			"role_id", session.RoleID(),
			"battle", battle,
			"target_player_id", target.StPlayerID,
			"target_level", target.Level,
			"target_integral", target.Integral,
			"target_role_id", target.RoleID,
		)

		challengePkt, err := protocol.BuildStArenaChallengePacket(protocol.StArenaChallengeRequest{
			StPlayerID: target.StPlayerID,
			Integral:   target.Integral,
		})
		if err != nil {
			slog.Warn("构造仙界竞技场挑战封包失败", "role_id", session.RoleID(), "error", err)
			return
		}

		beforeIntegral := panel.Integral
		var settled *protocol.StArenaPanel

		resp, err := call(challengePkt, protocol.ActionIDStArenaChallenge, stArenaChallengeTimeout)
		if err != nil || resp == nil {
			// 回包超时/丢失不代表挑战未被受理：challenge 回包携带完整战斗回放，
			// 跨服拥塞或战报体积过大时，服务端可能已完成战斗但回包未在预算内到达。
			// 重新拉取面板核对挑战次数，已扣次则按已结算继续，避免重复挑战同一目标。
			reconciled, rErr := loadStArenaPanel(call)
			if rErr != nil {
				slog.Warn("仙界竞技场挑战无结算回包且复核面板失败",
					"role_id", session.RoleID(),
					"target_player_id", target.StPlayerID,
					"error", err,
					"reconcile_error", rErr,
				)
				return
			}
			if reconciled.ReduceChallengeCount >= remaining {
				slog.Warn("仙界竞技场挑战无结算回包", "role_id", session.RoleID(), "target_player_id", target.StPlayerID, "error", err)
				return
			}
			slog.Warn("仙界竞技场挑战回包超时但服务端已扣次，按已结算继续",
				"role_id", session.RoleID(),
				"target_player_id", target.StPlayerID,
				"before_times", remaining,
				"after_times", reconciled.ReduceChallengeCount,
				"error", err,
			)
			settled = reconciled
		} else {
			result, pErr := protocol.ParseStArenaChallengeResult(resp.Payload)
			if pErr != nil {
				slog.Warn("解析仙界竞技场挑战结果失败", "role_id", session.RoleID(), "error", pErr)
				return
			}
			if !result.Success() {
				switch result.Result {
				case protocol.StArenaResultNotInList:
					// 目标列表已在服务端刷新，重新拉取面板并重试有限次数。
					staleRetries++
					if staleRetries > stArenaMaxStaleRetries {
						slog.Info("仙界竞技场目标持续不在列表，结束本轮挑战",
							"role_id", session.RoleID(), "result", result.Result, "retries", staleRetries)
						return
					}
					panel, err = loadStArenaPanel(call)
					if err != nil {
						slog.Warn("重新拉取仙界竞技场面板失败", "role_id", session.RoleID(), "error", err)
						return
					}
					if len(panel.ChallengeList) == 0 && panel.ReduceRefreshTime > 0 {
						panel, err = refreshStArenaOpponents(call, panel)
						if err != nil {
							slog.Warn("刷新仙界竞技场对手列表失败", "role_id", session.RoleID(), "error", err)
							return
						}
					}
					continue
				case protocol.StArenaResultCDTime:
					slog.Info("仙界竞技场挑战冷却中，结束本轮挑战",
						"role_id", session.RoleID(), "target_player_id", target.StPlayerID)
					return
				case protocol.StArenaResultFuncEnd:
					slog.Info("仙界竞技场不在积分赛阶段或功能已关闭，结束本轮挑战",
						"role_id", session.RoleID(), "result", result.Result)
					return
				case protocol.StArenaResultNotEnoughChallengeTimes:
					slog.Info("仙界竞技场挑战次数不足，结束本轮挑战", "role_id", session.RoleID())
					return
				default:
					slog.Info("仙界竞技场挑战被服务端拒绝，结束本轮挑战",
						"role_id", session.RoleID(),
						"target_player_id", target.StPlayerID,
						"result", result.Result,
					)
					return
				}
			}
		}
		staleRetries = 0

		// 5. 重新打开面板，读取最新积分、剩余次数，并根据战报判定胜负。
		refreshed := settled
		if refreshed == nil {
			var rErr error
			refreshed, rErr = loadStArenaPanel(call)
			if rErr != nil {
				slog.Warn("仙界竞技场结算后刷新面板失败", "role_id", session.RoleID(), "error", rErr)
				return
			}
		}
		if report := findStArenaReport(refreshed.ReportList, playerID, target.StPlayerID); report != nil {
			slog.Info("仙界竞技场战斗结算",
				"role_id", session.RoleID(),
				"battle", battle,
				"target_player_id", target.StPlayerID,
				"win", report.IsWin(playerID),
				"win_player_id", report.WinID,
				"integral_delta", refreshed.Integral-beforeIntegral,
				"integral", refreshed.Integral,
				"remain_challenge_times", refreshed.ReduceChallengeCount,
				"report_id", report.WarReportID,
				"attack_player", report.AttackName,
				"defense_player", report.DefenseName,
			)
		} else {
			slog.Info("仙界竞技场战斗已结算但未找到对应战报",
				"role_id", session.RoleID(),
				"battle", battle,
				"target_player_id", target.StPlayerID,
				"integral_delta", refreshed.Integral-beforeIntegral,
				"integral", refreshed.Integral,
				"remain_challenge_times", refreshed.ReduceChallengeCount,
			)
		}

		// 熔断保护：服务端未扣减挑战次数时立即退出，避免死循环刷榜。
		if refreshed.ReduceChallengeCount >= remaining {
			slog.Warn("仙界竞技场挑战次数未扣减，熔断退出",
				"role_id", session.RoleID(),
				"battle", battle,
				"before_times", remaining,
				"after_times", refreshed.ReduceChallengeCount,
			)
			return
		}
		remaining = refreshed.ReduceChallengeCount
		panel = refreshed

		if remaining > 0 && jitter != nil {
			_ = jitter.Wait(ctx)
		}
	}

	if remaining > 0 {
		slog.Warn("仙界竞技场达到单轮挑战上限，剩余次数留待下次调度",
			"role_id", session.RoleID(), "remaining", remaining)
		return
	}
	slog.Info("仙界竞技场挑战次数已用尽", "role_id", session.RoleID(), "remaining", remaining)
}

// loadStArenaPanel 打开仙界竞技场面板并解析完整状态。
func loadStArenaPanel(call stArenaCallFunc) (*protocol.StArenaPanel, error) {
	resp, err := call(protocol.BuildStArenaOpenPacket(), protocol.ActionIDStArenaOpen, stArenaCallTimeout)
	if err != nil {
		return nil, fmt.Errorf("请求仙界竞技场面板失败: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("仙界竞技场面板回包为空")
	}
	panel, err := protocol.ParseStArenaPanelResult(resp.Payload)
	if err != nil {
		return nil, fmt.Errorf("解析仙界竞技场面板失败: %w", err)
	}
	return panel, nil
}

// refreshStArenaOpponents 刷新对手列表，成功后重新拉取完整面板以同步刷新次数。
func refreshStArenaOpponents(call stArenaCallFunc, panel *protocol.StArenaPanel) (*protocol.StArenaPanel, error) {
	resp, err := call(protocol.BuildStArenaRefreshPlayerListPacket(), protocol.ActionIDStArenaRefreshPlayerList, stArenaCallTimeout)
	if err != nil {
		return nil, fmt.Errorf("请求刷新仙界竞技场对手失败: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("刷新仙界竞技场对手回包为空")
	}
	result, err := protocol.ParseStArenaRefreshResult(resp.Payload)
	if err != nil {
		return nil, fmt.Errorf("解析刷新仙界竞技场对手回包失败: %w", err)
	}
	if result.Result != protocol.StArenaResultSuccess {
		return nil, fmt.Errorf("刷新仙界竞技场对手被拒绝: result=%d", result.Result)
	}
	slog.Info("仙界竞技场对手列表已刷新",
		"opponents", len(result.ChallengeList),
		"remain_refresh_times", result.ReduceRefreshTime,
		"previous_opponents", len(panel.ChallengeList),
	)
	return loadStArenaPanel(call)
}

// SelectStArenaOpponent 软柿子筛选算法：优先选择等级最低的对手；
// 等级相同时选择积分更低者，积分再相同时选择玩家 ID 更小者。
// 服务端对手列表不下发战力，等级与积分是可直接比较的实力代理指标。
func SelectStArenaOpponent(opponents []protocol.StArenaOpponent) (protocol.StArenaOpponent, bool) {
	if len(opponents) == 0 {
		return protocol.StArenaOpponent{}, false
	}
	best := opponents[0]
	for _, opp := range opponents[1:] {
		if opp.Level < best.Level ||
			(opp.Level == best.Level && opp.Integral < best.Integral) ||
			(opp.Level == best.Level && opp.Integral == best.Integral && opp.StPlayerID < best.StPlayerID) {
			best = opp
		}
	}
	return best, true
}

// findStArenaReport 在面板战报中定位本方与目标之间最新的一条战报。
func findStArenaReport(reports []protocol.StArenaWarReport, playerID, targetID int32) *protocol.StArenaWarReport {
	var newest *protocol.StArenaWarReport
	for i := range reports {
		report := &reports[i]
		matched := (report.AttackID == playerID && report.DefenseID == targetID) ||
			(report.DefenseID == playerID && report.AttackID == targetID)
		if !matched {
			continue
		}
		if newest == nil || report.Time > newest.Time {
			newest = report
		}
	}
	return newest
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

// NewWorldBossRoutine 构造世界 Boss 自动参战任务 (Module 24, Mod_WorldBoss_Base)。
//
// 每 5 分钟巡检一次 Boss 列表，仅在 state=1 (开战中) 时发起一次免费战斗；
// 战斗回包携带完整战报，只解析首个结果码。永不调用
// clear_death_cd / reset_death_cd / strengthen_combat 等消耗元宝的接口。
func NewWorldBossRoutine() scheduler.ActivityRoutine {
	const callTimeout = 5 * time.Second
	const fightTimeout = 15 * time.Second
	return scheduler.NewBaseRoutine(
		"world_boss",
		"pvp",
		scheduler.ScheduleLoop,
		scheduler.PriorityHigh,
		5*time.Minute,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if session.State() != client.StateActive {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			// 1. 查询世界 Boss 列表
			lCtx, lCancel := context.WithTimeout(ctx, callTimeout)
			listResp, err := session.Call(lCtx, protocol.BuildWorldBossListPacket(), protocol.ActionIDWorldBossList)
			lCancel()
			if err != nil {
				return fmt.Errorf("查询世界Boss列表失败: %w", err)
			}
			bosses, err := protocol.ParseWorldBossListResult(listResp.Payload)
			if err != nil {
				return fmt.Errorf("解析世界Boss列表失败: %w", err)
			}

			var open *protocol.WorldBossEntry
			for i := range bosses {
				if bosses[i].State == protocol.WorldBossStateOpen {
					open = &bosses[i]
					break
				}
			}
			if open == nil {
				slog.Debug("世界Boss当前未开战", "role_id", session.RoleID(), "bosses", len(bosses))
				return nil
			}

			// 2. 发起一次免费战斗
			slog.Info("世界Boss开战中，发起参战",
				"role_id", session.RoleID(),
				"boss_id", open.BossID,
				"boss_level", open.Level,
				"map_id", open.MapID,
			)
			fCtx, fCancel := context.WithTimeout(ctx, fightTimeout)
			fightResp, err := session.Call(fCtx, protocol.BuildWorldBossFightPacket(open.BossID), protocol.ActionIDWorldBossFight)
			fCancel()
			if err != nil {
				return fmt.Errorf("世界Boss战斗请求失败: %w", err)
			}
			result, err := protocol.ParseWorldBossFightResult(fightResp.Payload)
			if err != nil {
				return fmt.Errorf("解析世界Boss战斗回包失败: %w", err)
			}
			switch result {
			case protocol.WorldBossResultSuccess:
				slog.Info("世界Boss参战成功", "role_id", session.RoleID(), "boss_id", open.BossID)
			case protocol.WorldBossResultNotEnoughIngot:
				slog.Warn("世界Boss参战提示元宝不足 (自动化不主动消耗元宝)",
					"role_id", session.RoleID(), "boss_id", open.BossID)
			default:
				slog.Info("世界Boss参战未成功",
					"role_id", session.RoleID(),
					"boss_id", open.BossID,
					"result", result,
					"reason", worldBossResultName(result),
				)
			}
			return nil
		},
	)
}

// worldBossResultName 返回世界 Boss 战斗结果码的中文描述。
func worldBossResultName(result uint8) string {
	switch result {
	case protocol.WorldBossResultFailed:
		return "战斗失败"
	case protocol.WorldBossResultNotBegin:
		return "未到开战时间"
	case protocol.WorldBossResultMissionOver:
		return "活动已结束"
	case protocol.WorldBossResultBossDead:
		return "Boss已被击杀"
	case protocol.WorldBossResultDeathStatus:
		return "角色处于死亡状态"
	case protocol.WorldBossResultNotEnoughIngot:
		return "元宝不足"
	case protocol.WorldBossResultMaxTimes:
		return "今日参战次数已满"
	case protocol.WorldBossResultNotEnoughSkill:
		return "技能点不足"
	case protocol.WorldBossResultCombatLimit:
		return "战力不足"
	default:
		return "未知结果"
	}
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
