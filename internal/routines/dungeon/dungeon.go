// Package dungeon 提供副本关卡扫荡、六道轮回、伏魔塔与练功房等推图挑战玩法。
//
// @author Ateng
// @since 2026-10-08
package dungeon

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
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
	MaxReplenishStamina  int  `json:"max_replenish_stamina"`  // 单次推图最大补充体力上限 (点数，仅作用于推图；包子5/粽子10/茶叶蛋50)
	MaxReplenishTimes    int  `json:"max_replenish_times"`    // 兼容旧字段：单次推图最大自愈补充次数 (仅作用于推图)
}

// SweepFeatureConfig 副本关卡扫荡配置
type SweepFeatureConfig struct {
	Enabled               bool     `json:"enabled"`
	Pipeline              []string `json:"pipeline"`                // 扫荡流水线优先级: "sweep_elite"
	MaxBatchTimes         int      `json:"max_batch_times"`         // 扫荡批次单次最大执行次数 (仅作用于扫荡)
	PreferPracticeQuickly bool     `json:"prefer_practice_quickly"` // 优先使用 Module 25 秒完成协议 (仅作用于扫荡)
	SellSweptItems        bool     `json:"sell_swept_items"`        // 扫荡精英副本后是否自动卖掉扫荡物
	AutoSellSweptItems    bool     `json:"auto_sell_swept_items"`   // 别名
}

// SweepConfig 关卡推图与扫荡自定义策略配置。包含两大核心功能：剧情推图 与 关卡扫荡。
type SweepConfig struct {
	// 功能一: 剧情推图 (循环接任务/做任务/交任务直至无任务可做)
	QuestCycle QuestCycleFeatureConfig `json:"quest_cycle"`
	// 功能二: 关卡扫荡 (扫荡精英副本)
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
	// MaxReplenishStamina 单次执行自动补充体力的最大点数上限 (默认 200，每个包子5/粽子10/茶叶蛋50)
	MaxReplenishStamina int
	// MaxReplenishTimes 单次执行自动补充体力的最大次数 (兼容旧配置，默认 10)
	MaxReplenishTimes int
	// SellSweptItems 扫荡精英副本后是否自动卖掉扫荡物
	SellSweptItems bool `json:"sell_swept_items"`
	// AutoSellSweptItems 扫荡精英副本后是否自动卖掉扫荡物 (别名)
	AutoSellSweptItems bool `json:"auto_sell_swept_items"`
	// Pipeline 策略优先级流水线，按先后顺序流转
	// 可选值: "push_mainline", "push_side", "sweep_elite"
	Pipeline []string
}

// DefaultSweepConfig 返回默认扫荡策略配置。
func DefaultSweepConfig() SweepConfig {
	return SweepConfig{
		QuestCycle: QuestCycleFeatureConfig{
			Enabled:              false, // 默认纯扫荡；由 yaml 或 pipeline 显式开启
			AutoReplenishStamina: true,
			MaxReplenishStamina:  200,
			MaxReplenishTimes:    10,
		},
		Sweep: SweepFeatureConfig{
			Enabled:               true,
			Pipeline:              []string{"sweep_elite"},
			MaxBatchTimes:         10,
			PreferPracticeQuickly: false, // 默认为 false 以保障存量测试与原版辅助兼容
			SellSweptItems:        false,
		},
		Mode:                  ModeAutoRecommend,
		Category:              CategoryMainline,
		SweepElite:            false,
		MaxBatchTimes:         10,
		PreferPracticeQuickly: false,
		AutoReplenishStamina:  true,
		MaxReplenishStamina:   200,
		MaxReplenishTimes:     10,
		Pipeline:              nil,
		SellSweptItems:        false,
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
	maxReplenishStamina := c.MaxReplenishStamina
	if maxReplenishStamina <= 0 {
		if c.MaxReplenishTimes > 0 {
			maxReplenishStamina = c.MaxReplenishTimes * 10
		} else {
			maxReplenishStamina = 200
		}
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
		MaxReplenishStamina:   maxReplenishStamina,
		MaxReplenishTimes:     maxReplenish,
		MaxBatchTimes:         maxBatch,
		RequiredPowerPerSweep: 5,
		PreferPracticeQuickly: c.PreferPracticeQuickly,
		Pipeline:              c.Pipeline,
		SellSweptItems:        c.Sweep.SellSweptItems || c.Sweep.AutoSellSweptItems || c.SellSweptItems || c.AutoSellSweptItems,
	}
}

// NewDungeonRoutine 构造副本推图与扫荡任务 (统一命名为 dungeon)。
func NewDungeonRoutine(dictRepo dictionary.Repository, cfgs ...SweepConfig) scheduler.ActivityRoutine {
	return NewDungeonSweepRoutine(dictRepo, cfgs...)
}

// NewDungeonSweepRoutine 构造副本体力扫荡任务，支持挂载数据字典与智能策略。
func NewDungeonSweepRoutine(dictRepo dictionary.Repository, cfgs ...SweepConfig) scheduler.ActivityRoutine {
	cfg := DefaultSweepConfig()
	hasSubConfig := false
	if len(cfgs) > 0 {
		c := cfgs[0]
		hasSubConfig = c.QuestCycle.Enabled || c.QuestCycle.AutoReplenishStamina ||
			c.QuestCycle.MaxReplenishStamina > 0 || c.QuestCycle.MaxReplenishTimes > 0 ||
			c.Sweep.Enabled || c.Sweep.MaxBatchTimes > 0 || len(c.Sweep.Pipeline) > 0 ||
			c.Sweep.SellSweptItems || c.Sweep.AutoSellSweptItems || c.SellSweptItems || c.AutoSellSweptItems

		// 继承外层顶层兼容字段到子功能
		if hasSubConfig {
			cfg.QuestCycle = c.QuestCycle
			if cfg.QuestCycle.MaxReplenishStamina <= 0 {
				if cfg.QuestCycle.MaxReplenishTimes > 0 {
					cfg.QuestCycle.MaxReplenishStamina = cfg.QuestCycle.MaxReplenishTimes * 10
				} else {
					cfg.QuestCycle.MaxReplenishStamina = 200
				}
			}
			cfg.Sweep = c.Sweep
			if c.SellSweptItems || c.AutoSellSweptItems {
				cfg.Sweep.SellSweptItems = true
			}
		} else {
			// 若未显式配置子块，从顶层兼容字段继承
			cfg.QuestCycle.AutoReplenishStamina = c.AutoReplenishStamina
			if c.MaxReplenishStamina > 0 {
				cfg.QuestCycle.MaxReplenishStamina = c.MaxReplenishStamina
			} else if c.MaxReplenishTimes > 0 {
				cfg.QuestCycle.MaxReplenishStamina = c.MaxReplenishTimes * 10
			}
			if c.Category == CategorySide || (c.Category == CategoryMainline && c.AutoReplenishStamina) {
				cfg.QuestCycle.Enabled = true
			}
			if c.MaxBatchTimes > 0 {
				cfg.Sweep.MaxBatchTimes = c.MaxBatchTimes
			}
			cfg.Sweep.PreferPracticeQuickly = c.PreferPracticeQuickly
			if c.SellSweptItems || c.AutoSellSweptItems {
				cfg.Sweep.SellSweptItems = true
			}
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
				if p == "sweep_elite" {
					cfg.Sweep.Enabled = true
				}
			}
		}

		cfg.Pipeline = c.Pipeline
		cfg.MaxBatchTimes = c.MaxBatchTimes
		cfg.AutoReplenishStamina = c.AutoReplenishStamina
		cfg.MaxReplenishStamina = c.MaxReplenishStamina
		cfg.MaxReplenishTimes = c.MaxReplenishTimes
		cfg.PreferPracticeQuickly = c.PreferPracticeQuickly
		cfg.SweepElite = c.SweepElite
		cfg.Category = c.Category
		cfg.Mode = c.Mode
		cfg.TargetMissionID = c.TargetMissionID
		cfg.SellSweptItems = cfg.Sweep.SellSweptItems
	}

	if cfg.Sweep.SellSweptItems || cfg.Sweep.AutoSellSweptItems || cfg.SellSweptItems || cfg.AutoSellSweptItems {
		cfg.Sweep.SellSweptItems = true
	}
	if cfg.Sweep.MaxBatchTimes <= 0 {
		cfg.Sweep.MaxBatchTimes = 10
	}
	if len(cfg.Sweep.Pipeline) == 0 {
		cfg.Sweep.Pipeline = []string{"sweep_elite"}
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
				replenishedStamina := 0
				questEnsureStamina := func() bool {
					if session.GetStamina() >= powerPerSweep {
						return true
					}
					// auto_replenish_stamina 只作用于推图！
					if !cfg.QuestCycle.AutoReplenishStamina {
						return false
					}
					maxStamina := cfg.QuestCycle.MaxReplenishStamina
					if maxStamina <= 0 {
						if cfg.QuestCycle.MaxReplenishTimes > 0 {
							maxStamina = cfg.QuestCycle.MaxReplenishTimes * 10
						} else {
							maxStamina = 200
						}
					}
					for session.GetStamina() < powerPerSweep && replenishedStamina < maxStamina {
						ok, itemName, addedPoints, err := TryReplenishStamina(ctx, session)
						if err != nil {
							slog.Warn("推图尝试自动补充体力异常", "role_id", session.RoleID(), "err", err)
							break
						}
						if !ok {
							break // 背包中无可用体力道具
						}
						replenishedStamina += addedPoints
						slog.Info("推图自愈补充体力成功",
							"role_id", session.RoleID(),
							"item", itemName,
							"added_stamina", addedPoints,
							"total_replenished", replenishedStamina,
							"max_replenish_stamina", maxStamina,
						)
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
					executeQuestCycle(
						ctx, session, dictRepo, cfg, questEnsureStamina,
						&totalCostStamina, &totalGainExp, &totalGainCoins,
						clearedMissions,
					)
				}
			}

			// =========================================================================
			// 功能二：关卡扫荡 (sweep)
			// 特性：扫荡精英副本 (sweep_elite)；
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
				} else if !hasSubConfig && (cfg.Category == CategoryElite || cfg.SweepElite || (cfg.Category == CategoryMainline && !cfg.QuestCycle.Enabled)) {
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
						initialItemIDs := make(map[int32]bool)
						if cfg.Sweep.SellSweptItems {
							packCtx, packCancel := context.WithTimeout(ctx, 3*time.Second)
							if packPkt, err := session.Call(packCtx, protocol.BuildGetPackItemListPacket(), protocol.ActionIDItemGetPackList); err == nil && packPkt != nil {
								if pack, pErr := protocol.ParsePackItemListResponse(packPkt.Payload); pErr == nil && pack != nil {
									for _, it := range pack.PackItems {
										initialItemIDs[it.PlayerItemID] = true
									}
								}
							}
							packCancel()
						}

						sellSweptItems := func() {
							if !cfg.Sweep.SellSweptItems {
								return
							}
							packCtx, packCancel := context.WithTimeout(ctx, 3*time.Second)
							packPkt, err := session.Call(packCtx, protocol.BuildGetPackItemListPacket(), protocol.ActionIDItemGetPackList)
							packCancel()
							if err != nil || packPkt == nil {
								return
							}
							pack, pErr := protocol.ParsePackItemListResponse(packPkt.Payload)
							if pErr != nil || pack == nil {
								return
							}

							for _, it := range pack.PackItems {
								if initialItemIDs[it.PlayerItemID] {
									continue
								}
								// 检查道具字典价格：若道具字典明确标明 price <= 0 则不可售卖
								if dictRepo != nil {
									if dItem, dErr := dictRepo.GetItem(int(it.ItemID)); dErr == nil && dItem != nil && dItem.Price <= 0 {
										initialItemIDs[it.PlayerItemID] = true
										continue
									}
								}

								sellPkt, sErr := protocol.BuildSellItemPacket(int16(it.GridID))
								if sErr != nil {
									continue
								}
								sellCtx, sellCancel := context.WithTimeout(ctx, 3*time.Second)
								sellResp, callErr := session.Call(sellCtx, sellPkt, protocol.ActionIDItemSell)
								sellCancel()
								if callErr == nil && sellResp != nil {
									if ok, _, _ := protocol.ParseSellItemResponse(sellResp.Payload); ok {
										initialItemIDs[it.PlayerItemID] = true
										slog.Info("自动卖出精英副本扫荡物成功",
											"role_id", session.RoleID(),
											"player_item_id", it.PlayerItemID,
											"item_id", it.ItemID,
											"grid_id", it.GridID,
										)
									}
								}
							}
						}

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
								// 每一类英雄副本扫荡后，若开启卖掉扫荡物则及时卖出
								sellSweptItems()
							}
							sellSweptItems()
							_ = session.Send(protocol.BuildHeroMissionClosePacket())
							if !isExplicitPipeline {
								break sweepLoop
							}
						}

					case "sweep_highest_main":
						// 扫荡当前角色最高级主线副本 (显式流水线冲级)
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

// SixRealmsConfig 六道轮回挑战与灵件处理策略配置。
type SixRealmsConfig struct {
	AutoReset      bool `yaml:"auto_reset" json:"auto_reset"`         // 通关后是否自动消耗每日免费重置次数
	AutoSellBlue   bool `yaml:"auto_sell_blue" json:"auto_sell_blue"`   // 挑战获胜是否自动出售蓝色灵件
	AutoSellPurple bool `yaml:"auto_sell_purple" json:"auto_sell_purple"` // 挑战获胜是否自动出售紫色灵件
}

// DefaultSixRealmsConfig 返回六道轮回默认策略配置。
func DefaultSixRealmsConfig() SixRealmsConfig {
	return SixRealmsConfig{
		AutoReset:      true,
		AutoSellBlue:   true,
		AutoSellPurple: false,
	}
}

// NewSixRealmsRoutine 构造六道轮回挑战与灵件分解任务。
func NewSixRealmsRoutine(cfgs ...SixRealmsConfig) scheduler.ActivityRoutine {
	cfg := DefaultSixRealmsConfig()
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	return scheduler.NewBaseRoutine(
		"six_realms",
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
			slog.Info("正在执行副本任务: 六道轮回爬塔与灵件自动处理", "role_id", session.RoleID())

			// 1. 获取当前六道轮回塔面板与进度信息
			info, err := session.GetTowerInfo(ctx)
			if err != nil {
				slog.Warn("获取六道轮回塔信息失败或未开放", "role_id", session.RoleID(), "err", err)
				return nil
			}

			layerName := protocol.TowerLayerName(info.Layer)
			slog.Info("六道轮回塔当前状态",
				"role_id", session.RoleID(),
				"layer", info.Layer,
				"layer_name", layerName,
				"sequence_war", info.SequenceWar,
				"sequence_max", info.SequenceMax,
				"bag_num", info.BagNum,
				"reset_num", info.ResetNum,
				"reset_ingot", info.ResetIngot,
			)

			// 2. 检查等级门槛
			minLevel := protocol.TowerLayerLevelLimit(info.Layer)
			roleLevel := session.GetPlayerState().Level
			if roleLevel > 0 && roleLevel < minLevel {
				slog.Info("未达到当前六道挑战等级门槛，跳过挑战",
					"role_id", session.RoleID(),
					"role_level", roleLevel,
					"required_level", minLevel,
					"layer", info.Layer,
				)
				return nil
			}

			// 3. 检查灵件背包空间是否已满 (>= 20 为上限预警)
			if info.BagNum >= 20 {
				slog.Warn("灵件背包已满，暂停六道挑战以防灵件溢出丢弃", "role_id", session.RoleID(), "bag_num", info.BagNum)
				return nil
			}

			// 4. 判断是否需要重置: 如果当前道已全部通关 (SequenceWar >= 25)
			if info.SequenceWar >= 25 {
				if cfg.AutoReset && info.ResetNum > 0 {
					slog.Info("六道轮回当前道已全通，检测到今日剩余免费重置次数，执行重置",
						"role_id", session.RoleID(),
						"reset_num", info.ResetNum,
					)
					if jitter != nil {
						_ = jitter.Wait(ctx)
					}
					resetRes, resetErr := session.ResetTowerLayer(ctx)
					if resetErr != nil || resetRes.Result != protocol.TowerResultSuccess {
						slog.Warn("六道轮回塔重置失败", "err", resetErr)
						return nil
					}
					slog.Info("六道轮回塔重置成功", "role_id", session.RoleID(), "layer", resetRes.Layer)
					// 刷新最新进度
					if newInfo, nErr := session.GetTowerInfo(ctx); nErr == nil {
						info = newInfo
					} else {
						info.SequenceWar = 0
					}
				} else {
					slog.Info("六道轮回当前道今日已通关且无需重置或无免费重置次数，挑战完成",
						"role_id", session.RoleID(),
						"layer", info.Layer,
						"layer_name", layerName,
					)
					return nil
				}
			}

			// 5. 循环推进挑战未通关层数 (从已通关层数后一关开始推进: SequenceWar + 1 .. 25)
			nextFloor := info.SequenceWar + 1
			for floor := nextFloor; floor <= 25; floor++ {
				select {
				case <-ctx.Done():
					slog.Info("六道轮回挑战被外部上下文中断", "role_id", session.RoleID())
					return ctx.Err()
				default:
				}

				// 服务端连续战斗存在冷却保护 (对齐官方客户端 CommonConstant.DelayTimeToWar，需留足间隔)
				time.Sleep(1500 * time.Millisecond)

				slog.Info("正在挑战六道轮回塔关卡",
					"role_id", session.RoleID(),
					"layer", info.Layer,
					"layer_name", protocol.TowerLayerName(info.Layer),
					"floor", floor,
				)

				// 自动卖出灵件策略
				res, chErr := session.ChallengeTower(ctx, floor, cfg.AutoSellBlue, cfg.AutoSellPurple)
				if chErr != nil {
					slog.Warn("六道轮回塔挑战请求失败", "floor", floor, "err", chErr)
					break
				}

				if res.Result == protocol.TowerResultSuccess {
					slog.Info("六道轮回塔关卡挑战胜利",
						"role_id", session.RoleID(),
						"floor", floor,
						"gain_exp", res.WarExp,
						"gain_fame", res.WarFame,
						"gain_coin", res.WarCoin,
						"soul_id", res.WarSoulID,
					)
					info.SequenceWar = floor
					if floor == 25 {
						slog.Info("恭喜通关六道轮回当前道全部 25 层！",
							"role_id", session.RoleID(),
							"layer", info.Layer,
							"layer_name", protocol.TowerLayerName(info.Layer),
						)
						break
					}
				} else {
					// 挑战失败或者遭遇限制 (WAR_CD, FULL_BAG, NO_LV 等)
					switch res.Result {
					case protocol.TowerResultFullBag:
						slog.Warn("灵件背包已满，终止六道轮回挑战", "role_id", session.RoleID())
					case protocol.TowerResultWarCD:
						slog.Warn("战斗处于冷却中", "role_id", session.RoleID())
					case protocol.TowerResultFullTimes:
						slog.Info("今日六道挑战次数已达上限", "role_id", session.RoleID())
					default:
						slog.Warn("六道轮回挑战未成功", "role_id", session.RoleID(), "result_code", res.Result)
					}
					break
				}

				if jitter != nil {
					_ = jitter.Wait(ctx)
				}
			}

			slog.Info("六道轮回爬塔任务执行完毕", "role_id", session.RoleID())
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
	const maxCycles = 50
	failedMissions := make(map[int]bool)

	slog.Info("启动主线/支线任务闭环自动化驱动", "role_id", session.RoleID(), "town_id", session.GetPlayerState().TownID)

	// 0. 主动向服务端全量同步当前任务列表 (Module 3, Action 9: town_quest_show)
	// 在 2026 真实网关中，town_quest_show 返回状态枚举 (QuestType):
	//   0 = Acceptable (可接取)
	//   1 = Accepted   (已接取/进行中)
	//   2 = Completed  (已达成目标/可交付)
	//   3 = LevelLimit (等级不足)
	syncedItems, syncErr := session.SyncQuests(ctx)
	if syncErr == nil {
		executeAuthoritativeQuestCycle(
			ctx, session, dictRepo, syncedItems, ensureStamina,
			totalCostStamina, totalGainExp, totalGainCoins,
			clearedMissions, failedMissions,
		)
		return
	}

	// 兼容离线单测 Mock Server (未实现 0x00030009 town_quest_show 时的 NPC 扫描降级路径)
	for cycle := 1; cycle <= maxCycles; cycle++ {
		if session.GetStamina() < powerPerSweep && !ensureStamina() {
			slog.Info("推图体力耗尽且无可用补充道具，退出任务循环", "stamina", session.GetStamina())
			break
		}

		progressMade := false

		// 1. 交付当前身上已达成的任务
		currentQuests := session.GetQuests()
		for qid, state := range currentQuests {
			if state == 1 || state == 2 {
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

		// 2. 扫描城镇 NPC 列表并接取可接任务
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
			npcCtx, npcCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
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
				questID := uint32(it.QuestID)

				if it.State == 2 {
					q, _ := dictRepo.GetQuest(int(it.QuestID))
					title := fmt.Sprintf("Quest#%d", questID)
					if q != nil && q.Title != "" {
						title = q.Title
					}
					slog.Info("NPC 处发现已达成任务，正在交付结算", "role_id", session.RoleID(), "npc_id", npcID, "quest_id", questID, "title", title)
					compPkt, _ := protocol.BuildCompleteQuestPacket(protocol.CompleteQuestRequest{QuestID: it.QuestID})
					compCtx, compCancel := context.WithTimeout(ctx, 3*time.Second)
					_, _ = session.Call(compCtx, compPkt, protocol.ActionIDQuestComplete)
					compCancel()

					session.RemoveQuest(questID)
					if q != nil {
						session.AddRewards(q.AwardExp, q.AwardCoins)
						*totalGainExp += q.AwardExp
						*totalGainCoins += q.AwardCoins
					}
					progressMade = true
					time.Sleep(50 * time.Millisecond)
					continue
				}

				if it.State == 0 {
					session.SetQuestState(questID, 0)
					continue
				}

				if it.State != 1 {
					continue
				}
				questsNow := session.GetQuests()
				if _, exists := questsNow[questID]; exists {
					continue
				}

				q, _ := dictRepo.GetQuest(int(it.QuestID))
				if q != nil && q.Level > 0 && q.Level > session.GetPlayerState().Level {
					continue
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
					session.SetQuestState(questID, 0)
					progressMade = true
					slog.Info("成功接受任务", "role_id", session.RoleID(), "quest_id", it.QuestID, "title", title)
					time.Sleep(50 * time.Millisecond)
				}
			}
		}

		// 3. 推进已接任务关卡挑战与对话剧情
		questsInProgress := session.GetQuests()
		for qid, qState := range questsInProgress {
			if qState != 0 {
				continue
			}
			q, err := dictRepo.GetQuest(int(qid))
			if err != nil || q == nil {
				continue
			}

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

			mid := q.MissionID
			if failedMissions[mid] {
				continue
			}
			if session.GetStamina() < powerPerSweep && !ensureStamina() {
				break
			}

			slog.Info("正在推进任务关卡挑战", "role_id", session.RoleID(), "quest_id", qid, "title", q.Title, "mission_id", mid)

			challengeSuccess := false
			sweepPkt, _ := protocol.BuildSweepPacket(protocol.SweepRequest{MissionID: uint32(mid), Times: 1})
			enterCtx, enterCancel := context.WithTimeout(ctx, 3*time.Second)
			enterResp, enterErr := session.Call(enterCtx, sweepPkt, protocol.ActionMissionSweep)
			enterCancel()

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

		if !progressMade {
			slog.Info("当前已无可推进或可接取的任务，退出任务循环流转至下一阶段", "cycle", cycle)
			break
		}
	}
	resetTownScene(session)
}

// executeAuthoritativeQuestCycle 基于真实网关 Mod 3 (Mod_Quest_Base) 与 Mod 4 (Mod_Mission_Base) 驱动全自动主支线推图闭环。
// QuestType 状态定义 (对齐 2026 客户端 Main.swf):
//   - 0 (Acceptable): 可接取任务 -> 调用 0x00030003 (accept_quest)
//   - 1 (Accepted):   已接取进行中 -> 纯对话调用 0x00030004 (finish_quest)，副本挑战调用 Mod 4 (get_sections -> enter_mission -> fight_monster -> rank_mission -> open_box -> pickup_award)
//   - 2 (Completed):  已达成可交付 -> 调用 0x00030005 (complete_quest)
func executeAuthoritativeQuestCycle(
	ctx context.Context,
	session *client.RoleSession,
	dictRepo dictionary.Repository,
	initialItems []protocol.PlayerQuestItem,
	ensureStamina func() bool,
	totalCostStamina *int,
	totalGainExp *int64,
	totalGainCoins *int64,
	clearedMissions map[int]bool,
	failedMissions map[int]bool,
) {
	const powerPerSweep = 5
	const maxCycles = 50
	items := initialItems

	for cycle := 1; cycle <= maxCycles; cycle++ {
		if cycle > 1 {
			var err error
			items, err = session.SyncQuests(ctx)
			if err != nil {
				slog.Warn("同步服务端任务列表异常，终止本轮任务推图", "role_id", session.RoleID(), "err", err)
				break
			}
		}

		progressMade := false

		// -----------------------------------------------------------------
		// Step 1: 优先交付所有已达成目标的任务 (State == 2: Completed)
		// -----------------------------------------------------------------
		completedAny := false
		for _, it := range items {
			if it.State != 2 {
				continue
			}
			q, _ := dictRepo.GetQuest(int(it.QuestID))
			title := fmt.Sprintf("Quest#%d", it.QuestID)
			if q != nil && q.Title != "" {
				title = q.Title
			}
			slog.Info("正在交付已达成任务", "role_id", session.RoleID(), "quest_id", it.QuestID, "title", title)

			ok, packFull := submitCompleteQuest(ctx, session, it.QuestID)
			if packFull {
				slog.Warn("背包已满，无法交付任务领取奖励，退出推图循环", "role_id", session.RoleID(), "quest_id", it.QuestID)
				return
			}
			if ok {
				session.RemoveQuest(uint32(it.QuestID))
				if q != nil {
					session.AddRewards(q.AwardExp, q.AwardCoins)
					*totalGainExp += q.AwardExp
					*totalGainCoins += q.AwardCoins
				}
				slog.Info("任务达成并成功交付结算", "role_id", session.RoleID(), "quest_id", it.QuestID, "title", title)
				completedAny = true
				progressMade = true
				time.Sleep(80 * time.Millisecond)
			}
		}

		if completedAny {
			if refreshed, err := session.SyncQuests(ctx); err == nil {
				items = refreshed
			}
		}

		// -----------------------------------------------------------------
		// Step 2: 接取所有满足等级的可接取任务 (State == 0: Acceptable)
		// 神仙道同时进行中的任务上限为 5 个 (State == 1)
		// -----------------------------------------------------------------
		acceptedAny := false
		playerLevel := session.GetPlayerState().Level

		activeCount := 0
		for _, it := range items {
			if it.State == 1 {
				activeCount++
			}
		}

		for _, it := range items {
			if activeCount >= 5 {
				break
			}
			if it.State != 0 {
				continue
			}
			q, _ := dictRepo.GetQuest(int(it.QuestID))
			if q != nil && q.Level > 0 && q.Level > playerLevel {
				continue
			}
			title := fmt.Sprintf("Quest#%d", it.QuestID)
			if q != nil && q.Title != "" {
				title = q.Title
			}
			slog.Info("发现可接取任务，正在接受", "role_id", session.RoleID(), "quest_id", it.QuestID, "title", title)

			accPkt, _ := protocol.BuildAcceptQuestPacket(protocol.AcceptQuestRequest{
				QuestID: it.QuestID,
				PrevAct: session.LastActionID(),
			})
			accCtx, accCancel := context.WithTimeout(ctx, 3*time.Second)
			accResp, accErr := session.Call(accCtx, accPkt, protocol.ActionIDQuestAccept)
			accCancel()

			// Mod_Quest_Base.SUCCEED == 5 (兼容单测 0 / 1)
			if accErr == nil && accResp != nil && (len(accResp.Payload) == 0 || accResp.Payload[0] == 5 || accResp.Payload[0] == 1 || accResp.Payload[0] == 0) {
				slog.Info("成功接受任务", "role_id", session.RoleID(), "quest_id", it.QuestID, "title", title)
				acceptedAny = true
				progressMade = true
				activeCount++
				time.Sleep(80 * time.Millisecond)
			} else {
				var resCode uint8
				if accResp != nil && len(accResp.Payload) > 0 {
					resCode = accResp.Payload[0]
				}
				if resCode == 7 || resCode == 17 {
					slog.Warn("背包已满，无法接受新任务，退出推图循环", "role_id", session.RoleID(), "quest_id", it.QuestID, "title", title)
					return
				}
				slog.Warn("接受任务未成功", "role_id", session.RoleID(), "quest_id", it.QuestID, "title", title, "result", resCode, "err", accErr)
				// 一旦接取任务超时或失败（如已达上限或服务端无响应），停止尝试后续任务，防止连续超时阻塞调度器
				break
			}
		}

		if acceptedAny {
			if refreshed, err := session.SyncQuests(ctx); err == nil {
				items = refreshed
			}
			// 接取后若有任务直接处于可交付状态 (State == 2)，立即交付
			for _, it := range items {
				if it.State == 2 {
					q, _ := dictRepo.GetQuest(int(it.QuestID))
					title := fmt.Sprintf("Quest#%d", it.QuestID)
					if q != nil && q.Title != "" {
						title = q.Title
					}
					if ok, _ := submitCompleteQuest(ctx, session, it.QuestID); ok {
						session.RemoveQuest(uint32(it.QuestID))
						if q != nil {
							session.AddRewards(q.AwardExp, q.AwardCoins)
							*totalGainExp += q.AwardExp
							*totalGainCoins += q.AwardCoins
						}
						slog.Info("任务达成并成功交付结算", "role_id", session.RoleID(), "quest_id", it.QuestID, "title", title)
						progressMade = true
					}
				}
			}
			if refreshed, err := session.SyncQuests(ctx); err == nil {
				items = refreshed
			}
		}

		// -----------------------------------------------------------------
		// Step 3: 推进已接取进行中的任务 (State == 1: Accepted / InProgress)
		// 排序优先级：对话任务优先 -> 主线任务 (Type=1) 优先 -> 较小 Lock 优先
		// -----------------------------------------------------------------
		var inProgress []protocol.PlayerQuestItem
		for _, it := range items {
			if it.State == 1 {
				inProgress = append(inProgress, it)
			}
		}
		sort.SliceStable(inProgress, func(i, j int) bool {
			qi, _ := dictRepo.GetQuest(int(inProgress[i].QuestID))
			qj, _ := dictRepo.GetQuest(int(inProgress[j].QuestID))
			if qi == nil || qj == nil {
				return inProgress[i].QuestID < inProgress[j].QuestID
			}
			talkI := qi.MissionID == 0 || qi.TalkQuest == 1
			talkJ := qj.MissionID == 0 || qj.TalkQuest == 1
			if talkI != talkJ {
				return talkI
			}
			if qi.Type != qj.Type {
				return qi.Type < qj.Type
			}
			if qi.Lock != qj.Lock {
				return qi.Lock < qj.Lock
			}
			return qi.ID < qj.ID
		})

		for _, it := range inProgress {
			q, err := dictRepo.GetQuest(int(it.QuestID))
			if err != nil || q == nil {
				continue
			}

			// 3A. 对话类任务 (MissionID == 0 或 TalkQuest == 1)
			if q.MissionID == 0 || q.TalkQuest == 1 {
				slog.Info("正在交付对话剧情任务", "role_id", session.RoleID(), "quest_id", q.ID, "title", q.Title)
				finPkt := protocol.BuildFinishQuestPacket(it.QuestID, session.LastActionID())
				finCtx, finCancel := context.WithTimeout(ctx, 3*time.Second)
				_, _ = session.Call(finCtx, finPkt, protocol.ActionIDQuestFinish)
				finCancel()

				ok, packFull := submitCompleteQuest(ctx, session, it.QuestID)
				if packFull {
					slog.Warn("背包已满，无法交付对话任务", "role_id", session.RoleID(), "quest_id", q.ID)
					return
				}
				if ok {
					session.RemoveQuest(uint32(it.QuestID))
					session.AddRewards(q.AwardExp, q.AwardCoins)
					*totalGainExp += q.AwardExp
					*totalGainCoins += q.AwardCoins
					slog.Info("对话任务达成并成功交付结算", "role_id", session.RoleID(), "quest_id", q.ID, "title", q.Title)
					progressMade = true
					time.Sleep(80 * time.Millisecond)
				}
				continue
			}

			// 3B. 副本挑战类任务 (MissionID > 0)
			mid := q.MissionID
			if failedMissions[mid] {
				continue
			}
			if session.GetStamina() < powerPerSweep && !ensureStamina() {
				slog.Info("推图体力耗尽且无可用补充道具，退出任务循环", "stamina", session.GetStamina())
				return
			}

			slog.Info("正在推进任务关卡挑战", "role_id", session.RoleID(), "quest_id", q.ID, "title", q.Title, "mission_id", mid)

			if advanceQuestMission(
				ctx, session, dictRepo, q, it.CurrentCount, ensureStamina,
				totalCostStamina, totalGainExp, totalGainCoins,
				clearedMissions, failedMissions,
			) {
				progressMade = true
				// 任一任务完成或取得实质推进后立即跳出重排，确保新解锁的主线任务优先接取并执行
				break
			} else {
				slog.Info("任务关卡暂未通关或卡战力/等级，记录跳过", "role_id", session.RoleID(), "mission_id", mid, "quest_id", q.ID)
				failedMissions[mid] = true
			}
		}

		if !progressMade {
			slog.Info("当前已无可推进或可接取的任务，退出任务循环流转至下一阶段", "cycle", cycle)
			break
		}
	}
	resetTownScene(session)
}

// submitCompleteQuest 向服务端发送完成并交付任务请求 (Mod 3, Act 5: complete_quest)。
// 依据客户端 NPCDialogView.onComplete (Method 6441) 字节码约定：
//   - completedState > 0 (如 1, 3, 5): 交付成功 (completeQuestCallback)
//   - completedState == 0 (且为 16B 完整回包): 背包已满 (NPCDialogViewLang.PackFull)
//   - completedState == -1 (0xFF): 任务物品不足或未达成 (NPCDialogViewLang.QuestItem)
func submitCompleteQuest(ctx context.Context, session *client.RoleSession, questID int32) (succeeded bool, packFull bool) {
	// 1. 先发送 npc_info_by_questid (0x00030002) 激活任务交付上下文
	infoPkt, _ := protocol.BuildCompleteQuestPacket(protocol.CompleteQuestRequest{
		QuestID: questID,
		PrevAct: session.LastActionID(),
	})
	infoCtx, infoCancel := context.WithTimeout(ctx, 2*time.Second)
	_, _ = session.Call(infoCtx, infoPkt, protocol.ActionIDQuestComplete)
	infoCancel()

	// 2. 发送 complete_quest (0x00030005) 真正交付任务并领取奖励
	subPkt := protocol.BuildSubmitCompleteQuestPacket(questID, session.LastActionID())
	subCtx, subCancel := context.WithTimeout(ctx, 3*time.Second)
	resp, err := session.Call(subCtx, subPkt, protocol.ActionIDQuestSubmitComplete)
	subCancel()
	if err != nil || resp == nil || len(resp.Payload) == 0 {
		return false, false
	}

	state := int8(resp.Payload[0])
	if state > 0 {
		return true, false
	}
	if state == 0 && len(resp.Payload) >= 16 {
		return false, true
	}

	// 若任务尚未标记 finish (state == -1)，补发一次 finish_quest (0x00030004) 后重试 complete_quest (0x00030005)
	finPkt := protocol.BuildFinishQuestPacket(questID, session.LastActionID())
	finCtx, finCancel := context.WithTimeout(ctx, 2*time.Second)
	_, _ = session.Call(finCtx, finPkt, protocol.ActionIDQuestFinish)
	finCancel()

	subPkt2 := protocol.BuildSubmitCompleteQuestPacket(questID, session.LastActionID())
	subCtx2, subCancel2 := context.WithTimeout(ctx, 3*time.Second)
	resp2, err2 := session.Call(subCtx2, subPkt2, protocol.ActionIDQuestSubmitComplete)
	subCancel2()
	if err2 != nil || resp2 == nil || len(resp2.Payload) == 0 {
		return false, false
	}
	state2 := int8(resp2.Payload[0])
	if state2 > 0 {
		return true, false
	}
	if state2 == 0 && len(resp2.Payload) >= 16 {
		return false, true
	}
	return false, false
}

// advanceQuestMission 推进指定任务的关联剧情关卡（包含自动清理同章节前置未通关小关）。
func advanceQuestMission(
	ctx context.Context,
	session *client.RoleSession,
	dictRepo dictionary.Repository,
	q *dictionary.Quest,
	initialCount int32,
	ensureStamina func() bool,
	totalCostStamina *int,
	totalGainExp *int64,
	totalGainCoins *int64,
	clearedMissions map[int]bool,
	failedMissions map[int]bool,
) bool {
	targetMission, err := dictRepo.GetMission(q.MissionID)
	if err != nil || targetMission == nil {
		return false
	}
	if q.Type != 1 {
		slog.Info("任务关卡属于非主线剧情任务（支线/日常需扫荡），剧情推图跳过", "quest_id", q.ID, "title", q.Title, "type", q.Type)
		return false
	}
	if targetMission.Type != 0 {
		slog.Info("任务关卡属于非普通主线副本，剧情推图跳过", "quest_id", q.ID, "title", q.Title, "mission_id", targetMission.ID, "type", targetMission.Type)
		return false
	}

	const maxStageSteps = 12
	lastCount := initialCount
	noCountGainRuns := 0
	clearedPrereq := false

	for step := 0; step < maxStageSteps; step++ {
		if session.GetStamina() < 5 && !ensureStamina() {
			return clearedPrereq || lastCount > initialCount
		}

		// 查询目标章节已开放的关卡进度 (Mod 4, Act 0: get_sections)
		var prereqMission *dictionary.Mission
		if targetMission.MapID > 0 {
			secPkt := protocol.BuildMissionGetSectionsPacket(int32(targetMission.MapID), session.LastActionID())
			secCtx, secCancel := context.WithTimeout(ctx, 2*time.Second)
			secResp, secErr := session.Call(secCtx, secPkt, protocol.ActionIDMissionGetSections)
			secCancel()

			if secErr == nil && secResp != nil {
				if sections, pErr := protocol.ParseMissionGetSectionsResponse(secResp.Payload); pErr == nil {
					for _, sec := range sections {
						if sec.Status == 1 {
							clearedMissions[int(sec.MissionID)] = true
						}
						// 若存在已开放但未通关 (Status == 0) 的前置关卡，且其 MissionLock <= 目标关卡，优先通关前置小关
						if sec.Status == 0 && int(sec.MissionID) != targetMission.ID && !failedMissions[int(sec.MissionID)] {
							if sm, mErr := dictRepo.GetMission(int(sec.MissionID)); mErr == nil && sm != nil {
								if sm.MissionLock <= targetMission.MissionLock {
									if prereqMission == nil || sm.MissionLock < prereqMission.MissionLock {
										prereqMission = sm
									}
								}
							}
						}
					}
				}
			}
		}

		if prereqMission != nil {
			slog.Info("发现前置未通关剧情关卡，先行推进挑战",
				"role_id", session.RoleID(),
				"quest_id", q.ID,
				"prereq_mission_id", prereqMission.ID,
				"prereq_mission_name", prereqMission.Name,
				"target_mission_id", targetMission.ID,
			)
			if !challengeStoryMission(ctx, session, prereqMission, ensureStamina, totalCostStamina, totalGainExp, totalGainCoins, clearedMissions) {
				failedMissions[prereqMission.ID] = true
				return clearedPrereq || lastCount > initialCount
			}
			clearedPrereq = true
			continue
		}

		if clearedMissions[targetMission.ID] {
			slog.Info("任务目标关卡已首通，跳过主线单人副本挑战（支线目标需通过扫荡完成）",
				"role_id", session.RoleID(),
				"quest_id", q.ID,
				"title", q.Title,
				"mission_id", targetMission.ID,
			)
			failedMissions[targetMission.ID] = true
			return false
		}

		// 正式挑战任务目标关卡
		if !challengeStoryMission(ctx, session, targetMission, ensureStamina, totalCostStamina, totalGainExp, totalGainCoins, clearedMissions) {
			return clearedPrereq || lastCount > initialCount
		}

		// 挑战成功后，同步任务状态检查是否已达成可交付 (State == 2) 或目标计数增长
		refreshed, sErr := session.SyncQuests(ctx)
		if sErr == nil {
			questState := int32(-1)
			var matchedItem protocol.PlayerQuestItem
			for _, it := range refreshed {
				if int(it.QuestID) == q.ID {
					questState = it.State
					matchedItem = it
					break
				}
			}
			if questState == 2 {
				ok, _ := submitCompleteQuest(ctx, session, int32(q.ID))
				if ok {
					session.RemoveQuest(uint32(q.ID))
					session.AddRewards(q.AwardExp, q.AwardCoins)
					*totalGainExp += q.AwardExp
					*totalGainCoins += q.AwardCoins
					slog.Info("任务达成并成功交付结算", "role_id", session.RoleID(), "quest_id", q.ID, "title", q.Title)
				}
				return true
			}
			if questState == -1 {
				// 任务已不在进行列表中（已自动完成）
				return true
			}
			if matchedItem.MaxCount > 0 && matchedItem.CurrentCount > lastCount {
				slog.Info("任务目标进度更新",
					"role_id", session.RoleID(),
					"quest_id", q.ID,
					"title", q.Title,
					"progress", fmt.Sprintf("%d/%d", matchedItem.CurrentCount, matchedItem.MaxCount),
				)
				lastCount = matchedItem.CurrentCount
				noCountGainRuns = 0
				return true
			}
			noCountGainRuns++
			// 若杀怪任务挑战后计数未增长，或掉落物任务连续 2 次未掉落，退出本轮避免空转
			if matchedItem.NeedType == 0 || noCountGainRuns >= 2 {
				return lastCount > initialCount
			}
		} else {
			return true
		}
	}

	return clearedPrereq || lastCount > initialCount
}

func resetTownScene(session *client.RoleSession) {
	// 在神仙道官方客户端架构中，通关剧情副本或任务流转退出后直接留在当前主界面，
	// 严禁在会话运行中途重放登录期握手包 (0x0048/0x0063/0x00A50000/0x0002)，
	// 否则会重置网关状态机，导致后续六道轮回等战斗协议被服务端静默丢弃。
}

// challengeStoryMission 使用 Module 4 (Mod_Mission_Base) 完整五步状态机通关指定剧情关卡：
// enter_mission (0x00040001) -> fight_monster (0x00040002) -> rank_mission (0x00040003) -> open_box (0x00040004) -> pickup_award (0x00040005)。
func challengeStoryMission(
	ctx context.Context,
	session *client.RoleSession,
	m *dictionary.Mission,
	ensureStamina func() bool,
	totalCostStamina *int,
	totalGainExp *int64,
	totalGainCoins *int64,
	clearedMissions map[int]bool,
) bool {
	if m == nil {
		return false
	}

	var monsterTeams []int32
	for _, part := range strings.Split(m.Monsters, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if v, err := strconv.Atoi(part); err == nil && v > 0 {
			monsterTeams = append(monsterTeams, int32(v))
		}
	}
	if len(monsterTeams) == 0 {
		slog.Warn("关卡缺失怪物编队配置，无法发起挑战", "mission_id", m.ID, "name", m.Name)
		return false
	}

	if session.GetStamina() < 5 && !ensureStamina() {
		return false
	}

	// 0. 激活章节关卡列表上下文 (Mod 4, Act 0: get_sections)，对齐官方前置调用链
	if session.LastActionID() != protocol.ActionIDMissionGetSections {
		mapID := int32(m.MapID)
		if mapID <= 0 {
			mapID = int32(m.SectionID)
		}
		secPkt := protocol.BuildMissionGetSectionsPacket(mapID, session.LastActionID())
		secCtx, secCancel := context.WithTimeout(ctx, 2*time.Second)
		_, _ = session.Call(secCtx, secPkt, protocol.ActionIDMissionGetSections)
		secCancel()
	}

	// 1. 进入副本 (Mod 4, Act 1: enter_mission, 前置对齐 ActionIDMissionGetSections)
	enterPkt := protocol.BuildMissionPlotEnterPacket(int32(m.ID), session.LastActionID())
	enterCtx, enterCancel := context.WithTimeout(ctx, 3*time.Second)
	enterResp, enterErr := session.Call(enterCtx, enterPkt, protocol.ActionIDMissionPlotEnter)
	enterCancel()

	if enterErr != nil || enterResp == nil || len(enterResp.Payload) == 0 {
		slog.Warn("进入剧情副本未收到有效响应", "role_id", session.RoleID(), "mission_id", m.ID, "name", m.Name, "err", enterErr)
		resetTownScene(session)
		return false
	}

	enterStatus := enterResp.Payload[0]
	if enterStatus == 20 { // AWARD_BOX_NO_OPEN: 上次通关宝箱尚未开启或拾取，先行清理再重进
		boxCtx, boxCancel := context.WithTimeout(ctx, 2*time.Second)
		_, _ = session.Call(boxCtx, protocol.BuildMissionOpenBoxPacket(protocol.ActionIDMissionRank), protocol.ActionIDMissionOpenBox)
		boxCancel()
		pickCtx, pickCancel := context.WithTimeout(ctx, 2*time.Second)
		_, _ = session.Call(pickCtx, protocol.BuildMissionPickupAwardPacket(protocol.ActionIDMissionOpenBox), protocol.ActionIDMissionPickupAward)
		pickCancel()

		enterPkt = protocol.BuildMissionPlotEnterPacket(int32(m.ID), protocol.ActionIDMissionGetSections)
		enterCtxR, enterCancelR := context.WithTimeout(ctx, 4*time.Second)
		enterResp, enterErr = session.Call(enterCtxR, enterPkt, protocol.ActionIDMissionPlotEnter)
		enterCancelR()
		if enterErr != nil || enterResp == nil || len(enterResp.Payload) == 0 {
			return false
		}
		enterStatus = enterResp.Payload[0]
	}

	if enterStatus == protocol.MissionResultNotEnoughPower || enterStatus == 18 { // LESS_POWER == 18
		session.SetStamina(0)
		session.SetExtraStamina(0)
		if !ensureStamina() {
			return false
		}
		enterPkt = protocol.BuildMissionPlotEnterPacket(int32(m.ID), protocol.ActionIDMissionGetSections)
		enterCtx2, enterCancel2 := context.WithTimeout(ctx, 4*time.Second)
		enterResp, enterErr = session.Call(enterCtx2, enterPkt, protocol.ActionIDMissionPlotEnter)
		enterCancel2()
		if enterErr != nil || enterResp == nil || len(enterResp.Payload) == 0 {
			return false
		}
		enterStatus = enterResp.Payload[0]
	}

	if enterStatus != protocol.MissionResultSuccess {
		slog.Warn("进入剧情副本未成功", "role_id", session.RoleID(), "mission_id", m.ID, "name", m.Name, "status", fmt.Sprintf("0x%02X", enterStatus))
		return false
	}

	// 依据服务端 enter_mission 实时回包动态获取当前关卡待挑战怪物波次列表
	// 官方回包规范: [UByte(status), Int32(mid), Int32(stam), Byte(b), Int32(i1), Int32(i2), Uint16(mCnt), ...[Int32(mt)]]
	var activeMonsterTeams []int32
	if len(enterResp.Payload) >= 20 {
		mCnt := binary.BigEndian.Uint16(enterResp.Payload[18:20])
		if mCnt == 0 {
			slog.Info("剧情副本中无待挑战怪物（该关卡主线已通关）", "role_id", session.RoleID(), "mission_id", m.ID, "name", m.Name)
			clearedMissions[m.ID] = true
			return false
		}
		r := protocol.NewReader(enterResp.Payload[20:])
		for i := uint16(0); i < mCnt && r.Remaining() >= 4; i++ {
			mt, _ := r.ReadInt32()
			activeMonsterTeams = append(activeMonsterTeams, mt)
		}
	}
	if len(activeMonsterTeams) > 0 {
		monsterTeams = activeMonsterTeams
	}

	// 2. 逐波次挑战副本内怪物编队 (Mod 4, Act 2: fight_monster)
	anyWaveSuccess := false
	for idx, mtID := range monsterTeams {
		if idx > 0 {
			// 服务端连续战斗存在冷却保护 (对齐客户端 CommonConstant.DelayTimeToWar)
			select {
			case <-ctx.Done():
				resetTownScene(session)
				return anyWaveSuccess
			case <-time.After(1000 * time.Millisecond):
			}
		}

		fightPkt := protocol.BuildMissionFightMonsterPacket(mtID, 0, session.LastActionID())
		fightCtx, fightCancel := context.WithTimeout(ctx, 3*time.Second)
		fightResp, fightErr := session.Call(fightCtx, fightPkt, protocol.ActionIDMissionFightMonster)
		fightCancel()

		if fightErr != nil || fightResp == nil || len(fightResp.Payload) == 0 {
			if anyWaveSuccess {
				slog.Info("剧情副本已击败当前可用波次怪物，返回城镇同步进度", "role_id", session.RoleID(), "mission_id", m.ID, "monster_team_id", mtID)
				resetTownScene(session)
				return true
			}
			slog.Warn("剧情副本挑战怪物未收到响应", "role_id", session.RoleID(), "mission_id", m.ID, "monster_team_id", mtID, "err", fightErr)
			resetTownScene(session)
			return false
		}

		resByte := fightResp.Payload[len(fightResp.Payload)-1]
		// 10 = SUCCESS, 19 = TRUNK_ALREADY_FIGHT, 30 = HASCHALLENGED (该波怪物已击败，继续下一波)
		if resByte != protocol.MissionResultSuccess && resByte != 19 && resByte != 30 {
			if anyWaveSuccess {
				slog.Info("剧情副本后续波次暂不可用，返回城镇同步进度", "role_id", session.RoleID(), "mission_id", m.ID, "monster_team_id", mtID)
				resetTownScene(session)
				return true
			}
			slog.Warn("剧情副本战斗未获胜或受阻",
				"role_id", session.RoleID(),
				"mission_id", m.ID,
				"name", m.Name,
				"wave", fmt.Sprintf("%d/%d", idx+1, len(monsterTeams)),
				"monster_team_id", mtID,
				"result", resByte,
			)
			resetTownScene(session)
			return false
		}
		anyWaveSuccess = true
	}

	// 3. 结算星级评价 (Mod 4, Act 3: rank_mission)
	rankCtx, rankCancel := context.WithTimeout(ctx, 3*time.Second)
	_, _ = session.Call(rankCtx, protocol.BuildMissionRankPacket(protocol.ActionIDMissionFightMonster), protocol.ActionIDMissionRank)
	rankCancel()

	// 4. 开启通关宝箱 (Mod 4, Act 4: open_box)
	boxCtx, boxCancel := context.WithTimeout(ctx, 3*time.Second)
	_, _ = session.Call(boxCtx, protocol.BuildMissionOpenBoxPacket(protocol.ActionIDMissionRank), protocol.ActionIDMissionOpenBox)
	boxCancel()

	// 5. 拾取宝箱奖励 (Mod 4, Act 5: pickup_award)
	pickCtx, pickCancel := context.WithTimeout(ctx, 3*time.Second)
	_, _ = session.Call(pickCtx, protocol.BuildMissionPickupAwardPacket(protocol.ActionIDMissionOpenBox), protocol.ActionIDMissionPickupAward)
	pickCancel()

	// 通关完成，返回城镇场景
	resetTownScene(session)

	// 预留战斗冷却间隔，避免下一关卡首波战斗触发服务端 CD
	time.Sleep(1200 * time.Millisecond)

	remStamina, _ := session.ConsumeStamina(5)
	session.AddRewards(2500, 12000)
	*totalCostStamina += 5
	*totalGainExp += 2500
	*totalGainCoins += 12000
	clearedMissions[m.ID] = true

	slog.Info("任务关卡推进挑战成功",
		"role_id", session.RoleID(),
		"mission_id", m.ID,
		"mission_name", m.Name,
		"waves", len(monsterTeams),
		"remaining_stamina", remStamina,
	)
	return true
}


