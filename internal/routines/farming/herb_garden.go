// Package farming 提供药园、取经护送/劫镖、仙界矿山、粗铁淬炼池与仙葫芦等资源养成自动化。
//
// 本文件实现药园自动采摘、种子补仓与播种 (Mod_Farm_Base, Module 13)。
package farming

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

// farmSeedBuyBlockedUntil 按角色记录药园种子购买的退避截止时间 (Unix 秒)。
// 不同角色/区服对 buy_new_herbs 的受理情况不同，退避必须按角色隔离，
// 避免 s1 的拒绝把 s0 的正常补仓一起阻塞。
// 购买请求需要药园场景上下文，部分会话下可能被丢弃；退避期间只巡检/采摘/播种。
var (
	farmSeedBuyBlockMu       sync.Mutex
	farmSeedBuyBlockedByRole = make(map[string]int64)
)

// farmCallTimeout 药园单次协议等待上限；测试可缩短以模拟静默丢弃。
var farmCallTimeout = 8 * time.Second

// maxSeedBuyDropProbes 单轮补仓最多尝试的种子数量。
// 第一种静默丢包后换一种种子确认是否仅该种子受限；连续两种都失败才 6 小时退避。
const maxSeedBuyDropProbes = 2

func farmSeedBuyBlockedUntil(roleID string) int64 {
	farmSeedBuyBlockMu.Lock()
	defer farmSeedBuyBlockMu.Unlock()
	return farmSeedBuyBlockedByRole[roleID]
}

func blockFarmSeedBuy(roleID string, until int64) {
	farmSeedBuyBlockMu.Lock()
	defer farmSeedBuyBlockMu.Unlock()
	farmSeedBuyBlockedByRole[roleID] = until
}

// farmLegacyPlantBlockedByRole 旧版摇钱树播种被服务端静默丢弃后的退避。
// 部分账号 13/5 请求恒定无回包（即使 13/8 资格检查 PASS），需要退避，
// 避免每 15 分钟药园巡检都空等 8 秒。
var (
	farmLegacyPlantBlockMu       sync.Mutex
	farmLegacyPlantBlockedByRole = make(map[string]int64)
)

func farmLegacyPlantBlockedUntil(roleID string) int64 {
	farmLegacyPlantBlockMu.Lock()
	defer farmLegacyPlantBlockMu.Unlock()
	return farmLegacyPlantBlockedByRole[roleID]
}

func blockFarmLegacyPlant(roleID string, until int64) {
	farmLegacyPlantBlockMu.Lock()
	defer farmLegacyPlantBlockMu.Unlock()
	farmLegacyPlantBlockedByRole[roleID] = until
}

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
				cCtx, cancel := context.WithTimeout(ctx, farmCallTimeout)
				defer cancel()
				resp, err := session.Call(cCtx, pkt, expectedAct)
				if err != nil {
					slog.Info("药园协议无回包（服务端静默丢弃）",
						"role_id", session.RoleID(),
						"action", fmt.Sprintf("0x%08X", expectedAct),
						"err", err,
					)
				}
				return resp, err
			}
			// abortOnDrop 统一处理服务端静默丢弃：Module 13 的 PrevAct 链是强校验的，
			// 一次无回包之后继续发送依赖请求只会连续空等（线上实测每次 8s，
			// 单块土地可放大到 50s+），因此立即结束本轮等待下次调度。
			abortOnDrop := func(resp *protocol.Packet, err error) bool {
				return err != nil || resp == nil
			}

			// 2. 发送查询土地状态请求 (ActionIDFarmGetInfo 0x000D0000)
			infoPkt, err := protocol.BuildFarmGetInfoPacket(protocol.FarmGetInfoRequest{
				PrevAct: protocol.ActionIDTownEnter,
			})
			if err != nil {
				return fmt.Errorf("farming: 构造药园查询封包失败: %w", err)
			}

			respPkt, err := callWithTimeout(infoPkt, protocol.ActionIDFarmGetInfo)
			if abortOnDrop(respPkt, err) {
				return fmt.Errorf("farming: 查询药园土地状态超时或失败: %w", err)
			}

			infoRes, err := protocol.ParseFarmGetInfoResult(respPkt.Payload)
			if err != nil {
				return fmt.Errorf("farming: 解析药园土地列表失败: %w", err)
			}
			slog.Debug("药园基础土地开垦信息同步完成", "fields_count", len(infoRes.Fields), "hex", hex.EncodeToString(respPkt.Payload))
			if len(infoRes.Fields) == 0 {
				slog.Info("药园功能未开放/当前无土地，跳过", "role_id", session.RoleID())
				return nil
			}

			prevAct := protocol.ActionIDFarmGetInfo

			// 3. 尝试拉取实时作物生长时间与倒计时 (ActionIDFarmGetPlantState 0x000D0015)
			plantStatePkt, pErr := protocol.BuildFarmPlantStatePacket(protocol.FarmPlantStateRequest{
				PrevAct: prevAct,
			})
			if pErr != nil {
				return fmt.Errorf("farming: 构造药园作物状态封包失败: %w", pErr)
			}
			plantResp, cErr := callWithTimeout(plantStatePkt, protocol.ActionIDFarmGetPlantState)
			if abortOnDrop(plantResp, cErr) {
				return nil
			}
			prevAct = protocol.ActionIDFarmGetPlantState
			if plantStateRes, psErr := protocol.ParseFarmPlantStateResult(plantResp.Payload); psErr != nil {
				slog.Warn("解析药园作物状态失败，继续使用基础土地状态",
					"role_id", session.RoleID(), "err", psErr)
			} else if len(plantStateRes.Fields) > 0 {
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

			// 4. 查询种子库存与单价 (ActionIDFarmGetSeeds 0x000D0016)
			// 优先主力种子 (176 经验丹果树 / 169 仙玲珑经验草)，同时允许
			// 任意铜钱计价种子补仓，避免主力种子只剩元宝价时土地荒废。

			// seedOwned 记录种子库存，供后续播种筛选；避免对 0 库存种子
			// 发送必然被服务端静默丢弃的 plant_new_herbs 请求。
			seedOwned := make(map[int32]int32)
			seedsPkt, sErr := protocol.BuildFarmSeedsPacket(protocol.FarmSeedsRequest{
				PrevAct: prevAct,
			})
			if sErr != nil {
				return fmt.Errorf("farming: 构造药园种子查询封包失败: %w", sErr)
			}
			seedsResp, scErr := callWithTimeout(seedsPkt, protocol.ActionIDFarmGetSeeds)
			if seedsResp == nil {
				// 单次无回包可能只是网关瞬断；用最后确认动作作为锚点重试一次。
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(500 * time.Millisecond):
				}
				retryPkt, rErr := protocol.BuildFarmSeedsPacket(protocol.FarmSeedsRequest{
					PrevAct: session.LastAckActionID(),
				})
				if rErr == nil {
					retryPkt.HasPrevAct = false
					seedsResp, scErr = callWithTimeout(retryPkt, protocol.ActionIDFarmGetSeeds)
				}
			}
			if abortOnDrop(seedsResp, scErr) {
				return nil
			}
			prevAct = protocol.ActionIDFarmGetSeeds
			seedsRes, sParseErr := protocol.ParseFarmSeedsResult(seedsResp.Payload)
			if sParseErr != nil {
				slog.Warn("解析药园种子库存失败，跳过补仓",
					"role_id", session.RoleID(), "err", sParseErr)
			} else {
				seedMeta := make(map[int32]protocol.FarmSeedItem, len(seedsRes.Seeds))
				for _, sItem := range seedsRes.Seeds {
					seedOwned[sItem.SeedID] = sItem.OwnedCount
					seedMeta[sItem.SeedID] = sItem
				}
				slog.Info("药园种子库存",
					"role_id", session.RoleID(),
					"seed_176_owned", seedOwned[176],
					"seed_169_owned", seedOwned[169],
					"seed_kinds", len(seedsRes.Seeds),
					"coin_candidates", farmCoinCandidateSummary(seedsRes.Seeds),
				)

				// 无空闲/待采摘土地时服务端会静默丢弃 buy_seed 请求；
				// 先判断是否存在可播种土地 (空闲 1 或成熟待采摘 3，采摘后会变空闲)。
				// 只有已空闲 (State==1) 的土地才需要补仓。成熟待采摘 (State==3)
				// 可能因特殊作物无法采摘而无法腾出土地；若把它当作可播种地，
				// 会出现“买了种子却无地可种”的纯消耗（s0 实测 150 万铜钱买种后 planted=0）。
				hasPlantableLand := false
				for _, f := range infoRes.Fields {
					if f.State == 1 {
						hasPlantableLand = true
						break
					}
				}

				// 确保有可种植的种子：优先主力种子 (176/169)，但绝不因主力种子
				// 含元宝计价就让土地长期空闲——任何铜钱计价种子都可补仓。
				// 购买请求连续无回包后终止本轮并退避，避免 PrevAct 链被污染后
				// 后续播种/采摘全部连续超时。
				boughtSeed := false
				ingotOnlySeeds := 0
				lowBalanceSeeds := 0
				buyDrops := 0
			seedBuy:
				for _, targetSeedID := range farmPurchaseCandidates(seedsRes.Seeds) {
					if blockedUntil := farmSeedBuyBlockedUntil(session.RoleID()); blockedUntil > time.Now().Unix() {
						slog.Info("药园种子购买近期被服务端静默丢弃，退避期间跳过补仓",
							"role_id", session.RoleID(),
							"blocked_until", time.Unix(blockedUntil, 0))
						break
					}
					if !hasPlantableLand {
						slog.Info("药园当前无空闲/待采摘土地，跳过种子补仓", "role_id", session.RoleID())
						break
					}
					meta, ok := seedMeta[targetSeedID]
					if !ok {
						slog.Info("药园种子未在商店列表中，跳过购买",
							"role_id", session.RoleID(), "seed_id", targetSeedID)
						continue
					}
					// 铁律：自动化绝不消耗元宝。新农田种子列表同时给出
					// buy_need_coin 与 buy_need_ingot，只要元宝价格 > 0 就绝不购买。
					if meta.BuyNeedIngot > 0 {
						ingotOnlySeeds++
						slog.Debug("药园种子含元宝计价，跳过购买以免消耗元宝",
							"role_id", session.RoleID(),
							"seed_id", targetSeedID,
							"buy_need_coin", meta.BuyNeedCoin,
							"buy_need_ingot", meta.BuyNeedIngot,
						)
						continue
					}
					if meta.BuyNeedCoin <= 0 || meta.RemainTimes <= 0 {
						slog.Debug("药园种子不可用铜钱购买或今日次数已用尽，跳过",
							"role_id", session.RoleID(),
							"seed_id", targetSeedID,
							"buy_need_coin", meta.BuyNeedCoin,
							"remain_times", meta.RemainTimes,
						)
						continue
					}
					// 保留与种子价格等额的铜钱余额：低余额账号 (s0 实测 190 万)
					// 不应把 79% 的铜钱押在一颗种子上。余额不足时优先使用库存种子。
					if coins := session.GetPlayerState().Coins; !farmCanAffordSeed(coins, meta.BuyNeedCoin) {
						lowBalanceSeeds++
						slog.Debug("药园铜钱余额不足，跳过购买该种子",
							"role_id", session.RoleID(),
							"seed_id", targetSeedID,
							"buy_need_coin", meta.BuyNeedCoin,
							"coins", coins,
						)
						continue
					}
					slog.Info("药园种子价格",
						"role_id", session.RoleID(),
						"seed_id", targetSeedID,
						"buy_need_coin", meta.BuyNeedCoin,
						"buy_need_ingot", meta.BuyNeedIngot,
						"remain_times", meta.RemainTimes,
						"owned", meta.OwnedCount,
					)
					owned := seedOwned[targetSeedID]
					if owned >= 4 {
						continue
					}
					// 铁律：每次运行每种种子最多购买 1 次。服务端首购按列表中的
					// 铜钱价结算，后续购买会切换为元宝价；买完后列表数据即过期，
					// 若继续循环会按旧价格连续扣元宝（线上实测 3 次 ×30 元宝）。
					buyPkt, bErr := protocol.BuildFarmBuySeedPacket(protocol.FarmBuySeedRequest{
						SeedID: targetSeedID,
					})
					if bErr != nil {
						return fmt.Errorf("farming: 构造药园种子购买封包失败: %w", bErr)
					}
					buyResp, bCallErr := callWithTimeout(buyPkt, protocol.ActionIDFarmBuySeed)
					if abortOnDrop(buyResp, bCallErr) {
						// 裸 [Int] 载荷在 s0 实测可购买；部分区服/账号会按种子
						// 静默丢弃。先换下一种铜钱种子确认是否仅该种子受限，
						// 连续两种都无回包才退避 6 小时，避免每 15 分钟空等。
						buyDrops++
						if buyDrops < maxSeedBuyDropProbes {
							slog.Info("药园种子购买无回包，尝试下一种铜钱种子",
								"role_id", session.RoleID(), "seed_id", targetSeedID,
								"drops", buyDrops, "err", bCallErr)
							select {
							case <-ctx.Done():
								return nil
							case <-time.After(time.Second):
							}
							continue seedBuy
						}
						break seedBuy
					}
					buyRes, bParseErr := protocol.ParseFarmBuySeedResult(buyResp.Payload)
					if bParseErr != nil {
						slog.Warn("解析药园种子购买回包失败",
							"role_id", session.RoleID(), "seed_id", targetSeedID, "err", bParseErr)
						continue
					}
					if !buyRes.Success {
						slog.Info("药园种子购买未成功（铜钱不足或已达上限），跳过该种子",
							"role_id", session.RoleID(), "seed_id", targetSeedID)
						continue
					}
					sName := fmt.Sprintf("种子#%d", targetSeedID)
					if dictRepo != nil {
						if item, dErr := dictRepo.GetItem(int(targetSeedID)); dErr == nil && item != nil {
							sName = item.Name
						}
					}
					seedOwned[targetSeedID]++
					slog.Info("药园种子自动购买成功（单次铜钱档）",
						"seed_id", targetSeedID, "seed_name", sName,
						"buy_need_coin", meta.BuyNeedCoin, "buy_need_ingot", meta.BuyNeedIngot)
					// 每次运行最多购买 1 颗：首购后服务端可能把后续购买切换为
					// 元宝计价，旧列表价格不再可信。剩余补仓交给下一轮。
					boughtSeed = true
					break seedBuy
				}
				if !boughtSeed && ingotOnlySeeds > 0 {
					slog.Info("药园所有可购种子均需元宝计价，跳过补仓（不消耗元宝）",
						"role_id", session.RoleID(), "ingot_only_seeds", ingotOnlySeeds)
				}
				if !boughtSeed && lowBalanceSeeds > 0 {
					slog.Info("药园铜钱余额不足，跳过种子补仓（优先使用库存种子）",
						"role_id", session.RoleID(), "low_balance_seeds", lowBalanceSeeds,
						"coins", session.GetPlayerState().Coins)
				}
				if !boughtSeed && buyDrops > 0 {
					// 连续多个种子购买均无回包，判定该区服/账号暂不支持 13/23，
					// 退避 6 小时；空出的土地继续交给旧版摇钱树与库存种子。
					until := time.Now().Add(6 * time.Hour).Unix()
					blockFarmSeedBuy(session.RoleID(), until)
					slog.Info("药园种子购买连续无回包，暂停补仓 6 小时并继续巡检土地",
						"role_id", session.RoleID(), "drops", buyDrops,
						"blocked_until", time.Unix(until, 0))
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
						LandID: field.LandID,
						// 使用最近一次成功往返的动作号。旧实现硬编码 ActionIDFarmPlant，
						// 但本流程中采摘前通常是 Act 21/22，硬编码会直接断链被静默丢弃。
						PrevAct: prevAct,
					})
					if hErr != nil {
						return fmt.Errorf("farming: 构造药园采摘封包失败: %w", hErr)
					}
					hResp, callErr := callWithTimeout(harvestPkt, protocol.ActionIDFarmHarvest)
					if abortOnDrop(hResp, callErr) {
						return nil
					}
					prevAct = protocol.ActionIDFarmHarvest
					if hResult, pErr := protocol.ParseFarmHarvestResult(hResp.Payload); pErr != nil {
						slog.Warn("解析药园采摘回包失败", "role_id", session.RoleID(), "land_id", field.LandID, "err", pErr)
					} else if hResult.Success {
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
					} else {
						slog.Info("药园土地暂不可采摘",
							"role_id", session.RoleID(), "land_id", field.LandID)
					}
				}

				// 2. 若土地空闲可播种 (State == 1，含采摘后地块)
				if field.State == 1 {
					candidates := farmPlantCandidates(seedOwned)

					for _, sID := range candidates {
						if seedOwned[sID] <= 0 {
							continue
						}
						plantPkt, pErr := protocol.BuildFarmPlantPacket(protocol.FarmPlantRequest{
							LandID:       field.LandID,
							SeedOrRoleID: sID,
							PrevAct:      prevAct,
						})
						if pErr != nil {
							return fmt.Errorf("farming: 构造药园播种封包失败: %w", pErr)
						}
						pResp, callErr := callWithTimeout(plantPkt, protocol.ActionIDFarmPlant)
						if abortOnDrop(pResp, callErr) {
							return nil
						}
						prevAct = protocol.ActionIDFarmPlant
						pResult, parseErr := protocol.ParseFarmPlantResult(pResp.Payload)
						if parseErr != nil {
							slog.Warn("解析药园播种回包失败",
								"role_id", session.RoleID(), "land_id", field.LandID,
								"seed_id", sID, "err", parseErr)
							break
						}
						if !pResult.Success {
							slog.Debug("药园播种被服务端拒绝，尝试下一种子",
								"role_id", session.RoleID(), "land_id", field.LandID, "seed_id", sID)
							continue
						}

						plantedCount++
						seedOwned[sID]--
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

						// 3. 紧随原版 pie.exe 交互链: 播种成功后立刻执行采摘 (Act 25)，
						// PrevAct 使用刚才成功发送的播种动作 (0x000D0018)。
						harvestPkt, hErr := protocol.BuildFarmHarvestPacket(protocol.FarmHarvestRequest{
							LandID:  field.LandID,
							PrevAct: prevAct,
						})
						if hErr != nil {
							return fmt.Errorf("farming: 构造药园采摘封包失败: %w", hErr)
						}
						hResp, hCallErr := callWithTimeout(harvestPkt, protocol.ActionIDFarmHarvest)
						if abortOnDrop(hResp, hCallErr) {
							return nil
						}
						prevAct = protocol.ActionIDFarmHarvest
						if hResult, hpErr := protocol.ParseFarmHarvestResult(hResp.Payload); hpErr != nil {
							slog.Warn("解析药园采摘回包失败",
								"role_id", session.RoleID(), "land_id", field.LandID, "err", hpErr)
						} else if hResult.Success {
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
						} else {
							slog.Info("药园播种后暂不可采摘",
								"role_id", session.RoleID(), "land_id", field.LandID)
						}

						break
					}
				} else if field.State == 2 {
					growingCount++
				}
			}

			// 新农场种子商店不可用时（低等级账号 buy_new_herbs 静默丢弃），
			// 回退旧版摇钱树：消耗免费恢复的摇钱树次数，用伙伴在空闲地播种。
			if plantedCount == 0 {
				legacyPlanted, retryAfter := tryLegacyCoinTreePlanting(ctx, session, infoRes.Fields, 8*time.Second)
				if legacyPlanted > 0 {
					plantedCount += legacyPlanted
					slog.Info("旧版摇钱树播种完成",
						"role_id", session.RoleID(), "planted", legacyPlanted)
				} else if retryAfter > 0 {
					slog.Info("摇钱树免费次数恢复中，按恢复时间唤醒",
						"role_id", session.RoleID(), "retry_after", retryAfter.Round(time.Second))
					return scheduler.NewCooldown(retryAfter, "摇钱树免费次数恢复")
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

// farmCanAffordSeed 判断当前铜钱余额是否足以购买种子且保留等额余额。
//
// 余额未知 (<=0，例如尚未同步) 时不拦截，避免误伤正常账号；
// 已知余额时要求 coins >= 2 × price，防止低余额账号被单颗种子掏空。
func farmCanAffordSeed(coins int64, price int32) bool {
	if coins <= 0 {
		return true
	}
	return int64(price)*2 <= coins
}

// farmSeedPreference 药园主力种子优先顺序 (经验丹果树/仙玲珑经验草等)。
var farmSeedPreference = []int32{176, 169, 171, 174, 173, 172, 170}

// farmCoinCandidateSummary 汇总铜钱计价且有剩余购买次数的种子，便于线上诊断
// 「补仓为何选不到种子」。格式: id:price:remain 逗号分隔。
func farmCoinCandidateSummary(seeds []protocol.FarmSeedItem) string {
	parts := make([]string, 0, len(seeds))
	for _, s := range seeds {
		if s.BuyNeedIngot > 0 || s.BuyNeedCoin <= 0 || s.RemainTimes <= 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%d:%d:%d", s.SeedID, s.BuyNeedCoin, s.RemainTimes))
	}
	return strings.Join(parts, ",")
}

// farmPurchaseCandidates 返回可参与补仓判定的种子编号，主力种子优先，
// 其余种子按铜钱价格升序（同价按编号）排列，优先选择最便宜的铜钱档，
// 避免随机买到高价种子。是否可购买由调用方按元宝/铜钱价格判定。
func farmPurchaseCandidates(seeds []protocol.FarmSeedItem) []int32 {
	available := make(map[int32]protocol.FarmSeedItem, len(seeds))
	others := make([]int32, 0, len(seeds))
	for _, s := range seeds {
		if s.SeedID <= 0 {
			continue
		}
		if _, dup := available[s.SeedID]; dup {
			continue
		}
		available[s.SeedID] = s
		preferred := false
		for _, id := range farmSeedPreference {
			if id == s.SeedID {
				preferred = true
				break
			}
		}
		if !preferred {
			others = append(others, s.SeedID)
		}
	}
	sort.Slice(others, func(i, j int) bool {
		left, right := available[others[i]], available[others[j]]
		if left.BuyNeedCoin != right.BuyNeedCoin {
			return left.BuyNeedCoin < right.BuyNeedCoin
		}
		return left.SeedID < right.SeedID
	})

	out := make([]int32, 0, len(seeds))
	for _, id := range farmSeedPreference {
		if _, ok := available[id]; ok {
			out = append(out, id)
		}
	}
	return append(out, others...)
}

// farmPlantCandidates 返回当前有库存的可播种种子，主力种子优先，
// 其余有库存的种子按编号升序追加，避免只能识别固定几种种子。
func farmPlantCandidates(seedOwned map[int32]int32) []int32 {
	others := make([]int32, 0, len(seedOwned))
	preferred := make(map[int32]bool, len(farmSeedPreference))
	for _, id := range farmSeedPreference {
		preferred[id] = true
	}
	for id, owned := range seedOwned {
		if owned <= 0 || preferred[id] {
			continue
		}
		others = append(others, id)
	}
	sort.Slice(others, func(i, j int) bool { return others[i] < others[j] })

	out := make([]int32, 0, len(seedOwned))
	for _, id := range farmSeedPreference {
		if seedOwned[id] > 0 {
			out = append(out, id)
		}
	}
	return append(out, others...)
}

// tryLegacyCoinTreePlanting 旧版摇钱树回退：使用免费恢复的摇钱树次数，
// 与伙伴列表中的角色在空闲土地上播种 (13/5 type=3)。
//
// 返回 planted 为本轮成功播种数；retryAfter > 0 表示当前次数为 0，
// 应在免费次数恢复后再唤醒。
func tryLegacyCoinTreePlanting(
	ctx context.Context,
	session *client.RoleSession,
	fields []protocol.FarmField,
	callTimeout time.Duration,
) (planted int, retryAfter time.Duration) {
	if until := farmLegacyPlantBlockedUntil(session.RoleID()); until > time.Now().Unix() {
		return 0, 0
	}
	empty := make([]int32, 0, len(fields))
	for _, f := range fields {
		if f.State == 1 {
			empty = append(empty, f.LandID)
		}
	}
	if len(empty) == 0 {
		return 0, 0
	}

	caller := kit.NewCaller(ctx, session, callTimeout)

	infoResp := caller.TryCall(protocol.BuildFarmCoinTreeInfoPacket(), protocol.ActionIDFarmCoinTreeInfo)
	if infoResp == nil {
		slog.Debug("摇钱树次数查询无回包，跳过旧版播种", "role_id", session.RoleID(), "err", caller.LastErr())
		return 0, 0
	}
	info, pErr := protocol.ParseFarmCoinTreeInfo(infoResp.Payload)
	if pErr != nil {
		slog.Warn("解析摇钱树次数失败", "role_id", session.RoleID(), "err", pErr)
		return 0, 0
	}
	if info.Count <= 0 {
		if info.RecoverCD > 0 {
			return 0, time.Duration(info.RecoverCD+5) * time.Second
		}
		return 0, 0
	}

	rolesResp := caller.TryCall(protocol.BuildFarmRoleListPacket(), protocol.ActionIDFarmRoleList)
	if rolesResp == nil {
		slog.Debug("旧版伙伴列表无回包，跳过旧版播种", "role_id", session.RoleID(), "err", caller.LastErr())
		return 0, 0
	}
	roles, rErr := protocol.ParseFarmRoleIDs(rolesResp.Payload)
	if rErr != nil || len(roles) == 0 {
		slog.Debug("旧版伙伴列表不可用", "role_id", session.RoleID(), "err", rErr)
		return 0, 0
	}

	// 找一个能种摇钱树的伙伴 (check_role_state == PASS)。
	var chosen int32
	for _, roleID := range roles {
		checkResp := caller.TryCall(protocol.BuildFarmCheckRoleStatePacket(roleID, protocol.FarmHerbTypeCoin),
			protocol.ActionIDFarmCheckRoleState)
		if checkResp == nil {
			if caller.AbortErr() != nil {
				return 0, 0
			}
			continue
		}
		code, cErr := protocol.ParseFarmCheckRoleStateResult(checkResp.Payload)
		if cErr == nil && code == protocol.FarmCheckRoleStatePass {
			chosen = roleID
			break
		}
	}
	if chosen == 0 {
		slog.Info("旧版伙伴均不满足摇钱树播种条件", "role_id", session.RoleID())
		return 0, 0
	}

	remaining := info.Count
	for _, landID := range empty {
		if remaining <= 0 {
			break
		}
		plantResp := caller.TryCall(protocol.BuildFarmPlantHerbsPacket(landID, chosen, protocol.FarmHerbTypeCoin, 5),
			protocol.ActionIDFarmPlantHerbs)
		if plantResp == nil {
			if caller.AbortErr() != nil {
				break
			}
			// 服务端对该账号恒定丢弃 13/5：退避 6 小时，避免每轮药园空等。
			until := time.Now().Add(6 * time.Hour).Unix()
			blockFarmLegacyPlant(session.RoleID(), until)
			slog.Info("旧版摇钱树播种无回包，暂停旧版补种 6 小时",
				"role_id", session.RoleID(), "land_id", landID,
				"blocked_until", time.Unix(until, 0))
			break
		}
		code, cErr := protocol.ParseFarmPlantHerbsResult(plantResp.Payload)
		if cErr != nil {
			break
		}
		if code != protocol.FarmPlantSuccess {
			slog.Info("旧版摇钱树播种未成功",
				"role_id", session.RoleID(), "land_id", landID, "result", code)
			break
		}
		planted++
		remaining--
		slog.Info("旧版摇钱树播种成功",
			"role_id", session.RoleID(), "land_id", landID, "role_id_used", chosen)
	}
	return planted, 0
}
