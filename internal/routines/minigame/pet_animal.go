// Package minigame 提供吉星高照、仙旅奇缘答题、喜从天降、刮刮卡、钓鱼等益智趣味活动。
//
// 本文件实现「叶公好龙」(Mod_PetAnimal_Base, Module 48) 普通培养自动化：
// 读取今日剩余培养次数后，仅使用 feed_type=0 的铜钱普通培养，次数耗尽即停止。
// 元宝培养 (feed_type=1)、购买培养次数与升阶均不在自动化范围内。
package minigame

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

const (
	// maxPetAnimalFeeds 单轮最大培养次数，防止服务端异常时死循环。
	// 暴击培养不消耗每日次数，需允许超过每日 20 次的尝试才能把次数耗完。
	maxPetAnimalFeeds = 100
	// maxPetAnimalStaleFeeds 连续多少次回包/面板次数都不递减后判定异常熔断。
	maxPetAnimalStaleFeeds = 20
	// petAnimalCallTimeout 单次协议请求超时。
	petAnimalCallTimeout = 8 * time.Second
	// petAnimalPriority 叶公好龙调度优先级。
	//
	// 2026-11-07 线上实测：Module 48 在角色经过副本/农场等场景切换后再查询会
	// 被服务端静默丢弃 (8s 超时)，而在登录后较早的主服城镇会话中可正常返回面板。
	// 因此将其排在孩子日常 (120) 之前、天帝宝库 (200) 之后尽早执行。
	petAnimalPriority scheduler.RoutinePriority = 130
)

// NewPetAnimalRoutine 构造叶公好龙普通培养任务。
//
// 官方客户端流程 (com.views.PetView / com.assist.view.info.PetLongInfo)：
//  1. pet_animal_info 读取 lv / star / exp / feed_num；
//  2. 点击「普通培养」调用 feed_pet_animal(0)，消耗铜钱；
//  3. 每次成功后刷新 feed_num，直至为 0；
//  4. lv/star 达到满级后不再培养。
//
// 自动化永不调用 feed_pet_animal(1) (元宝培养)，也不调用任何购买次数接口。
func NewPetAnimalRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"pet_animal",
		"minigame",
		scheduler.ScheduleCron,
		petAnimalPriority,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			if session.State() != client.StateActive {
				return nil
			}
			slog.Info("正在执行益智任务: 叶公好龙普通培养", "role_id", session.RoleID())
			if session.PlayerID() == 0 {
				slog.Warn("叶公好龙缺少角色 player_id，跳过", "role_id", session.RoleID())
				return nil
			}

			open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDPetAnimal)
			if fErr != nil {
				slog.Info("叶公好龙功能开放状态查询失败，跳过", "role_id", session.RoleID(), "err", fErr)
				return nil
			}
			if !open {
				slog.Info("叶公好龙功能未开放，跳过", "role_id", session.RoleID(), "function_id", protocol.FunctionIDPetAnimal)
				return nil
			}

			call := func(pkt *protocol.Packet, actionID uint32) *protocol.Packet {
				cCtx, cancel := context.WithTimeout(ctx, petAnimalCallTimeout)
				defer cancel()
				resp, err := session.Call(cCtx, pkt, actionID)
				if err != nil {
					slog.Info("叶公好龙协议无回包，跳过本轮",
						"role_id", session.RoleID(), "action", actionID, "err", err)
					return nil
				}
				return resp
			}

			// tryInfoOnce 发送一次面板请求，不打印重复的无回包日志。
			tryInfoOnce := func(pkt *protocol.Packet) *protocol.Packet {
				cCtx, cancel := context.WithTimeout(ctx, petAnimalCallTimeout)
				defer cancel()
				resp, err := session.Call(cCtx, pkt, protocol.ActionIDPetAnimalInfo)
				if err != nil {
					return nil
				}
				return resp
			}
			fetchInfo := func() *protocol.PetAnimalInfo {
				playerID := session.PlayerID()
				resp := tryInfoOnce(protocol.BuildPetAnimalInfoPacket(playerID, session.LastActionID()))
				if resp == nil {
					// Module 48 对会话上下文敏感：先用功能开放查询重置动作链，
					// 再重试带 PrevAct 请求，最后退回无 PrevAct 裸载荷。
					if _, err := session.CheckFunctionOpen(ctx, protocol.FunctionIDPetAnimal); err == nil {
						resp = tryInfoOnce(protocol.BuildPetAnimalInfoPacket(playerID, session.LastActionID()))
					}
				}
				if resp == nil {
					resp = tryInfoOnce(protocol.BuildPetAnimalInfoPacketBare(playerID))
				}
				if resp == nil {
					slog.Info("叶公好龙面板多次无回包，稍后重试", "role_id", session.RoleID())
					return nil
				}
				info, err := protocol.ParsePetAnimalInfoResult(resp.Payload)
				if err != nil {
					slog.Warn("解析叶公好龙面板失败", "role_id", session.RoleID(), "err", err)
					return nil
				}
				return info
			}

			info := fetchInfo()
			if info == nil {
				return scheduler.NewCooldown(10*time.Minute, "叶公好龙面板无回包")
			}
			slog.Info("叶公好龙面板",
				"role_id", session.RoleID(),
				"player_id", session.PlayerID(),
				"name", info.Name,
				"lv", info.Lv,
				"star", info.Star,
				"exp", info.Exp,
				"feed_num", info.FeedNum,
			)
			if info.Lv >= 10 && info.Star >= 10 {
				slog.Info("叶公好龙已达满级，跳过", "role_id", session.RoleID())
				return nil
			}
			if info.FeedNum <= 0 {
				slog.Info("叶公好龙今日培养次数已用尽", "role_id", session.RoleID())
				return nil
			}

			// 暴击培养可能不消耗每日次数，且回包次数有时滞后；以“次数归零/
			// 服务端失败码”为停止条件，仅对连续不递减做异常熔断。
			attemptLimit := maxPetAnimalFeeds
			remaining := info.FeedNum
			feeds := 0
			staleFeeds := 0
			for feeds < attemptLimit && remaining > 0 {
				if ctx.Err() != nil {
					return nil
				}
				prevRemaining := remaining
				resp := call(protocol.BuildPetAnimalFeedPacket(protocol.PetAnimalFeedTypeCoin, session.LastActionID()),
					protocol.ActionIDPetAnimalFeed)
				if resp == nil {
					return nil
				}
				res, err := protocol.ParsePetAnimalFeedResult(resp.Payload)
				if err != nil {
					slog.Warn("解析叶公好龙培养回包失败", "role_id", session.RoleID(), "err", err)
					return nil
				}
				switch res.Result {
				case protocol.PetAnimalResultSuccess:
					if res.FeedType != protocol.PetAnimalFeedTypeCoin {
						slog.Warn("叶公好龙培养回包方式异常，已停止以避免元宝消耗",
							"role_id", session.RoleID(), "feed_type", res.FeedType)
						return nil
					}
					feeds++
					remaining = res.FeedNum
					if remaining >= prevRemaining {
						// 线上实测首次培养的回包可能仍带旧次数；以面板
						// feed_num 为准复核，避免误判为“次数未递减”而提前退出。
						if info := fetchInfo(); info != nil {
							remaining = info.FeedNum
						}
					}
					if remaining >= prevRemaining {
						staleFeeds++
						slog.Info("叶公好龙培养次数暂未递减 (暴击可能不消耗次数)",
							"role_id", session.RoleID(), "before", prevRemaining,
							"after", remaining, "feeds", feeds,
							"stale_feeds", staleFeeds, "max_stale", maxPetAnimalStaleFeeds)
						if staleFeeds >= maxPetAnimalStaleFeeds {
							slog.Warn("叶公好龙连续培养次数不递减，熔断本轮",
								"role_id", session.RoleID(), "stale_feeds", staleFeeds)
							return nil
						}
					} else {
						staleFeeds = 0
					}
					slog.Info("叶公好龙普通培养成功",
						"role_id", session.RoleID(),
						"lv", res.Lv,
						"star", res.Star,
						"exp", res.Exp,
						"exp_add", res.ExpAdd,
						"feed_type", res.FeedType,
						"feed_num", remaining,
						"hit", res.Hit,
					)
				case protocol.PetAnimalResultTooMuchExp:
					slog.Info("叶公好龙经验已满，需先升阶；本轮停止培养", "role_id", session.RoleID())
					return nil
				case protocol.PetAnimalResultNoEnoughCoin:
					slog.Info("叶公好龙普通培养铜钱不足，结束本轮", "role_id", session.RoleID())
					return nil
				case protocol.PetAnimalResultMaxLv:
					slog.Info("叶公好龙已达满级，结束本轮", "role_id", session.RoleID())
					return nil
				default:
					slog.Info("叶公好龙普通培养未成功，结束本轮", "role_id", session.RoleID(),
						"result", res.Result, "result_text", protocol.PetAnimalResultText(res.Result))
					return nil
				}
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(200 * time.Millisecond):
				}
			}
			slog.Info("叶公好龙普通培养巡检完成", "role_id", session.RoleID(),
				"feeds", feeds, "feed_num_left", remaining)
			return nil
		},
	)
}
