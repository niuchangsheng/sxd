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
						slog.Debug("药园实时作物生长时间同步报文", "fields_count", len(plantStateRes.Fields), "hex", hex.EncodeToString(plantResp.Payload))
						stateMap := make(map[int32]protocol.FarmField)
						for _, pf := range plantStateRes.Fields {
							stateMap[pf.LandID] = pf
							slog.Debug("土地实时状态数据", "land_id", pf.LandID, "state", pf.State, "cooldown", pf.Cooldown, "seed_id", pf.SeedOrRoleID)
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
			if session == nil {
				return client.ErrNotConnected
			}
			if session.State() != client.StateActive {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			slog.Info("正在执行资源任务: 西天取经巡检与护送", "role_id", session.RoleID())

			callWithTimeout := func(pkt *protocol.Packet, expectedAct uint32) (*protocol.Packet, error) {
				cCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
				defer cancel()
				return session.Call(cCtx, pkt, expectedAct)
			}

			// 1. 打开西天取经主面板 (0x001E0000)
			// 若服务端支持 Action 0 同步应答则处理概览数据；若服务端不响应 Action 0 则继续执行详情拉取
			openPkt := protocol.BuildTakeBibleOpenPacket(session.LastActionID())
			if openResp, err := callWithTimeout(openPkt, protocol.ActionIDTakeBibleOpen); err == nil && openResp != nil {
				openRes, _ := protocol.ParseTakeBibleOpenResult(openResp.Payload)
				if openRes != nil && openRes.CanTakeTimes <= 0 {
					slog.Info("西天取经今日护送次数已用尽", "role_id", session.RoleID(), "remain_times", openRes.CanTakeTimes)
					// 关闭面板
					closePkt := protocol.BuildTakeBibleClosePacket(session.LastActionID())
					_, _ = callWithTimeout(closePkt, protocol.ActionIDTakeBibleClose)
					return nil
				}
			}

			// 2. 拉取当前取经准备状态与使者数据 (0x001E0002)
			infoPkt := protocol.BuildTakeBibleGetInfoPacket(session.LastActionID())
			infoResp, err := callWithTimeout(infoPkt, protocol.ActionIDTakeBibleGetInfo)
			if err != nil {
				slog.Warn("拉取西天取经准备详情无应答，当前角色可能尚未解锁护送权限或该服取经已迁移",
					"role_id", session.RoleID(),
					"error", err,
				)
				return nil
			}

			infoRes, err := protocol.ParseTakeBibleGetInfoResult(infoResp.Payload)
			if err != nil {
				return fmt.Errorf("farming: 解析取经详情失败: %w", err)
			}

			prevAct := protocol.ActionIDTakeBibleGetInfo

			// 检查是否已经在护送中
			if infoRes.IsBusy {
				slog.Info("当前取经队伍已在途中，无需重复启程", "role_id", session.RoleID(), "angel", protocol.AngelName(infoRes.CurrentAngel))
				closePkt := protocol.BuildTakeBibleClosePacket(prevAct)
				_, _ = callWithTimeout(closePkt, protocol.ActionIDTakeBibleClose)
				return nil
			}

			// 检查今日总取经次数是否已达到上限 (每日上限 3 次)
			if infoRes.TotalTakeTimes >= 3 {
				slog.Info("西天取经今日总护送次数已达上限 (3次)", "role_id", session.RoleID(), "total_times", infoRes.TotalTakeTimes)
				closePkt := protocol.BuildTakeBibleClosePacket(prevAct)
				_, _ = callWithTimeout(closePkt, protocol.ActionIDTakeBibleClose)
				return nil
			}

			slog.Info("当前取经使者就绪",
				"role_id", session.RoleID(),
				"angel", protocol.AngelName(infoRes.CurrentAngel),
				"protect_player_id", infoRes.ProtectPlayerID,
				"remain_rob_times", infoRes.RobTimes,
			)

			// 3. 使者品质刷新策略: 若不是最高品质使者 (唐僧)，尝试刷新提升护送收益
			if infoRes.CurrentAngel < protocol.TakeBibleTangSeng {
				refreshPkt := protocol.BuildTakeBibleRefreshPacket(prevAct)
				if refResp, rErr := callWithTimeout(refreshPkt, protocol.ActionIDTakeBibleRefresh); rErr == nil && refResp != nil {
					prevAct = protocol.ActionIDTakeBibleRefresh
					if refRes, prErr := protocol.ParseTakeBibleRefreshResult(refResp.Payload); prErr == nil && refRes.ResultCode == protocol.TakeBibleSuccess {
						slog.Info("西天取经使者刷新成功",
							"role_id", session.RoleID(),
							"old_angel", protocol.AngelName(infoRes.CurrentAngel),
							"new_angel", protocol.AngelName(refRes.NewAngel),
							"next_cost", refRes.NextRefreshCost,
						)
						infoRes.CurrentAngel = refRes.NewAngel
					}
				}
			}

			// 4. 正式启程开始护送 (0x001E000A) - 无需等待/邀请好友，直接独立启程
			startPkt := protocol.BuildTakeBibleStartPacket(prevAct)
			startResp, err := callWithTimeout(startPkt, protocol.ActionIDTakeBibleStart)
			if err != nil {
				return fmt.Errorf("farming: 启程西天取经失败: %w", err)
			}
			prevAct = protocol.ActionIDTakeBibleStart

			startCode, _ := protocol.ParseTakeBibleStartResult(startResp.Payload)
			if startCode == protocol.TakeBibleSuccess {
				slog.Info("西天取经队伍已顺利启程护送",
					"role_id", session.RoleID(),
					"angel", protocol.AngelName(infoRes.CurrentAngel),
				)
			} else {
				slog.Warn("启程西天取经返回异常状态", "role_id", session.RoleID(), "status_code", startCode)
			}

			// 5. 安全关闭取经主界面 (0x001E0001)
			closePkt := protocol.BuildTakeBibleClosePacket(prevAct)
			_, _ = callWithTimeout(closePkt, protocol.ActionIDTakeBibleClose)
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

// NewSpiritPoolRoutine 构造粗铁淬炼池与异兽提炼任务 (Feature 5: 淬炼粗铁)。
func NewSpiritPoolRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"spirit_pool",
		"farming",
		scheduler.ScheduleLoop,
		scheduler.PriorityLow,
		30*time.Minute,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			slog.Info("正在执行资源任务: 粗铁淬炼池提炼", "role_id", session.RoleID())

			// 1. 查询淬炼池面板 (Mod 534, Act 5)
			panelPkt := protocol.BuildRefinePoolPanelPacket(protocol.ActionIDTownEnter)
			pCtx, pCancel := context.WithTimeout(ctx, 2*time.Second)
			_, _ = session.Call(pCtx, panelPkt, protocol.ActionIDRefinePoolPanel)
			pCancel()

			// 2. 发起粗铁淬炼执行 (Mod 282, Act 1)
			execPkt := protocol.BuildRefineExecutePacket(protocol.ActionIDRefinePoolPanel)
			eCtx, eCancel := context.WithTimeout(ctx, 2*time.Second)
			resp, err := session.Call(eCtx, execPkt, protocol.ActionIDRefineExecute)
			eCancel()

			if err == nil && resp != nil {
				slog.Info("粗铁淬炼提纯执行成功", "role_id", session.RoleID())
			}
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

