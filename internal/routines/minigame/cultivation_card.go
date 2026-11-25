// 本文件实现神仙卡牌收藏集 (Mod_CultivationCardGame_Base, Module 859)。
//
// 流程：shop_info(7) 仅购买铜钱定价卡包 → card_bag_info(3) → open_card_bag(4)
// 打开我的卡包 → task_info(5) 领取觅寻仙踪已完成任务。
// shop_info 的第二个 Int 不是准确价格（实测购买一次约扣 5,000,000 铜钱），
// 因此每轮只购买「值最小且 >0」的一个档位；只消耗铜钱，不涉及元宝。
package minigame

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

const (
	maxCultivationTaskAward = 30
	maxCultivationOpenBags  = 50
)

// NewCultivationCardRoutine 构造神仙卡牌收藏集日常任务。
func NewCultivationCardRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"cultivation_card",
		"minigame",
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
			caller := kit.NewCaller(ctx, session, callTimeout)
			refreshedOnce := false
			call := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
				resp, err := caller.Call(pkt, act)
				if err != nil {
					slog.Info("神仙卡牌协议无回包", "role_id", session.RoleID(), "action", act, "err", err)
					// 仅当长会话上请求发生静默丢包时按需刷新一次，避免无事可做时平白断连。
					if !refreshedOnce && caller.AbortErr() == nil {
						if since := session.ConnectionSince(); !since.IsZero() && time.Since(since) > client.HealthySessionAge {
							refreshedOnce = true
							if rErr := session.EnsureFreshConnection(ctx, client.HealthySessionAge); rErr == nil {
								if retryResp, retryErr := caller.Call(pkt, act); retryErr == nil {
									return retryResp
								}
							}
						}
					}
					return nil
				}
				return resp
			}

			// 1. 卡包商城：shop_info 只回 [shop_id, left_number]；价格与货币查静态表。
			//    严格只购买铜钱结算 (cost_item_id=1747) 且余额足够的条目，绝不碰元宝档。
			shopResp := call(protocol.BuildCultivationCardShopInfoPacket(), protocol.ActionIDCultivationShopInfo)
			if shopResp == nil {
				if abortErr := caller.AbortErr(); abortErr != nil {
					return abortErr
				}
				return nil
			}
			items, extra, err := protocol.ParseCultivationCardShop(shopResp.Payload)
			if err != nil {
				slog.Warn("神仙卡牌商城解析失败", "role_id", session.RoleID(), "err", err)
			} else if len(items) > 0 {
				slog.Info("神仙卡牌商城", "role_id", session.RoleID(), "items", len(items), "extra", extra)
				coins := session.GetPlayerState().Coins
				var chosen *protocol.CultivationCardShopGood
				for _, it := range items {
					if it.LeftNumber <= 0 {
						continue
					}
					good := protocol.GetCultivationCardShopGood(it.ShopID)
					if good == nil {
						continue
					}
					if good.CostItemID != protocol.CultivationCardCostCoin {
						continue // 元宝/活动积分档位一律跳过
					}
					if coins < int64(good.CostItemNum) {
						continue
					}
					if chosen == nil || good.CostItemNum < chosen.CostItemNum {
						chosen = good
					}
				}
				if chosen == nil {
					slog.Info("神仙卡牌商城无可购买的铜钱档位", "role_id", session.RoleID(), "coins", coins)
				} else {
					buyResp := call(protocol.BuildCultivationCardBuyShopPacket(chosen.ID),
						protocol.ActionIDCultivationBuyShop)
					if buyResp == nil {
						if abortErr := caller.AbortErr(); abortErr != nil {
							return abortErr
						}
					} else {
						code, bErr := protocol.ParseActionFirstByte(buyResp.Payload, "神仙卡牌购买")
						if bErr == nil {
							slog.Info("神仙卡牌商城购买结果", "role_id", session.RoleID(),
								"shop_id", chosen.ID, "cost_coin", chosen.CostItemNum,
								"result", code, "result_text", protocol.CultivationCardResultText(code))
						}
					}
				}
			}

			// 2. 打开我的卡包。
			opened := 0
			bagResp := call(protocol.BuildCultivationCardBagInfoPacket(), protocol.ActionIDCultivationBagInfo)
			if bagResp == nil {
				if abortErr := caller.AbortErr(); abortErr != nil {
					return abortErr
				}
			}
			if bagResp != nil {
				bags, bErr := protocol.ParseCultivationCardBags(bagResp.Payload)
				if bErr != nil {
					slog.Warn("神仙卡牌卡包解析失败", "role_id", session.RoleID(), "err", bErr)
				}
				for _, bag := range bags {
					if ctx.Err() != nil || opened >= maxCultivationOpenBags {
						break
					}
					if bag.Count <= 0 {
						continue
					}
					slog.Info("神仙卡牌卡包", "role_id", session.RoleID(), "item_id", bag.ItemID, "count", bag.Count)
					stop := false
					for i := int32(0); i < bag.Count && opened < maxCultivationOpenBags; i++ {
						openResp := call(protocol.BuildCultivationCardOpenBagPacket(bag.ItemID, 1),
							protocol.ActionIDCultivationOpenBag)
						if openResp == nil {
							if abortErr := caller.AbortErr(); abortErr != nil {
								return abortErr
							}
							return nil
						}
						code, oErr := protocol.ParseActionFirstByte(openResp.Payload, "神仙卡牌开卡包")
						if oErr != nil {
							stop = true
							break
						}
						switch code {
						case protocol.CultivationCardSuccess:
							opened++
						case protocol.CultivationCardItemPackFull:
							return scheduler.ErrBagFull
						case protocol.CultivationCardNotEnoughItem, protocol.CultivationCardBuyLimit:
							stop = true
						default:
							slog.Info("神仙卡牌开卡包未成功", "role_id", session.RoleID(),
								"item_id", bag.ItemID, "result", code,
								"result_text", protocol.CultivationCardResultText(code))
							stop = true
						}
						if stop {
							break
						}
					}
					if stop {
						break
					}
				}
			}

			// 3. 觅寻仙踪任务奖励。
			awarded := 0
			attempts := 0
			taskDrops := 0
			// 2026-11-22 实机探针：859/5 的合法 step 为 1/3/4/5，
			// step=2 不存在且会被服务端静默丢弃；step=5 回包为空列表。
			// 只请求真实存在的 step，避免每次巡检空等 5 秒。
			taskSteps := []int32{1, 3, 4, 5}
			for _, group := range taskSteps {
				if attempts >= maxCultivationTaskAward || ctx.Err() != nil {
					break
				}
				taskResp := call(protocol.BuildCultivationCardTaskInfoPacket(group), protocol.ActionIDCultivationTaskInfo)
				if taskResp == nil {
					if abortErr := caller.AbortErr(); abortErr != nil {
						return abortErr
					}
					taskDrops++
					continue
				}
				_, tasks, tErr := protocol.ParseCultivationCardTasks(taskResp.Payload)
				if tErr != nil {
					slog.Warn("神仙卡牌任务解析失败", "role_id", session.RoleID(), "group", group, "err", tErr)
					continue
				}
				for _, task := range tasks {
					if ctx.Err() != nil || attempts >= maxCultivationTaskAward {
						break
					}
					// 实测 task_info 的第三个字段为完成标志：1=已完成/可领，0=未完成。
					// 之前误按 0 领取，导致大量 result=1/9/11 失败。
					if task.Progress <= 0 || task.Flag != 1 {
						continue
					}
					attempts++
					awardResp := call(protocol.BuildCultivationCardTaskAwardPacket(task.TaskID),
						protocol.ActionIDCultivationTaskAward)
					if awardResp == nil {
						if abortErr := caller.AbortErr(); abortErr != nil {
							return abortErr
						}
						goto done
					}
					code, aErr := protocol.ParseActionFirstByte(awardResp.Payload, "神仙卡牌任务奖励")
					if aErr != nil {
						continue
					}
					switch code {
					case protocol.CultivationCardSuccess:
						awarded++
						slog.Info("神仙卡牌任务奖励领取成功", "role_id", session.RoleID(),
							"task_id", task.TaskID, "progress", task.Progress)
					case protocol.CultivationCardHadGet, protocol.CultivationCardNotComplete:
						// 已领取 / 未达成，继续下一条。
					case protocol.CultivationCardActNotOpen:
						slog.Info("神仙卡牌活动未开放", "role_id", session.RoleID())
						goto done
					case protocol.CultivationCardItemPackFull:
						return scheduler.ErrBagFull
					default:
						slog.Info("神仙卡牌任务奖励未成功", "role_id", session.RoleID(),
							"task_id", task.TaskID, "result", code,
							"result_text", protocol.CultivationCardResultText(code))
					}
				}
			}
			if taskDrops >= len(taskSteps) {
				// 全部任务面板被静默丢弃（2026-11-15 20:05 实测整点批量唤醒时
				// 859/3、859/5 连续无回包）：本轮已无收益，1 小时后再试。
				return scheduler.NewCooldown(time.Hour, "神仙卡牌任务面板全部无回包")
			}
		done:
			slog.Info("神仙卡牌巡检完成", "role_id", session.RoleID(),
				"task_awarded", awarded, "task_drops", taskDrops, "bags_opened", opened)
			return nil
		},
	)
}
