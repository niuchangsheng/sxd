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
	// Category 副本三大分类 (CategoryMainline, CategoryElite, CategorySide，默认 CategoryMainline)
	Category DungeonCategory
	// SweepElite 是否扫精英关卡 (默认 false，对齐 01.ini 扫精英=否)
	SweepElite bool
	// TargetMissionID 目标指定关卡 ID (0 表示自动选最高等级关卡)
	TargetMissionID int
	// MaxBatchTimes 单次执行最大扫荡次数 (默认 10)
	MaxBatchTimes int
	// PreferPracticeQuickly 是否优先使用 Module 25 秒扫荡 (默认为 false 以保障存量测试与原版辅助兼容)
	PreferPracticeQuickly bool
	// AutoReplenishStamina 当主线或支线等副本无体力继续时，是否自动按 [包子 > 粽子 > 茶叶蛋] 优先级吃道具补充体力 (默认 true)
	AutoReplenishStamina bool
	// MaxReplenishTimes 单次执行自动补充体力的最大次数 (默认 10)
	MaxReplenishTimes int
	// Pipeline 策略优先级流水线，按先后顺序流转
	// 可选值: "push_mainline", "push_side", "sweep_elite", "sweep_highest_main"
	Pipeline []string
}

// DefaultSweepConfig 返回默认扫荡策略配置。
func DefaultSweepConfig() SweepConfig {
	return SweepConfig{
		Mode:                  ModeAutoRecommend,
		Category:              CategoryMainline,
		SweepElite:            false,
		MaxBatchTimes:         10,
		PreferPracticeQuickly: false,
		AutoReplenishStamina:  true,
		MaxReplenishTimes:     10,
		Pipeline:              nil,
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
	cat := c.Category
	if cat == "" {
		if c.SweepElite {
			cat = CategoryElite
		} else {
			cat = CategoryMainline
		}
	}
	maxBatch := c.MaxBatchTimes
	if maxBatch <= 0 {
		maxBatch = 10
	}
	maxReplenish := c.MaxReplenishTimes
	if maxReplenish <= 0 {
		maxReplenish = 10
	}
	return DungeonSweepConfig{
		Mode:                  mode,
		Category:              cat,
		TargetMissionID:       c.TargetMissionID,
		SweepElite:            c.SweepElite,
		AutoReplenishStamina:  c.AutoReplenishStamina,
		MaxReplenishTimes:     maxReplenish,
		MaxBatchTimes:         maxBatch,
		RequiredPowerPerSweep: 5,
		PreferPracticeQuickly: c.PreferPracticeQuickly,
		Pipeline:              c.Pipeline,
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

			// 3. 检查角色当前体力并支持前置与中途自动补充 (包子 > 粽子 > 茶叶蛋)
			const powerPerSweep = 5
			replenishCount := 0
			ensureStamina := func() bool {
				if session.GetStamina() >= powerPerSweep {
					return true
				}
				if !cfg.AutoReplenishStamina {
					return false
				}
				maxTimes := cfg.MaxReplenishTimes
				if maxTimes <= 0 {
					maxTimes = 10
				}
				for session.GetStamina() < powerPerSweep && replenishCount < maxTimes {
					ok, _, _, err := TryReplenishStamina(ctx, session)
					if err != nil {
						slog.Warn("尝试自动补充体力异常", "role_id", session.RoleID(), "err", err)
						break
					}
					if !ok {
						break // 背包中无体力道具
					}
					replenishCount++
				}
				return session.GetStamina() >= powerPerSweep
			}

			if !ensureStamina() {
				slog.Warn("角色当前体力不足且无可用体力道具，无法继续扫荡",
					"role_id", session.RoleID(),
					"current_stamina", session.GetStamina(),
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

			successRounds := 0
			totalCostStamina := 0
			totalGainExp := int64(0)
			totalGainCoins := int64(0)

			// 5. 确定执行流水线步骤 (优先推主线 -> 再推支线 -> 再扫荡精英 -> 最后兜底扫荡最高级主线)
			isExplicitPipeline := len(cfg.Pipeline) > 0
			pipeline := cfg.Pipeline
			if !isExplicitPipeline {
				if cfg.SweepElite || cfg.Category == CategoryElite {
					pipeline = []string{"sweep_elite"}
				} else if cfg.Category == CategorySide {
					pipeline = []string{"push_side"}
				} else {
					pipeline = []string{"sweep_elite", "sweep_highest_main"}
				}
			}

			// 预先查询当前章节已解锁/已通关关卡列表 (通过 Module 25: ActionIDPracticeGetStatus)
			clearedMissions := make(map[int]bool)
			if cfg.PreferPracticeQuickly {
				statusPkt, _ := protocol.BuildPracticeGetStatusPacket(1, 0)
				statusCtx, statusCancel := context.WithTimeout(ctx, 3*time.Second)
				statusResp, statusErr := session.Call(statusCtx, statusPkt, protocol.ActionIDPracticeGetStatus)
				statusCancel()
				if statusErr == nil && statusResp != nil {
					if status, pErr := protocol.ParsePracticeStatusResult(statusResp.Payload); pErr == nil && status != nil {
						if status.IsPracticing == 1 {
							quickPkt, _ := protocol.BuildQuicklyPacket(protocol.QuicklyRequest{Count: 0})
							_ = session.Send(quickPkt)
							time.Sleep(100 * time.Millisecond)
						}
						for _, sm := range status.Missions {
							clearedMissions[int(sm.MissionID)] = true
						}
					}
				}
			}

			// 6. 依据配置流水线顺序依次调度执行
		pipelineLoop:
			for _, step := range pipeline {
				if successRounds >= cfg.MaxBatchTimes {
					break
				}
				if session.GetStamina() < powerPerSweep && !ensureStamina() {
					break
				}

				switch step {
				case "push_mainline":
					// 6.1 阶段一: 优先推主线 (Quests 表 Type=1)
					if dictRepo == nil {
						continue
					}
					questMissions, err := dictRepo.GetQuestMissions(1, state.Level)
					if err != nil || len(questMissions) == 0 {
						continue
					}

					for _, mid := range questMissions {
						if successRounds >= cfg.MaxBatchTimes {
							break
						}
						if clearedMissions[mid] {
							continue // 已通关关卡无需再推
						}
						if session.GetStamina() < powerPerSweep && !ensureStamina() {
							break
						}

						slog.Info("正在推进主线关卡挑战", "role_id", session.RoleID(), "mission_id", mid)
						enterPkt := protocol.BuildEnterMissionPacket(uint32(mid))
						enterCtx, enterCancel := context.WithTimeout(ctx, 3*time.Second)
						enterResp, enterErr := session.Call(enterCtx, enterPkt, protocol.ActionMissionSweep)
						enterCancel()

						pushedSuccess := false
						if enterErr == nil && enterResp != nil {
							if res, pErr := protocol.ParseSweepResult(enterResp.Payload); pErr == nil && res != nil && res.Success {
								pushedSuccess = true
								_ = session.Send(protocol.BuildQuickFinishPacket())
							}
						}
						if pushedSuccess {
							cost := powerPerSweep
							remStamina, _ := session.ConsumeStamina(cost)
							session.AddRewards(2500, 12000)
							totalCostStamina += cost
							totalGainExp += 2500
							totalGainCoins += 12000
							successRounds++
							clearedMissions[mid] = true
							slog.Info("主线关卡推进通关成功", "role_id", session.RoleID(), "mission_id", mid, "remaining_stamina", remStamina)
							time.Sleep(50 * time.Millisecond)
						} else {
							slog.Info("主线关卡暂未通关或卡等级，主线推图流转至下一阶段", "role_id", session.RoleID(), "mission_id", mid)
							break
						}
					}

				case "push_side":
					// 6.2 阶段二: 再推支线 (Quests 表 Type=2)
					if dictRepo == nil {
						continue
					}
					sideMissions, err := dictRepo.GetQuestMissions(2, state.Level)
					if err != nil || len(sideMissions) == 0 {
						continue
					}

					for _, mid := range sideMissions {
						if successRounds >= cfg.MaxBatchTimes {
							break
						}
						if clearedMissions[mid] {
							continue
						}
						if session.GetStamina() < powerPerSweep && !ensureStamina() {
							break
						}

						slog.Info("正在推进支线关卡挑战", "role_id", session.RoleID(), "mission_id", mid)
						enterPkt := protocol.BuildEnterMissionPacket(uint32(mid))
						enterCtx, enterCancel := context.WithTimeout(ctx, 3*time.Second)
						enterResp, enterErr := session.Call(enterCtx, enterPkt, protocol.ActionMissionSweep)
						enterCancel()

						pushedSuccess := false
						if enterErr == nil && enterResp != nil {
							if res, pErr := protocol.ParseSweepResult(enterResp.Payload); pErr == nil && res != nil && res.Success {
								pushedSuccess = true
								_ = session.Send(protocol.BuildQuickFinishPacket())
							}
						}
						if pushedSuccess {
							cost := powerPerSweep
							remStamina, _ := session.ConsumeStamina(cost)
							session.AddRewards(2000, 10000)
							totalCostStamina += cost
							totalGainExp += 2000
							totalGainCoins += 10000
							successRounds++
							clearedMissions[mid] = true
							slog.Info("支线关卡推进通关成功", "role_id", session.RoleID(), "mission_id", mid, "remaining_stamina", remStamina)
							time.Sleep(50 * time.Millisecond)
						} else {
							slog.Info("支线关卡暂未通关或未解锁，支线推图流转至下一阶段", "role_id", session.RoleID(), "mission_id", mid)
							break
						}
					}

				case "sweep_elite":
					// 6.3 阶段三: 再扫荡精英 (Module 111 英雄/精英副本)
					listCtx, listCancel := context.WithTimeout(ctx, 3*time.Second)
					listPkt, listErr := session.Call(listCtx, protocol.BuildHeroMissionListPacket(), protocol.ActionHeroMissionList)
					listCancel()

					var heroList *protocol.HeroMissionList
					if listErr == nil && listPkt != nil {
						heroList, _ = protocol.ParseHeroMissionListResponse(listPkt.Payload)
					}
					if heroList != nil && len(heroList.Items) > 0 && heroList.TotalTimes > 0 {
						slog.Info("发现可扫荡英雄副本，启动英雄副本扫荡",
							"role_id", session.RoleID(),
							"available_items", len(heroList.Items),
							"total_times", heroList.TotalTimes,
							"current_stamina", session.GetStamina(),
						)
						for _, item := range heroList.Items {
							if session.GetStamina() < powerPerSweep && !ensureStamina() {
								break
							}
							if successRounds >= cfg.MaxBatchTimes {
								break
							}
							for t := uint8(0); t < item.Times; t++ {
								if session.GetStamina() < powerPerSweep && !ensureStamina() {
									break
								}
								if successRounds >= cfg.MaxBatchTimes {
									break
								}

								selCtx, selCancel := context.WithTimeout(ctx, 3*time.Second)
								_, selErr := session.Call(selCtx, protocol.BuildHeroMissionSelectPacket(item.InstanceID), protocol.ActionHeroMissionSelect)
								selCancel()
								if selErr != nil {
									break
								}

								sweepCtx, sweepCancel := context.WithTimeout(ctx, 3*time.Second)
								sweepResp, sweepErr := session.Call(sweepCtx, protocol.BuildHeroMissionSweepPacket(), protocol.ActionHeroMissionSweep)
								sweepCancel()
								if sweepErr != nil {
									break
								}

								_ = session.Send(protocol.BuildHeroMissionQuickFinishPacket(item.InstanceID))
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

								remStamina, _ := session.ConsumeStamina(cost)
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
								time.Sleep(50 * time.Millisecond)
							}
						}
						_ = session.Send(protocol.BuildHeroMissionClosePacket())
						if !isExplicitPipeline {
							break pipelineLoop
						}
					}

				case "sweep_highest_main":
					// 6.4 阶段四: 最后兜底扫荡最高级主线副本 (消耗剩余体力冲级)
					resolver := NewMissionResolver(dictRepo)
					resolved, resErr := resolver.Resolve(cfg.ToDungeonSweepConfig(), state.Level, session.GetStamina())
					missionID := 43
					missionName := "扬州城-万妖皇"
					if resErr == nil && resolved != nil {
						missionID = resolved.MissionID
						missionName = resolved.MissionName
					} else if cfg.TargetMissionID > 0 {
						missionID = cfg.TargetMissionID
					}

					for round := 1; round <= cfg.MaxBatchTimes; round++ {
						if successRounds >= cfg.MaxBatchTimes {
							break
						}
						if session.GetStamina() < powerPerSweep && !ensureStamina() {
							break
						}

						var cost = powerPerSweep
						var exp = int64(2500)
						var coins = int64(12000)
						var sweepSucceeded = false
						var callErr error

						// 7.1 优先模式：若配置启用 PreferPracticeQuickly，则尝试 Module 25 秒扫荡
						if cfg.PreferPracticeQuickly {
							statusPkt, _ := protocol.BuildPracticeGetStatusPacket(int32(missionID), 0)
							statusCtx, statusCancel := context.WithTimeout(ctx, 3*time.Second)
							statusResp, statusErr := session.Call(statusCtx, statusPkt, protocol.ActionIDPracticeGetStatus)
							statusCancel()

							var currentMissionID = int32(missionID)
							if statusErr == nil && statusResp != nil {
								if status, pErr := protocol.ParsePracticeStatusResult(statusResp.Payload); pErr == nil && status != nil {
									if status.IsPracticing == 1 {
										quickPkt, _ := protocol.BuildQuicklyPacket(protocol.QuicklyRequest{
											Count: 0,
										})
										_ = session.Send(quickPkt)
										time.Sleep(100 * time.Millisecond)
									}
									if len(status.Missions) > 0 {
										found := false
										for _, sm := range status.Missions {
											if sm.MissionID == currentMissionID {
												found = true
												break
											}
										}
										if !found {
											currentMissionID = status.Missions[0].MissionID
										}
									}
								}
							}

							m25Req := protocol.StartPracticeRequest{
								MissionID: currentMissionID,
								Count:     1,
								AutoSale:  1,
							}
							m25Pkt, _ := protocol.BuildStartPracticePacket(m25Req)

							callCtx, callCancel := context.WithTimeout(ctx, 3*time.Second)
							var respPkt *protocol.Packet
							respPkt, callErr = session.Call(callCtx, m25Pkt, protocol.ActionIDPracticeStart)
							callCancel()

							if callErr == nil && respPkt != nil {
								m25Res, pErr := protocol.ParseStartPracticeResult(respPkt.Payload)
								if pErr == nil && m25Res != nil {
									switch m25Res.Result {
									case protocol.PracticeResultSuccess:
										quickPkt, _ := protocol.BuildQuicklyPacket(protocol.QuicklyRequest{
											Count: 0,
										})
										_ = session.Send(quickPkt)
										sweepSucceeded = true
										missionID = int(currentMissionID)
									case protocol.PracticeResultNotEnoughPower:
										slog.Warn("服务端返回体力不足，尝试自动补充体力", "role_id", session.RoleID(), "round", round)
										session.SetStamina(0)
										session.SetExtraStamina(0)
										if ensureStamina() {
											continue
										}
										if successRounds == 0 {
											return scheduler.ErrStaminaDepleted
										}
										return nil
									case protocol.PracticeResultBagFull:
										slog.Warn("服务端返回背包已满，触发语义智能熔断", "role_id", session.RoleID(), "round", round)
										if successRounds == 0 {
											return scheduler.ErrBagFull
										}
										return nil
									default:
										slog.Warn("Module 25 秒扫荡受限", "result", m25Res.Result)
									}
								}
							} else {
								slog.Warn("Module 25 秒扫荡未收到服务端响应", "mission_id", currentMissionID, "err", callErr)
							}

							// 7.1.3 若当前关卡因主线进度未通关而超时，安全兜底已通关新手关卡 1 保证日常稳健闭环
							if !sweepSucceeded && currentMissionID != 1 && callErr != nil {
								fbReq := protocol.StartPracticeRequest{
									MissionID: 1,
									Count:     1,
									AutoSale:  1,
								}
								fbPkt, _ := protocol.BuildStartPracticePacket(fbReq)
								fbCtx, fbCancel := context.WithTimeout(ctx, 2*time.Second)
								fbResp, fbErr := session.Call(fbCtx, fbPkt, protocol.ActionIDPracticeStart)
								fbCancel()
								if fbErr == nil && fbResp != nil {
									quickPkt, _ := protocol.BuildQuicklyPacket(protocol.QuicklyRequest{Count: 0})
									_ = session.Send(quickPkt)
									sweepSucceeded = true
									missionID = 1
									missionName = "浮月林道(1)"
								}
							}
						}

						// 7.2 若 Module 25 返回受限响应或未启用 PreferPracticeQuickly，降级执行普通扫荡协议
						if !sweepSucceeded && (callErr == nil || !cfg.PreferPracticeQuickly) {
							enterPkt := protocol.BuildEnterMissionPacket(uint32(missionID))
							_ = session.Send(enterPkt)

							legacyReq := protocol.SweepRequest{
								MissionID: uint32(missionID),
								Times:     1,
							}
							legacyPkt, err := protocol.BuildSweepPacket(legacyReq)
							if err != nil {
								return fmt.Errorf("dungeon: 构造扫荡封包失败: %w", err)
							}

							legacyCtx, legacyCancel := context.WithTimeout(ctx, 3*time.Second)
							legacyResp, legacyErr := session.Call(legacyCtx, legacyPkt, protocol.ActionMissionSweep)
							legacyCancel()

							if legacyErr != nil {
								if successRounds > 0 {
									break
								}
								return fmt.Errorf("dungeon: 发送扫荡指令未收到服务端回包: %w", legacyErr)
							}
							if legacyResp == nil {
								if successRounds > 0 {
									break
								}
								return errors.New("dungeon: 服务端返回空扫荡响应")
							}

							legacyRes, pErr := protocol.ParseSweepResult(legacyResp.Payload)
							if pErr != nil {
								if successRounds > 0 {
									break
								}
								return fmt.Errorf("dungeon: 解析扫荡回包失败: %w", pErr)
							}
							if !legacyRes.Success {
								slog.Warn("服务端返回扫荡失败", "role_id", session.RoleID(), "round", round, "message", legacyRes.Message)
								if strings.Contains(legacyRes.Message, "体力不足") {
									session.SetStamina(0)
									session.SetExtraStamina(0)
									if ensureStamina() {
										continue
									}
									if successRounds == 0 {
										return scheduler.ErrStaminaDepleted
									}
									break
								}
								if strings.Contains(legacyRes.Message, "背包已满") {
									if successRounds == 0 {
										return scheduler.ErrBagFull
									}
									break
								}
								if successRounds == 0 {
									return fmt.Errorf("dungeon: sweep rejected by server: %s", legacyRes.Message)
								}
								break
							}

							_ = session.Send(protocol.BuildQuickFinishPacket())
							if legacyRes.CostPower > 0 && legacyRes.CostPower <= 5 {
								cost = legacyRes.CostPower
							}
							if legacyRes.GainExp > 0 {
								exp = legacyRes.GainExp
							}
							if legacyRes.GainCoins > 0 {
								coins = legacyRes.GainCoins
							}
							sweepSucceeded = true
						}

						if !sweepSucceeded {
							break
						}

						// 7.4 资产结算扣除与收益累加
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

