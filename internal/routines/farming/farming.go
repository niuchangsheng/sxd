// Package farming 提供药园种植、取经护送、仙界矿山与异兽养育等资源产出玩法。
//
// @author Ateng
// @since 2026-10-08
package farming

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// farmSeedBuyBlockedUntil 记录药园种子购买被服务端静默丢弃后的退避截止时间
// (Unix 秒)。购买请求需要药园场景上下文，主服会话下可能被丢弃；退避期间
// 只巡检/采摘/播种，不再重复发送必然超时的请求。
var farmSeedBuyBlockedUntil atomic.Int64

// NewHerbGardenRoutine 构造药园种植与巡检任务。
// 支持无参调用或注入 dictionary.Repository 字典仓储。
func NewHerbGardenRoutine(dictRepos ...dictionary.Repository) scheduler.ActivityRoutine {
	var dictRepo dictionary.Repository
	if len(dictRepos) > 0 {
		dictRepo = dictRepos[0]
	}
	// 每个例程实例独立开始退避周期；生产环境启动时只构造一次。
	farmSeedBuyBlockedUntil.Store(0)

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
				cCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
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
			// 确定待种植的种子/伙伴编号 (默认 176 经验丹果树 / 169 仙玲珑经验草)
			seedID := int32(176) // 0xB0 经验丹果树
			seedName := "经验丹果树"
			if dictRepo != nil {
				if item, dErr := dictRepo.GetItem(int(seedID)); dErr == nil && item != nil {
					seedName = item.Name
				}
			}

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
				)

				// 无空闲/待采摘土地时服务端会静默丢弃 buy_seed 请求；
				// 先判断是否存在可播种土地 (空闲 1 或成熟待采摘 3，采摘后会变空闲)。
				hasPlantableLand := false
				for _, f := range infoRes.Fields {
					if f.State == 1 || f.State == 3 {
						hasPlantableLand = true
						break
					}
				}

				// 确保常用主力种子 (176 经验丹果树 与 169 仙玲珑经验草) 库存充足。
				// 购买请求一旦无回包立即终止本轮，避免 PrevAct 链被污染后
				// 后续播种/采摘全部连续超时。
			seedBuy:
				for _, targetSeedID := range []int32{seedID, 169} {
					if blockedUntil := farmSeedBuyBlockedUntil.Load(); blockedUntil > time.Now().Unix() {
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
						slog.Info("药园种子含元宝计价，跳过购买以免消耗元宝",
							"role_id", session.RoleID(),
							"seed_id", targetSeedID,
							"buy_need_coin", meta.BuyNeedCoin,
							"buy_need_ingot", meta.BuyNeedIngot,
						)
						continue
					}
					if meta.BuyNeedCoin <= 0 || meta.RemainTimes <= 0 {
						slog.Info("药园种子不可用铜钱购买或今日次数已用尽，跳过",
							"role_id", session.RoleID(),
							"seed_id", targetSeedID,
							"buy_need_coin", meta.BuyNeedCoin,
							"remain_times", meta.RemainTimes,
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
						// 裸 [Int] 载荷在 s0 实测可购买；部分区服/账号仍会被
						// 服务端静默丢弃。退避 6 小时并继续巡检土地，
						// 避免每次 15 分钟都空等 8 秒。
						until := time.Now().Add(6 * time.Hour).Unix()
						farmSeedBuyBlockedUntil.Store(until)
						slog.Info("药园种子购买被静默丢弃，暂停补仓 6 小时并继续巡检土地",
							"role_id", session.RoleID(), "seed_id", targetSeedID,
							"blocked_until", time.Unix(until, 0), "err", bCallErr)
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
					sName := seedName
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
					break seedBuy
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
					candidates := []int32{seedID}
					for _, c := range []int32{169, 176, 171, 174, 173, 172, 170} {
						if c != seedID {
							candidates = append(candidates, c)
						}
					}

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

// crossTakeBibleCallFunc 跨服取经协议调用函数签名。
type crossTakeBibleCallFunc func(pkt *protocol.Packet, expectedAct uint32, d time.Duration) (*protocol.Packet, error)

// moduleSaTakeBibleBase 圣域取经模块基址 (Mod_SaTakeBible_Base, Module 395 / 0x018B)。
//
// 圣域取经与跨服取经 (Mod_StTakeBible_Base, Module 114 / 0x0072) 的报文结构完全一致，
// 仅模块号不同；因此可复用同一套请求构造与回包解析，只把 ActionID 的模块段替换为 0x018B。
const moduleSaTakeBibleBase uint32 = 0x018B0000

// saTakeBibleAction 将跨服取经 ActionID 映射为圣域取经 ActionID (低 16 位动作号不变)。
func saTakeBibleAction(stAction uint32) uint32 {
	return moduleSaTakeBibleBase | (stAction & 0xFFFF)
}

// normalizeSaTakeBibleMsg 将圣域取经结果码映射为跨服取经结果码。
//
// 两套协议的报文结构一致，但 Mod_SaTakeBible_Base 的业务结果码整体后移：
// 例如 SUCCESS 在圣域是 11、仙界是 9；FULL_TAKE_BIBLE_TIMES 是 20/18。
// 复用仙界解析器后，需要在业务判断前统一归一化，避免把成功误判为失败。
func normalizeSaTakeBibleMsg(msg uint8) uint8 {
	switch msg {
	case 11:
		return 9 // SUCCESS
	case 12:
		return 10 // INVALID_PLAYER_ID
	case 13:
		return 11 // FULL_PROTECT_TIMES
	case 14:
		return 12 // NOT_ONLINE
	case 15:
		return 13 // TAKE_BIBLE_STATUS
	case 16:
		return 14 // CANCEL_APPLY
	case 17:
		return 15 // PROTECT_STATUS
	case 18:
		return 16 // NOT_ENOUGH_INGOT
	case 19:
		return 17 // REQUEST_VIP
	case 20:
		return 18 // FULL_TAKE_BIBLE_TIMES
	case 21:
		return 19 // REROB
	case 22:
		return 20 // PROTECT_TIME
	case 23:
		return 21 // FULL_ROB_TIMES
	case 24:
		return 22 // HAVE_CD_TIME
	case 25:
		return 23 // BE_ROB_ALL
	case 26:
		return 24 // INVALID_SEQUENCE_ID
	case 40:
		return 38 // FAILED
	default:
		return msg
	}
}

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

// SpiritPoolRefineTarget 粗铁淬炼池一键操作的产出目标。
type SpiritPoolRefineTarget int

const (
	// SpiritPoolRefineXuanTie 淬炼玄铁 (协议 REFINE_1)。
	SpiritPoolRefineXuanTie SpiritPoolRefineTarget = iota + 1
	// SpiritPoolRefineLingCai 提炼灵材 (协议 REFINE_2)。
	SpiritPoolRefineLingCai
)

// SpiritPoolConfig 粗铁淬炼池策略配置。
//
// 无论选择玄铁还是灵材, 均固定使用铜钱消耗, 永不消耗元宝。
type SpiritPoolConfig struct {
	// Target 一键操作的产出目标: 玄铁 (默认) 或灵材。
	Target SpiritPoolRefineTarget
}

// DefaultSpiritPoolConfig 返回默认淬炼池策略: 一键淬炼玄铁。
func DefaultSpiritPoolConfig() SpiritPoolConfig {
	return SpiritPoolConfig{Target: SpiritPoolRefineXuanTie}
}

// ParseSpiritPoolRefineTarget 解析配置中的淬炼目标文案。
// 支持 xuantie / xuan_tie / 玄铁 / iron 与 lingcai / ling_cai / 灵材;
// 空值按默认玄铁处理, 无法识别时返回 false。
func ParseSpiritPoolRefineTarget(raw string) (SpiritPoolRefineTarget, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "xuantie", "xuan_tie", "玄铁", "iron":
		return SpiritPoolRefineXuanTie, true
	case "lingcai", "ling_cai", "灵材":
		return SpiritPoolRefineLingCai, true
	default:
		return 0, false
	}
}

// normalizeSpiritPoolConfig 将淬炼池策略收敛到合法取值。
func normalizeSpiritPoolConfig(cfg SpiritPoolConfig) SpiritPoolConfig {
	if cfg.Target != SpiritPoolRefineXuanTie && cfg.Target != SpiritPoolRefineLingCai {
		cfg.Target = SpiritPoolRefineXuanTie
	}
	return cfg
}

// SaintAreaFallbackMode 圣域取经通道不可用时的回退策略。
type SaintAreaFallbackMode int

const (
	// SaintAreaAuto 自动判定 (默认):
	//   - 圣域功能未开放的角色 (例如尚未解锁圣域的新区角色) 说明取经仍在仙界/主服，
	//     圣域通道不可用时回退仙界跨服/主服链路；
	//   - 圣域功能已开放的角色说明取经已迁入圣域，圣域关闭/维护时跳过本轮，
	//     不再向可能已下线的旧链路发送请求。
	SaintAreaAuto SaintAreaFallbackMode = iota
	// SaintAreaForceOnly 固定圣域：圣域不可用时一律跳过，绝不回退。
	SaintAreaForceOnly
	// SaintAreaAllowFallback 总是允许回退：圣域不可用时回退仙界跨服/主服链路。
	SaintAreaAllowFallback
)

// PilgrimageConfig 西天取经护送与打劫自动化策略配置。
type PilgrimageConfig struct {
	// AutoRob 是否在启程护送后自动打劫取经路上其他玩家队伍。
	AutoRob bool
	// MaxRobAttempts 单轮最多尝试打劫次数 (上限 5)。
	MaxRobAttempts int
	// MaxProbeTargets 单轮最多探测的目标队伍数量 (上限 50)。
	MaxProbeTargets int
	// SaintAreaFallback 圣域取经通道不可用时的回退策略 (默认 SaintAreaAuto)。
	SaintAreaFallback SaintAreaFallbackMode
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

// String 返回圣域回退策略的可读名称，便于日志排查。
func (m SaintAreaFallbackMode) String() string {
	switch m {
	case SaintAreaForceOnly:
		return "force_saint_area"
	case SaintAreaAllowFallback:
		return "allow_fallback"
	default:
		return "auto"
	}
}

// saintAreaFallbackAllowed 报告圣域通道不可用时是否应回退仙界跨服/主服取经。
func (cfg PilgrimageConfig) saintAreaFallbackAllowed(ctx context.Context, session *client.RoleSession) bool {
	switch cfg.SaintAreaFallback {
	case SaintAreaForceOnly:
		return false
	case SaintAreaAllowFallback:
		return true
	}

	// 自动模式: 以圣域功能是否开放判定取经链路归属。
	// 已开放 = 该角色取经已迁入圣域，圣域关闭/维护时等待下一轮，不回退旧链路；
	// 未开放 = 取经仍在仙界跨服或主服，允许回退。
	open, err := session.IsFunctionOpen(ctx, protocol.FunctionIDSaintArea)
	if err != nil {
		// 无法确认功能开放状态时保守跳过本轮，避免向可能已被服务端静默丢弃的
		// 旧链路发送请求；下一轮调度会重新探测。
		slog.Warn("取经: 圣域功能开放状态查询失败, 跳过本轮",
			"role_id", session.RoleID(), "error", err)
		return false
	}
	if !open {
		slog.Info("取经: 圣域功能未开放, 回退仙界跨服/主服取经",
			"role_id", session.RoleID(), "function_id", protocol.FunctionIDSaintArea)
		return true
	}
	slog.Info("取经: 圣域功能已开放但当前未开启, 跳过本轮",
		"role_id", session.RoleID(), "function_id", protocol.FunctionIDSaintArea)
	return false
}

// handleCrossPilgrimage 在官方仙界跨服连接 (Mod 114) 上执行一次取经巡检、启程与打劫。
//
// 官方客户端流程为：Mod 96 取跨服凭据 → Mod 94 登录跨服节点 → Mod 95 进入仙界城镇
// → Mod 114 get_take_bible_info → (可选 refresh) → start_take_bible
// → open_take_bible → get_rob_take_bible_cd_time → rob。
// 返回 true 表示本轮已处理完毕（跨服链路完成，或按圣域回退策略主动跳过）；
// 返回 false 表示当前环境不支持仙界跨服连接（凭据缺失或登录失败），
// 调用方应回退到主服传统取经协议。
func handleCrossPilgrimage(ctx context.Context, session *client.RoleSession, cfg PilgrimageConfig) bool {
	cfg = normalizePilgrimageConfig(cfg)
	// 一次巡检最多包含护送、刷新、路况查询、目标探测与多次打劫，放宽整体超时。
	// rob 回包需等待跨服节点完成战斗计算 (单次预算 15s)，整体预算相应放宽。
	crossCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	var (
		cross  *client.RoleSession
		isSa   bool
		origin string
	)

	// 1. 优先走圣域取经 (Mod_SaTakeBible_Base, Module 395)。
	// 当前区服已将西天取经迁移至圣域，跨服取经入口可能关闭。
	// 注意: 圣域状态查询线上实测 ~0.6s, 原 1 秒超时余量过小, 高负载时会把
	// 已开启的圣域误判为不可用, 从而整轮跳过取经/打劫 (自动/固定圣域模式均会跳过)。
	// 这里放宽到 3 秒 (mock 服务器会立即返回关闭状态, 不影响单测)。
	statusCtx, statusCancel := context.WithTimeout(crossCtx, 3*time.Second)
	statusResp, statusErr := session.Call(statusCtx, protocol.BuildSaintAreaStatusPacket(), protocol.ActionIDSaintAreaStatus)
	statusCancel()
	if statusErr == nil && statusResp != nil {
		if st, pErr := protocol.ParseSaintAreaStatusResult(statusResp.Payload); pErr == nil && st.Status == protocol.SaintAreaStatusOpen {
			// 注意: 不能给 OpenSaintAreaSession 传入带 cancel 的子 context，
			// 该 context 会作为圣域会话的内部生命周期 context，提前 cancel 会立刻关闭连接。
			sa, openErr := session.OpenSaintAreaSession(crossCtx)
			if openErr == nil {
				cross = sa
				isSa = true
				origin = "圣域"
				defer cross.Close()
			} else {
				slog.Warn("圣域会话建立失败，尝试跨服取经入口",
					"role_id", session.RoleID(), "error", openErr)
			}
		}
	}

	// 2. 圣域通道不可用：按回退策略决定跳过本轮还是回退仙界跨服/主服。
	if cross == nil && !cfg.saintAreaFallbackAllowed(crossCtx, session) {
		slog.Info("取经圣域通道不可用，按策略跳过本轮且不回退仙界",
			"role_id", session.RoleID(), "fallback_mode", cfg.SaintAreaFallback)
		return true
	}

	// 3. 回退仙界跨服取经 (Mod 114)，兼容仍开放跨服取经的区服。
	if cross == nil {
		credCtx, credCancel := context.WithTimeout(crossCtx, 2*time.Second)
		cred, err := session.FetchCrossServerCredentials(credCtx)
		credCancel()
		if err != nil {
			slog.Debug("仙界跨服取经不可用，回退传统取经协议", "role_id", session.RoleID(), "error", err)
			return false
		}
		st, err := session.OpenCrossServerSessionWithInfo(crossCtx, cred)
		if err != nil {
			slog.Warn("仙界跨服会话建立失败，回退传统取经协议", "role_id", session.RoleID(), "error", err)
			return false
		}
		cross = st
		origin = "仙界"
		defer cross.Close()
	}

	crossCall := func(pkt *protocol.Packet, expectedAct uint32, d time.Duration) (*protocol.Packet, error) {
		act := expectedAct
		if isSa {
			act = saTakeBibleAction(expectedAct)
			pkt = protocol.NewPacket(act, pkt.Payload)
		}
		cCtx, cCancel := context.WithTimeout(crossCtx, d)
		defer cCancel()
		return cross.Call(cCtx, pkt, act)
	}

	slog.Info("取经链路已建立", "role_id", session.RoleID(), "channel", origin)
	normalizeMsg := func(msg uint8) uint8 { return msg }
	if isSa {
		normalizeMsg = normalizeSaTakeBibleMsg
	}
	crossPilgrimageEscort(ctx, session, crossCall, normalizeMsg)
	if cfg.AutoRob {
		// 路况队伍与战报中的玩家 ID 均为跨服 ID 空间，必须使用跨服会话的身份
		// 判断“自己发起的队伍”与打劫胜负，不能混用主服 PlayerID。
		selfID := cross.PlayerID()
		if selfID <= 0 {
			selfID = session.PlayerID()
		}
		if isSa {
			crossPilgrimageRob(ctx, session, selfID, crossCall, normalizeMsg, cfg, protocol.ParseSaTakeBibleRoadResult)
		} else {
			crossPilgrimageRob(ctx, session, selfID, crossCall, normalizeMsg, cfg)
		}
	} else {
		slog.Debug("取经自动打劫已关闭，仅执行护送巡检", "role_id", session.RoleID())
	}
	return true
}

// crossPilgrimageEscort 在圣域/仙界取经连接上巡检使者并启程护送。
//
// 线上实测圣域取经有 3 个独立队列，可一次连续发起 3 支护送队伍；
// 循环重新拉取准备详情，按剩余总次数最多启程 3 次。
func crossPilgrimageEscort(ctx context.Context, session *client.RoleSession, crossCall crossTakeBibleCallFunc, normalize func(uint8) uint8) {
	const maxStartsPerRun = 3
	for started := 0; started < maxStartsPerRun; started++ {
		infoResp, err := crossCall(protocol.BuildStTakeBibleCrossGetInfoPacket(), protocol.ActionIDStTakeBibleCrossGetInfo, 3*time.Second)
		if err != nil {
			slog.Info("跨服取经详情无应答", "role_id", session.RoleID(), "error", err)
			return
		}
		info, err := protocol.ParseStTakeBibleCrossInfoResult(infoResp.Payload)
		if err != nil {
			slog.Warn("解析跨服取经详情失败", "role_id", session.RoleID(), "error", err)
			return
		}
		slog.Info("跨服取经准备详情",
			"role_id", session.RoleID(),
			"started", started,
			"take_times", info.TakeBibleTimes,
			"total_times", info.TotalTakeBibleTimes,
			"refresh_price", info.RefreshPrice,
			"status", info.TakeBibleStatus,
			"protection", protocol.StTakeBibleCrossProtectionName(info.CanProtection),
			"protect_player", info.ProtectPlayer,
			"protection_options", len(info.ProtectionList),
			"protect_players", len(info.ProtectPlayerList),
		)

		if info.TotalTakeBibleTimes <= 0 || info.TakeBibleTimes >= uint8(info.TotalTakeBibleTimes) {
			slog.Info("跨服取经今日护送次数已用满",
				"role_id", session.RoleID(),
				"started", started,
				"take_times", info.TakeBibleTimes,
				"total_times", info.TotalTakeBibleTimes,
			)
			return
		}

		// 使者非最高品质时尝试刷新一次，仅在本次刷新免费时执行。
		protection := info.CanProtection
		if protection < protocol.StTakeBibleCrossProtectionTangSeng {
			if info.RefreshPrice > 0 {
				slog.Info("取经使者刷新需要元宝，跳过刷新",
					"role_id", session.RoleID(), "refresh_price", info.RefreshPrice)
			} else {
				refreshResp, rErr := crossCall(protocol.BuildStTakeBibleCrossRefreshPacket(), protocol.ActionIDStTakeBibleCrossRefresh, 2*time.Second)
				if rErr == nil && refreshResp != nil {
					msg, newProtection, nextPrice, pErr := protocol.ParseStTakeBibleCrossRefreshResult(refreshResp.Payload)
					if pErr == nil {
						msg = normalize(msg)
					}
					if pErr == nil && msg == protocol.StTakeBibleCrossMsgSuccess {
						slog.Info("跨服取经使者刷新成功",
							"role_id", session.RoleID(),
							"old_protection", protocol.StTakeBibleCrossProtectionName(protection),
							"new_protection", protocol.StTakeBibleCrossProtectionName(newProtection),
							"next_price", nextPrice,
						)
						protection = newProtection
					}
				}
			}
		}

		startResp, err := crossCall(protocol.BuildStTakeBibleCrossStartPacket(), protocol.ActionIDStTakeBibleCrossStart, 3*time.Second)
		if err != nil {
			slog.Info("跨服取经启程无应答", "role_id", session.RoleID(), "error", err)
			return
		}
		startMsg, itemID, err := protocol.ParseStTakeBibleCrossStartResult(startResp.Payload)
		if err != nil {
			slog.Warn("解析跨服取经启程回包失败", "role_id", session.RoleID(), "error", err)
			return
		}
		startMsg = normalize(startMsg)

		switch startMsg {
		case protocol.StTakeBibleCrossMsgSuccess:
			slog.Info("跨服取经队伍已顺利启程护送",
				"role_id", session.RoleID(),
				"queue", started+1,
				"protection", protocol.StTakeBibleCrossProtectionName(protection),
				"item_id", itemID,
			)
			// 继续下一队列，重新拉取详情计算剩余次数。
		case protocol.StTakeBibleCrossMsgTakeBibleStatus:
			slog.Info("跨服取经队伍正在护送途中，当前无可启程队列",
				"role_id", session.RoleID(), "started", started)
			return
		case protocol.StTakeBibleCrossMsgFullTakeBibleTimes:
			slog.Info("跨服取经今日护送次数已用满", "role_id", session.RoleID(), "total_times", info.TotalTakeBibleTimes)
			return
		case protocol.StTakeBibleCrossMsgProtectTime:
			slog.Info("跨服取经当前处于护送保护时间", "role_id", session.RoleID())
			return
		case protocol.StTakeBibleCrossMsgNotEnoughIngot:
			slog.Info("跨服取经刷新元宝不足", "role_id", session.RoleID())
			return
		default:
			slog.Info("跨服取经完成巡检", "role_id", session.RoleID(), "status_code", startMsg)
			return
		}
	}
}

// crossPilgrimageRob 在跨服取经路上执行一轮打劫。
//
// 官方客户端流程：open_take_bible 取得路况队伍列表 → get_rob_take_bible_cd_time
// 确认冷却 → get_player_take_bible 校验目标是否可劫 → rob(player_id, sequence_id)。
// 单次调度的探测与尝试上限由 PilgrimageConfig 控制 (默认 8 支/5 次)；
// 每次打劫后重新查询冷却与剩余次数，冷却中立即收手。
// 本流程仅使用免费次数，不消耗元宝清冷却或购买额外打劫次数。
//
// selfID 为跨服会话玩家 ID：路况队伍、战报胜负位与 stageServer 均使用跨服 ID 空间。
func crossPilgrimageRob(ctx context.Context, session *client.RoleSession, selfID int32, crossCall crossTakeBibleCallFunc, normalize func(uint8) uint8, cfg PilgrimageConfig, roadParsers ...func([]byte) (*protocol.StTakeBibleCrossRoadInfo, error)) {
	parseRoad := protocol.ParseStTakeBibleCrossRoadResult
	if len(roadParsers) > 0 && roadParsers[0] != nil {
		parseRoad = roadParsers[0]
	}
	openResp, err := crossCall(protocol.BuildStTakeBibleCrossOpenPacket(), protocol.ActionIDStTakeBibleCrossOpen, 3*time.Second)
	if err != nil {
		slog.Info("跨服取经路况查询无应答", "role_id", session.RoleID(), "error", err)
		return
	}
	road, err := parseRoad(openResp.Payload)
	if err != nil {
		slog.Warn("解析跨服取经路况失败", "role_id", session.RoleID(), "error", err)
		return
	}
	defer func() {
		if _, cErr := crossCall(protocol.BuildStTakeBibleCrossClosePacket(), protocol.ActionIDStTakeBibleCrossClose, time.Second); cErr != nil {
			slog.Debug("关闭跨服取经路况面板失败", "role_id", session.RoleID(), "error", cErr)
		}
	}()

	slog.Info("跨服取经路况同步完成",
		"role_id", session.RoleID(),
		"road_teams", len(road.Teams),
		"can_rob_times", road.CanRobTimes,
		"last_robs", len(road.LastRobs),
	)
	if road.CanRobTimes <= 0 {
		slog.Info("跨服取经今日打劫次数已用尽", "role_id", session.RoleID())
		return
	}

	cd, ok := crossTakeBibleRobCD(session, crossCall)
	if !ok {
		return
	}
	if cd.CDSeconds > 0 {
		slog.Info("跨服取经当前处于打劫冷却中", "role_id", session.RoleID(), "cooldown_sec", cd.CDSeconds)
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
		slog.Info("跨服取经路上暂无可打劫队伍", "role_id", session.RoleID())
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
			slog.Debug("跨服取经目标详情无应答", "role_id", session.RoleID(), "player_id", team.PlayerID, "error", cErr)
			continue
		}
		info, pErr := protocol.ParseStTakeBibleCrossPlayerInfoResult(infoResp.Payload)
		if pErr != nil {
			slog.Debug("解析跨服取经目标详情失败", "role_id", session.RoleID(), "player_id", team.PlayerID, "error", pErr)
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
		slog.Info("跨服取经目标探测存在无应答",
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
		slog.Info("跨服取经路上暂无可打劫目标", "role_id", session.RoleID(), "candidates", len(candidates))
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
		slog.Info("跨服取经发起打劫",
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
				slog.Info("跨服取经打劫无应答且冷却复核失败，保守结束本轮打劫",
					"role_id", session.RoleID(),
					"target_player_id", target.team.PlayerID,
					"error", cErr,
				)
				break
			}
			if accepted {
				slog.Info("跨服取经打劫无应答但服务端已进入冷却，按已受理结束本轮打劫",
					"role_id", session.RoleID(),
					"target_player_id", target.team.PlayerID,
					"error", cErr,
				)
				break
			}
			slog.Info("跨服取经打劫无应答且服务端未进入冷却，跳过该目标",
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
		if pErr == nil {
			result.Msg = normalize(result.Msg)
		}
		won := false
		switch {
		case pErr != nil:
			slog.Warn("解析跨服取经打劫回包失败", "role_id", session.RoleID(), "target_player_id", target.team.PlayerID, "error", pErr)
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
				slog.Info("跨服取经打劫获胜",
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
				slog.Warn("跨服取经打劫战斗失败，未消耗每日打劫次数",
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
			slog.Info("跨服取经打劫未成功",
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
			slog.Info("跨服取经打劫进入冷却，结束本轮打劫", "role_id", session.RoleID(), "cooldown_sec", cd.CDSeconds)
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
			slog.Info("跨服取经打劫后剩余次数同步",
				"role_id", session.RoleID(),
				"remain_rob_times", update.CanRobTimes,
				"remain_take_times", update.CanTakeBibleTimes,
			)
			if update.CanRobTimes <= 0 {
				slog.Info("跨服取经今日打劫次数已用尽", "role_id", session.RoleID())
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

	slog.Info("跨服取经打劫巡检完成",
		"role_id", session.RoleID(),
		"robbed", robbed,
		"rob_coin", totalCoin,
		"rob_fame", totalFame,
		"rob_st_exp", totalStExp,
	)
}

// crossTakeBibleRobCD 查询跨服取经打劫冷却。
func crossTakeBibleRobCD(session *client.RoleSession, crossCall crossTakeBibleCallFunc) (*protocol.StTakeBibleCrossRobCD, bool) {
	resp, err := crossCall(protocol.BuildStTakeBibleCrossGetRobCDPacket(), protocol.ActionIDStTakeBibleCrossGetRobCD, crossTakeBibleRobCDTimeout)
	if err != nil {
		slog.Info("跨服取经打劫冷却查询无应答", "role_id", session.RoleID(), "error", err)
		return nil, false
	}
	cd, err := protocol.ParseStTakeBibleCrossRobCDResult(resp.Payload)
	if err != nil {
		slog.Warn("解析跨服取经打劫冷却失败", "role_id", session.RoleID(), "error", err)
		return nil, false
	}
	return cd, true
}

// crossTakeBibleRobTimes 查询跨服取经今日剩余护送/打劫次数。
func crossTakeBibleRobTimes(session *client.RoleSession, crossCall crossTakeBibleCallFunc) (*protocol.StTakeBibleCrossUpdateData, error) {
	resp, err := crossCall(protocol.BuildStTakeBibleCrossUpdateDataPacket(), protocol.ActionIDStTakeBibleCrossUpdateData, crossTakeBibleRobCDTimeout)
	if err != nil {
		slog.Info("跨服取经剩余次数查询无应答", "role_id", session.RoleID(), "error", err)
		return nil, err
	}
	update, err := protocol.ParseStTakeBibleCrossUpdateDataResult(resp.Payload)
	if err != nil {
		slog.Warn("解析跨服取经剩余次数失败", "role_id", session.RoleID(), "error", err)
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
// NewCrystalMineRoutine 构造仙界矿山收益领取任务 (Module 264, Mod_StMine_Base)。
//
// 先查询矿山状态；未开放/超级城镇关闭时跳过。开放时进入当前矿脉、
// 领取收益并离开。不购买抢夺次数、不清除冷却、不延长保护时间。
func NewCrystalMineRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"crystal_mine",
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

			queryInfo := func() (*protocol.StMineInfo, error) {
				iCtx, iCancel := context.WithTimeout(ctx, callTimeout)
				infoResp, qErr := session.Call(iCtx, protocol.BuildStMineInfoPacket(), protocol.ActionIDStMineInfo)
				iCancel()
				if qErr != nil || infoResp == nil {
					return nil, qErr
				}
				return protocol.ParseStMineInfoResult(infoResp.Payload)
			}
			info, err := queryInfo()
			if err != nil {
				slog.Info("仙界矿山不可用，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(30*time.Minute, "仙界矿山信息无回包")
			}
			slog.Info("仙界矿山状态",
				"role_id", session.RoleID(),
				"result", info.Result,
				"level", info.Level,
				"cur_mountain", info.CurMountain,
			)
			if info.Result == protocol.StMineResultResting {
				// 休息中表示角色仍停留在矿脉/矿洞内。官方客户端关闭
				// StMineHolesView 时会发送 leave_mine_mountain 退出休息状态，
				// 然后重新打开矿山面板；这里照做后再重新查询一次。
				slog.Info("仙界矿山休息中，先退出休息状态", "role_id", session.RoleID())
				lCtx, lCancel := context.WithTimeout(ctx, callTimeout)
				leaveResp, lErr := session.Call(lCtx, protocol.BuildStMineLeavePacket(), protocol.ActionIDStMineLeave)
				lCancel()
				if lErr != nil || leaveResp == nil {
					slog.Info("退出矿山休息状态无回包，稍后重试", "role_id", session.RoleID(), "err", lErr)
					return scheduler.NewCooldown(10*time.Minute, "退出矿山休息状态无回包")
				}
				if leaveCode, pErr := protocol.ParseStMineActionResult(leaveResp.Payload); pErr != nil {
					slog.Warn("解析退出矿山回包失败，稍后重试", "role_id", session.RoleID(), "err", pErr)
					return scheduler.NewCooldown(10*time.Minute, "退出矿山回包解析失败")
				} else if leaveCode != protocol.StMineResultSuccess {
					slog.Info("退出矿山休息状态未成功", "role_id", session.RoleID(), "result", leaveCode)
					return scheduler.NewCooldown(10*time.Minute, "退出矿山休息状态未成功")
				}
				info, err = queryInfo()
				if err != nil {
					return scheduler.NewCooldown(30*time.Minute, "退出休息后矿山信息无回包")
				}
				slog.Info("退出休息状态后重新查询矿山",
					"role_id", session.RoleID(), "result", info.Result,
					"level", info.Level, "cur_mountain", info.CurMountain)
			}
			switch info.Result {
			case protocol.StMineResultSuccess:
				// 继续
			case protocol.StMineResultFunctionUnopen, protocol.StMineResultSuperTownClosed:
				slog.Info("仙界矿山未开放，稍后重试", "role_id", session.RoleID(), "result", info.Result)
				return scheduler.NewCooldown(2*time.Hour, "仙界矿山未开放")
			default:
				slog.Info("仙界矿山暂不可进入，稍后重试", "role_id", session.RoleID(), "result", info.Result)
				return scheduler.NewCooldown(time.Hour, "仙界矿山暂不可进入")
			}

			// 进入当前矿脉
			eCtx, eCancel := context.WithTimeout(ctx, 8*time.Second)
			enterResp, err := session.Call(eCtx, protocol.BuildStMineEnterPacket(info.CurMountain), protocol.ActionIDStMineEnter)
			eCancel()
			if err != nil {
				return fmt.Errorf("进入仙界矿山失败: %w", err)
			}
			enterResult, err := protocol.ParseStMineActionResult(enterResp.Payload)
			if err != nil {
				return fmt.Errorf("解析进入矿山回包失败: %w", err)
			}
			if enterResult != protocol.StMineResultSuccess {
				slog.Info("进入仙界矿山未成功", "role_id", session.RoleID(), "result", enterResult)
				return nil
			}
			defer func() {
				lCtx, lCancel := context.WithTimeout(ctx, callTimeout)
				defer lCancel()
				_, _ = session.Call(lCtx, protocol.BuildStMineLeavePacket(), protocol.ActionIDStMineLeave)
			}()

			// 领取收益
			rCtx, rCancel := context.WithTimeout(ctx, callTimeout)
			rewardResp, err := session.Call(rCtx, protocol.BuildStMineReceiveAwardPacket(), protocol.ActionIDStMineReceiveAward)
			rCancel()
			if err != nil {
				slog.Info("矿山收益领取无应答", "role_id", session.RoleID(), "err", err)
				return nil
			}
			rewardResult, err := protocol.ParseStMineActionResult(rewardResp.Payload)
			if err != nil {
				return fmt.Errorf("解析矿山收益回包失败: %w", err)
			}
			if rewardResult == protocol.StMineResultSuccess {
				slog.Info("仙界矿山收益领取成功", "role_id", session.RoleID())
			} else {
				slog.Info("仙界矿山收益未领取", "role_id", session.RoleID(), "result", rewardResult)
			}
			return nil
		},
	)
}

// NewSpiritPoolRoutine 构造粗铁淬炼池与异兽提炼任务 (Feature 5: 淬炼粗铁)。
//
// 交互序列与实机抓包 (haha.pcapng) 对齐:
//  1. Module 282, Action 0 打开淬炼池面板, 回包携带全部铁槽生产状态;
//  2. Module 534, Action 5 查询淬炼池资源面板;
//  3. Module 282, Action 1 一键执行淬炼 (玄铁) 或提炼 (灵材)。
//
// 请求第 1 字节为产出目标 (5 玄铁 / 6 灵材), 第 2 字节为消耗类型 (固定铜钱 7),
// 第 3 字节为操作方式 (一键 10); 服务端自动处理全部已完成槽位, 请求不携带槽位数量。
// 协议基准为原版 Mod_RefinePool_Base 反编译常量, 追踪 issue #1。
// spiritPoolTarget 返回淬炼目标与名称。
func spiritPoolTarget(cfg SpiritPoolConfig) (uint8, string) {
	if cfg.Target == SpiritPoolRefineLingCai {
		return protocol.RefinePoolTargetLingCai, "灵材"
	}
	return protocol.RefinePoolTargetXuanTie, "玄铁"
}

// spiritPoolLoginAttempted 标记本进程内已在登录后执行过淬炼池操作。
var spiritPoolLoginAttempted sync.Map

// RunSpiritPoolOnLogin 在会话激活后、调度器启动前执行一次淬炼池一键操作。
//
// 服务端对 282/1 的 PrevAct 上下文敏感：登录后第一时间执行可稳定成功，
// 长会话中再执行可能被静默丢弃（2026-11-11 实测）。
func RunSpiritPoolOnLogin(ctx context.Context, session *client.RoleSession, cfg SpiritPoolConfig) {
	if session == nil {
		return
	}
	deadline := time.Now().Add(30 * time.Second)
	for session.State() != client.StateActive && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	if session.State() != client.StateActive {
		return
	}
	if _, loaded := spiritPoolLoginAttempted.LoadOrStore(session, true); loaded {
		return
	}
	cfg = normalizeSpiritPoolConfig(cfg)
	target, targetName := spiritPoolTarget(cfg)
	slog.Info("登录后粗铁淬炼池一键操作", "role_id", session.RoleID(), "target", targetName)
	if err := runSpiritPoolCore(ctx, session, cfg, target, targetName); err != nil {
		slog.Info("登录后粗铁淬炼池操作未完成", "role_id", session.RoleID(), "err", err)
	}
}

// NewSpiritPoolRoutine 构造粗铁淬炼池任务。
func NewSpiritPoolRoutine(cfgs ...SpiritPoolConfig) scheduler.ActivityRoutine {
	cfg := DefaultSpiritPoolConfig()
	if len(cfgs) > 0 {
		cfg = normalizeSpiritPoolConfig(cfgs[0])
	}
	target, targetName := spiritPoolTarget(cfg)
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
			slog.Info("正在执行资源任务: 粗铁淬炼池一键操作",
				"role_id", session.RoleID(),
				"target", targetName,
			)
			return runSpiritPoolCore(ctx, session, cfg, target, targetName)
		},
	)
}

// runSpiritPoolCore 刷新 PrevAct 链、读取面板并对已完成槽位执行一键淬炼。
func runSpiritPoolCore(
	ctx context.Context,
	session *client.RoleSession,
	cfg SpiritPoolConfig,
	target uint8,
	targetName string,
) error {
	const callTimeout = 8 * time.Second
	// 一键淬炼是一次性结算全部已完成槽位的重操作（线上 done_slots=10 时
	// 8s 内未能返回），给结算请求更长的等待窗口，避免把慢回包误判为丢包。
	const executeTimeout = 20 * time.Second

	// 1. 不再额外调用功能开放检查：check_function_open 会推进 PrevAct 链，
	// 导致后续 282/1 一键淬炼被网关静默丢弃。282/0 面板本身就是开放门禁。
	// 2. 打开淬炼池面板 (Mod 282, Act 0), 获取各铁槽生产状态。
	//    先用 0/2 全量资产刷新 PrevAct 链锚点：长会话中被静默丢弃的请求会
	//    污染本地 lastActionID，导致 282/1 一键淬炼被服务端静默丢弃
	//    （2026-11-11 与 852/16 同源问题）。
	anchor := session.LastAckActionID()
	if anchor == 0 {
		anchor = session.LastActionID()
	}
	syncCtx, syncCancel := context.WithTimeout(ctx, callTimeout)
	syncResp, syncErr := session.Call(syncCtx, protocol.BuildPlayerGetInfoPacket(int32(anchor)),
		protocol.ActionIDPlayerGetInfo)
	syncCancel()
	if syncErr != nil || syncResp == nil {
		slog.Debug("淬炼池 PrevAct 链刷新无回包，沿用本地锚点",
			"role_id", session.RoleID(), "err", syncErr)
	} else if ack := session.LastAckActionID(); ack != 0 {
		anchor = ack
	}
	openPkt := protocol.BuildRefinePoolOpenPacket(anchor)
	oCtx, oCancel := context.WithTimeout(ctx, callTimeout)
	openResp, err := session.Call(oCtx, openPkt, protocol.ActionIDRefinePoolOpen)
	oCancel()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			slog.Info("打开粗铁淬炼池面板无回包（可能为网关瞬断），稍后重试",
				"role_id", session.RoleID(),
				"target", targetName,
			)
			return scheduler.NewCooldown(10*time.Minute, "粗铁淬炼池面板无回包")
		} else {
			slog.Warn("打开粗铁淬炼池面板失败", "role_id", session.RoleID(), "err", err)
		}
		return nil
	}
	panel, err := protocol.ParseRefinePoolOpenResponse(openResp.Payload)
	if err != nil {
		slog.Warn("解析粗铁淬炼池面板失败", "role_id", session.RoleID(), "err", err)
		return nil
	}
	// 诊断：输出每个槽位的生产状态，定位 DoneCount=0 的根因。
	{
		statusCounts := make(map[uint8]int, 5)
		for _, iron := range panel.Irons {
			statusCounts[iron.ProduceStatus]++
		}
		slog.Info("粗铁淬炼池面板状态",
			"role_id", session.RoleID(),
			"irons", len(panel.Irons),
			"status_counts", statusCounts,
			"level", panel.Level,
			"lian_qi_jing_hua", panel.LianQiJingHua,
			"coins_need", panel.CoinsNeed,
			"one_key_lock_flag", panel.OneKeyLockFlag,
		)
	}

	// 3. 线上实测（2026-11-07 22:00，s0-niuchangsheng）：534 资源面板
	// 在本服可被省略，且该请求经常被静默丢弃。open_panel 之后直接发送
	// 一键淬炼 (282/1, PrevAct=282/0) 可正常返回 result=11 SUCCESS，
	// 因此不再把 534 作为必选链路。保留 protocol 层构造器供探针使用。
	done := panel.DoneCount()
	if done == 0 {
		statusCounts := make(map[uint8]int, 5)
		type ironInfo struct {
			Seq     int8
			Status  uint8
			Need    int32
			Remain  int32
			Quality int8
		}
		irons := make([]ironInfo, 0, len(panel.Irons))
		for _, iron := range panel.Irons {
			statusCounts[iron.ProduceStatus]++
			irons = append(irons, ironInfo{
				Seq: iron.Seq, Status: iron.ProduceStatus,
				Need: iron.ProduceNeedTime, Remain: iron.ProduceRemainTime,
				Quality: iron.Quality,
			})
		}
		slog.Info("淬炼池暂无已完成的粗铁, 跳过一键操作",
			"role_id", session.RoleID(),
			"target", targetName,
			"iron_count", len(panel.Irons),
			"status_counts", statusCounts,
			"irons", irons,
			"level", panel.Level,
			"lian_qi_jing_hua", panel.LianQiJingHua,
		)
		return nil
	}

	// 4. 一键操作所有已完成槽位 (固定铜钱消耗, 永不扣除元宝)
	execPkt := protocol.BuildRefineExecutePacket(
		target,
		protocol.RefinePoolCostCoins,
		protocol.RefinePoolTypeOneKey,
		protocol.ActionIDRefinePoolOpen,
	)
	eCtx, eCancel := context.WithTimeout(ctx, executeTimeout)
	resp, err := session.Call(eCtx, execPkt, protocol.ActionIDRefineExecute)
	eCancel()
	if err != nil {
		slog.Warn("粗铁淬炼执行无回包，稍后重试",
			"role_id", session.RoleID(), "target", targetName, "done_slots", done, "err", err)
		return scheduler.NewCooldown(10*time.Minute, "粗铁淬炼执行无回包")
	}

	result, err := protocol.ParseRefineExecuteResponse(resp.Payload)
	if err != nil {
		slog.Warn("解析粗铁淬炼回包失败", "role_id", session.RoleID(), "err", err)
		return nil
	}
	var essence, xuantie, lingcai, awardItems int32
	for _, award := range result.Awards {
		for _, item := range award.Items {
			awardItems += item.Count
			switch item.ItemID {
			case protocol.ItemRefineEssence:
				essence += item.Count
			case protocol.ItemXuanTie:
				xuantie += item.Count
			}
		}
		for _, item := range award.LingCaiItems {
			lingcai += item.Count
		}
	}
	attrs := []any{
		"role_id", session.RoleID(),
		"target", targetName,
		"result", result.Result,
		"refined_slots", len(result.Awards),
		"xuantie_gained", xuantie,
		"essence_gained", essence,
		"lingcai_gained", lingcai,
		"lian_qi_jing_hua", result.LianQiJingHua,
		"level", result.Level,
	}

	switch result.Result {
	case protocol.RefinePoolResultSuccess:
		produced := awardItems > 0 || lingcai > 0 || result.XuanTie > panel.XuanTie
		if cfg.Target == SpiritPoolRefineXuanTie {
			produced = xuantie > 0 || essence > 0 || result.XuanTie > panel.XuanTie
		}
		if produced {
			slog.Info("粗铁一键淬炼完成", attrs...)
			return nil
		}
		slog.Warn("粗铁一键淬炼返回成功码但未产出目标奖励", attrs...)
		return fmt.Errorf("粗铁%s成功码无产出", targetName)
	case protocol.RefinePoolResultLackOfCoins:
		slog.Warn("粗铁淬炼铜钱不足, 本轮跳过", attrs...)
		return scheduler.ErrConditionNotMet
	case protocol.RefinePoolResultLackOfIngot:
		// 本任务固定使用铜钱, 出现元宝不足通常意味着协议字段错位。
		slog.Warn("粗铁淬炼返回元宝不足, 疑似协议字段错位", attrs...)
		return scheduler.ErrConditionNotMet
	case protocol.RefinePoolResultFailed:
		slog.Warn("粗铁淬炼失败", attrs...)
		return fmt.Errorf("粗铁%s失败 (result=%d)", targetName, result.Result)
	default:
		slog.Warn("粗铁淬炼返回未知结果码", attrs...)
		return fmt.Errorf("粗铁%s返回未知结果码 %d", targetName, result.Result)
	}
	return nil
}

// NewGourdRealmRoutine 构造壶中界炼丹与灵气合成任务。
// NewGourdRealmRoutine 构造仙葫芦收益领取任务 (Module 220, Mod_FairyGourd_Base)。
//
// 先读取活动时间窗，再逐日领取已累积的铜钱/经验收益；
// 活动未开始/已结束或无可领日期时直接跳过。全程不消耗元宝。
func NewGourdRealmRoutine(dictRepo dictionary.Repository) scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"gourd_realm",
		"farming",
		scheduler.ScheduleLoop,
		scheduler.PriorityNormal,
		30*time.Minute,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			_ = dictRepo // 保留构造签名，当前领取流程不依赖静态字典

			// 1. 活动时间窗检测
			dCtx, dCancel := context.WithTimeout(ctx, callTimeout)
			dateResp, err := session.Call(dCtx, protocol.BuildFairyGourdDatePacket(), protocol.ActionIDFairyGourdDate)
			dCancel()
			if err != nil {
				slog.Info("仙葫芦功能不可用，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(30*time.Minute, "仙葫芦活动时间无回包")
			}
			date, err := protocol.ParseFairyGourdDateResult(dateResp.Payload)
			if err != nil {
				return fmt.Errorf("解析仙葫芦活动时间失败: %w", err)
			}
			now := time.Now().Unix()
			if date.StartTime > 0 && now < int64(date.StartTime) {
				slog.Info("仙葫芦活动尚未开始，等待开启", "role_id", session.RoleID(), "start_time", date.StartTime)
				return scheduler.NewCooldownUntil(
					time.Unix(int64(date.StartTime), 0).Add(2*time.Second), "仙葫芦活动尚未开始")
			}
			if date.EndTime > 0 && now > int64(date.EndTime) {
				slog.Info("仙葫芦活动已结束，跳过", "role_id", session.RoleID(), "end_time", date.EndTime)
				return scheduler.ErrAttemptsExhausted
			}

			// 2. 收益列表
			lCtx, lCancel := context.WithTimeout(ctx, callTimeout)
			listResp, err := session.Call(lCtx, protocol.BuildFairyGourdListPacket(), protocol.ActionIDFairyGourdList)
			lCancel()
			if err != nil {
				return fmt.Errorf("查询仙葫芦收益列表失败: %w", err)
			}
			entries, err := protocol.ParseFairyGourdListResult(listResp.Payload)
			if err != nil {
				return fmt.Errorf("解析仙葫芦收益列表失败: %w", err)
			}
			if len(entries) == 0 {
				slog.Info("仙葫芦暂无可领取收益", "role_id", session.RoleID())
				return nil
			}

			// 3. 逐日领取
			claimed := 0
			for _, entry := range entries {
				cCtx, cCancel := context.WithTimeout(ctx, callTimeout)
				resp, cErr := session.Call(cCtx, protocol.BuildFairyGourdClaimPacket(entry.Date), protocol.ActionIDFairyGourdClaim)
				cCancel()
				if cErr != nil {
					slog.Info("仙葫芦领取无应答", "role_id", session.RoleID(), "date", entry.Date, "err", cErr)
					break
				}
				result, pErr := protocol.ParseFairyGourdClaimResult(resp.Payload)
				if pErr != nil {
					slog.Warn("解析仙葫芦领取回包失败", "role_id", session.RoleID(), "err", pErr)
					break
				}
				switch result {
				case protocol.FairyGourdResultSuccess:
					claimed++
					slog.Info("仙葫芦收益领取成功",
						"role_id", session.RoleID(),
						"date", entry.Date,
						"coin", entry.Coin,
						"exp", entry.Exp,
					)
				case protocol.FairyGourdResultAlreadyGet:
					continue
				case protocol.FairyGourdResultNoGrid:
					return scheduler.ErrBagFull
				case protocol.FairyGourdResultTimeError:
					slog.Info("仙葫芦领取时间不符，结束本轮", "role_id", session.RoleID(), "date", entry.Date)
					return nil
				default:
					slog.Warn("仙葫芦领取失败", "role_id", session.RoleID(), "date", entry.Date, "result", result)
					return nil
				}
			}
			slog.Info("仙葫芦收益领取完成", "role_id", session.RoleID(), "claimed", claimed)
			return nil
		},
	)
}

// NewRuneRefineRoutine 构造符文提炼与八卦炼丹任务。
// NewRuneRefineRoutine 构造符文批量炼化任务 (Module 16, Mod_Rune_Base)。
//
// 先查询批量炼化信息；字段语义依赖客户端资源 SWF，当前只做数据检测，
// 语义对齐前不执行 batch_rune_use，避免误消耗符文/元宝。
func NewRuneRefineRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"rune_refine",
		"farming",
		scheduler.ScheduleLoop,
		scheduler.PriorityLow,
		45*time.Minute,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			cCtx, cCancel := context.WithTimeout(ctx, callTimeout)
			resp, err := session.Call(cCtx, protocol.BuildRuneBatchListPacket(), protocol.ActionIDRuneBatchList)
			cCancel()
			if err != nil {
				slog.Info("符文炼化功能不可用，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			info, err := protocol.ParseRuneBatchListResult(resp.Payload)
			if err != nil {
				return fmt.Errorf("解析符文批量信息失败: %w", err)
			}
			slog.Info("符文批量炼化信息（语义待资源 SWF 对齐，暂不执行）",
				"role_id", session.RoleID(),
				"field1", info.Field1,
				"field2", info.Field2,
				"field3", info.Field3,
				"field4", info.Field4,
			)
			return nil
		},
	)
}
