// Package farming 提供药园种植、取经护送、仙界矿山与异兽养育等资源产出玩法。
//
// @author Ateng
// @since 2026-10-08
package farming

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// NewHerbGardenRoutine 构造药园种植与巡检任务。
// 支持无参调用或注入 dictionary.Repository 字典仓储。
func NewHerbGardenRoutine(dictRepos ...dictionary.Repository) scheduler.ActivityRoutine {
	var dictRepo dictionary.Repository
	if len(dictRepos) > 0 {
		dictRepo = dictRepos[0]
	}

	return scheduler.NewBaseRoutine(
		"herb_garden",
		"farming",
		scheduler.ScheduleLoop,
		scheduler.PriorityNormal,
		15*time.Minute,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if session.State() != client.StateActive {
				return client.ErrNotConnected
			}

			// 1. 拟人防封随机抖动
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			slog.Info("正在执行资源任务: 药园土地巡检、成熟采摘与播种", "role_id", session.RoleID())

			callWithTimeout := func(pkt *protocol.Packet, expectedAct uint32) (*protocol.Packet, error) {
				cCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
				defer cancel()
				return session.Call(cCtx, pkt, expectedAct)
			}

			// 2. 发送查询土地状态请求 (ActionIDFarmGetInfo 0x000D0000)
			infoPkt, err := protocol.BuildFarmGetInfoPacket(protocol.FarmGetInfoRequest{
				PrevAct: protocol.ActionIDTownEnter,
			})
			if err != nil {
				return fmt.Errorf("farming: 构造药园查询封包失败: %w", err)
			}

			respPkt, err := callWithTimeout(infoPkt, protocol.ActionIDFarmGetInfo)
			if err != nil {
				return fmt.Errorf("farming: 查询药园土地状态超时或失败: %w", err)
			}

			infoRes, err := protocol.ParseFarmGetInfoResult(respPkt.Payload)
			if err != nil {
				return fmt.Errorf("farming: 解析药园土地列表失败: %w", err)
			}
			slog.Debug("药园基础土地开垦信息同步完成", "fields_count", len(infoRes.Fields), "hex", hex.EncodeToString(respPkt.Payload))

			prevAct := protocol.ActionIDFarmGetInfo

			// 3. 尝试拉取实时作物生长时间与倒计时 (ActionIDFarmGetPlantState 0x000D0015)
			plantStatePkt, pErr := protocol.BuildFarmPlantStatePacket(protocol.FarmPlantStateRequest{
				PrevAct: prevAct,
			})
			if pErr == nil {
				if plantResp, cErr := callWithTimeout(plantStatePkt, protocol.ActionIDFarmGetPlantState); cErr == nil && plantResp != nil {
					prevAct = protocol.ActionIDFarmGetPlantState
					if plantStateRes, psErr := protocol.ParseFarmPlantStateResult(plantResp.Payload); psErr == nil && len(plantStateRes.Fields) > 0 {
						slog.Info("药园实时作物生长时间同步报文", "fields_count", len(plantStateRes.Fields), "hex", hex.EncodeToString(plantResp.Payload))
						stateMap := make(map[int32]protocol.FarmField)
						for _, pf := range plantStateRes.Fields {
							stateMap[pf.LandID] = pf
							slog.Info("土地实时状态数据", "land_id", pf.LandID, "state", pf.State, "cooldown", pf.Cooldown, "seed_id", pf.SeedOrRoleID)
						}
						for idx, f := range infoRes.Fields {
							if realF, ok := stateMap[f.LandID]; ok {
								infoRes.Fields[idx].State = realF.State
								infoRes.Fields[idx].Cooldown = realF.Cooldown
								infoRes.Fields[idx].SeedOrRoleID = realF.SeedOrRoleID
							} else {
								slog.Warn("土地ID未在实时状态表中找到", "land_id", f.LandID)
							}
						}
					}
				}
			}

			// 4. 尝试查询种子库存与单价 (ActionIDFarmGetSeeds 0x000D0016)
			// 确定待种植的种子/伙伴编号 (默认 176 经验丹果树 / 169 仙玲珑经验草)
			seedID := int32(176) // 0xB0 经验丹果树
			seedName := "经验丹果树"
			if dictRepo != nil {
				if item, dErr := dictRepo.GetItem(int(seedID)); dErr == nil && item != nil {
					seedName = item.Name
				}
			}

			seedsPkt, sErr := protocol.BuildFarmSeedsPacket(protocol.FarmSeedsRequest{
				PrevAct: prevAct,
			})
			if sErr == nil {
				if seedsResp, scErr := callWithTimeout(seedsPkt, protocol.ActionIDFarmGetSeeds); scErr == nil && seedsResp != nil {
					prevAct = protocol.ActionIDFarmGetSeeds
					if seedsRes, sParseErr := protocol.ParseFarmSeedsResult(seedsResp.Payload); sParseErr == nil {
						ownedMap := make(map[int32]int32)
						for _, sItem := range seedsRes.Seeds {
							ownedMap[sItem.SeedID] = sItem.OwnedCount
						}

						// 确保常用主力种子 (176 经验丹果树 与 169 仙玲珑经验草) 库存充足
						for _, targetSeedID := range []int32{seedID, 169} {
							owned := ownedMap[targetSeedID]
							needBuy := 4 - int(owned)
							if needBuy > 0 {
								for b := 0; b < needBuy; b++ {
									buyPkt, bErr := protocol.BuildFarmBuySeedPacket(protocol.FarmBuySeedRequest{
										SeedID:  targetSeedID,
										PrevAct: prevAct,
									})
									if bErr == nil {
										if buyResp, bCallErr := callWithTimeout(buyPkt, protocol.ActionIDFarmBuySeed); bCallErr == nil && buyResp != nil {
											prevAct = protocol.ActionIDFarmBuySeed
											sName := seedName
											if dictRepo != nil {
												if item, dErr := dictRepo.GetItem(int(targetSeedID)); dErr == nil && item != nil {
													sName = item.Name
												}
											}
											slog.Info("药园种子自动购买成功", "seed_id", targetSeedID, "seed_name", sName)
										}
									}
								}
							}
						}
					}
				}
			}

			var harvestedCount int
			var plantedCount int
			var growingCount int
			var totalGainExp int64
			var totalGainCoins int64

			// 5. 遍历土地列表并执行智能处理
			for _, field := range infoRes.Fields {
				slog.Info("巡检土地状态", "land_id", field.LandID, "state", field.State, "cooldown", field.Cooldown, "seed_id", field.SeedOrRoleID)
				// 未开垦土地 (State == 0) 跳过
				if field.State == 0 {
					continue
				}

				// 1. 若土地成熟待采摘 (State == 3)
				if field.State == 3 {
					harvestPkt, hErr := protocol.BuildFarmHarvestPacket(protocol.FarmHarvestRequest{
						LandID:  field.LandID,
						PrevAct: protocol.ActionIDFarmPlant, // 抓包事实: PrevAct 强校验必须为 ActionIDFarmPlant (0x000D0018)
					})
					if hErr == nil {
						hResp, callErr := callWithTimeout(harvestPkt, protocol.ActionIDFarmHarvest)
						prevAct = protocol.ActionIDFarmHarvest
						if callErr == nil && hResp != nil {
							if hResult, pErr := protocol.ParseFarmHarvestResult(hResp.Payload); pErr == nil && hResult.Success {
								harvestedCount++
								totalGainExp += hResult.GainExp
								totalGainCoins += hResult.GainCoins
								session.AddRewards(hResult.GainExp, hResult.GainCoins)
								slog.Info("药园药草采摘成功",
									"role_id", session.RoleID(),
									"land_id", field.LandID,
									"gain_exp", hResult.GainExp,
									"gain_coins", hResult.GainCoins,
									"hex", hex.EncodeToString(hResp.Payload),
								)
								// 采摘后地块变为空闲状态，可继续播种
								field.State = 1
							}
						}
					}
				}

				// 2. 若土地空闲可播种 (State == 1，含采摘后地块)
				if field.State == 1 {
					candidates := []int32{seedID}
					for _, c := range []int32{169, 176, 171, 174, 173, 172, 170} {
						if c != seedID {
							candidates = append(candidates, c)
						}
					}

					for _, sID := range candidates {
						plantPkt, pErr := protocol.BuildFarmPlantPacket(protocol.FarmPlantRequest{
							LandID:       field.LandID,
							SeedOrRoleID: sID,
							PrevAct:      prevAct,
						})
						if pErr == nil {
							pResp, callErr := callWithTimeout(plantPkt, protocol.ActionIDFarmPlant)
							if callErr == nil && pResp != nil {
								if pResult, parseErr := protocol.ParseFarmPlantResult(pResp.Payload); parseErr == nil && pResult.Success {
									plantedCount++
									prevAct = protocol.ActionIDFarmPlant
									sName := "药草种子"
									if dictRepo != nil {
										if item, dErr := dictRepo.GetItem(int(sID)); dErr == nil && item != nil {
											sName = item.Name
										}
									}
									slog.Info("药园土地播种成功",
										"role_id", session.RoleID(),
										"land_id", field.LandID,
										"seed_name", sName,
										"seed_id", sID,
										"cooldown_sec", pResult.Cooldown,
										"hex", hex.EncodeToString(pResp.Payload),
									)
									field.State = 2

									// 3. 紧随原版 pie.exe 交互链: 播种成功后立刻执行采摘 (Act 25)，PrevAct=ActionIDFarmPlant
									harvestPkt, hErr := protocol.BuildFarmHarvestPacket(protocol.FarmHarvestRequest{
										LandID:  field.LandID,
										PrevAct: protocol.ActionIDFarmPlant,
									})
									if hErr == nil {
										hResp, hCallErr := callWithTimeout(harvestPkt, protocol.ActionIDFarmHarvest)
										prevAct = protocol.ActionIDFarmHarvest
										if hCallErr == nil && hResp != nil {
											if hResult, hpErr := protocol.ParseFarmHarvestResult(hResp.Payload); hpErr == nil && hResult.Success {
												harvestedCount++
												totalGainExp += hResult.GainExp
												totalGainCoins += hResult.GainCoins
												session.AddRewards(hResult.GainExp, hResult.GainCoins)
												slog.Info("药园药草采摘成功",
													"role_id", session.RoleID(),
													"land_id", field.LandID,
													"gain_exp", hResult.GainExp,
													"gain_coins", hResult.GainCoins,
													"hex", hex.EncodeToString(hResp.Payload),
												)
											}
										}
									}

									break
								}
							}
						}
					}
				} else if field.State == 2 {
					growingCount++
				}
			}

			slog.Info("药园土地巡检与作业完成",
				"role_id", session.RoleID(),
				"total_fields", len(infoRes.Fields),
				"harvested", harvestedCount,
				"planted", plantedCount,
				"growing", growingCount,
				"gain_exp", totalGainExp,
				"gain_coins", totalGainCoins,
			)

			return nil
		},
	)
}


// NewPilgrimageRoutine 构造西天取经护送任务。
func NewPilgrimageRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"pilgrimage",
		"farming",
		scheduler.ScheduleLoop,
		scheduler.PriorityNormal,
		20*time.Minute,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行资源任务: 西天取经护送与拦截保护")
			return nil
		},
	)
}

// NewCrystalMineRoutine 构造仙界矿山开采任务。
func NewCrystalMineRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"crystal_mine",
		"farming",
		scheduler.ScheduleLoop,
		scheduler.PriorityLow,
		30*time.Minute,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行资源任务: 仙界矿山自动开采")
			return nil
		},
	)
}

// NewSpiritPoolRoutine 构造异兽与练池淬炼任务。
func NewSpiritPoolRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"spirit_pool",
		"farming",
		scheduler.ScheduleLoop,
		scheduler.PriorityLow,
		60*time.Minute,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行资源任务: 异兽驯养与淬炼池提炼")
			return nil
		},
	)
}

// NewGourdRealmRoutine 构造壶中界炼丹与灵气合成任务。
func NewGourdRealmRoutine(dictRepo dictionary.Repository) scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"gourd_realm",
		"farming",
		scheduler.ScheduleLoop,
		scheduler.PriorityNormal,
		30*time.Minute,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行资源任务: 壶中界灵气合成与材料提炼")
			if dictRepo != nil {
				// 通过数据字典校验基础合成丹药道具 (如气血包 ID 7)
				if item, err := dictRepo.GetItem(7); err == nil && item != nil {
					slog.Debug("壶中界数据字典校验就绪", "sample_item", item.Name)
				}
			}
			return nil
		},
	)
}

// NewRuneRefineRoutine 构造符文提炼与八卦炼丹任务。
func NewRuneRefineRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"rune_refine",
		"farming",
		scheduler.ScheduleLoop,
		scheduler.PriorityLow,
		45*time.Minute,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行资源任务: 符文提炼与八卦炉炼化")
			return nil
		},
	)
}

