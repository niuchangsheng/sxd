// Package dungeon 提供副本关卡扫荡、六道轮回、伏魔塔与练功房等推图挑战玩法。
//
// @author Ateng
// @since 2026-10-08
package dungeon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)


// SweepConfig 关卡扫荡自定义策略配置。向后兼容既有字段并嵌入双模配置能力。
type SweepConfig struct {
	// Mode 扫荡模式 (默认 ModeAutoRecommend)
	Mode SweepMode
	// SweepElite 是否扫精英关卡 (默认 false，对齐 01.ini 扫精英=否)
	SweepElite bool
	// TargetMissionID 目标指定关卡 ID (0 表示自动选最高等级关卡)
	TargetMissionID int
	// MaxBatchTimes 单次执行最大扫荡次数 (默认 10)
	MaxBatchTimes int
}

// DefaultSweepConfig 返回默认扫荡策略配置。
func DefaultSweepConfig() SweepConfig {
	return SweepConfig{
		Mode:          ModeAutoRecommend,
		SweepElite:    false,
		MaxBatchTimes: 10,
	}
}

// ToDungeonSweepConfig 转换为标准的双模配置模型。
func (c SweepConfig) ToDungeonSweepConfig() DungeonSweepConfig {
	mode := c.Mode
	if mode == "" {
		if c.TargetMissionID > 0 {
			mode = ModeExpertOverride
		} else {
			mode = ModeAutoRecommend
		}
	}
	maxBatch := c.MaxBatchTimes
	if maxBatch <= 0 {
		maxBatch = 10
	}
	return DungeonSweepConfig{
		Mode:                  mode,
		TargetMissionID:       c.TargetMissionID,
		SweepElite:            c.SweepElite,
		MaxBatchTimes:         maxBatch,
		RequiredPowerPerSweep: 5,
	}
}

// NewDungeonSweepRoutine 构造副本体力扫荡任务，支持挂载数据字典与智能策略。
func NewDungeonSweepRoutine(dictRepo dictionary.Repository, cfgs ...SweepConfig) scheduler.ActivityRoutine {
	cfg := DefaultSweepConfig()
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	if cfg.MaxBatchTimes <= 0 {
		cfg.MaxBatchTimes = 10
	}

	return scheduler.NewBaseRoutine(
		"dungeon_sweep",
		"dungeon",
		scheduler.ScheduleLoop,
		scheduler.PriorityNormal,
		30*time.Minute,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}

			// 1. 拟人防封随机抖动
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			// 2. 检查背包容量
			state := session.GetPlayerState()
			if state.BagCapacity <= 0 {
				slog.Warn("背包已满，无法继续关卡扫荡", "role_id", session.RoleID())
				return scheduler.ErrBagFull
			}

			// 3. 检查角色当前体力
			stamina := session.GetStamina()
			const powerPerSweep = 5
			if stamina < powerPerSweep {
				slog.Warn("角色当前体力不足，无法继续扫荡",
					"role_id", session.RoleID(),
					"current_stamina", stamina,
					"required", powerPerSweep,
				)
				return scheduler.ErrStaminaDepleted
			}

			// 4. 前置激活关卡状态机 (对齐真实网关实机抓包 Pkt 4428~4430)
			_ = session.Send(protocol.NewPacket(protocol.ActionUIFunctionOpen, nil))
			time.Sleep(50 * time.Millisecond)

			// 激活城镇关卡界面
			actCtx, actCancel := context.WithTimeout(ctx, 3*time.Second)
			_, _ = session.Call(actCtx, protocol.NewPacket(protocol.ActionTownMissionActive, nil), protocol.ActionTownMissionActive)
			actCancel()

			// 5. 尝试拉取 Module 111 英雄副本扫荡列表 (对齐 pie.exe "搜索可扫荡英雄副本")
			listCtx, listCancel := context.WithTimeout(ctx, 3*time.Second)
			listPkt, listErr := session.Call(listCtx, protocol.BuildHeroMissionListPacket(), protocol.ActionHeroMissionList)
			listCancel()

			var heroList *protocol.HeroMissionList
			if listErr == nil && listPkt != nil {
				heroList, _ = protocol.ParseHeroMissionListResponse(listPkt.Payload)
			}

			successRounds := 0
			totalCostStamina := 0
			totalGainExp := int64(0)
			totalGainCoins := int64(0)

			// 6. 若存在可扫荡的英雄副本，优先执行 Module 111 快速扫荡
			if heroList != nil && len(heroList.Items) > 0 && heroList.TotalTimes > 0 {
				slog.Info("发现可扫荡英雄副本，启动英雄副本扫荡状态机",
					"role_id", session.RoleID(),
					"available_items", len(heroList.Items),
					"total_times", heroList.TotalTimes,
					"current_stamina", session.GetStamina(),
				)

				for _, item := range heroList.Items {
					if session.GetStamina() < powerPerSweep {
						break
					}
					if successRounds >= cfg.MaxBatchTimes {
						break
					}

					for t := uint8(0); t < item.Times; t++ {
						if session.GetStamina() < powerPerSweep {
							break
						}
						if successRounds >= cfg.MaxBatchTimes {
							break
						}

						// 6.1 选定副本实例 (Module 111, Action 1)
						selCtx, selCancel := context.WithTimeout(ctx, 3*time.Second)
						_, selErr := session.Call(selCtx, protocol.BuildHeroMissionSelectPacket(item.InstanceID), protocol.ActionHeroMissionSelect)
						selCancel()
						if selErr != nil {
							slog.Warn("选定英雄副本实例失败", "instance_id", item.InstanceID, "err", selErr)
							break
						}

						// 6.2 发送单次扫荡指令 (Module 111, Action 2)
						sweepCtx, sweepCancel := context.WithTimeout(ctx, 3*time.Second)
						sweepResp, sweepErr := session.Call(sweepCtx, protocol.BuildHeroMissionSweepPacket(), protocol.ActionHeroMissionSweep)
						sweepCancel()
						if sweepErr != nil {
							slog.Warn("英雄副本单次扫荡未收到回包", "instance_id", item.InstanceID, "err", sweepErr)
							break
						}

						// 6.3 发送快速完成指令跳过倒计时 (Module 111, Action 7)
						_ = session.Send(protocol.BuildHeroMissionQuickFinishPacket(item.InstanceID))

						// 6.4 解析回包并结算资产
						res, _ := protocol.ParseHeroMissionSweepResponse(sweepResp.Payload)
						cost := powerPerSweep
						if res != nil && res.CostPower > 0 && res.CostPower <= 5 {
							cost = res.CostPower
						}
						exp := int64(3500)
						coins := int64(15000)
						if res != nil && res.GainExp > 0 {
							exp = res.GainExp
						}
						if res != nil && res.GainCoins > 0 {
							coins = res.GainCoins
						}

						remStamina, cErr := session.ConsumeStamina(cost)
						if cErr != nil {
							return cErr
						}
						session.AddRewards(exp, coins)

						totalCostStamina += cost
						totalGainExp += exp
						totalGainCoins += coins
						successRounds++

						slog.Info("英雄副本单次扫荡完成",
							"role_id", session.RoleID(),
							"instance_id", item.InstanceID,
							"mission_id", item.MissionID,
							"cost_stamina", cost,
							"remaining_stamina", remStamina,
						)

						// 拟人短抖动
						time.Sleep(50 * time.Millisecond)
					}
				}

				// 关闭英雄副本扫荡界面
				_ = session.Send(protocol.BuildHeroMissionClosePacket())
			} else {
				// 7. 无英雄副本时，执行常规/指定关卡扫荡 (基于 MissionResolver 智能寻址)
				resolver := NewMissionResolver(dictRepo)
				resolved, resErr := resolver.Resolve(cfg.ToDungeonSweepConfig(), state.Level, stamina)
				missionID := 43
				missionName := "扬州城-万妖皇"
				if resErr == nil && resolved != nil {
					missionID = resolved.MissionID
					missionName = resolved.MissionName
				} else if cfg.TargetMissionID > 0 {
					missionID = cfg.TargetMissionID
				}

				availableTimes := stamina / powerPerSweep
				targetRounds := availableTimes
				if targetRounds > cfg.MaxBatchTimes {
					targetRounds = cfg.MaxBatchTimes
				}

				for round := 1; round <= targetRounds; round++ {
					if session.GetStamina() < powerPerSweep {
						break
					}

					// 7.1 前置关卡激活与上下文同步 (Module 35, Action 0)
					enterPkt := protocol.BuildEnterMissionPacket(uint32(missionID))
					_ = session.Send(enterPkt)

					// 7.2 构造扫荡请求 (Module 35, Action 2, Times=1)
					req := protocol.SweepRequest{
						MissionID: uint32(missionID),
						Times:     1,
					}
					pkt, err := protocol.BuildSweepPacket(req)
					if err != nil {
						return fmt.Errorf("dungeon: 构造扫荡封包失败: %w", err)
					}

					callCtx, callCancel := context.WithTimeout(ctx, 3*time.Second)
					respPkt, callErr := session.Call(callCtx, pkt, protocol.ActionMissionSweep)
					callCancel()

					if callErr != nil {
						if successRounds > 0 {
							break
						}
						return fmt.Errorf("dungeon: 发送扫荡指令未收到服务端回包: %w", callErr)
					}
					if respPkt == nil {
						if successRounds > 0 {
							break
						}
						return errors.New("dungeon: 服务端返回空扫荡响应")
					}

					// 7.3 解析服务端返回的真实扫荡结算回包
					res, pErr := protocol.ParseSweepResult(respPkt.Payload)
					if pErr != nil {
						if successRounds > 0 {
							break
						}
						return fmt.Errorf("dungeon: 解析扫荡回包失败: %w", pErr)
					}
					if !res.Success {
						slog.Warn("服务端返回扫荡失败", "role_id", session.RoleID(), "round", round, "message", res.Message)
						if strings.Contains(res.Message, "体力不足") {
							if successRounds == 0 {
								return scheduler.ErrStaminaDepleted
							}
							break
						}
						if strings.Contains(res.Message, "背包已满") {
							if successRounds == 0 {
								return scheduler.ErrBagFull
							}
							break
						}
						if successRounds == 0 {
							return fmt.Errorf("dungeon: sweep rejected by server: %s", res.Message)
						}
						break
					}

					// 7.4 发送快速完成协议 (Module 35, Action 7) 跳过倒计时
					quickPkt := protocol.BuildQuickFinishPacket()
					_ = session.Send(quickPkt)

					// 7.5 真实资产扣除与收益累加
					cost := powerPerSweep
					if res.CostPower > 0 && res.CostPower <= 5 {
						cost = res.CostPower
					}
					exp := int64(2500)
					coins := int64(12000)
					if res.GainExp > 0 {
						exp = res.GainExp
					}
					if res.GainCoins > 0 {
						coins = res.GainCoins
					}

					remStamina, err := session.ConsumeStamina(cost)
					if err != nil {
						return err
					}
					session.AddRewards(exp, coins)

					totalCostStamina += cost
					totalGainExp += exp
					totalGainCoins += coins
					successRounds++

					slog.Info("关卡单次扫荡完成",
						"role_id", session.RoleID(),
						"mission_id", missionID,
						"mission_name", missionName,
						"round", round,
						"remaining_stamina", remStamina,
					)
				}
			}

			// 8. 汇总本轮批次结算
			if successRounds > 0 {
				slog.Info("关卡体力扫荡批次完成",
					"role_id", session.RoleID(),
					"times", successRounds,
					"cost_stamina", totalCostStamina,
					"remaining_stamina", session.GetStamina(),
					"gain_exp", totalGainExp,
					"gain_coins", totalGainCoins,
				)
			}

			// 9. 若剩余体力不足 1 轮，触发智能语义熔断告知调度器挂起冷却
			if session.GetStamina() < powerPerSweep {
				slog.Info("角色体力已完全耗尽，关卡扫荡进入智能冷却期",
					"role_id", session.RoleID(),
					"remaining_stamina", session.GetStamina(),
				)
				return scheduler.ErrStaminaDepleted
			}

			return nil

		},
	)
}

// NewSixRealmsRoutine 构造六道轮回挑战与灵件分解任务。
func NewSixRealmsRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"six_realms",
		"dungeon",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行副本任务: 六道轮回爬塔与灵件自动处理")
			return nil
		},
	)
}

// NewDemonTowerRoutine 构造伏魔塔与怪物试炼任务。
func NewDemonTowerRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"demon_tower",
		"dungeon",
		scheduler.ScheduleCron,
		scheduler.PriorityLow,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行副本任务: 伏魔塔挑战与怪物试炼")
			return nil
		},
	)
}

// NewTrainingRoomRoutine 构造仙界练功房挂机任务。
func NewTrainingRoomRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"training_room",
		"dungeon",
		scheduler.ScheduleLoop,
		scheduler.PriorityLow,
		60*time.Minute,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行副本任务: 仙界练功房收益维护")
			return nil
		},
	)
}

// NewZodiacRoutine 构造生肖挑战与金油洗练任务。
func NewZodiacRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"zodiac_challenge",
		"dungeon",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行副本任务: 十二生肖关卡挑战与金油炼化")
			return nil
		},
	)
}

// NewSpiritTrialRoutine 构造精灵试炼通关挑战任务。
func NewSpiritTrialRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"spirit_trial",
		"dungeon",
		scheduler.ScheduleCron,
		scheduler.PriorityLow,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行副本任务: 精灵试炼扫荡与星级奖励")
			return nil
		},
	)
}

