// Package minigame 提供吉星高照、仙履奇缘答题、刮刮卡、钓鱼等益智趣味活动。
//
// 本文件实现个人吉星高照 (Mod_RollCake_Base, module 38) 自动化：
// 查询未领取记录与剩余次数 -> 投掷 6 颗骰子 -> 免费逆天改运至六吉 -> 领取奖励。
// 帮派吉星高照 (Mod_Faction_Base, module 10) 见 NewFactionLuckyStarRoutine。
package minigame

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

const (
	// defaultRollCakeMaxRerollsPerRoll 单次投掷默认最大逆天改运次数 (对齐客户端 9 次上限)。
	defaultRollCakeMaxRerollsPerRoll = 9
	// maxRollCakeRollsPerRun 单次调度的最大投掷轮数，防止服务端异常未扣减次数时死循环。
	maxRollCakeRollsPerRun = 20
	// rollCakeRerollMinJi 触发逆天改运的最低吉数阈值 (吉 = 骰子点数 4)。
	rollCakeRerollMinJi = 2
	// rollCakeCallTimeout 单次协议请求超时。
	rollCakeCallTimeout = 3 * time.Second
)

// LuckyStarConfig 个人吉星高照自动化策略配置。
type LuckyStarConfig struct {
	// MaxRerollsPerRoll 每次投掷最多逆天改运次数 (默认 9，对齐客户端上限)
	MaxRerollsPerRoll int
}

// DefaultLuckyStarConfig 返回个人吉星高照默认策略：只使用免费改运次数，不消耗元宝。
func DefaultLuckyStarConfig() LuckyStarConfig {
	return LuckyStarConfig{MaxRerollsPerRoll: defaultRollCakeMaxRerollsPerRoll}
}

// NewLuckyStarRoutine 构造个人吉星高照摇骰子任务。
//
// 协议对齐 2026 客户端 Mod_RollCake_Base (module 38):
//   - Action 5 get_state 查询是否有未领取的投掷记录
//   - Action 4 get_award 领取投掷奖励 (含补领上次未领取记录)
//   - Action 2 get_count 查询今日剩余投掷次数与免费改运次数
//   - Action 0 roll 投掷 6 颗骰子
//   - Action 3 reroll 逆天改运，直至六颗骰子全部为吉(4)或次数用尽
func NewLuckyStarRoutine(cfgs ...LuckyStarConfig) scheduler.ActivityRoutine {
	cfg := DefaultLuckyStarConfig()
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	if cfg.MaxRerollsPerRoll <= 0 {
		cfg.MaxRerollsPerRoll = defaultRollCakeMaxRerollsPerRoll
	}

	return scheduler.NewBaseRoutine(
		"lucky_star",
		"minigame",
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
			slog.Info("正在执行益智任务: 吉星高照摇骰子", "role_id", session.RoleID())

			call := func(pkt *protocol.Packet, actionID uint32) ([]byte, error) {
				callCtx, cancel := context.WithTimeout(ctx, rollCakeCallTimeout)
				defer cancel()
				resp, err := session.Call(callCtx, pkt, actionID)
				if err != nil {
					return nil, err
				}
				if resp == nil {
					return nil, client.ErrSessionClosed
				}
				return resp.Payload, nil
			}

			prevAct := uint32(protocol.ActionIDTownEnter)

			// 1. 查询是否存在上次未领取的投掷记录，存在则先补领，避免阻塞本次投掷。
			statePayload, err := call(protocol.BuildRollCakeGetStatePacket(prevAct), protocol.ActionIDRollCakeGetState)
			if err != nil {
				slog.Warn("查询吉星高照未领取记录失败", "role_id", session.RoleID(), "err", err)
				return nil
			}
			state, err := protocol.ParseRollCakeStateResult(statePayload)
			if err != nil {
				slog.Warn("解析吉星高照未领取记录失败", "role_id", session.RoleID(), "err", err)
				return nil
			}
			prevAct = protocol.ActionIDRollCakeGetState

			if state.Msg == protocol.RollCakeCodeHaveRecord {
				if _, err := call(protocol.BuildRollCakeGetAwardPacket(prevAct), protocol.ActionIDRollCakeGetAward); err != nil {
					slog.Warn("补领上次吉星高照奖励失败", "role_id", session.RoleID(), "err", err)
					return nil
				}
				prevAct = protocol.ActionIDRollCakeGetAward
				slog.Info("已补领上次吉星高照奖励",
					"role_id", session.RoleID(),
					"reward_type", state.Type,
					"dice", state.Dice,
				)
			}

			// 2. 查询今日剩余投掷次数与免费改运次数。
			countPayload, err := call(protocol.BuildRollCakeGetCountPacket(prevAct), protocol.ActionIDRollCakeGetCount)
			if err != nil {
				slog.Warn("查询吉星高照剩余次数失败", "role_id", session.RoleID(), "err", err)
				return nil
			}
			info, err := protocol.ParseRollCakeCountResult(countPayload)
			if err != nil {
				slog.Warn("解析吉星高照剩余次数失败", "role_id", session.RoleID(), "err", err)
				return nil
			}
			prevAct = protocol.ActionIDRollCakeGetCount

			remain := info.Count
			freeRerolls := info.FreeRerollTimes
			slog.Info("吉星高照当前状态",
				"role_id", session.RoleID(),
				"remain_times", remain,
				"free_reroll_times", freeRerolls,
				"max_buy_reroll_times", info.MaxBuyRerollTimes,
				"roll_count", info.RollCount,
			)
			if remain <= 0 {
				slog.Info("今日吉星高照投掷次数已用尽", "role_id", session.RoleID())
				return nil
			}

			// 3. 循环投掷、改运并领奖，直至次数用尽 (仅使用免费改运次数)。
			rolls := 0
			for remain > 0 && rolls < maxRollCakeRollsPerRun {
				if err := ctx.Err(); err != nil {
					slog.Info("吉星高照投掷已取消，提前退出", "role_id", session.RoleID(), "err", err)
					return nil
				}

				beforeRemain := remain
				rollPayload, err := call(protocol.BuildRollCakeRollPacket(prevAct), protocol.ActionIDRollCakeRoll)
				if err != nil {
					slog.Info("吉星高照投掷未收到回包", "role_id", session.RoleID(), "err", err)
					return nil
				}
				roll, err := protocol.ParseRollCakeRollResult(rollPayload)
				if err != nil {
					slog.Warn("解析吉星高照投掷回包失败", "role_id", session.RoleID(), "err", err)
					return nil
				}
				prevAct = protocol.ActionIDRollCakeRoll

				if roll.Msg != protocol.RollCakeCodeSuccess {
					slog.Warn("吉星高照投掷被服务端拒绝", "role_id", session.RoleID(), "result", roll.Msg)
					break
				}
				rolls++

				// 逆天改运：六颗骰子全部为吉(4)或免费次数用尽时停止，永不消耗元宝。
				// 优化策略：仅当本手已有 2 吉及以上，或这是今日最后一次投掷时才改运，
				// 否则保留改运次数给后续更有潜力的投掷。
				dice := roll.Dice
				lastType := roll.Type
				rerolls := 0
				if shouldRerollRollCake(dice, remain <= 1) {
					for rerolls < cfg.MaxRerollsPerRoll && !allRollCakeJi(dice) {
						if freeRerolls <= 0 {
							break
						}

						rrPayload, err := call(protocol.BuildRollCakeRerollPacket(prevAct), protocol.ActionIDRollCakeReroll)
						if err != nil {
							slog.Info("吉星高照逆天改运未收到回包", "role_id", session.RoleID(), "err", err)
							return nil
						}
						rr, err := protocol.ParseRollCakeRerollResult(rrPayload)
						if err != nil {
							slog.Warn("解析吉星高照逆天改运回包失败", "role_id", session.RoleID(), "err", err)
							return nil
						}
						prevAct = protocol.ActionIDRollCakeReroll

						if rr.Msg != protocol.RollCakeCodeSuccess {
							switch rr.Msg {
							case protocol.RollCakeCodeNoRerollTimes:
								slog.Info("今日吉星高照改运次数已用尽", "role_id", session.RoleID())
							case protocol.RollCakeCodeNotEnoughIngot:
								slog.Warn("吉星高照逆天改运元宝不足", "role_id", session.RoleID())
							default:
								slog.Warn("吉星高照逆天改运被服务端拒绝", "role_id", session.RoleID(), "result", rr.Msg)
							}
							break
						}

						rerolls++
						dice = rr.Dice
						lastType = rr.Type
						freeRerolls = rr.FreeRerollTimes
					}
				} else {
					slog.Info("吉星高照本手吉数不足，保留逆天改运次数",
						"role_id", session.RoleID(),
						"dice", dice,
						"ji_count", countRollCakeJi(dice),
						"remain_times", remain,
					)
				}

				// 领取本次投掷奖励，未领奖会阻塞下一次投掷。
				awardPayload, err := call(protocol.BuildRollCakeGetAwardPacket(prevAct), protocol.ActionIDRollCakeGetAward)
				if err != nil {
					slog.Info("吉星高照领取奖励未收到回包", "role_id", session.RoleID(), "err", err)
					return nil
				}
				award, err := protocol.ParseRollCakeAwardResult(awardPayload)
				if err != nil {
					slog.Warn("解析吉星高照领取奖励回包失败", "role_id", session.RoleID(), "err", err)
					return nil
				}
				prevAct = protocol.ActionIDRollCakeGetAward
				if award.Msg != protocol.RollCakeCodeSuccess {
					slog.Warn("吉星高照领取奖励被服务端拒绝", "role_id", session.RoleID(), "result", award.Msg)
					break
				}

				// 领奖后刷新剩余次数与免费改运次数 (服务端可能在此阶段扣减次数)。
				countUpdated := false
				if payload, err := call(protocol.BuildRollCakeGetCountPacket(prevAct), protocol.ActionIDRollCakeGetCount); err == nil {
					if latest, err := protocol.ParseRollCakeCountResult(payload); err == nil {
						remain = latest.Count
						freeRerolls = latest.FreeRerollTimes
						countUpdated = true
					}
					prevAct = protocol.ActionIDRollCakeGetCount
				}
				if !countUpdated {
					remain--
				}

				slog.Info("吉星高照投掷完成",
					"role_id", session.RoleID(),
					"roll", rolls,
					"reward_type", lastType,
					"dice", dice,
					"all_ji", allRollCakeJi(dice),
					"rerolls", rerolls,
					"remain_times", remain,
					"free_reroll_times", freeRerolls,
				)

				// 熔断保护：服务端未扣减次数时立即退出，避免重复消耗。
				if countUpdated && remain >= beforeRemain {
					slog.Warn("吉星高照投掷次数未扣减，熔断退出",
						"role_id", session.RoleID(),
						"before_times", beforeRemain,
						"after_times", remain,
					)
					break
				}

				if remain > 0 && jitter != nil {
					_ = jitter.Wait(ctx)
				}
			}

			slog.Info("吉星高照摇骰子完成",
				"role_id", session.RoleID(),
				"rolls", rolls,
				"remain_times", remain,
			)
			return nil
		},
	)
}

// shouldRerollRollCake 判断当前投掷是否触发逆天改运：
// 今日最后一次投掷必定改运；其余情况仅当吉数达到 rollCakeRerollMinJi 时才改运。
func shouldRerollRollCake(dice []int8, isLastRoll bool) bool {
	if isLastRoll {
		return true
	}
	return countRollCakeJi(dice) >= rollCakeRerollMinJi
}

// countRollCakeJi 统计六颗骰子中吉(点数 4)的数量。
func countRollCakeJi(dice []int8) int {
	count := 0
	for _, num := range dice {
		if num == 4 {
			count++
		}
	}
	return count
}

// allRollCakeJi 判断六颗骰子是否全部为吉(点数 4)，对齐客户端 isALLJi 判定。
func allRollCakeJi(dice []int8) bool {
	return len(dice) > 0 && countRollCakeJi(dice) == len(dice)
}
