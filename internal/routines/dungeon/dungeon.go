// Package dungeon 提供副本推图、主线任务循环、精英扫荡、六道轮回、伏魔塔等关卡挑战自动化。
package dungeon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// NewQiMenRoutine 构造奇门遁甲一/二（Module 251/392）普通修炼与突破试炼任务。
func NewQiMenRoutine() scheduler.ActivityRoutine {
	return newQiMenMiracleRoutine()
}

// NewZodiacRoutine 构造生肖挑战与金油洗练任务。
// NewZodiacRoutine 构造生肖挑战任务 (Module 44, Mod_Zodiac_Base)。
//
// 挑战前先读取 require_level 校验角色等级；不满足或已通关/冷却时直接跳过。
// 同一关卡战败后最多重试 zodiacMaxLossRetries 次。
func NewZodiacRoutine() scheduler.ActivityRoutine {
	const zodiacMaxLossRetries = 3
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"zodiac_challenge",
		"dungeon",
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

			// 0. 先通过官方功能开放列表判定门槛，未开放时不再发送业务请求。
			open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDZodiac)
			if fErr != nil {
				slog.Info("生肖挑战功能开放状态查询失败，跳过", "role_id", session.RoleID(), "err", fErr)
				return nil
			}
			if !open {
				slog.Info("生肖挑战功能未开放，跳过", "role_id", session.RoleID(), "function_id", protocol.FunctionIDZodiac)
				return nil
			}

			iCtx, iCancel := context.WithTimeout(ctx, callTimeout)
			infoResp, err := session.Call(iCtx, protocol.BuildZodiacInfoPacket(), protocol.ActionIDZodiacInfo)
			iCancel()
			if err != nil {
				slog.Info("生肖挑战功能不可用，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			info, err := protocol.ParseZodiacInfoResult(infoResp.Payload)
			if err != nil {
				return fmt.Errorf("解析生肖信息失败: %w", err)
			}
			level := session.GetPlayerState().Level
			slog.Info("生肖挑战状态",
				"role_id", session.RoleID(),
				"now_level", info.NowZodiacLevel,
				"now_barrier", info.NowBarrier,
				"next_level", info.NextZodiacLevel,
				"next_barrier", info.NextBarrier,
				"require_level", info.RequireLevel,
				"player_level", level,
				"heaven", info.Heaven,
			)

			// 面板全 0 表示生肖挑战尚未开放/未解锁；此时服务端会静默丢弃 challenge 请求。
			if info.NowZodiacLevel == 0 && info.NextZodiacLevel == 0 && info.RequireLevel <= 0 && info.Heaven == 0 {
				slog.Info("生肖挑战面板为空，功能未开放/未解锁，跳过", "role_id", session.RoleID())
				return nil
			}

			// 挑战前置校验：角色等级必须达到 require_level。
			if info.RequireLevel > int32(level) {
				slog.Info("角色等级不足，跳过生肖挑战",
					"role_id", session.RoleID(),
					"require_level", info.RequireLevel,
					"player_level", level,
				)
				return nil
			}
			// 已通关最高天 (require_level 超过上限 250 由客户端标记 is_end)。
			if info.RequireLevel > 250 {
				slog.Info("生肖挑战已全部通关", "role_id", session.RoleID(), "require_level", info.RequireLevel)
				return nil
			}

			losses := 0
			for {
				cCtx, cCancel := context.WithTimeout(ctx, 15*time.Second) // 回包携带完整战报
				resp, err := session.Call(cCtx, protocol.BuildZodiacChallengePacket(), protocol.ActionIDZodiacChallenge)
				cCancel()
				if err != nil {
					if errSessionTerminated(ctx, err) {
						return fmt.Errorf("生肖挑战请求失败: %w", err)
					}
					// 功能未开放/未解锁时服务端对 challenge 静默不回包，按不可用跳过而非报错。
					slog.Info("生肖挑战请求无回包，判定功能未开放/不可用，跳过",
						"role_id", session.RoleID(), "err", err)
					return nil
				}
				result, err := protocol.ParseZodiacChallengeResult(resp.Payload)
				if err != nil {
					return fmt.Errorf("解析生肖挑战回包失败: %w", err)
				}
				switch result.Result {
				case protocol.ZodiacResultSuccess:
					slog.Info("生肖挑战成功",
						"role_id", session.RoleID(),
						"zodiac_level", result.ZodiacLevel,
						"barrier", result.Barrier,
					)
					return nil
				case protocol.ZodiacResultFail:
					losses++
					if losses >= zodiacMaxLossRetries {
						slog.Info("生肖挑战连续失败，达到重试上限，结束本轮",
							"role_id", session.RoleID(), "losses", losses,
							"max_retries", zodiacMaxLossRetries)
						return nil
					}
					slog.Info("生肖挑战失败，重试同一关卡",
						"role_id", session.RoleID(), "losses", losses,
						"max_retries", zodiacMaxLossRetries)
					continue
				case protocol.ZodiacResultTopHeaven:
					slog.Info("生肖挑战已通关最高天", "role_id", session.RoleID())
				case protocol.ZodiacResultNotEnoughPlayerLevel:
					slog.Info("生肖挑战等级不足", "role_id", session.RoleID())
				case protocol.ZodiacResultBagFull:
					return scheduler.ErrBagFull
				case protocol.ZodiacResultCDTime:
					slog.Info("生肖挑战冷却中，稍后重试", "role_id", session.RoleID())
					return scheduler.NewCooldown(30*time.Minute, "生肖挑战冷却")
				case protocol.ZodiacResultNoEnoughStatePoint:
					slog.Info("生肖挑战境界点不足", "role_id", session.RoleID())
				case protocol.ZodiacResultLock:
					slog.Info("生肖关卡未解锁", "role_id", session.RoleID())
				default:
					slog.Warn("生肖挑战返回未知结果", "role_id", session.RoleID(), "result", result.Result)
				}
				return nil
			}
		},
	)
}

// NewSpiritTrialRoutine 构造精灵试炼通关挑战任务。
// NewSpiritTrialRoutine 构造精灵试炼任务 (Module 205, Mod_StTrials_Base)。
//
// 先读各模式剩余挑战次数；仅对仍有次数的模式领奖并逐次挑战。
// 挑战失败/不可挑战/次数耗尽时立即停止该模式。全程不消耗元宝。
func NewSpiritTrialRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"spirit_trial",
		"dungeon",
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

			mCtx, mCancel := context.WithTimeout(ctx, callTimeout)
			statusResp, err := session.Call(mCtx, protocol.BuildStTrialsModeStatusPacket(), protocol.ActionIDStTrialsModeStatus)
			mCancel()
			if err != nil {
				slog.Info("精灵试炼功能不可用，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			modes, err := protocol.ParseStTrialsModeStatusResult(statusResp.Payload)
			if err != nil {
				return fmt.Errorf("解析精灵试炼模式失败: %w", err)
			}

			const stTrialsMaxLossRetries = 3
			active := 0
			for _, mode := range modes {
				if mode.RemainChallengeTimes == 0 {
					continue
				}
				active++

				fetchInfo := func() (*protocol.StTrialsModeInfo, error) {
					cCtx, cancel := context.WithTimeout(ctx, callTimeout)
					resp, cErr := session.Call(cCtx, protocol.BuildStTrialsModeInfoPacket(mode.ModeType), protocol.ActionIDStTrialsModeInfo)
					cancel()
					if cErr != nil {
						return nil, cErr
					}
					return protocol.ParseStTrialsModeInfoResult(resp.Payload)
				}
				claim := func(info *protocol.StTrialsModeInfo) error {
					aCtx, aCancel := context.WithTimeout(ctx, callTimeout)
					resp, aErr := session.Call(aCtx, protocol.BuildStTrialsGetAwardPacket(info.Floor, info.Pass, mode.ModeType), protocol.ActionIDStTrialsGetAward)
					aCancel()
					if aErr != nil {
						return aErr
					}
					result, awards, pErr := protocol.ParseStTrialsAwardResult(resp.Payload)
					if pErr != nil {
						return pErr
					}
					switch result {
					case protocol.StTrialsResultSuccess:
						slog.Info("精灵试炼奖励领取成功", "role_id", session.RoleID(), "mode", mode.ModeType, "floor", info.Floor, "awards", awards)
					case protocol.StTrialsResultIsGet:
						// 已领取，忽略
					case protocol.StTrialsResultItemPackFull,
						protocol.StTrialsResultFatePackFull,
						protocol.StTrialsResultFivePackFull:
						return scheduler.ErrBagFull
					default:
						slog.Debug("精灵试炼奖励未领取", "role_id", session.RoleID(), "mode", mode.ModeType, "result", result)
					}
					return nil
				}

				info, iErr := fetchInfo()
				if iErr != nil {
					slog.Warn("查询精灵试炼模式详情失败", "role_id", session.RoleID(), "mode", mode.ModeType, "err", iErr)
					continue
				}
				if err := claim(info); err != nil {
					if errors.Is(err, scheduler.ErrBagFull) {
						return err
					}
					slog.Warn("领取精灵试炼奖励失败", "role_id", session.RoleID(), "mode", mode.ModeType, "err", err)
				}

				losses := 0
				for i := uint8(0); i < mode.RemainChallengeTimes; i++ {
					cCtx, cCancel := context.WithTimeout(ctx, 15*time.Second) // 战报较大
					resp, cErr := session.Call(cCtx, protocol.BuildStTrialsChallengePacket(mode.ModeType), protocol.ActionIDStTrialsChallenge)
					cCancel()
					if cErr != nil {
						slog.Info("精灵试炼挑战无应答", "role_id", session.RoleID(), "mode", mode.ModeType, "err", cErr)
						break
					}
					result, pErr := protocol.ParseStTrialsChallengeResult(resp.Payload)
					if pErr != nil {
						slog.Warn("解析精灵试炼挑战回包失败", "role_id", session.RoleID(), "err", pErr)
						break
					}
					switch result {
					case protocol.StTrialsResultSuccess:
						losses = 0
						slog.Info("精灵试炼挑战成功", "role_id", session.RoleID(), "mode", mode.ModeType, "index", i+1)
						if info, iErr = fetchInfo(); iErr == nil {
							if cErr := claim(info); cErr != nil {
								if errors.Is(cErr, scheduler.ErrBagFull) {
									return cErr
								}
							}
						}
					case protocol.StTrialsResultFaild:
						losses++
						if losses >= stTrialsMaxLossRetries {
							slog.Info("精灵试炼连续挑战失败，达到重试上限，停止该模式",
								"role_id", session.RoleID(), "mode", mode.ModeType,
								"losses", losses, "max_retries", stTrialsMaxLossRetries)
							i = mode.RemainChallengeTimes
						} else {
							slog.Info("精灵试炼挑战失败，重试同一模式",
								"role_id", session.RoleID(), "mode", mode.ModeType,
								"losses", losses, "max_retries", stTrialsMaxLossRetries)
						}
					case protocol.StTrialsResultCannotChallenge, protocol.StTrialsResultTimeOut:
						slog.Info("精灵试炼当前不可挑战", "role_id", session.RoleID(), "mode", mode.ModeType, "result", result)
						i = mode.RemainChallengeTimes
					default:
						slog.Info("精灵试炼挑战未成功", "role_id", session.RoleID(), "mode", mode.ModeType, "result", result)
						i = mode.RemainChallengeTimes
					}
				}
			}
			slog.Info("精灵试炼巡检完成", "role_id", session.RoleID(), "modes", len(modes), "active_modes", active)
			return nil
		},
	)
}
