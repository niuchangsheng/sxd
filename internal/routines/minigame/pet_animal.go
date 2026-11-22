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
	// 暴击培养不消耗每日次数，实际培养次数会远大于每日 20 次；
	// 100 次仅覆盖约 80% 暴击率下的极端情况，这里放宽到 200。
	maxPetAnimalFeeds = 200
	// maxPetAnimalStaleFeeds 连续多少次「非暴击」培养回包/面板次数都不递减
	// 后判定服务端异常熔断。暴击不计入（Hit>0 时次数本就不消耗）。
	maxPetAnimalStaleFeeds = 5
	// petAnimalCallTimeout 单次协议请求超时。
	petAnimalCallTimeout = 8 * time.Second
	// petAnimalFeedInterval 连续培养之间的节流间隔。
	//
	// 2026-11-20 实机：连续快速培养约 2 次后服务端即静默丢弃（剩 18/12 次
	// 未用完）。按人工点击节奏留出间隔可显著降低被限流的概率。
	petAnimalFeedInterval = 600 * time.Millisecond
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
				// 面板被静默丢弃通常意味着当日状态已结束/动作链失效；连续 3 次后
				// 挂起到下一次每日重置，避免每 10 分钟无限空转。
				return scheduler.HandleSilentDrop(session, "pet_animal", 3,
					10*time.Minute, "叶公好龙面板无回包")
			}
			// 注意：面板成功不代表培养动作可用，此处不清零丢包计数；
			// 计数只在培养请求真正回包成功时清零。
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
			crits := 0
			for feeds < attemptLimit && remaining > 0 {
				if ctx.Err() != nil {
					return nil
				}
				prevRemaining := remaining
				// Module 48 对 PrevAct 链敏感：首次无回包时按「最后收到的包」
				// 重同步重试一次，避免一次丢包就把剩余培养次数拖到下一轮
				// （2026-11-18 s1 实测多次因单次丢包提前中止）。
				resp, _ := session.CallWithPrevActResync(ctx, petAnimalCallTimeout,
					protocol.BuildPetAnimalFeedPacket(protocol.PetAnimalFeedTypeCoin, session.LastActionID()),
					protocol.ActionIDPetAnimalFeed)
				if resp == nil {
					// Module 48 在上下文链失效时会连续丢弃培养请求；先发一次
					// 功能开放查询（0/69）重置动作链，再重试一次（与面板查询
					// 的恢复手段一致）。
					if _, fErr := session.CheckFunctionOpen(ctx, protocol.FunctionIDPetAnimal); fErr == nil {
						resp, _ = session.CallWithPrevActResync(ctx, petAnimalCallTimeout,
							protocol.BuildPetAnimalFeedPacket(protocol.PetAnimalFeedTypeCoin, session.LastActionID()),
							protocol.ActionIDPetAnimalFeed)
					}
				}
				if resp == nil {
					slog.Info("叶公好龙培养无回包，本轮中止并保留剩余次数",
						"role_id", session.RoleID(), "remaining", remaining)
					return scheduler.HandleSilentDrop(session, "pet_animal", 3,
						10*time.Minute, "叶公好龙培养无回包")
				}
				scheduler.ResetSilentDrops(session, "pet_animal")
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
					if res.Hit > 0 {
						// 暴击培养（小暴击/大暴击）不消耗每日次数：FeedNum 不变是
						// 正常业务语义，不能当作「次数未递减」去发面板复核或熔断。
						crits++
						staleFeeds = 0
						if remaining <= 0 && prevRemaining > 0 {
							// 极端情况：服务端同时下发了暴击与次数归零。
							remaining = 0
						}
						slog.Info("叶公好龙暴击培养成功（不消耗次数）",
							"role_id", session.RoleID(),
							"hit", res.Hit, "lv", res.Lv, "star", res.Star,
							"exp", res.Exp, "exp_add", res.ExpAdd,
							"feed_num", remaining, "feeds", feeds, "crits", crits)
					} else {
						if remaining >= prevRemaining {
							// 非暴击却未递减：线上实测首次回包可能仍带旧次数，
							// 以面板 feed_num 为准复核一次。
							if info := fetchInfo(); info != nil {
								remaining = info.FeedNum
							}
						}
						if remaining >= prevRemaining {
							staleFeeds++
							slog.Info("叶公好龙培养次数未递减且非暴击 (回包滞后或服务端异常)",
								"role_id", session.RoleID(), "before", prevRemaining,
								"after", remaining, "feeds", feeds, "hit", res.Hit,
								"stale_feeds", staleFeeds, "max_stale", maxPetAnimalStaleFeeds)
							if staleFeeds >= maxPetAnimalStaleFeeds {
								slog.Warn("叶公好龙连续非暴击培养次数不递减，熔断本轮",
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
					}
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
				case <-time.After(petAnimalFeedInterval):
				}
			}
			slog.Info("叶公好龙普通培养巡检完成", "role_id", session.RoleID(),
				"feeds", feeds, "feed_num_left", remaining)
			return nil
		},
	)
}
