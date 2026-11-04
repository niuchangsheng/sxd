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
	"sort"
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

// crossTakeBibleCallFunc 仙界跨服取经协议调用函数签名。
type crossTakeBibleCallFunc func(pkt *protocol.Packet, expectedAct uint32, d time.Duration) (*protocol.Packet, error)

const (
	// crossTakeBibleRobMaxProbes 单轮打劫最多探测的目标数量，避免遍历整条取经路。
	crossTakeBibleRobMaxProbes = 8
	// crossTakeBibleRobMaxAttempts 单轮打劫最多尝试次数 (与游戏每日上限 5 次保持一致)。
	crossTakeBibleRobMaxAttempts = 5
	// crossTakeBibleRobProbeHardLimit 探测数量硬上限，防止异常配置造成大量请求。
	crossTakeBibleRobProbeHardLimit = 50
	// crossTakeBibleRobInterval 连续打劫之间的拟人化间隔。
	crossTakeBibleRobInterval = 500 * time.Millisecond
	// crossTakeBibleRobTimeout 单次打劫等待结算回包的超时。
	//
	// rob 回包携带完整战报 (实机约 8KB，随目标战力与战斗回合数增长)，
	// 跨服节点需要先完成战斗计算再下发；4s 预算曾把高战力/拥塞场景误判为无应答，
	// 因此与仙界竞技场 challenge 对齐，给足战报结算时间。
	crossTakeBibleRobTimeout = 15 * time.Second
	// crossTakeBibleRobCDTimeout 打劫冷却查询超时 (也用于超时后的受理复核)。
	crossTakeBibleRobCDTimeout = 3 * time.Second
)

// PilgrimageConfig 西天取经护送与打劫自动化策略配置。
type PilgrimageConfig struct {
	// AutoRob 是否在启程护送后自动打劫取经路上其他玩家队伍。
	AutoRob bool
	// MaxRobAttempts 单轮最多尝试打劫次数 (上限 5)。
	MaxRobAttempts int
	// MaxProbeTargets 单轮最多探测的目标队伍数量 (上限 50)。
	MaxProbeTargets int
}

// DefaultPilgrimageConfig 返回默认取经策略：自动打劫、最多尝试 5 次、探测 8 支队伍。
func DefaultPilgrimageConfig() PilgrimageConfig {
	return PilgrimageConfig{
		AutoRob:         true,
		MaxRobAttempts:  crossTakeBibleRobMaxAttempts,
		MaxProbeTargets: crossTakeBibleRobMaxProbes,
	}
}

// normalizePilgrimageConfig 将取经策略参数收敛到安全范围。
func normalizePilgrimageConfig(cfg PilgrimageConfig) PilgrimageConfig {
	if cfg.MaxRobAttempts <= 0 {
		cfg.MaxRobAttempts = crossTakeBibleRobMaxAttempts
	}
	if cfg.MaxRobAttempts > crossTakeBibleRobMaxAttempts {
		cfg.MaxRobAttempts = crossTakeBibleRobMaxAttempts
	}
	if cfg.MaxProbeTargets <= 0 {
		cfg.MaxProbeTargets = crossTakeBibleRobMaxProbes
	}
	if cfg.MaxProbeTargets > crossTakeBibleRobProbeHardLimit {
		cfg.MaxProbeTargets = crossTakeBibleRobProbeHardLimit
	}
	return cfg
}

// handleCrossPilgrimage 在官方仙界跨服连接 (Mod 114) 上执行一次取经巡检、启程与打劫。
//
// 官方客户端流程为：Mod 96 取跨服凭据 → Mod 94 登录跨服节点 → Mod 95 进入仙界城镇
// → Mod 114 get_take_bible_info → (可选 refresh) → start_take_bible
// → open_take_bible → get_rob_take_bible_cd_time → rob。
// 返回 true 表示跨服链路已建立并完成处理；返回 false 表示当前环境不支持仙界跨服
// 连接（凭据缺失或登录失败），调用方应回退到主服传统取经协议。
func handleCrossPilgrimage(ctx context.Context, session *client.RoleSession, cfg PilgrimageConfig) bool {
	cfg = normalizePilgrimageConfig(cfg)
	// 一次巡检最多包含护送、刷新、路况查询、目标探测与多次打劫，放宽整体超时。
	// rob 回包需等待跨服节点完成战斗计算 (单次预算 15s)，整体预算相应放宽。
	crossCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	// 1. 先以短超时申请凭据：单机/测试环境无此模块时应快速回退，不阻塞传统链路。
	credCtx, credCancel := context.WithTimeout(crossCtx, 2*time.Second)
	cred, err := session.FetchCrossServerCredentials(credCtx)
	credCancel()
	if err != nil {
		slog.Debug("仙界跨服取经不可用，回退传统取经协议", "role_id", session.RoleID(), "error", err)
		return false
	}

	// 2. 建立仙界跨服会话并调用 Mod 114。
	cross, err := session.OpenCrossServerSessionWithInfo(crossCtx, cred)
	if err != nil {
		slog.Warn("仙界跨服会话建立失败，回退传统取经协议", "role_id", session.RoleID(), "error", err)
		return false
	}
	defer cross.Close()

	crossCall := func(pkt *protocol.Packet, expectedAct uint32, d time.Duration) (*protocol.Packet, error) {
		cCtx, cCancel := context.WithTimeout(crossCtx, d)
		defer cCancel()
		return cross.Call(cCtx, pkt, expectedAct)
	}

	crossPilgrimageEscort(ctx, session, crossCall)
	if cfg.AutoRob {
		// 路况队伍与战报中的玩家 ID 均为跨服 ID 空间，必须使用跨服会话的身份
		// 判断“自己发起的队伍”与打劫胜负，不能混用主服 PlayerID。
		selfID := cross.PlayerID()
		if selfID <= 0 {
			selfID = session.PlayerID()
		}
		crossPilgrimageRob(ctx, session, selfID, crossCall, cfg)
	} else {
		slog.Debug("仙界取经自动打劫已关闭，仅执行护送巡检", "role_id", session.RoleID())
	}
	return true
}

// crossPilgrimageEscort 在仙界跨服连接上完成一次取经使者巡检与启程。
func crossPilgrimageEscort(ctx context.Context, session *client.RoleSession, crossCall crossTakeBibleCallFunc) {
	infoResp, err := crossCall(protocol.BuildStTakeBibleCrossGetInfoPacket(), protocol.ActionIDStTakeBibleCrossGetInfo, 3*time.Second)
	if err != nil {
		slog.Warn("仙界取经详情无应答", "role_id", session.RoleID(), "error", err)
		return
	}
	info, err := protocol.ParseStTakeBibleCrossInfoResult(infoResp.Payload)
	if err != nil {
		slog.Warn("解析仙界取经详情失败", "role_id", session.RoleID(), "error", err)
		return
	}
	slog.Info("仙界取经准备详情",
		"role_id", session.RoleID(),
		"take_times", info.TakeBibleTimes,
		"total_times", info.TotalTakeBibleTimes,
		"refresh_price", info.RefreshPrice,
		"status", info.TakeBibleStatus,
		"protection", protocol.StTakeBibleCrossProtectionName(info.CanProtection),
		"protect_player", info.ProtectPlayer,
		"protection_options", len(info.ProtectionList),
		"protect_players", len(info.ProtectPlayerList),
	)

	// 每日护送上限 (3 次) 已用满时无需再启程。
	if info.TotalTakeBibleTimes <= 0 || info.TakeBibleTimes >= uint8(info.TotalTakeBibleTimes) {
		slog.Info("仙界取经今日护送次数已用满，无需启程",
			"role_id", session.RoleID(),
			"take_times", info.TakeBibleTimes,
			"total_times", info.TotalTakeBibleTimes,
		)
		return
	}

	// 使者非最高品质时尝试刷新一次 (与原 Mod 30 策略保持一致，刷新消耗元宝)。
	protection := info.CanProtection
	if protection < protocol.StTakeBibleCrossProtectionTangSeng {
		refreshResp, rErr := crossCall(protocol.BuildStTakeBibleCrossRefreshPacket(), protocol.ActionIDStTakeBibleCrossRefresh, 2*time.Second)
		if rErr == nil && refreshResp != nil {
			msg, newProtection, nextPrice, pErr := protocol.ParseStTakeBibleCrossRefreshResult(refreshResp.Payload)
			if pErr == nil && msg == protocol.StTakeBibleCrossMsgSuccess {
				slog.Info("仙界取经使者刷新成功",
					"role_id", session.RoleID(),
					"old_protection", protocol.StTakeBibleCrossProtectionName(protection),
					"new_protection", protocol.StTakeBibleCrossProtectionName(newProtection),
					"next_price", nextPrice,
				)
				protection = newProtection
			}
		}
	}

	startResp, err := crossCall(protocol.BuildStTakeBibleCrossStartPacket(), protocol.ActionIDStTakeBibleCrossStart, 3*time.Second)
	if err != nil {
		slog.Warn("仙界取经启程无应答", "role_id", session.RoleID(), "error", err)
		return
	}
	startMsg, itemID, err := protocol.ParseStTakeBibleCrossStartResult(startResp.Payload)
	if err != nil {
		slog.Warn("解析仙界取经启程回包失败", "role_id", session.RoleID(), "error", err)
		return
	}

	switch startMsg {
	case protocol.StTakeBibleCrossMsgSuccess:
		slog.Info("仙界西天取经队伍已顺利启程护送",
			"role_id", session.RoleID(),
			"protection", protocol.StTakeBibleCrossProtectionName(protection),
			"item_id", itemID,
		)
	case protocol.StTakeBibleCrossMsgTakeBibleStatus:
		slog.Info("仙界西天取经队伍正在护送途中，无需重复启程", "role_id", session.RoleID())
	case protocol.StTakeBibleCrossMsgFullTakeBibleTimes:
		slog.Info("仙界西天取经今日护送次数已用满", "role_id", session.RoleID(), "total_times", info.TotalTakeBibleTimes)
	case protocol.StTakeBibleCrossMsgProtectTime:
		slog.Info("仙界西天取经当前处于护送保护时间", "role_id", session.RoleID())
	case protocol.StTakeBibleCrossMsgNotEnoughIngot:
		slog.Info("仙界西天取经刷新元宝不足", "role_id", session.RoleID())
	default:
		slog.Info("仙界西天取经完成巡检", "role_id", session.RoleID(), "status_code", startMsg)
	}
}

// crossPilgrimageRob 在仙界取经路上执行一轮打劫。
//
// 官方客户端流程：open_take_bible 取得路况队伍列表 → get_rob_take_bible_cd_time
// 确认冷却 → get_player_take_bible 校验目标是否可劫 → rob(player_id, sequence_id)。
// 单次调度的探测与尝试上限由 PilgrimageConfig 控制 (默认 8 支/5 次)；
// 每次打劫后重新查询冷却与剩余次数，冷却中立即收手。
// 本流程仅使用免费次数，不消耗元宝清冷却或购买额外打劫次数。
//
// selfID 为跨服会话玩家 ID：路况队伍、战报胜负位与 stageServer 均使用跨服 ID 空间。
func crossPilgrimageRob(ctx context.Context, session *client.RoleSession, selfID int32, crossCall crossTakeBibleCallFunc, cfg PilgrimageConfig) {
	openResp, err := crossCall(protocol.BuildStTakeBibleCrossOpenPacket(), protocol.ActionIDStTakeBibleCrossOpen, 3*time.Second)
	if err != nil {
		slog.Warn("仙界取经路况查询无应答", "role_id", session.RoleID(), "error", err)
		return
	}
	road, err := protocol.ParseStTakeBibleCrossRoadResult(openResp.Payload)
	if err != nil {
		slog.Warn("解析仙界取经路况失败", "role_id", session.RoleID(), "error", err)
		return
	}
	defer func() {
		if _, cErr := crossCall(protocol.BuildStTakeBibleCrossClosePacket(), protocol.ActionIDStTakeBibleCrossClose, time.Second); cErr != nil {
			slog.Debug("关闭仙界取经路况面板失败", "role_id", session.RoleID(), "error", cErr)
		}
	}()

	slog.Info("仙界取经路况同步完成",
		"role_id", session.RoleID(),
		"road_teams", len(road.Teams),
		"can_rob_times", road.CanRobTimes,
		"last_robs", len(road.LastRobs),
	)
	if road.CanRobTimes <= 0 {
		slog.Info("仙界取经今日打劫次数已用尽", "role_id", session.RoleID())
		return
	}

	cd, ok := crossTakeBibleRobCD(session, crossCall)
	if !ok {
		return
	}
	if cd.CDSeconds > 0 {
		slog.Info("仙界取经当前处于打劫冷却中", "role_id", session.RoleID(), "cooldown_sec", cd.CDSeconds)
		return
	}

	candidates := make([]protocol.StTakeBibleCrossRoadTeam, 0, len(road.Teams))
	for _, team := range road.Teams {
		// 跳过自己发起的队伍，以及自己正在保护的队伍：官方客户端同样禁止打劫。
		if team.PlayerID <= 0 || team.PlayerID == selfID || team.ProtectPlayerID == selfID {
			continue
		}
		candidates = append(candidates, team)
	}
	if len(candidates) == 0 {
		slog.Info("仙界取经路上暂无可打劫队伍", "role_id", session.RoleID())
		return
	}

	// 优先探测高品质使者队伍：收益更高，同时把请求量控制在固定上限内。
	// 注意这里只决定“探测谁”，最终出手顺序由目标详情中的等级决定 (软柿子优先)：
	// 打劫失败的战斗不扣除每日次数，因此先打等级最低的目标才能实际消耗次数。
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Protection != candidates[j].Protection {
			return candidates[i].Protection > candidates[j].Protection
		}
		if candidates[i].Distance != candidates[j].Distance {
			return candidates[i].Distance < candidates[j].Distance
		}
		return candidates[i].PlayerID < candidates[j].PlayerID
	})
	probeCount := len(candidates)
	if probeCount > cfg.MaxProbeTargets {
		probeCount = cfg.MaxProbeTargets
	}

	type robTarget struct {
		team  protocol.StTakeBibleCrossRoadTeam
		info  protocol.StTakeBibleCrossPlayerInfo
		score int64
	}
	level := int16(session.GetPlayerState().Level)
	targets := make([]robTarget, 0, probeCount)
	probeFailed := 0
	for _, team := range candidates[:probeCount] {
		infoResp, cErr := crossCall(
			protocol.BuildStTakeBibleCrossGetPlayerPacket(team.PlayerID, level, 0, team.SequenceID),
			protocol.ActionIDStTakeBibleCrossGetPlayer,
			2*time.Second,
		)
		if cErr != nil {
			probeFailed++
			slog.Debug("仙界取经目标详情无应答", "role_id", session.RoleID(), "player_id", team.PlayerID, "error", cErr)
			continue
		}
		info, pErr := protocol.ParseStTakeBibleCrossPlayerInfoResult(infoResp.Payload)
		if pErr != nil {
			slog.Debug("解析仙界取经目标详情失败", "role_id", session.RoleID(), "player_id", team.PlayerID, "error", pErr)
			continue
		}
		// 已打劫过、目标已被抢光或处于保护时间时跳过。
		if info.IsRobbed != 0 ||
			(info.TotalCanRobTimes > 0 && info.BeRobTimes >= info.TotalCanRobTimes) ||
			info.ProtectTime > 0 {
			continue
		}
		if info.RobCoin <= 0 && info.RobFame <= 0 && info.RobStExp <= 0 {
			continue
		}
		score := int64(info.RobCoin)*1000 + int64(info.RobFame)*10 + int64(info.RobStExp)
		targets = append(targets, robTarget{team: team, info: *info, score: score})
	}
	// 软柿子优先：服务端只按战斗胜负扣减每日打劫次数，攻击打不过的高战目标
	// 会白白浪费一次 10 分钟冷却而不消耗次数。等级是 get_player_take_bible
	// 下发的唯一实力代理指标 (与仙界竞技场软柿子算法一致)，等级越低越先打；
	// 等级相同时再按预期收益从高到低选择。等级未知 (0) 的目标排到最后。
	sort.SliceStable(targets, func(i, j int) bool {
		li, lj := targets[i].info.Level, targets[j].info.Level
		if (li > 0) != (lj > 0) {
			return li > 0
		}
		if li != lj {
			return li < lj
		}
		if targets[i].score != targets[j].score {
			return targets[i].score > targets[j].score
		}
		return targets[i].team.PlayerID < targets[j].team.PlayerID
	})
	// 探测超时会被静默跳过；聚合到一条 Info 日志，便于区分“路上没人可劫”
	// 与“跨服节点繁忙/丢包”，避免线上只能看到打劫无应答而缺少上下文。
	if probeFailed > 0 {
		slog.Info("仙界取经目标探测存在无应答",
			"role_id", session.RoleID(),
			"probes", probeCount,
			"failed", probeFailed,
			"robbable", len(targets),
		)
	}

	attempts := int(road.CanRobTimes)
	if attempts > cfg.MaxRobAttempts {
		attempts = cfg.MaxRobAttempts
	}
	if attempts > len(targets) {
		attempts = len(targets)
	}
	if attempts <= 0 {
		slog.Info("仙界取经路上暂无可打劫目标", "role_id", session.RoleID(), "candidates", len(candidates))
		return
	}

	var (
		robbed     int
		totalCoin  int64
		totalFame  int64
		totalStExp int64
	)
	for i := 0; i < attempts; i++ {
		target := targets[i]
		// 官方客户端使用 get_player_take_bible 回包的 sequence_id 发起打劫；
		// 路况包中的 sequence_id 仅用于探测。两者不一致时以目标详情为准。
		sequenceID := target.info.SequenceID
		if sequenceID == 0 {
			sequenceID = target.team.SequenceID
		}
		slog.Info("仙界取经发起打劫",
			"role_id", session.RoleID(),
			"target_player_id", target.team.PlayerID,
			"target_nickname", target.info.NickName,
			"target_level", target.info.Level,
			"protection", protocol.StTakeBibleCrossProtectionName(target.team.Protection),
			"protect_player_id", target.team.ProtectPlayerID,
			"is_deeds_so", target.team.IsDeedsSo,
			"distance", target.team.Distance,
			"sequence_id", sequenceID,
			"be_rob_times", target.info.BeRobTimes,
			"total_can_rob_times", target.info.TotalCanRobTimes,
			"expect_coin", target.info.RobCoin,
			"expect_fame", target.info.RobFame,
		)

		robResp, cErr := crossCall(
			protocol.BuildStTakeBibleCrossRobPacket(target.team.PlayerID, sequenceID),
			protocol.ActionIDStTakeBibleCrossRob,
			crossTakeBibleRobTimeout,
		)
		if cErr != nil {
			// 回包超时不等于打劫未被受理：rob 回包要先由服务端完成战斗计算，
			// 高战力目标或跨服节点拥塞时可能超过任何固定预算。
			// 立即补查冷却：已进入冷却说明服务端已结算，绝不能重复发起；
			// 冷却仍为 0 才说明请求确实未被受理，可安全尝试下一个目标。
			accepted, known := crossTakeBibleRobReconcile(session, crossCall)
			if !known {
				slog.Warn("仙界取经打劫无应答且冷却复核失败，保守结束本轮打劫",
					"role_id", session.RoleID(),
					"target_player_id", target.team.PlayerID,
					"error", cErr,
				)
				break
			}
			if accepted {
				slog.Warn("仙界取经打劫无应答但服务端已进入冷却，按已受理结束本轮打劫",
					"role_id", session.RoleID(),
					"target_player_id", target.team.PlayerID,
					"error", cErr,
				)
				break
			}
			slog.Warn("仙界取经打劫无应答且服务端未进入冷却，跳过该目标",
				"role_id", session.RoleID(),
				"target_player_id", target.team.PlayerID,
				"error", cErr,
			)
			if i+1 < attempts {
				select {
				case <-ctx.Done():
					return
				case <-time.After(crossTakeBibleRobInterval):
				}
			}
			continue
		}
		result, pErr := protocol.ParseStTakeBibleCrossRobResult(robResp.Payload)
		won := false
		switch {
		case pErr != nil:
			slog.Warn("解析仙界取经打劫回包失败", "role_id", session.RoleID(), "target_player_id", target.team.PlayerID, "error", pErr)
		case result.Msg == protocol.StTakeBibleCrossMsgSuccess:
			// msg=SUCCESS 只代表战斗已完成结算，不代表打劫获胜。服务端只有在
			// 战胜目标时才会扣减每日打劫次数并发放奖励；战败时 rob_coin/fame/exp
			// 均为 0，次数保持不变。必须依据战报胜负位如实记录，不能记成成功。
			won = result.IsWin(selfID)
			if !result.WarParsed {
				// 兼容不回传战报的节点：以是否发放奖励粗判胜负。
				won = result.RobCoin > 0 || result.RobFame > 0 || result.RobStExp > 0
			}
			if won {
				robbed++
				totalCoin += int64(result.RobCoin)
				totalFame += int64(result.RobFame)
				totalStExp += int64(result.RobStExp)
				session.AddRewards(int64(result.RobStExp), int64(result.RobCoin))
				slog.Info("仙界取经打劫获胜",
					"role_id", session.RoleID(),
					"target_player_id", result.TargetPlayerID,
					"target_nickname", result.TargetNickName,
					"target_level", target.info.Level,
					"rob_coin", result.RobCoin,
					"rob_fame", result.RobFame,
					"rob_st_exp", result.RobStExp,
					"win_player_id", result.WinPlayerID,
					"deploy_type", result.DeployType,
					"war_report_id", result.WarReportID,
				)
			} else {
				slog.Warn("仙界取经打劫战斗失败，未消耗每日打劫次数",
					"role_id", session.RoleID(),
					"target_player_id", target.team.PlayerID,
					"target_nickname", target.info.NickName,
					"target_level", target.info.Level,
					"self_level", level,
					"win_player_id", result.WinPlayerID,
					"war_parsed", result.WarParsed,
					"war_report_id", result.WarReportID,
				)
			}
		default:
			slog.Info("仙界取经打劫未成功",
				"role_id", session.RoleID(),
				"target_player_id", target.team.PlayerID,
				"msg", result.Msg,
				"reason", protocol.StTakeBibleCrossRobMsgName(result.Msg),
			)
		}

		// 官方客户端在 rob 回包后无论成功还是失败都会重新查询冷却。
		// 打劫失败同样可能触发冷却，必须立即收手，避免继续发送无效请求。
		cd, ok = crossTakeBibleRobCD(session, crossCall)
		if !ok {
			break
		}
		if cd.CDSeconds > 0 {
			slog.Info("仙界取经打劫进入冷却，结束本轮打劫", "role_id", session.RoleID(), "cooldown_sec", cd.CDSeconds)
			break
		}
		if pErr != nil {
			// 回包无法解析时保守结束本轮，避免在打劫状态未知时继续操作。
			break
		}
		if result.Msg == protocol.StTakeBibleCrossMsgFullRobTimes ||
			result.Msg == protocol.StTakeBibleCrossMsgHaveCDTime {
			break
		}
		// 官方客户端每次 rob 回包后都会同步 get_take_bible_updata_data。
		// 打劫失败不会扣减每日次数，以服务端下发的剩余次数为准决定是否继续，
		// 避免仅凭路况快照把“未消耗次数”误当成“还有次数”。
		if update, uErr := crossTakeBibleRobTimes(session, crossCall); uErr == nil {
			slog.Info("仙界取经打劫后剩余次数同步",
				"role_id", session.RoleID(),
				"remain_rob_times", update.CanRobTimes,
				"remain_take_times", update.CanTakeBibleTimes,
			)
			if update.CanRobTimes <= 0 {
				slog.Info("仙界取经今日打劫次数已用尽", "role_id", session.RoleID())
				break
			}
		}
		if i+1 < attempts {
			select {
			case <-ctx.Done():
				return
			case <-time.After(crossTakeBibleRobInterval):
			}
		}
	}

	slog.Info("仙界取经打劫巡检完成",
		"role_id", session.RoleID(),
		"robbed", robbed,
		"rob_coin", totalCoin,
		"rob_fame", totalFame,
		"rob_st_exp", totalStExp,
	)
}

// crossTakeBibleRobCD 查询仙界取经打劫冷却。
func crossTakeBibleRobCD(session *client.RoleSession, crossCall crossTakeBibleCallFunc) (*protocol.StTakeBibleCrossRobCD, bool) {
	resp, err := crossCall(protocol.BuildStTakeBibleCrossGetRobCDPacket(), protocol.ActionIDStTakeBibleCrossGetRobCD, crossTakeBibleRobCDTimeout)
	if err != nil {
		slog.Warn("仙界取经打劫冷却查询无应答", "role_id", session.RoleID(), "error", err)
		return nil, false
	}
	cd, err := protocol.ParseStTakeBibleCrossRobCDResult(resp.Payload)
	if err != nil {
		slog.Warn("解析仙界取经打劫冷却失败", "role_id", session.RoleID(), "error", err)
		return nil, false
	}
	return cd, true
}

// crossTakeBibleRobTimes 查询仙界取经今日剩余护送/打劫次数。
func crossTakeBibleRobTimes(session *client.RoleSession, crossCall crossTakeBibleCallFunc) (*protocol.StTakeBibleCrossUpdateData, error) {
	resp, err := crossCall(protocol.BuildStTakeBibleCrossUpdateDataPacket(), protocol.ActionIDStTakeBibleCrossUpdateData, crossTakeBibleRobCDTimeout)
	if err != nil {
		slog.Warn("仙界取经剩余次数查询无应答", "role_id", session.RoleID(), "error", err)
		return nil, err
	}
	update, err := protocol.ParseStTakeBibleCrossUpdateDataResult(resp.Payload)
	if err != nil {
		slog.Warn("解析仙界取经剩余次数失败", "role_id", session.RoleID(), "error", err)
		return nil, err
	}
	return update, nil
}

// crossTakeBibleRobReconcile 在 rob 回包超时后核对本次打劫是否已被服务端受理。
//
// 官方客户端每次打劫后都会重新查询打劫冷却；服务端一旦受理 (无论胜负) 都会
// 设置冷却。因此回包缺失时以冷却状态作为唯一可靠的受理凭证：
//   - accepted=true：冷却已触发，按已结算处理，绝不能重复发起；
//   - accepted=false 且 known=true：冷却仍为 0，确认请求未被受理，可安全尝试下一目标；
//   - known=false：冷却查询本身失败，状态未知，调用方必须保守收手。
func crossTakeBibleRobReconcile(session *client.RoleSession, crossCall crossTakeBibleCallFunc) (accepted, known bool) {
	cd, ok := crossTakeBibleRobCD(session, crossCall)
	if !ok {
		return false, false
	}
	return cd.CDSeconds > 0, true
}

// NewPilgrimageRoutine 构造西天取经护送与打劫任务。
// 可传入 PilgrimageConfig 覆盖默认策略；不传时使用 DefaultPilgrimageConfig。
func NewPilgrimageRoutine(configs ...PilgrimageConfig) scheduler.ActivityRoutine {
	cfg := DefaultPilgrimageConfig()
	if len(configs) > 0 {
		cfg = normalizePilgrimageConfig(configs[0])
	}
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

			callWithTimeout := func(pkt *protocol.Packet, expectedAct uint32, d time.Duration) (*protocol.Packet, error) {
				cCtx, cancel := context.WithTimeout(ctx, d)
				defer cancel()
				return session.Call(cCtx, pkt, expectedAct)
			}

			// 1. 官方仙界跨服取经 (Module 114, Mod_StTakeBible_Base)
			// 取经模块只在 Mod 94 登录的仙界跨服连接上生效，主服网关不会响应。
			if handleCrossPilgrimage(ctx, session, cfg) {
				return nil
			}

			// 2. 传统 Web 协议兜底回退 (Module 30, Mod_TakeBible_Base)
			openPkt := protocol.BuildTakeBibleOpenPacket(session.LastActionID())
			if openResp, err := callWithTimeout(openPkt, protocol.ActionIDTakeBibleOpen, 2*time.Second); err == nil && openResp != nil {
				openRes, _ := protocol.ParseTakeBibleOpenResult(openResp.Payload)
				if openRes != nil && openRes.CanTakeTimes <= 0 {
					slog.Info("西天取经今日护送次数已用尽", "role_id", session.RoleID(), "remain_times", openRes.CanTakeTimes)
					closePkt := protocol.BuildTakeBibleClosePacket(session.LastActionID())
					_, _ = callWithTimeout(closePkt, protocol.ActionIDTakeBibleClose, 1*time.Second)
					return nil
				}
			}

			// 2.2 拉取当前取经准备状态与使者数据 (0x001E0002)
			infoPkt := protocol.BuildTakeBibleGetInfoPacket(session.LastActionID())
			infoResp, err := callWithTimeout(infoPkt, protocol.ActionIDTakeBibleGetInfo, 2*time.Second)
			if err != nil {
				slog.Info("西天取经准备详情无应答，当前角色可能尚未解锁护送权限或该服取经已迁移",
					"role_id", session.RoleID(),
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
				_, _ = callWithTimeout(closePkt, protocol.ActionIDTakeBibleClose, 1*time.Second)
				return nil
			}

			// 检查今日总取经次数是否已达到上限 (每日上限 3 次)
			if infoRes.TotalTakeTimes >= 3 {
				slog.Info("西天取经今日总护送次数已达上限 (3次)", "role_id", session.RoleID(), "total_times", infoRes.TotalTakeTimes)
				closePkt := protocol.BuildTakeBibleClosePacket(prevAct)
				_, _ = callWithTimeout(closePkt, protocol.ActionIDTakeBibleClose, 1*time.Second)
				return nil
			}

			slog.Info("当前取经使者就绪",
				"role_id", session.RoleID(),
				"angel", protocol.AngelName(infoRes.CurrentAngel),
				"protect_player_id", infoRes.ProtectPlayerID,
				"remain_rob_times", infoRes.RobTimes,
			)

			// 2.3 使者品质刷新策略: 若不是最高品质使者 (唐僧)，尝试刷新提升护送收益
			if infoRes.CurrentAngel < protocol.TakeBibleTangSeng {
				refreshPkt := protocol.BuildTakeBibleRefreshPacket(prevAct)
				if refResp, rErr := callWithTimeout(refreshPkt, protocol.ActionIDTakeBibleRefresh, 2*time.Second); rErr == nil && refResp != nil {
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

			// 2.4 正式启程开始护送 (0x001E000A)
			startPkt := protocol.BuildTakeBibleStartPacket(prevAct)
			startResp, err := callWithTimeout(startPkt, protocol.ActionIDTakeBibleStart, 2*time.Second)
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
				slog.Info("启程西天取经返回状态", "role_id", session.RoleID(), "status_code", startCode)
			}

			// 2.5 安全关闭取经主界面 (0x001E0001)
			closePkt := protocol.BuildTakeBibleClosePacket(prevAct)
			_, _ = callWithTimeout(closePkt, protocol.ActionIDTakeBibleClose, 1*time.Second)
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
//
// 交互序列与实机抓包 (haha.pcapng) 对齐:
//  1. Module 282, Action 0 打开淬炼池面板, 回包携带全部铁槽生产状态;
//  2. Module 534, Action 5 查询淬炼池资源面板;
//  3. Module 282, Action 1 以“一键淬炼”方式提炼所有已完成槽位。
func NewSpiritPoolRoutine() scheduler.ActivityRoutine {
	const callTimeout = 3 * time.Second
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
			slog.Info("正在执行资源任务: 粗铁淬炼池一键淬炼", "role_id", session.RoleID())

			// 1. 打开淬炼池面板 (Mod 282, Act 0), 获取各铁槽生产状态
			openPkt := protocol.BuildRefinePoolOpenPacket(session.LastActionID())
			oCtx, oCancel := context.WithTimeout(ctx, callTimeout)
			openResp, err := session.Call(oCtx, openPkt, protocol.ActionIDRefinePoolOpen)
			oCancel()
			if err != nil {
				slog.Warn("打开粗铁淬炼池面板失败", "role_id", session.RoleID(), "err", err)
				return nil
			}
			panel, err := protocol.ParseRefinePoolOpenResponse(openResp.Payload)
			if err != nil {
				slog.Warn("解析粗铁淬炼池面板失败", "role_id", session.RoleID(), "err", err)
				return nil
			}

			// 2. 查询淬炼池资源面板 (Mod 534, Act 5), 与实机抓包序列保持一致
			panelPkt := protocol.BuildRefinePoolPanelPacket(protocol.ActionIDRefinePoolOpen)
			pCtx, pCancel := context.WithTimeout(ctx, callTimeout)
			_, _ = session.Call(pCtx, panelPkt, protocol.ActionIDRefinePoolPanel)
			pCancel()

			done := panel.DoneCount()
			if done == 0 {
				slog.Info("淬炼池暂无已完成的粗铁, 跳过一键淬炼",
					"role_id", session.RoleID(),
					"iron_count", len(panel.Irons),
					"level", panel.Level,
					"lian_qi_jing_hua", panel.LianQiJingHua,
				)
				return nil
			}

			// TODO(spirit_pool): 面板上应有两个「一键淬炼」入口, 分别对应两种产出 (追踪: #1)。
			// 当前实现的是铜钱一键淬炼 (产出玄铁 + 炼器精华), 另一种产出尚未确认,
			// 待后续实机验证后补充对应的消耗类型/产物解析。
			// 3. 一键淬炼所有已完成槽位 (铜钱消耗, 避免扣除元宝)
			execPkt := protocol.BuildRefineExecutePacket(
				protocol.RefinePoolQualityCoins,
				protocol.RefinePoolTypeOneKey,
				uint8(done),
				protocol.ActionIDRefinePoolPanel,
			)
			eCtx, eCancel := context.WithTimeout(ctx, callTimeout)
			resp, err := session.Call(eCtx, execPkt, protocol.ActionIDRefineExecute)
			eCancel()
			if err != nil {
				slog.Warn("粗铁淬炼执行失败", "role_id", session.RoleID(), "done_slots", done, "err", err)
				return nil
			}

			result, err := protocol.ParseRefineExecuteResponse(resp.Payload)
			if err != nil {
				slog.Warn("解析粗铁淬炼回包失败", "role_id", session.RoleID(), "err", err)
				return nil
			}
			var essence int32
			for _, award := range result.Awards {
				for _, item := range award.Items {
					if item.ItemID == protocol.ItemRefineEssence {
						essence += item.Count
					}
				}
				for _, item := range award.ExtraItems {
					if item.ItemID == protocol.ItemRefineEssence {
						essence += item.Count
					}
				}
			}
			// 实测服务端在批量淬炼成功时也可能回传非 SUCCESS 的结果码,
			// 因此以是否实际产出炼器精华作为成功判据, 避免误报失败。
			attrs := []any{
				"role_id", session.RoleID(),
				"result", result.Result,
				"refined_slots", len(result.Awards),
				"essence_gained", essence,
				"lian_qi_jing_hua", result.LianQiJingHua,
				"level", result.Level,
			}
			if result.Result == protocol.RefinePoolResultSuccess || essence > 0 {
				slog.Info("粗铁一键淬炼完成", attrs...)
			} else {
				slog.Warn("粗铁一键淬炼未产出炼器精华", attrs...)
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
