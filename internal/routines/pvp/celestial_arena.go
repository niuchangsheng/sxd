// Package pvp 提供竞技场、仙界竞技场、神魔大战、取经劫镖、世界 Boss 与跨服天梯等对抗玩法。
//
// 本文件实现仙界竞技场 (Mod_StArena_Base, Module 214) 自动挑战。
package pvp

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

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
			return runStArenaChallenge(ctx, session, jitter)
		},
	)
}

// runStArenaChallenge 在仙界跨服连接上完成一轮竞技场积分赛挑战。
//
// 跨服凭据缺失或节点登录失败时仅记录日志并跳过，不阻断主服日常任务批次。
func runStArenaChallenge(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
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
		return nil
	}

	// 2. 建立仙界跨服会话并调用 Mod 214。
	cross, err := session.OpenCrossServerSessionWithInfo(crossCtx, cred)
	if err != nil {
		slog.Warn("仙界竞技场跨服会话建立失败", "role_id", session.RoleID(), "error", err)
		return nil
	}
	defer cross.Close()

	crossCaller := kit.NewCaller(crossCtx, cross, 10*time.Second)
	call := func(pkt *protocol.Packet, expectedAct uint32, d time.Duration) (*protocol.Packet, error) {
		return crossCaller.Call(pkt, expectedAct, d)
	}

	panel, err := loadStArenaPanel(call)
	if err != nil {
		slog.Warn("打开仙界竞技场面板失败", "role_id", session.RoleID(), "error", err)
		return nil
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
		// 面板次数为 0：可能是今日已用尽，也可能是当前不在积分赛阶段 (此时服务端
		// 不下发次数)。按 1 小时退避重新探测，避免全天挂起错过活动开启。
		slog.Info("仙界竞技场面板挑战次数为 0，1 小时后再查", "role_id", session.RoleID())
		return scheduler.NewCooldown(time.Hour, "仙界竞技场面板次数为 0")
	}

	// 3. 对手列表为空且仍有刷新次数时，主动刷新一次。
	if len(panel.ChallengeList) == 0 {
		if panel.ReduceRefreshTime <= 0 {
			slog.Info("仙界竞技场暂无可挑战对手且刷新次数已用尽", "role_id", session.RoleID())
			return nil
		}
		panel, err = refreshStArenaOpponents(call, panel)
		if err != nil {
			slog.Warn("刷新仙界竞技场对手列表失败", "role_id", session.RoleID(), "error", err)
			return nil
		}
		if len(panel.ChallengeList) == 0 {
			slog.Info("仙界竞技场刷新后仍无可挑战对手", "role_id", session.RoleID())
			return nil
		}
	}

	playerID := cross.PlayerID()
	remaining := panel.ReduceChallengeCount
	staleRetries := 0

	// 4. 循环消耗挑战次数：每场结束后重新打开面板确认积分、剩余次数与战报。
	for battle := 1; remaining > 0 && battle <= stArenaMaxBattles; battle++ {
		if err := ctx.Err(); err != nil {
			slog.Info("仙界竞技场挑战已取消，提前退出", "role_id", session.RoleID(), "error", err)
			return nil
		}

		target, ok := SelectStArenaOpponent(panel.ChallengeList)
		if !ok {
			slog.Info("仙界竞技场暂无可挑战对手", "role_id", session.RoleID())
			return nil
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
			return nil
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
				return nil
			}
			if reconciled.ReduceChallengeCount >= remaining {
				slog.Warn("仙界竞技场挑战无结算回包", "role_id", session.RoleID(), "target_player_id", target.StPlayerID, "error", err)
				return nil
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
				return nil
			}
			if !result.Success() {
				switch result.Result {
				case protocol.StArenaResultNotInList:
					// 目标列表已在服务端刷新，重新拉取面板并重试有限次数。
					staleRetries++
					if staleRetries > stArenaMaxStaleRetries {
						slog.Info("仙界竞技场目标持续不在列表，结束本轮挑战",
							"role_id", session.RoleID(), "result", result.Result, "retries", staleRetries)
						return nil
					}
					panel, err = loadStArenaPanel(call)
					if err != nil {
						slog.Warn("重新拉取仙界竞技场面板失败", "role_id", session.RoleID(), "error", err)
						return nil
					}
					if len(panel.ChallengeList) == 0 && panel.ReduceRefreshTime > 0 {
						panel, err = refreshStArenaOpponents(call, panel)
						if err != nil {
							slog.Warn("刷新仙界竞技场对手列表失败", "role_id", session.RoleID(), "error", err)
							return nil
						}
					}
					continue
				case protocol.StArenaResultCDTime:
					// 回包未带冷却秒数，按 10 分钟低频复查；比等下一次
					// Cron 更早恢复剩余挑战次数。
					slog.Info("仙界竞技场挑战冷却中，10 分钟后再试",
						"role_id", session.RoleID(), "target_player_id", target.StPlayerID)
					return scheduler.NewCooldown(10*time.Minute, "仙界竞技场挑战冷却")
				case protocol.StArenaResultFuncEnd:
					slog.Info("仙界竞技场不在积分赛阶段或功能已关闭，30 分钟后再试",
						"role_id", session.RoleID(), "result", result.Result)
					return scheduler.NewCooldown(30*time.Minute, "仙界竞技场非积分赛阶段")
				case protocol.StArenaResultNotEnoughChallengeTimes:
					// 服务端判定次数不足；按 1 小时退避重新探测（次数可能按活动时段补充）。
					slog.Info("仙界竞技场挑战次数不足，1 小时后再试", "role_id", session.RoleID())
					return scheduler.NewCooldown(time.Hour, "仙界竞技场挑战次数不足")
				default:
					slog.Info("仙界竞技场挑战被服务端拒绝，结束本轮挑战",
						"role_id", session.RoleID(),
						"target_player_id", target.StPlayerID,
						"result", result.Result,
					)
					return nil
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
				return nil
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
			return nil
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
		return nil
	}
	slog.Info("仙界竞技场挑战次数已用尽", "role_id", session.RoleID(), "remaining", remaining)
	return nil
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
