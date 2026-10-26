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


// QuestCycleFeatureConfig 剧情推图任务闭环配置
type QuestCycleFeatureConfig struct {
	Enabled              bool `json:"enabled"`
	AutoReplenishStamina bool `json:"auto_replenish_stamina"` // 体力不足时自动按 [包子 > 粽子 > 茶叶蛋] 自愈 (仅作用于推图)
	MaxReplenishTimes    int  `json:"max_replenish_times"`    // 单次推图最大自愈补充次数 (仅作用于推图)
}

// SweepFeatureConfig 副本关卡扫荡配置
type SweepFeatureConfig struct {
	Enabled               bool     `json:"enabled"`
	Pipeline              []string `json:"pipeline"`                // 扫荡流水线优先级: "sweep_elite", "sweep_highest_main"
	MaxBatchTimes         int      `json:"max_batch_times"`         // 扫荡批次单次最大执行次数 (仅作用于扫荡)
	PreferPracticeQuickly bool     `json:"prefer_practice_quickly"` // 优先使用 Module 25 秒完成协议 (仅作用于扫荡)
}

// SweepConfig 关卡推图与扫荡自定义策略配置。包含两大核心功能：剧情推图 与 关卡扫荡。
type SweepConfig struct {
	// 功能一: 剧情推图 (循环接任务/做任务/交任务直至无任务可做)
	QuestCycle QuestCycleFeatureConfig `json:"quest_cycle"`
	// 功能二: 关卡扫荡 (优先扫荡精英副本，再扫荡最高等级主线副本)
	Sweep SweepFeatureConfig `json:"sweep"`

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
		QuestCycle: QuestCycleFeatureConfig{
			Enabled:              false, // 默认纯扫荡；由 yaml 或 pipeline 显式开启
			AutoReplenishStamina: true,
			MaxReplenishTimes:    10,
		},
		Sweep: SweepFeatureConfig{
			Enabled:               true,
			Pipeline:              []string{"sweep_elite", "sweep_highest_main"},
			MaxBatchTimes:         10,
			PreferPracticeQuickly: false, // 默认为 false 以保障存量测试与原版辅助兼容
		},
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

// NewDungeonRoutine 构造副本推图与扫荡任务 (统一命名为 dungeon)。
func NewDungeonRoutine(dictRepo dictionary.Repository, cfgs ...SweepConfig) scheduler.ActivityRoutine {
	return NewDungeonSweepRoutine(dictRepo, cfgs...)
}

// NewDungeonSweepRoutine 构造副本体力扫荡任务，支持挂载数据字典与智能策略。
func NewDungeonSweepRoutine(dictRepo dictionary.Repository, cfgs ...SweepConfig) scheduler.ActivityRoutine {
	cfg := DefaultSweepConfig()
	if len(cfgs) > 0 {
		c := cfgs[0]
		// 继承外层顶层兼容字段到子功能
		if c.QuestCycle.Enabled || c.QuestCycle.AutoReplenishStamina || c.QuestCycle.MaxReplenishTimes > 0 {
			cfg.QuestCycle = c.QuestCycle
		} else {
			// 若未显式配置子块，从顶层兼容字段继承
			cfg.QuestCycle.AutoReplenishStamina = c.AutoReplenishStamina
			if c.MaxReplenishTimes > 0 {
				cfg.QuestCycle.MaxReplenishTimes = c.MaxReplenishTimes
			}
			if c.Category == CategorySide || (c.Category == CategoryMainline && c.AutoReplenishStamina) {
				cfg.QuestCycle.Enabled = true
			}
		}

		if c.Sweep.Enabled || c.Sweep.MaxBatchTimes > 0 || len(c.Sweep.Pipeline) > 0 {
			cfg.Sweep = c.Sweep
		} else {
			// 从顶层兼容字段继承
			if c.MaxBatchTimes > 0 {
				cfg.Sweep.MaxBatchTimes = c.MaxBatchTimes
			}
			cfg.Sweep.PreferPracticeQuickly = c.PreferPracticeQuickly
			if len(c.Pipeline) > 0 {
				cfg.Sweep.Pipeline = c.Pipeline
			}
		}

		if len(c.Pipeline) > 0 {
			cfg.Pipeline = c.Pipeline
			for _, p := range c.Pipeline {
				if p == "quest_cycle" || p == "push_mainline" || p == "push_side" || p == "quest" {
					cfg.QuestCycle.Enabled = true
				}
				if p == "sweep_elite" || p == "sweep_highest_main" {
					cfg.Sweep.Enabled = true
				}
			}
		}

		cfg.Pipeline = c.Pipeline
		cfg.MaxBatchTimes = c.MaxBatchTimes
		cfg.AutoReplenishStamina = c.AutoReplenishStamina
		cfg.MaxReplenishTimes = c.MaxReplenishTimes
		cfg.PreferPracticeQuickly = c.PreferPracticeQuickly
		cfg.SweepElite = c.SweepElite
		cfg.Category = c.Category
		cfg.Mode = c.Mode
		cfg.TargetMissionID = c.TargetMissionID
	}

	if cfg.Sweep.MaxBatchTimes <= 0 {
		cfg.Sweep.MaxBatchTimes = 10
	}
	if len(cfg.Sweep.Pipeline) == 0 {
		cfg.Sweep.Pipeline = []string{"sweep_elite", "sweep_highest_main"}
	}

	return scheduler.NewBaseRoutine(
		"dungeon",
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
				slog.Warn("背包已满，无法继续关卡推图与扫荡", "role_id", session.RoleID())
				return scheduler.ErrBagFull
			}

			const powerPerSweep = 5

			// 3. 激活关卡界面状态机 (对齐真实网关实机抓包 Pkt 4428~4430)
			_ = session.Send(protocol.NewPacket(protocol.ActionUIFunctionOpen, nil))
			time.Sleep(50 * time.Millisecond)

			actCtx, actCancel := context.WithTimeout(ctx, 3*time.Second)
			_, _ = session.Call(actCtx, protocol.NewPacket(protocol.ActionTownMissionActive, nil), protocol.ActionTownMissionActive)
			actCancel()

			totalCostStamina := 0
			totalGainExp := int64(0)
			totalGainCoins := int64(0)

			// =========================================================================
			// 功能一：主线/支线剧情推图 (quest_cycle)
			// 特性：循环接任务/做任务/交任务直至无可做任务 break；
			// 约束：auto_replenish_stamina 仅作用于推图；推图不受 max_batch_times 限制！
			// =========================================================================
			isExplicitPipeline := len(cfg.Pipeline) > 0
			shouldRunQuest := cfg.QuestCycle.Enabled
			if isExplicitPipeline {
				hasQuestStep := false
				for _, p := range cfg.Pipeline {
					if p == "quest_cycle" || p == "push_mainline" || p == "push_side" || p == "quest" {
						hasQuestStep = true
						break
					}
				}
				shouldRunQuest = hasQuestStep
			}

			if shouldRunQuest {
				replenishCount := 0
				questEnsureStamina := func() bool {
					if session.GetStamina() >= powerPerSweep {
						return true
					}
					// auto_replenish_stamina 只作用于推图！
					if !cfg.QuestCycle.AutoReplenishStamina {
						return false
					}
					maxTimes := cfg.QuestCycle.MaxReplenishTimes
					if maxTimes <= 0 {
						maxTimes = 10
					}
					for session.GetStamina() < powerPerSweep && replenishCount < maxTimes {
						ok, _, _, err := TryReplenishStamina(ctx, session)
						if err != nil {
							slog.Warn("推图尝试自动补充体力异常", "role_id", session.RoleID(), "err", err)
							break
						}
						if !ok {
							break // 背包中无可用体力道具
						}
						replenishCount++
					}
					return session.GetStamina() >= powerPerSweep
				}

				// 推图前置检查体力自愈
				_ = questEnsureStamina()

				if dictRepo != nil {
					if session.State() != client.StateActive {
						for i := 0; i < 15 && session.State() != client.StateActive; i++ {
							time.Sleep(200 * time.Millisecond)
						}
					}

					clearedMissions := make(map[int]bool)
					if cfg.Sweep.PreferPracticeQuickly {
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

					executeQuestCycle(
						ctx, session, dictRepo, cfg, questEnsureStamina,
						&totalCostStamina, &totalGainExp, &totalGainCoins,
						clearedMissions,
					)
				}
			}

			// =========================================================================
			// 功能二：关卡扫荡 (sweep)
			// 特性：优先扫荡精英 (sweep_elite)，再扫荡最高等级主线 (sweep_highest_main)；
			// 约束：max_batch_times 仅作用于扫荡；prefer_practice_quickly 仅作用于扫荡；
			// 保护：扫荡阶段不吃体力药，仅消耗自然体力！
			// =========================================================================
			shouldRunSweep := false
			sweepPipeline := cfg.Sweep.Pipeline
			if isExplicitPipeline {
				var filteredSweep []string
				for _, p := range cfg.Pipeline {
					if p == "sweep_elite" || p == "sweep_highest_main" {
						filteredSweep = append(filteredSweep, p)
					}
				}
				if len(filteredSweep) > 0 {
					sweepPipeline = filteredSweep
					shouldRunSweep = true
				}
			} else {
				if cfg.Sweep.Enabled {
					shouldRunSweep = true
				} else if cfg.Category == CategoryElite || cfg.SweepElite || (cfg.Category == CategoryMainline && !cfg.QuestCycle.Enabled) {
					shouldRunSweep = true
				}
			}

			sweepSuccessRounds := 0
			if shouldRunSweep {
				// 扫荡仅消耗自然体力，不补充体力药
				sweepHasStamina := func() bool {
					return session.GetStamina() >= powerPerSweep
				}

				if !sweepHasStamina() && totalCostStamina == 0 {
					slog.Warn("角色当前体力不足，无法继续扫荡",
						"role_id", session.RoleID(),
						"current_stamina", session.GetStamina(),
						"required", powerPerSweep,
					)
					return scheduler.ErrStaminaDepleted
				}

			sweepLoop:
				for _, step := range sweepPipeline {
					if sweepSuccessRounds >= cfg.Sweep.MaxBatchTimes {
						break
					}
					if session.State() != client.StateActive {
						for i := 0; i < 15 && session.State() != client.StateActive; i++ {
							time.Sleep(200 * time.Millisecond)
						}
					}
					if !sweepHasStamina() {
						slog.Info("当前角色体力不足，关卡扫荡阶段结束", "role_id", session.RoleID(), "remaining_stamina", session.GetStamina())
						break
					}

					switch step {
					case "sweep_elite":
						// 扫荡精英 (Module 111 英雄/精英副本)
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
								if !sweepHasStamina() || sweepSuccessRounds >= cfg.Sweep.MaxBatchTimes {
									break
								}
								for t := uint8(0); t < item.Times; t++ {
									if !sweepHasStamina() || sweepSuccessRounds >= cfg.Sweep.MaxBatchTimes {
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
									sweepSuccessRounds++
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
							slog.Info("DEBUG isExplicitPipeline check", "isExplicitPipeline", isExplicitPipeline, "cfgPipeline", cfg.Pipeline, "sweepPipeline", sweepPipeline)
							if !isExplicitPipeline {
								break sweepLoop
							}
						}

					case "sweep_highest_main":
						// 扫荡当前角色最高级主线副本 (消耗剩余体力冲级)
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

						for round := 1; round <= cfg.Sweep.MaxBatchTimes; round++ {
							if sweepSuccessRounds >= cfg.Sweep.MaxBatchTimes {
								break
							}
							if !sweepHasStamina() {
								break
							}

							var cost = powerPerSweep
							var exp = int64(2500)
							var coins = int64(12000)
							var sweepSucceeded = false
							var callErr error

							// 若启用 PreferPracticeQuickly，使用 Module 25 秒扫荡协议
							if cfg.Sweep.PreferPracticeQuickly {
								statusPkt, _ := protocol.BuildPracticeGetStatusPacket(int32(missionID), 0)
								statusCtx, statusCancel := context.WithTimeout(ctx, 3*time.Second)
								statusResp, statusErr := session.Call(statusCtx, statusPkt, protocol.ActionIDPracticeGetStatus)
								statusCancel()

								var currentMissionID = int32(missionID)
								if statusErr == nil && statusResp != nil {
									if status, pErr := protocol.ParsePracticeStatusResult(statusResp.Payload); pErr == nil && status != nil {
										if status.IsPracticing == 1 {
											quickPkt, _ := protocol.BuildQuicklyPacket(protocol.QuicklyRequest{Count: 0})
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
											quickPkt, _ := protocol.BuildQuicklyPacket(protocol.QuicklyRequest{Count: 0})
											_ = session.Send(quickPkt)
											sweepSucceeded = true
											missionID = int(currentMissionID)
										case protocol.PracticeResultNotEnoughPower:
											slog.Info("服务端返回体力不足，扫荡终止", "role_id", session.RoleID(), "round", round)
											session.SetStamina(0)
											session.SetExtraStamina(0)
											break sweepLoop
										case protocol.PracticeResultBagFull:
											slog.Warn("服务端返回背包已满，触发智能熔断", "role_id", session.RoleID(), "round", round)
											if sweepSuccessRounds == 0 && totalCostStamina == 0 {
												return scheduler.ErrBagFull
											}
											break sweepLoop
										default:
											slog.Warn("Module 25 秒扫荡受限", "result", m25Res.Result)
										}
									}
								} else {
									slog.Warn("Module 25 秒扫荡未收到服务端响应", "mission_id", currentMissionID, "err", callErr)
								}

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

							// 普通扫荡降级
							if !sweepSucceeded && (callErr == nil || !cfg.Sweep.PreferPracticeQuickly) {
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
									if sweepSuccessRounds > 0 {
										break
									}
									return fmt.Errorf("dungeon: 发送扫荡指令未收到服务端回包: %w", legacyErr)
								}
								if legacyResp == nil {
									if sweepSuccessRounds > 0 {
										break
									}
									return errors.New("dungeon: 服务端返回空扫荡响应")
								}

								legacyRes, pErr := protocol.ParseSweepResult(legacyResp.Payload)
								if pErr != nil {
									if sweepSuccessRounds > 0 {
										break
									}
									return fmt.Errorf("dungeon: 解析扫荡回包失败: %w", pErr)
								}
								if !legacyRes.Success {
									slog.Warn("服务端返回扫荡失败", "role_id", session.RoleID(), "round", round, "message", legacyRes.Message)
									if strings.Contains(legacyRes.Message, "体力不足") {
										session.SetStamina(0)
										session.SetExtraStamina(0)
										break sweepLoop
									}
									if strings.Contains(legacyRes.Message, "背包已满") {
										if sweepSuccessRounds == 0 && totalCostStamina == 0 {
											return scheduler.ErrBagFull
										}
										break sweepLoop
									}
									if sweepSuccessRounds == 0 && totalCostStamina == 0 {
										return fmt.Errorf("dungeon: sweep rejected by server: %s", legacyRes.Message)
									}
									break sweepLoop
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

							remStamina, err := session.ConsumeStamina(cost)
							if err != nil {
								return err
							}
							session.AddRewards(exp, coins)

							totalCostStamina += cost
							totalGainExp += exp
							totalGainCoins += coins
							sweepSuccessRounds++

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
			}

			// 8. 汇总本轮批次结算
			if sweepSuccessRounds > 0 || totalCostStamina > 0 {
				slog.Info("关卡推图与体力扫荡批次完成",
					"role_id", session.RoleID(),
					"sweep_times", sweepSuccessRounds,
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

// NewDemonTowerRoutine 构造锁妖塔与怪物试炼挑战任务 (Feature 9: 锁妖塔)。
func NewDemonTowerRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"demon_tower",
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
			slog.Info("正在执行副本任务: 锁妖塔挑战与试炼推图", "role_id", session.RoleID())

			// 1. 查询锁妖塔当前进度与层级 (Mod 701, Act 0)
			layer := int32(3)
			trialID := int32(31)
			prevAct := protocol.ActionIDTownEnter

			panelPkt := protocol.BuildHuanshouPanelPacket(prevAct)
			pCtx, pCancel := context.WithTimeout(ctx, 2*time.Second)
			panelResp, pErr := session.Call(pCtx, panelPkt, protocol.ActionIDHuanshouPanel)
			pCancel()
			if pErr == nil && panelResp != nil {
				if l, t, err := protocol.ParseDemonTowerPanelResult(panelResp.Payload); err == nil {
					layer = l
					trialID = t
				}
				prevAct = protocol.ActionIDHuanshouPanel
			}

			// 2. 发送进入锁妖塔当前层指令 (Mod 701, Act 1)
			enterPkt := protocol.BuildDemonTowerEnterPacket(layer, trialID, prevAct)
			callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			resp, err := session.Call(callCtx, enterPkt, protocol.ActionIDDemonTowerEnter)
			cancel()

			if err != nil || resp == nil {
				slog.Warn("进入锁妖塔超时或未开放", "err", err)
				return nil
			}

			// 3. 选择战斗 Buff (Mod 705, Act 6)
			buffPkt := protocol.BuildDemonTowerBuffPacket(1, protocol.ActionIDDemonTowerEnter)
			bCtx, bCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
			_, _ = session.Call(bCtx, buffPkt, protocol.ActionIDDemonTowerBuff)
			bCancel()

			// 4. 校验并同步阵型 (Mod 312, Act 38)
			deployPkt := protocol.NewPacket(protocol.ActionIDDemonTowerDeploy, protocol.NewWriter().WriteUint32(protocol.ActionIDDemonTowerBuff).Bytes())
			dCtx, dCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
			_, _ = session.Call(dCtx, deployPkt, protocol.ActionIDDemonTowerDeploy)
			dCancel()

			// 5. 触发实际战斗结算 (Mod 705, Act 1: 依据抓包对齐)
			battlePkt := protocol.BuildDemonTowerBattlePacket(protocol.ActionIDDemonTowerDeploy)
			batCtx, batCancel := context.WithTimeout(ctx, 2*time.Second)
			batResp, batErr := session.Call(batCtx, battlePkt, protocol.ActionIDDemonTowerBattle)
			batCancel()

			if batErr == nil && batResp != nil {
				slog.Info("锁妖塔战斗结算完成", "role_id", session.RoleID(), "layer", layer, "trial_id", trialID)
			} else {
				slog.Info("锁妖塔挑战指令已下发", "role_id", session.RoleID(), "layer", layer, "trial_id", trialID)
			}
			return nil
		},
	)
}

// NewQiMenRoutine 构造奇门遁甲阵法星图升级与布设任务 (Feature 4: 奇门遁甲)。
func NewQiMenRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"qimen_dunjia",
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
			slog.Info("正在执行玩法任务: 奇门遁甲法阵巡检与星图注灵", "role_id", session.RoleID())

			// 1. 查询法阵信息 (Mod 486, Act 7)
			arrayPkt := protocol.BuildQiMenArrayInfoPacket(protocol.ActionIDTownEnter)
			aCtx, aCancel := context.WithTimeout(ctx, 2*time.Second)
			_, _ = session.Call(aCtx, arrayPkt, protocol.ActionIDQiMenArrayInfo)
			aCancel()

			// 2. 查询阵眼傀儡 (Mod 485, Act 0)
			puppetPkt := protocol.BuildQiMenPuppetInfoPacket(protocol.ActionIDQiMenArrayInfo)
			pCtx, pCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
			_, _ = session.Call(pCtx, puppetPkt, protocol.ActionIDQiMenPuppetInfo)
			pCancel()

			// 3. 升级星图注灵 (Mod 486, Act 1)
			starPkt := protocol.BuildQiMenUpgradeStarPacket(protocol.ActionIDQiMenPuppetInfo)
			sCtx, sCancel := context.WithTimeout(ctx, 2*time.Second)
			_, _ = session.Call(sCtx, starPkt, protocol.ActionIDQiMenUpgradeStar)
			sCancel()

			// 4. 布阵生效与确认 (Mod 584, Act 1 & 3)
			depPkt := protocol.BuildQiMenFormationDeployPacket(protocol.ActionIDQiMenUpgradeStar)
			dCtx, dCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
			_, _ = session.Call(dCtx, depPkt, protocol.ActionIDQiMenFormationDeploy)
			dCancel()

			confPkt := protocol.BuildQiMenFormationConfirmPacket(protocol.ActionIDQiMenFormationDeploy)
			cCtx, cCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
			_, _ = session.Call(cCtx, confPkt, protocol.ActionIDQiMenFormationConfirm)
			cCancel()

			slog.Info("奇门遁甲阵法维护完成", "role_id", session.RoleID())
			return nil
		},
	)
}

// NewXunXianRoutine 构造寻仙之路自动推图与宝箱领用任务 (Feature 8: 寻仙之路)。
func NewXunXianRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"xunxian_road",
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
			slog.Info("正在执行玩法任务: 寻仙之路自动推图与节点扫荡", "role_id", session.RoleID())

			// 1. 查询当前寻仙关卡进度 (Mod 466, Act 1)
			progPkt := protocol.BuildXunXianGetProgressPacket(protocol.ActionIDTownEnter)
			pCtx, pCancel := context.WithTimeout(ctx, 2*time.Second)
			resp, err := session.Call(pCtx, progPkt, protocol.ActionIDXunXianGetProgress)
			pCancel()

			if err != nil || resp == nil {
				slog.Warn("查询寻仙之路进度超时", "err", err)
				return nil
			}

			// 2. 依次挑战节点 (依据抓包 0xBC, 0xBD, 0xBE) 并领取节点宝箱 (Act 13)
			nodes := []int32{0xBC, 0xBD, 0xBE}
			var clearedCount int

			for _, nID := range nodes {
				chalPkt := protocol.BuildXunXianChallengePacket(nID, protocol.ActionIDXunXianGetProgress)
				cCtx, cCancel := context.WithTimeout(ctx, 2*time.Second)
				cResp, cErr := session.Call(cCtx, chalPkt, protocol.ActionIDXunXianChallenge)
				cCancel()

				if cErr == nil && cResp != nil {
					// 领取节点宝箱 (Act 13)
					chestPkt := protocol.BuildXunXianClaimChestPacket(protocol.ActionIDXunXianChallenge)
					chCtx, chCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
					_, _ = session.Call(chCtx, chestPkt, protocol.ActionIDXunXianClaimChest)
					chCancel()
					clearedCount++
				}
			}

			slog.Info("寻仙之路节点挑战与宝箱领取完成", "role_id", session.RoleID(), "cleared_nodes", clearedCount)
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

// executeQuestCycle 任务闭环状态机:
// 循环接任务、完成任务，一直到没有任务可做了再 break，随后流转执行后续流水线 (精英扫荡与最高主线扫荡)。
func executeQuestCycle(
	ctx context.Context,
	session *client.RoleSession,
	dictRepo dictionary.Repository,
	cfg SweepConfig,
	ensureStamina func() bool,
	totalCostStamina *int,
	totalGainExp *int64,
	totalGainCoins *int64,
	clearedMissions map[int]bool,
) {
	if dictRepo == nil || session == nil {
		return
	}

	const powerPerSweep = 5
	const maxCycles = 20
	failedMissions := make(map[int]bool)

	slog.Info("启动主线/支线任务闭环自动化驱动", "role_id", session.RoleID(), "town_id", session.GetPlayerState().TownID)

	for cycle := 1; cycle <= maxCycles; cycle++ {
		if session.GetStamina() < powerPerSweep && !ensureStamina() {
			slog.Info("推图体力耗尽且无可用补充道具，退出任务循环", "stamina", session.GetStamina())
			break
		}

		progressMade := false

		// -------------------------------------------------------------
		// 1. 优先交付当前身上已达成的任务 (State == 1)
		// -------------------------------------------------------------
		currentQuests := session.GetQuests()
		for qid, state := range currentQuests {
			if state == 1 {
				q, _ := dictRepo.GetQuest(int(qid))
				title := fmt.Sprintf("Quest#%d", qid)
				if q != nil && q.Title != "" {
					title = q.Title
				}
				slog.Info("正在交付已达成任务", "role_id", session.RoleID(), "quest_id", qid, "title", title)

				compPkt, _ := protocol.BuildCompleteQuestPacket(protocol.CompleteQuestRequest{QuestID: int32(qid)})
				compCtx, compCancel := context.WithTimeout(ctx, 3*time.Second)
				_, _ = session.Call(compCtx, compPkt, protocol.ActionIDQuestComplete)
				compCancel()

				session.RemoveQuest(qid)
				if q != nil {
					session.AddRewards(q.AwardExp, q.AwardCoins)
					*totalGainExp += q.AwardExp
					*totalGainCoins += q.AwardCoins
				}
				progressMade = true
				time.Sleep(50 * time.Millisecond)
			}
		}

		// -------------------------------------------------------------
		// 2. 扫描城镇 NPC 列表并接取可接任务 (State == 1)
		// -------------------------------------------------------------
		curTown := session.GetPlayerState().TownID
		var towns []int
		if curTown > 0 {
			towns = append(towns, curTown)
		}
		for _, fallbackTown := range []int{22, 4, 1} {
			if fallbackTown != curTown {
				towns = append(towns, fallbackTown)
			}
		}

		validQuestNPCs, _ := dictRepo.GetQuestNPCs()
		var candidateNPCs []int
		seenNPC := make(map[int]bool)
		for _, tid := range towns {
			npcs, err := dictRepo.GetTownNPCs(tid)
			if err == nil {
				for _, nid := range npcs {
					if !seenNPC[nid] {
						seenNPC[nid] = true
						if validQuestNPCs == nil || validQuestNPCs[nid] {
							candidateNPCs = append(candidateNPCs, nid)
						}
					}
				}
			}
		}

		for _, npcID := range candidateNPCs {
			npcPkt, _ := protocol.BuildGetNPCQuestsPacket(protocol.GetNPCQuestsRequest{NPCID: int32(npcID)})
			npcCtx, npcCancel := context.WithTimeout(ctx, 300*time.Millisecond)
			npcResp, npcErr := session.Call(npcCtx, npcPkt, protocol.ActionIDQuestGetNPCQuests)
			npcCancel()

			if npcErr != nil || npcResp == nil {
				continue
			}

			items, pErr := protocol.ParseNPCQuestsResponse(npcResp.Payload)
			if pErr != nil || len(items) == 0 {
				continue
			}

			for _, it := range items {
				if it.State != 1 {
					continue // 仅接取状态为可接 (State == 1) 的任务
				}
				questID := uint32(it.QuestID)
				questsNow := session.GetQuests()
				if _, exists := questsNow[questID]; exists {
					continue
				}

				q, _ := dictRepo.GetQuest(int(it.QuestID))
				if q != nil && q.Level > 0 && q.Level > session.GetPlayerState().Level {
					continue // 角色等级未达到要求
				}

				title := fmt.Sprintf("Quest#%d", it.QuestID)
				if q != nil && q.Title != "" {
					title = q.Title
				}
				slog.Info("发现可接取任务，正在接受", "role_id", session.RoleID(), "npc_id", npcID, "quest_id", it.QuestID, "title", title)

				accPkt, _ := protocol.BuildAcceptQuestPacket(protocol.AcceptQuestRequest{QuestID: it.QuestID})
				accCtx, accCancel := context.WithTimeout(ctx, 3*time.Second)
				_, accErr := session.Call(accCtx, accPkt, protocol.ActionIDQuestAccept)
				accCancel()

				if accErr == nil {
					session.SetQuestState(questID, 0) // 加入进行中状态
					progressMade = true
					slog.Info("成功接受任务", "role_id", session.RoleID(), "quest_id", it.QuestID, "title", title)
					time.Sleep(50 * time.Millisecond)
				}
			}
		}

		// -------------------------------------------------------------
		// 3. 推进已接任务关卡挑战与对话剧情
		// -------------------------------------------------------------
		questsInProgress := session.GetQuests()
		for qid, qState := range questsInProgress {
			if qState != 0 {
				continue
			}
			q, err := dictRepo.GetQuest(int(qid))
			if err != nil || q == nil {
				continue
			}

			// 对话类任务 (MissionID == 0)
			if q.MissionID == 0 {
				slog.Info("正在交付对话剧情任务", "role_id", session.RoleID(), "quest_id", qid, "title", q.Title)
				compPkt, _ := protocol.BuildCompleteQuestPacket(protocol.CompleteQuestRequest{QuestID: int32(qid)})
				compCtx, compCancel := context.WithTimeout(ctx, 3*time.Second)
				_, _ = session.Call(compCtx, compPkt, protocol.ActionIDQuestComplete)
				compCancel()

				session.RemoveQuest(qid)
				session.AddRewards(q.AwardExp, q.AwardCoins)
				*totalGainExp += q.AwardExp
				*totalGainCoins += q.AwardCoins
				progressMade = true
				time.Sleep(50 * time.Millisecond)
				continue
			}

			// 关卡挑战类任务 (MissionID > 0)
			mid := q.MissionID
			if failedMissions[mid] {
				continue
			}
			if session.GetStamina() < powerPerSweep && !ensureStamina() {
				break
			}

			slog.Info("正在推进任务关卡挑战", "role_id", session.RoleID(), "quest_id", qid, "title", q.Title, "mission_id", mid)

			enterPkt := protocol.BuildEnterMissionPacket(uint32(mid))
			enterCtx, enterCancel := context.WithTimeout(ctx, 3*time.Second)
			enterResp, enterErr := session.Call(enterCtx, enterPkt, protocol.ActionMissionSweep)
			enterCancel()

			challengeSuccess := false
			if enterErr == nil && enterResp != nil {
				if res, pErr := protocol.ParseSweepResult(enterResp.Payload); pErr == nil && res != nil && res.Success {
					challengeSuccess = true
					_ = session.Send(protocol.BuildQuickFinishPacket())
				}
			}

			if challengeSuccess {
				cost := powerPerSweep
				remStamina, _ := session.ConsumeStamina(cost)
				session.AddRewards(2500, 12000)
				*totalCostStamina += cost
				*totalGainExp += 2500
				*totalGainCoins += 12000
				clearedMissions[mid] = true
				progressMade = true
				slog.Info("任务关卡推进挑战成功", "role_id", session.RoleID(), "mission_id", mid, "remaining_stamina", remStamina)

				// 任务达成，交付结算
				compPkt, _ := protocol.BuildCompleteQuestPacket(protocol.CompleteQuestRequest{QuestID: int32(qid)})
				compCtx, compCancel := context.WithTimeout(ctx, 3*time.Second)
				_, _ = session.Call(compCtx, compPkt, protocol.ActionIDQuestComplete)
				compCancel()

				session.RemoveQuest(qid)
				session.AddRewards(q.AwardExp, q.AwardCoins)
				*totalGainExp += q.AwardExp
				*totalGainCoins += q.AwardCoins
				slog.Info("任务达成并成功交付结算", "role_id", session.RoleID(), "quest_id", qid, "title", q.Title)
				time.Sleep(50 * time.Millisecond)
			} else {
				slog.Info("任务关卡暂未通关或卡战力/等级，记录跳过", "role_id", session.RoleID(), "mission_id", mid, "quest_id", qid)
				failedMissions[mid] = true
			}
		}

		// -------------------------------------------------------------
		// 4. 兜底检查: 若无活跃任务，通过 GetQuestMissions 探测是否有未通关主线/支线
		// -------------------------------------------------------------
		if !progressMade && len(session.GetQuests()) == 0 {
			playerLevel := session.GetPlayerState().Level
			for _, qType := range []int{1, 2} {
				qMissions, _ := dictRepo.GetQuestMissions(qType, playerLevel)
				for _, mid := range qMissions {
					if clearedMissions[mid] || failedMissions[mid] {
						continue
					}
					if session.GetStamina() < powerPerSweep && !ensureStamina() {
						break
					}

					slog.Info("探测并推进未通关剧情关卡", "role_id", session.RoleID(), "type", qType, "mission_id", mid)
					enterPkt := protocol.BuildEnterMissionPacket(uint32(mid))
					enterCtx, enterCancel := context.WithTimeout(ctx, 3*time.Second)
					enterResp, enterErr := session.Call(enterCtx, enterPkt, protocol.ActionMissionSweep)
					enterCancel()

					if enterErr == nil && enterResp != nil {
						if res, pErr := protocol.ParseSweepResult(enterResp.Payload); pErr == nil && res != nil && res.Success {
							_ = session.Send(protocol.BuildQuickFinishPacket())
							cost := powerPerSweep
							remStamina, _ := session.ConsumeStamina(cost)
							session.AddRewards(2500, 12000)
							*totalCostStamina += cost
							*totalGainExp += 2500
							*totalGainCoins += 12000
							clearedMissions[mid] = true
							progressMade = true
							slog.Info("剧情关卡推进挑战成功", "role_id", session.RoleID(), "mission_id", mid, "remaining_stamina", remStamina)
							time.Sleep(50 * time.Millisecond)
							break
						}
					}
					failedMissions[mid] = true
					break
				}
			}
		}

		// -------------------------------------------------------------
		// 5. 循环终止卫语句: 没有任务可做了，退出循环
		// -------------------------------------------------------------
		if !progressMade {
			slog.Info("当前已无可推进或可接取的任务，退出任务循环流转至下一阶段", "cycle", cycle)
			break
		}
	}
}


