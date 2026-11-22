// Package minigame 提供吉星高照、仙旅奇缘答题、喜从天降、刮刮卡、钓鱼等益智趣味活动。
package minigame

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

// maxLuckyStarRolls 单次调度的最大投掷轮数，防止服务端异常未扣减次数时死循环。
const maxLuckyStarRolls = 10

// NewFactionLuckyStarRoutine 构造帮派吉星高照扔骰子任务。
//
// 协议对齐 2026 客户端 Mod_Faction_Base:
//   - Action 45 faction_roll_cake_info 查询战绩与剩余次数
//   - Action 44 faction_roll_cake_remain_times 查询剩余次数 (信息查询失败时兜底)
//   - Action 46 roll_cake 投掷骰子
//
// 个人吉星高照见 NewLuckyStarRoutine (Mod_RollCake_Base, module 38)。
func NewFactionLuckyStarRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"faction_lucky_star",
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
			if session.IsFactionFeatureUnavailable() {
				slog.Info("帮派模块对当前角色不可用，跳过帮派吉星高照", "role_id", session.RoleID())
				return nil
			}
			slog.Info("正在执行益智任务: 帮派吉星高照摇骰子", "role_id", session.RoleID())

			// 1. 先查询剩余投掷次数 (Mod 10, Act 44)。
			// 该动作在未加入帮派的角色上依然正常回包（实测 s0 返回 0），
			// 是唯一可用于提前判断“今日还有没有次数”的轻量状态查询；
			// 战绩面板 (Act 45) 会携带帮派成员列表，无帮派角色上会被服务端静默丢弃，
			// 因此不能把面板查询放在最前面。
			remainPkt := protocol.BuildFactionRollCakeRemainTimesPacket(protocol.ActionIDTownEnter)
			rCtx, rCancel := context.WithTimeout(ctx, 8*time.Second)
			remainResp, remainErr := session.Call(rCtx, remainPkt, protocol.ActionIDFactionRollCakeRemainTimes)
			rCancel()

			var remain int8
			remainKnown := false
			if remainErr == nil && remainResp != nil {
				if parsed, err := protocol.ParseFactionRollCakeRemainTimesResult(remainResp.Payload); err != nil {
					slog.Warn("解析吉星高照剩余次数回包失败，尝试查询战绩面板", "role_id", session.RoleID(), "err", err)
				} else {
					remain = parsed
					remainKnown = true
				}
			} else {
				slog.Warn("查询吉星高照剩余次数失败，尝试查询战绩面板", "role_id", session.RoleID(), "err", remainErr)
			}

			if remainKnown && remain <= 0 {
				slog.Info("今日帮派吉星高照投掷次数已用尽", "role_id", session.RoleID())
				return nil
			}

			// 2. 查询战绩面板 (Mod 10, Act 45)：仅用于日志与 44 失败时的兜底。
			prevAct := protocol.ActionIDFactionRollCakeRemainTimes
			infoPkt := protocol.BuildFactionRollCakeInfoPacket(protocol.ActionIDTownEnter)
			infoCtx, infoCancel := context.WithTimeout(ctx, 8*time.Second)
			infoResp, infoErr := session.Call(infoCtx, infoPkt, protocol.ActionIDFactionRollCakeInfo)
			infoCancel()
			if infoErr == nil && infoResp != nil {
				if info, err := protocol.ParseFactionRollCakeInfoResult(infoResp.Payload); err != nil {
					slog.Warn("解析吉星高照战绩回包失败", "role_id", session.RoleID(), "err", err)
				} else {
					slog.Info("吉星高照当前战绩",
						"role_id", session.RoleID(),
						"week_best_score", info.WeekBestScore,
						"today_best_score", info.TodayBestScore,
						"remain_times", info.RemainTimes,
						"faction_score", info.FactionScore,
						"member_count", len(info.Members),
					)
					if !remainKnown {
						remain = info.RemainTimes
						remainKnown = true
						prevAct = protocol.ActionIDFactionRollCakeInfo
					}
				}
			} else {
				slog.Warn("查询吉星高照战绩失败", "role_id", session.RoleID(), "err", infoErr)
			}

			if !remainKnown {
				if errors.Is(remainErr, context.DeadlineExceeded) || errors.Is(infoErr, context.DeadlineExceeded) {
					// 两个只读动作都没有任何回包：判定帮派模块不可用并熔断后续帮派任务。
					session.MarkFactionFeatureUnavailable()
					slog.Info("帮派吉星高照剩余次数与战绩面板均无任何回包，判定帮派模块不可用（角色可能未加入帮派），跳过本轮帮派任务",
						"role_id", session.RoleID())
				} else {
					slog.Warn("无法确定帮派吉星高照剩余次数，跳过本轮", "role_id", session.RoleID())
				}
				return nil
			}

			if remain <= 0 {
				slog.Info("今日帮派吉星高照投掷次数已用尽", "role_id", session.RoleID())
				return nil
			}

			// 3. 循环投掷直至次数用尽 (Mod 10, Act 46)
			rolls := 0
			for remain > 0 && rolls < maxLuckyStarRolls {
				if err := ctx.Err(); err != nil {
					slog.Info("吉星高照投掷已取消，提前退出", "role_id", session.RoleID(), "err", err)
					return nil
				}

				rollPkt := protocol.BuildFactionRollCakePacket(prevAct)
				rollCtx, rollCancel := context.WithTimeout(ctx, 8*time.Second)
				rollResp, err := session.Call(rollCtx, rollPkt, protocol.ActionIDFactionRollCake)
				rollCancel()
				if err != nil || rollResp == nil {
					slog.Info("吉星高照投掷未收到回包", "role_id", session.RoleID(), "err", err)
					return nil
				}

				res, err := protocol.ParseFactionRollCakeResult(rollResp.Payload)
				if err != nil {
					slog.Warn("解析吉星高照投掷回包失败", "role_id", session.RoleID(), "err", err)
					return nil
				}

				if res.Msg != protocol.RollCakeResultSuccess {
					switch res.Msg {
					case protocol.RollCakeResultNoTimes:
						slog.Info("今日吉星高照投掷次数已用尽", "role_id", session.RoleID())
					case protocol.RollCakeResultAwarding:
						slog.Info("吉星高照正在结算排名奖励，稍后再试", "role_id", session.RoleID())
					default:
						slog.Warn("吉星高照投掷被服务端拒绝", "role_id", session.RoleID(), "result", res.Msg)
					}
					break
				}

				rolls++
				slog.Info("吉星高照投掷成功",
					"role_id", session.RoleID(),
					"roll", rolls,
					"reward_type", res.Type,
					"score", res.Score,
					"today_score", res.TodayScore,
					"dice", res.Dice,
					"remain_times", res.RemainTimes,
				)

				// 熔断保护: 服务端未扣减次数时立即退出，避免死循环。
				if res.RemainTimes >= remain {
					slog.Warn("吉星高照投掷次数未扣减，熔断退出",
						"role_id", session.RoleID(),
						"before_times", remain,
						"after_times", res.RemainTimes,
					)
					remain = 0
					break
				}
				remain = res.RemainTimes
				prevAct = protocol.ActionIDFactionRollCake

				if remain > 0 && jitter != nil {
					_ = jitter.Wait(ctx)
				}
			}

			slog.Info("吉星高照摇骰子完成", "role_id", session.RoleID(), "rolls", rolls, "remain_times", remain)
			return nil
		},
	)
}

// NewFishingRoutine 构造自动钓鱼与命格整理任务。
// NewFishingRoutine 构造自动钓鱼任务 (Module 217, Mod_Fish_Base)。
//
// 先查询鱼钩数量，再逐个下钩直到鱼钩耗尽或背包/命格位不足；
// 只使用免费鱼钩，不消耗元宝。
func NewFishingRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	const fishInterval = 200 * time.Millisecond
	return scheduler.NewBaseRoutine(
		"fishing",
		"minigame",
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

			// 0. 功能开放检查：未开放时服务端会静默丢弃钓鱼请求。
			open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDFishing)
			if fErr != nil {
				slog.Info("查询钓鱼功能开放状态失败，跳过", "role_id", session.RoleID(), "err", fErr)
				return nil
			}
			if !open {
				slog.Info("钓鱼功能未开放，跳过", "role_id", session.RoleID(),
					"function_id", protocol.FunctionIDFishing)
				return nil
			}

			// 1. 查询鱼钩数量；为 0 时说明免费次数已用尽。
			hCtx, hCancel := context.WithTimeout(ctx, callTimeout)
			hooksResp, err := session.Call(hCtx, protocol.BuildFishHooksPacket(), protocol.ActionIDFishHooks)
			hCancel()
			if err != nil {
				if abortErr, ok := kit.IsAbortErr(err); ok {
					return abortErr
				}
				// 查询被静默丢弃时不能 return nil：要等到下一个 cron 时段。
				slog.Info("查询鱼钩数量无回包，按连续丢包退避", "role_id", session.RoleID(), "err", err)
				return scheduler.HandleSilentDropCtx(ctx, session, "fishing", 3,
					10*time.Minute, "查询鱼钩数量无回包")
			}
			hooks, err := protocol.ParseFishHooksResult(hooksResp.Payload)
			if err != nil {
				return fmt.Errorf("解析鱼钩数量失败: %w", err)
			}
			if hooks <= 0 {
				slog.Info("今日鱼钩已用尽，跳过钓鱼", "role_id", session.RoleID())
				return nil
			}
			slog.Info("开始自动钓鱼", "role_id", session.RoleID(), "fishhooks", hooks)

			caught := 0
			for i := int32(0); i < hooks; i++ {
				fCtx, fCancel := context.WithTimeout(ctx, callTimeout)
				resp, cErr := session.Call(fCtx, protocol.BuildFishDoPacket(), protocol.ActionIDFishDo)
				fCancel()
				if cErr != nil {
					if abortErr, ok := kit.IsAbortErr(cErr); ok {
						return abortErr
					}
					// 中途静默丢包不能 break + return nil：剩余鱼钩次数会被丢到
					// 下一个 cron 时段。按连续丢包退避后继续。
					slog.Info("钓鱼无应答，按连续丢包退避",
						"role_id", session.RoleID(), "index", i+1, "err", cErr)
					return scheduler.HandleSilentDropCtx(ctx, session, "fishing", 3,
						10*time.Minute, "钓鱼无应答")
				}
				result, pErr := protocol.ParseFishDoResult(resp.Payload)
				if pErr != nil {
					slog.Warn("解析钓鱼回包失败", "role_id", session.RoleID(), "err", pErr)
					break
				}
				switch result.Result {
				case protocol.FishResultSuccess:
					scheduler.ResetSilentDropsCtx(ctx, session, "fishing")
					caught++
					slog.Info("钓鱼成功",
						"role_id", session.RoleID(),
						"index", i+1,
						"item_id", result.ItemID,
						"number", result.Number,
					)
				case protocol.FishResultNotEnoughFishhook:
					slog.Info("鱼钩已用尽", "role_id", session.RoleID(), "caught", caught)
					i = hooks
				case protocol.FishResultNotEnoughFateGrid,
					protocol.FishResultItemPackFull,
					protocol.FishResultFiveElementsPackFull,
					protocol.FishResultDragonballNoFreeGrid,
					protocol.FishResultCanJuanPackFull:
					slog.Warn("钓鱼奖励因背包/命格位不足未能领取",
						"role_id", session.RoleID(), "result", result.Result)
					i = hooks
				default:
					slog.Warn("钓鱼返回未知结果", "role_id", session.RoleID(), "result", result.Result)
					i = hooks
				}
				if i+1 < hooks {
					select {
					case <-ctx.Done():
						return nil
					case <-time.After(fishInterval):
					}
				}
			}
			slog.Info("自动钓鱼完成", "role_id", session.RoleID(), "caught", caught)
			return nil
		},
	)
}

// NewScratchCardRoutine 构造幸运刮刮卡福利任务 (Module 185, Mod_ScratchCard_Base)。
//
// 只使用每日免费刮卡次数；结果可能为积分/元宝/形象卡/普通道具。
// 次数不足、背包已满、刮卡失败时立即收手，不消耗任何元宝。
func NewScratchCardRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	const shaveInterval = 300 * time.Millisecond
	return scheduler.NewBaseRoutine(
		"scratch_card",
		"minigame",
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

			// 1. 查询活动状态与剩余免费次数
			iCtx, iCancel := context.WithTimeout(ctx, callTimeout)
			infoResp, err := session.Call(iCtx, protocol.BuildScratchCardInfoPacket(), protocol.ActionIDScratchCardInfo)
			iCancel()
			if err != nil {
				if abortErr, ok := kit.IsAbortErr(err); ok {
					return abortErr
				}
				// 状态查询被静默丢弃时不能返回普通 error：ScheduleCron 会等到
				// 下一个 cron 时段。按连续丢包退避，恢复回包后计数自动清零。
				slog.Info("刮刮卡状态查询无回包，按连续丢包退避",
					"role_id", session.RoleID(), "err", err)
				return scheduler.HandleSilentDropCtx(ctx, session, "scratch_card", 3,
					30*time.Minute, "刮刮卡状态查询无回包")
			}
			scheduler.ResetSilentDropsCtx(ctx, session, "scratch_card")
			info, err := protocol.ParseScratchCardInfoResult(infoResp.Payload)
			if err != nil {
				return scheduler.NewCooldown(10*time.Minute, "刮刮卡状态解析失败")
			}
			if info.InActivity == 0 {
				// 协议只下发 end_time，没有开启时间；活动当天稍后开启时
				// 每小时探测一次，避免等到下一个 cron 时段才重试。
				slog.Info("刮刮卡活动未开放，1 小时后再探测",
					"role_id", session.RoleID(),
					"active_degree", info.ActiveDegree,
					"card_times", info.CardTimes,
					"end_time", info.EndTime,
				)
				return scheduler.NewCooldown(time.Hour, "刮刮卡活动未开放")
			}
			if info.CardTimes == 0 {
				slog.Info("刮刮卡活动已开放但免费次数已用尽，挂起至次日",
					"role_id", session.RoleID(),
					"active_degree", info.ActiveDegree,
					"end_time", info.EndTime,
				)
				return scheduler.ErrAttemptsExhausted
			}
			slog.Info("开始刮刮卡",
				"role_id", session.RoleID(),
				"card_times", info.CardTimes,
				"active_degree", info.ActiveDegree,
			)

			// 2. 用完全部免费次数
			shaved, ingotHits := 0, 0
			for i := 0; i < int(info.CardTimes); i++ {
				sCtx, sCancel := context.WithTimeout(ctx, callTimeout)
				resp, cErr := session.Call(sCtx, protocol.BuildScratchCardShavePacket(), protocol.ActionIDScratchCardShave)
				sCancel()
				if cErr != nil {
					if abortErr, ok := kit.IsAbortErr(cErr); ok {
						return abortErr
					}
					// 中途静默丢包不能 break + return nil：剩余免费次数会被当成
					// 已完成，直到下一个 cron 时段。按连续丢包退避后重试剩余次数。
					slog.Info("刮卡无应答，按连续丢包退避",
						"role_id", session.RoleID(), "index", i+1, "err", cErr)
					return scheduler.HandleSilentDropCtx(ctx, session, "scratch_card_shave", 3,
						10*time.Minute, "刮卡无回包")
				}
				scheduler.ResetSilentDropsCtx(ctx, session, "scratch_card_shave")
				result, pErr := protocol.ParseScratchCardShaveResult(resp.Payload)
				if pErr != nil {
					slog.Warn("解析刮卡回包失败", "role_id", session.RoleID(), "index", i+1, "err", pErr)
					return scheduler.NewCooldown(10*time.Minute, "刮卡回包解析失败")
				}
				switch result.Result {
				case protocol.ScratchCardResultSuccess,
					protocol.ScratchCardResultPoint,
					protocol.ScratchCardResultImage,
					protocol.ScratchCardResultAward:
					shaved++
					slog.Info("刮卡成功",
						"role_id", session.RoleID(),
						"index", i+1,
						"result", result.Result,
						"item_id", result.ItemID,
					)
				case protocol.ScratchCardResultIngot:
					shaved++
					ingotHits++
					slog.Info("刮出元宝", "role_id", session.RoleID(), "index", i+1, "item_id", result.ItemID)
				case protocol.ScratchCardResultPackFull,
					protocol.ScratchCardResultItemPackFull,
					protocol.ScratchCardResultFatePackFull:
					slog.Warn("刮卡奖励因背包已满未能领取", "role_id", session.RoleID(), "result", result.Result)
					return scheduler.ErrBagFull
				case protocol.ScratchCardResultNotEnoughTimes:
					slog.Info("刮卡次数已用尽", "role_id", session.RoleID(), "index", i+1)
					i = int(info.CardTimes)
				case protocol.ScratchCardResultFailed,
					protocol.ScratchCardResultNotExistCard:
					slog.Warn("刮卡失败", "role_id", session.RoleID(), "result", result.Result)
					i = int(info.CardTimes)
				default:
					slog.Warn("刮卡返回未知结果", "role_id", session.RoleID(), "result", result.Result)
					i = int(info.CardTimes)
				}
				if i+1 < int(info.CardTimes) {
					select {
					case <-ctx.Done():
						return nil
					case <-time.After(shaveInterval):
					}
				}
			}
			slog.Info("刮刮卡任务完成", "role_id", session.RoleID(), "shaved", shaved, "ingot_hits", ingotHits)
			return nil
		},
	)
}

// NewIceCaveRoutine 构造一键冰窟探险任务。
// NewIceCaveRoutine 构造一键冰窟探险任务 (Module 393, Mod_IceCaveTreasure_Base)。
//
// 先查询活动面板：无应答/未开放时安全跳过；开放时按剩余免费锤子逐块敲击，
// 活跃度不足/完成/背包满时停止；最后在冰晶足够时兑换奖励。
// 不调用 buy_hammer (消耗元宝)。
func NewIceCaveRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"ice_cave",
		"minigame",
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

			// 1. 活动/功能检测：无应答视为未开放。
			iCtx, iCancel := context.WithTimeout(ctx, callTimeout)
			infoResp, err := session.Call(iCtx, protocol.BuildIceCaveInfoPacket(), protocol.ActionIDIceCaveInfo)
			iCancel()
			if err != nil {
				// 冰窟探险属于节日狂欢子活动，未开放时服务端不回包（无独立开启时间接口）。
				// 按“活动未开放”每 30 分钟复查，节日开启后即可自动敲冰。
				slog.Info("冰窟探险活动未开放，稍后由定时唤醒复查", "role_id", session.RoleID())
				return scheduler.NewCooldown(30*time.Minute, "冰窟探险活动未开放")
			}
			info, err := protocol.ParseIceCaveInfoResult(infoResp.Payload)
			if err != nil {
				return fmt.Errorf("解析冰窟面板失败: %w", err)
			}
			slog.Info("冰窟探险状态",
				"role_id", session.RoleID(),
				"slots", len(info.Slots),
				"hammers", info.HammerNum,
				"active_num", info.ActiveNum,
				"active_limit", info.ActiveLimit,
				"gold", info.GoldNum,
				"gift_count", info.GiftCount,
				"gift_limit", info.GiftLimit,
			)
			if info.HammerNum <= 0 {
				slog.Info("冰窟锤子已用尽，跳过", "role_id", session.RoleID())
				return nil
			}
			if info.ActiveLimit > 0 && info.ActiveNum < info.ActiveLimit {
				slog.Info("活跃度不足，跳过冰窟探险",
					"role_id", session.RoleID(),
					"active_num", info.ActiveNum,
					"active_limit", info.ActiveLimit,
				)
				return nil
			}

			// 2. 逐块敲击未开启的冰面。
			hits := int32(0)
			for _, slot := range info.Slots {
				if hits >= info.HammerNum {
					break
				}
				if slot.State != 0 {
					continue
				}
				hCtx, hCancel := context.WithTimeout(ctx, callTimeout)
				resp, cErr := session.Call(hCtx, protocol.BuildIceCaveHitPacket(slot.Ice, slot.IsIron), protocol.ActionIDIceCaveHit)
				hCancel()
				if cErr != nil {
					if abortErr, ok := kit.IsAbortErr(cErr); ok {
						return abortErr
					}
					// 中途静默丢包不能 break：剩余锤子会丢到下一个 cron 时段。
					slog.Info("冰窟敲击无应答，按连续丢包退避",
						"role_id", session.RoleID(), "ice", slot.Ice, "err", cErr)
					return scheduler.HandleSilentDropCtx(ctx, session, "ice_cave", 3,
						10*time.Minute, "冰窟敲击无应答")
				}
				result, pErr := protocol.ParseIceCaveHitResult(resp.Payload)
				if pErr != nil {
					slog.Warn("解析冰窟敲击回包失败", "role_id", session.RoleID(), "err", pErr)
					break
				}
				switch result.Result {
				case protocol.IceCaveResultSuccess:
					scheduler.ResetSilentDropsCtx(ctx, session, "ice_cave")
					hits++
					slog.Info("冰窟敲击成功", "role_id", session.RoleID(), "ice", slot.Ice, "awards", result.AwardItems)
				case protocol.IceCaveResultCompleted:
					slog.Info("冰窟探险已完成", "role_id", session.RoleID(), "completed_awards", result.CompletedItems)
					hits = info.HammerNum
				case protocol.IceCaveResultItemPackFull:
					return scheduler.ErrBagFull
				case protocol.IceCaveResultNotEnoughActive:
					slog.Info("冰窟活跃度不足，结束敲击", "role_id", session.RoleID())
					hits = info.HammerNum
				case protocol.IceCaveResultFuncNotOpen,
					protocol.IceCaveResultNonActivityTime,
					protocol.IceCaveResultNotActivityTime:
					slog.Info("冰窟活动未开放/非活动时间，稍后重试",
						"role_id", session.RoleID(), "result", result.Result)
					return scheduler.NewCooldown(30*time.Minute, "冰窟活动非活动时间")
				default:
					slog.Warn("冰窟敲击返回未知结果", "role_id", session.RoleID(), "result", result.Result)
					hits = info.HammerNum
				}
			}
			slog.Info("冰窟敲击完成", "role_id", session.RoleID(), "hits", hits)

			// 3. 冰晶足够时兑换奖励。
			if info.GiftGold > 0 && info.GoldNum >= info.GiftGold && info.GiftCount < info.GiftLimit {
				gCtx, gCancel := context.WithTimeout(ctx, callTimeout)
				gResp, gErr := session.Call(gCtx, protocol.BuildIceCaveGetGiftPacket(), protocol.ActionIDIceCaveGetGift)
				gCancel()
				if gErr != nil {
					if abortErr, ok := kit.IsAbortErr(gErr); ok {
						return abortErr
					}
					slog.Info("冰窟奖励兑换无应答，按连续丢包退避", "role_id", session.RoleID(), "err", gErr)
					return scheduler.HandleSilentDropCtx(ctx, session, "ice_cave", 3,
						10*time.Minute, "冰窟奖励兑换无应答")
				}
				result, pErr := protocol.ParseIceCaveGiftResult(gResp.Payload)
				if pErr != nil {
					slog.Warn("解析冰窟兑换回包失败", "role_id", session.RoleID(), "err", pErr)
					return nil
				}
				switch result {
				case protocol.IceCaveResultSuccess:
					slog.Info("冰窟奖励兑换成功", "role_id", session.RoleID(), "item_id", info.GiftItem)
				case protocol.IceCaveResultItemPackFull:
					return scheduler.ErrBagFull
				case protocol.IceCaveResultAlreadyGet:
					slog.Info("冰窟奖励已兑换", "role_id", session.RoleID())
				case protocol.IceCaveResultNotEnoughGold:
					slog.Info("冰晶不足，跳过兑换", "role_id", session.RoleID(), "gold", info.GoldNum)
				default:
					slog.Info("冰窟奖励未兑换", "role_id", session.RoleID(), "result", result)
				}
			}
			return nil
		},
	)
}

// NewEraTravelRoutine 构造山河纪元游历掷骰与奇遇领奖任务 (Feature 10: 山河游历)。
func NewEraTravelRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"era_travel",
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
			slog.Info("正在执行益智任务: 山河纪元游历掷骰与寻宝", "role_id", session.RoleID())

			// 1. 查询游历状态 (Mod 850, Act 4)。该回包首字节为业务结果码。
			statePkt := protocol.BuildEraTravelGetStatePacket(protocol.ActionIDTownEnter)
			sCtx, sCancel := context.WithTimeout(ctx, 8*time.Second)
			resp, err := session.Call(sCtx, statePkt, protocol.ActionIDEraTravelGetState)
			sCancel()

			if err != nil || resp == nil || len(resp.Payload) == 0 {
				slog.Info("山河游历功能未开放/不可用，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(30*time.Minute, "山河游历状态查询无回包")
			}
			if code := resp.Payload[0]; code != 0 {
				slog.Info("山河游历状态查询返回失败，稍后重试",
					"role_id", session.RoleID(), "result", code)
				return scheduler.NewCooldown(time.Hour, "山河游历状态不可用")
			}

			// 2. 掷骰移动 (Mod 850, Act 0)
			rollPkt := protocol.BuildEraTravelRollDicePacket(1, protocol.ActionIDEraTravelGetState)
			rCtx, rCancel := context.WithTimeout(ctx, 8*time.Second)
			rollResp, rollErr := session.Call(rCtx, rollPkt, protocol.ActionIDEraTravelRollDice)
			rCancel()
			if rollErr != nil || rollResp == nil {
				slog.Info("山河游历掷骰无回包，稍后重试", "role_id", session.RoleID(), "err", rollErr)
				return scheduler.NewCooldown(10*time.Minute, "山河游历掷骰无回包")
			}
			if len(rollResp.Payload) == 0 {
				slog.Info("山河游历掷骰回包为空，稍后重试", "role_id", session.RoleID())
				return scheduler.NewCooldown(10*time.Minute, "山河游历掷骰回包为空")
			}

			// 3. 奇遇画卷事件结算 (Mod 635, Act 0)
			eventPkt := protocol.NewPacket(protocol.ActionIDEraEventSettle, protocol.NewWriter().WriteUint32(protocol.ActionIDEraTravelRollDice).Bytes())
			eCtx, eCancel := context.WithTimeout(ctx, 8*time.Second)
			eventResp, eventErr := session.Call(eCtx, eventPkt, protocol.ActionIDEraEventSettle)
			eCancel()
			if eventErr != nil || eventResp == nil {
				slog.Info("山河游历事件结算无回包，稍后重试", "role_id", session.RoleID(), "err", eventErr)
				return scheduler.NewCooldown(10*time.Minute, "山河游历事件结算无回包")
			}

			slog.Info("山河游历掷骰与事件结算完成",
				"role_id", session.RoleID(),
				"roll_reply_len", len(rollResp.Payload),
				"event_reply_len", len(eventResp.Payload),
			)
			return nil
		},
	)
}

// NewLingDaoRoutine 构造灵道初解任务领悟任务 (Feature 11: 灵道初解)。
func NewLingDaoRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"lingdao_practice",
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
			slog.Info("正在执行益智任务: 灵道初解修炼与任务领悟", "role_id", session.RoleID())

			// 1. 查询灵道修炼进度 (Mod 658, Act 0)
			statusPkt := protocol.BuildLingDaoGetStatusPacket(protocol.ActionIDTownEnter)
			sCtx, sCancel := context.WithTimeout(ctx, 8*time.Second)
			resp, err := session.Call(sCtx, statusPkt, protocol.ActionIDLingDaoGetStatus)
			sCancel()

			if err != nil || resp == nil || len(resp.Payload) == 0 {
				slog.Info("灵道初解功能未开放/不可用，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}

			// 2. 提交初解任务领悟 (Mod 711, Act 0)
			compPkt := protocol.BuildLingDaoComprehendPacket(protocol.ActionIDLingDaoGetStatus)
			cCtx, cCancel := context.WithTimeout(ctx, 8*time.Second)
			compResp, cErr := session.Call(cCtx, compPkt, protocol.ActionIDLingDaoComprehend)
			cCancel()
			if cErr != nil || compResp == nil {
				slog.Info("灵道初解领悟请求无回包，稍后重试",
					"role_id", session.RoleID(), "err", cErr)
				return scheduler.NewCooldown(30*time.Minute, "灵道初解领悟无回包")
			}

			slog.Info("灵道初解战斗通行证面板", "role_id", session.RoleID(),
				"reply_len", len(compResp.Payload))

			// 3. 一键领取战斗通行证免费奖励 (Mod 711, Act 3)。
			awardPkt := protocol.BuildLingDaoOneKeyAwardPacket()
			aCtx, aCancel := context.WithTimeout(ctx, 8*time.Second)
			awardResp, aErr := session.Call(aCtx, awardPkt, protocol.ActionIDLingDaoOneKeyAward)
			aCancel()
			if aErr != nil || awardResp == nil {
				slog.Info("灵道初解一键领取无回包，稍后重试", "role_id", session.RoleID(), "err", aErr)
				return scheduler.NewCooldown(30*time.Minute, "灵道初解一键领取无回包")
			}
			code, pErr := protocol.ParseActionFirstByte(awardResp.Payload, "灵道初解一键领取")
			if pErr != nil {
				slog.Warn("灵道初解一键领取解析失败", "role_id", session.RoleID(), "err", pErr)
				return nil
			}
			switch code {
			case 0:
				slog.Info("灵道初解战斗通行证奖励领取成功", "role_id", session.RoleID())
			case 8, 11:
				slog.Info("灵道初解暂无可领取奖励", "role_id", session.RoleID(), "result", code)
			case 10:
				return scheduler.ErrBagFull
			default:
				slog.Info("灵道初解一键领取未成功", "role_id", session.RoleID(), "result", code)
			}
			return nil
		},
	)
}

// NewHuanshouRoutine 构造山海幻兽入侵挑战与扫荡任务 (Feature 12: 山海幻兽)。
func NewHuanshouRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"huanshou_invade",
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
			slog.Info("正在执行益智任务: 山海幻兽入侵推图与扫荡", "role_id", session.RoleID())

			// 0. 先通过官方功能开放列表判定门槛，未开放时不再发送会被静默丢弃的业务请求。
			open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDEudemons)
			if fErr != nil {
				slog.Info("山海幻兽功能开放状态查询失败，稍后重试", "role_id", session.RoleID(), "err", fErr)
				return scheduler.NewCooldown(30*time.Minute, "山海幻兽功能状态无回包")
			}
			if !open {
				slog.Info("山海幻兽功能未开放，稍后重试", "role_id", session.RoleID(), "function_id", protocol.FunctionIDEudemons)
				return scheduler.NewCooldown(2*time.Hour, "山海幻兽功能未开放")
			}

			// 1. 查询幻兽面板 (Mod 701, Act 0)
			panelPkt := protocol.BuildHuanshouPanelPacket(protocol.ActionIDTownEnter)
			pCtx, pCancel := context.WithTimeout(ctx, 8*time.Second)
			panelResp, pErr := session.Call(pCtx, panelPkt, protocol.ActionIDHuanshouPanel)
			pCancel()
			if pErr != nil || panelResp == nil || len(panelResp.Payload) == 0 {
				// 功能已开放但入侵子活动未到开启时段时，服务端同样不回包。
				// 按“活动未开放”每 30 分钟复查，开启后自动继续推图/扫荡。
				slog.Info("山海幻兽入侵活动未开放，稍后由定时唤醒复查", "role_id", session.RoleID())
				return scheduler.NewCooldown(30*time.Minute, "山海幻兽入侵活动未开放")
			}

			// 2. 选定入侵章节并挑战 (Mod 696, Act 1 & 2)
			chapPkt := protocol.NewPacket(protocol.ActionIDHuanshouInvadeChapter, protocol.NewWriter().WriteUint32(protocol.ActionIDHuanshouPanel).Bytes())
			cCtx, cCancel := context.WithTimeout(ctx, 8*time.Second)
			chapResp, chapErr := session.Call(cCtx, chapPkt, protocol.ActionIDHuanshouInvadeChapter)
			cCancel()
			if chapErr != nil || chapResp == nil {
				if abortErr, ok := kit.IsAbortErr(chapErr); ok {
					return abortErr
				}
				slog.Info("山海幻兽入侵章节请求无回包，按连续丢包退避",
					"role_id", session.RoleID(), "err", chapErr)
				return scheduler.HandleSilentDropCtx(ctx, session, "huanshou_invade", 3,
					10*time.Minute, "山海幻兽入侵章节无回包")
			}

			chalPkt := protocol.BuildHuanshouInvadeChallengePacket(protocol.ActionIDHuanshouInvadeChapter)
			chCtx, chCancel := context.WithTimeout(ctx, 8*time.Second)
			chalResp, chalErr := session.Call(chCtx, chalPkt, protocol.ActionIDHuanshouInvadeChallenge)
			chCancel()
			if chalErr != nil || chalResp == nil {
				if abortErr, ok := kit.IsAbortErr(chalErr); ok {
					return abortErr
				}
				slog.Info("山海幻兽入侵挑战请求无回包，按连续丢包退避",
					"role_id", session.RoleID(), "err", chalErr)
				return scheduler.HandleSilentDropCtx(ctx, session, "huanshou_invade", 3,
					10*time.Minute, "山海幻兽入侵挑战无回包")
			}

			// 3. 极速秒完成扫荡 (Mod 696, Act 3)
			sweepPkt := protocol.BuildHuanshouInvadeSweepPacket(protocol.ActionIDHuanshouInvadeChallenge)
			swCtx, swCancel := context.WithTimeout(ctx, 8*time.Second)
			sweepResp, sweepErr := session.Call(swCtx, sweepPkt, protocol.ActionIDHuanshouInvadeSweep)
			swCancel()
			if sweepErr != nil || sweepResp == nil {
				if abortErr, ok := kit.IsAbortErr(sweepErr); ok {
					return abortErr
				}
				// 扫荡是每日限次玩法，服务端对「当日次数已用完/资格不满足」会静默
				// 丢弃请求。不能返回 nil（Cron 会等到次日才重试），也不能每 10 分钟
				// 无限空转：按连续丢包退避，第 3 次起挂起至 min(次日 00:05, 6h)。
				slog.Info("山海幻兽入侵扫荡请求无回包，按连续丢包退避",
					"role_id", session.RoleID(), "err", sweepErr)
				return scheduler.HandleSilentDropCtx(ctx, session, "huanshou_invade", 3,
					10*time.Minute, "山海幻兽入侵扫荡无回包")
			}
			// 扫荡回包成功说明整条链路已经打通，清零章节/挑战/扫荡的连续丢包计数。
			scheduler.ResetSilentDropsCtx(ctx, session, "huanshou_invade")

			// 4. 逐关扫荡入侵节点 (Mod 696, Act 4: 节点 1~8 依据抓包实采流)
			prevAct := protocol.ActionIDHuanshouInvadeSweep
			var clearedNodes int
			var nodeDrop int
			for nodeID := int32(1); nodeID <= 8; nodeID++ {
				nodePkt := protocol.BuildHuanshouInvadeNodePacket(nodeID, prevAct)
				nCtx, nCancel := context.WithTimeout(ctx, 8*time.Second)
				nResp, nErr := session.Call(nCtx, nodePkt, protocol.ActionIDHuanshouInvadeSettle)
				nCancel()
				if nErr != nil || nResp == nil {
					if abortErr, ok := kit.IsAbortErr(nErr); ok {
						return abortErr
					}
					nodeDrop++
					break
				}
				clearedNodes++
				prevAct = protocol.ActionIDHuanshouInvadeSettle
				if !kit.Sleep(ctx, 30*time.Millisecond) {
					break
				}
			}

			slog.Info("山海幻兽入侵扫荡轮次结束",
				"role_id", session.RoleID(),
				"cleared_nodes", clearedNodes,
				"node_drops", nodeDrop,
				"panel_reply_len", len(panelResp.Payload),
				"chapter_reply_len", len(chapResp.Payload),
				"challenge_reply_len", len(chalResp.Payload),
				"sweep_reply_len", len(sweepResp.Payload),
			)
			return nil
		},
	)
}

// NewWuFaRoutine 构造悟法领域感悟抽卡与技能配置任务 (Feature 14: 悟法领域)。
//
// 协议对齐原版 Main.swf Mod_WuFaField_Base:
//   - Action 0 panel_info 查询秘馆、资源与免费次数
//   - Action 1 draw 感悟抽卡，请求 [Int field_type, UByte draw_type]
//
// 仅使用剩余免费次数，不主动消耗元宝或感悟道具备用次数；
// 服务端返回的业务失败码全部解析并按语义熔断。
func NewWuFaRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"wufa_field",
		"minigame",
		scheduler.ScheduleCron,
		// 冥想是每日活跃度的高权重大头 (每次 +10)，提升优先级确保尽早执行。
		scheduler.PriorityHigh,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			// 0. 功能开放检查：使用 Mod 0 Act 6 的会话缓存 (一次查询覆盖整轮日常)。
			// 逐功能的 check_function_open 在部分状态下会被服务端静默丢弃，不能再作为唯一判定。
			open, chkErr := session.IsFunctionOpen(ctx, protocol.FunctionIDWuFaField)
			if chkErr != nil {
				slog.Info("查询悟法领域功能开放状态失败，继续尝试面板查询",
					"role_id", session.RoleID(), "err", chkErr)
			} else if !open {
				slog.Info("悟法领域功能未开启，跳过", "role_id", session.RoleID())
				return nil
			}
			slog.Info("正在执行益智任务: 悟法领域技能感悟与领悟", "role_id", session.RoleID())

			// 1. 查询悟法领域面板 (Mod 658, Act 0)。
			// 官方面板回包没有 result_code，只能通过结构完整性校验判断是否可信。
			infoPkt := protocol.BuildWuFaFieldInfoPacket(protocol.ActionIDTownEnter)
			iCtx, iCancel := context.WithTimeout(ctx, wuFaCallTimeout)
			infoResp, err := session.Call(iCtx, infoPkt, protocol.ActionIDWuFaFieldInfo)
			iCancel()
			if err != nil || infoResp == nil {
				slog.Info("查询悟法领域面板未收到回包", "role_id", session.RoleID(), "err", err)
				return nil
			}

			panel, err := protocol.ParseWuFaPanel(infoResp.Payload)
			if err != nil {
				slog.Warn("解析悟法领域面板回包失败",
					"role_id", session.RoleID(),
					"err", err,
					"payload_hex", fmt.Sprintf("%x", infoResp.Payload),
				)
				return fmt.Errorf("解析悟法领域面板回包失败: %w", err)
			}

			// 2. 读取每日活跃度，确认「悟法领域冥想」任务还有多少次可计入，
			//    避免免费次数/冥想道具在任务满额后被白白消耗。
			actCtx, actCancel := context.WithTimeout(ctx, wuFaCallTimeout)
			actResp, actErr := session.Call(actCtx, protocol.BuildGameAssistantInfoPacket(), protocol.ActionIDGameAssistantInfo)
			actCancel()
			if actErr != nil || actResp == nil {
				slog.Info("查询每日活跃度失败，跳过悟法领域抽卡",
					"role_id", session.RoleID(), "err", actErr)
				return nil
			}
			actPanel, actErr := protocol.ParseGameAssistantInfo(actResp.Payload)
			if actErr != nil {
				return fmt.Errorf("解析每日活跃度失败: %w", actErr)
			}
			remain := actPanel.RemainingTimesFor(wuFaDailyTaskMeditation)
			if remain <= 0 {
				slog.Info("悟法领域冥想活跃度已完成，跳过抽卡", "role_id", session.RoleID())
				return nil
			}
			if remain > maxWuFaDrawsPerRun {
				remain = maxWuFaDrawsPerRun
			}

			// 3. 逐次感悟：优先免费次数，其次使用已持有的冥想道具。
			//    线上实测道具映射：field 3 <- 冥想书卷 8641，field 1 <- 冥想竹简 8358，
			//    field 7 <- 冥想玄简 8700。全部为非元宝道具，绝不触发元宝档。
			blockedFields := make(map[int32]bool)
			for draw := 0; draw < remain; draw++ {
				if ctx.Err() != nil {
					return nil
				}
				fieldType, drawType, ok := selectWuFaFreeDraw(panel, blockedFields)
				if !ok {
					fieldType, drawType, ok = selectWuFaItemDraw(panel, blockedFields)
					if !ok {
						slog.Info("悟法领域无可用秘馆/免费次数/冥想道具，停止抽卡",
							"role_id", session.RoleID(),
							"rest_times", panel.RestTimes,
							"blocked_fields", len(blockedFields),
						)
						return nil
					}
				}

				drawPkt := protocol.BuildWuFaDrawCardPacket(fieldType, drawType)
				dCtx, dCancel := context.WithTimeout(ctx, wuFaCallTimeout)
				drawResp, err := session.Call(dCtx, drawPkt, protocol.ActionIDWuFaDrawCard)
				dCancel()
				if err != nil || drawResp == nil {
					slog.Info("悟法领域感悟抽卡未收到回包",
						"role_id", session.RoleID(),
						"field_type", fieldType,
						"draw_type", drawType,
						"err", err,
					)
					return nil
				}

				result, err := protocol.ParseWuFaDrawResult(drawResp.Payload)
				if err != nil {
					slog.Warn("解析悟法感悟抽卡回包失败",
						"role_id", session.RoleID(),
						"err", err,
						"payload_hex", fmt.Sprintf("%x", drawResp.Payload),
					)
					return fmt.Errorf("解析悟法感悟抽卡回包失败: %w", err)
				}

				if result.Result != protocol.WuFaResultSuccess {
					if result.Result == protocol.WuFaResultFunctionNotOpen && !blockedFields[fieldType] {
						// 该秘馆对当前角色未开放；换其它秘馆继续，避免整轮放弃。
						blockedFields[fieldType] = true
						slog.Info("悟法领域秘馆未开放，换下一个秘馆",
							"role_id", session.RoleID(), "field_type", fieldType,
							"blocked_fields", len(blockedFields))
						continue
					}
					err = classifyWuFaDrawResult(result.Result)
					if err == nil {
						slog.Info("悟法领域感悟抽卡无需执行",
							"role_id", session.RoleID(),
							"field_type", fieldType,
							"draw_type", drawType,
							"result", result.Result,
							"result_text", protocol.WuFaResultText(result.Result),
							"rest_times", result.RestTimes,
						)
						return nil
					}
					// 功能未开启等已归类的业务状态按 Info 记录，避免每轮产生 WARN 噪音；
					// 只有真正未识别/异常的结果码才用 WARN。
					var cdErr *scheduler.CooldownError
					if errors.As(err, &cdErr) {
						slog.Info("悟法领域感悟暂不可执行，按冷却重试",
							"role_id", session.RoleID(),
							"field_type", fieldType,
							"draw_type", drawType,
							"result", result.Result,
							"result_text", protocol.WuFaResultText(result.Result),
							"rest_times", result.RestTimes,
							"cooldown", cdErr.Reason,
						)
						return err
					}
					slog.Warn("悟法领域感悟抽卡返回业务失败",
						"role_id", session.RoleID(),
						"field_type", fieldType,
						"draw_type", drawType,
						"result", result.Result,
						"result_text", protocol.WuFaResultText(result.Result),
						"rest_times", result.RestTimes,
					)
					return err
				}

				slog.Info("悟法领域技能感悟成功",
					"role_id", session.RoleID(),
					"field_type", fieldType,
					"draw_type", drawType,
					"award_count", len(result.Awards),
					"rest_times", result.RestTimes,
				)

				// 刷新面板，确保下一轮只使用真实剩余的免费次数或道具。
				refreshPkt := protocol.BuildWuFaFieldInfoPacket(protocol.ActionIDWuFaDrawCard)
				rCtx, rCancel := context.WithTimeout(ctx, wuFaCallTimeout)
				refreshResp, rErr := session.Call(rCtx, refreshPkt, protocol.ActionIDWuFaFieldInfo)
				rCancel()
				if rErr != nil || refreshResp == nil {
					return nil
				}
				refreshed, rErr := protocol.ParseWuFaPanel(refreshResp.Payload)
				if rErr != nil {
					return nil
				}
				panel = refreshed
			}
			return nil
		},
	)
}

// 悟法领域冥想相关常量。
const (
	// wuFaCallTimeout 悟法领域单次协议调用超时。实测网关响应在 2s 上下波动，
	// 过短的超时会把"功能已开放"误判为静默丢弃，导致例程整轮跳过。
	wuFaCallTimeout = 8 * time.Second
	// wuFaDailyTaskMeditation 每日活跃度任务「每日悟法领域冥想」编号
	wuFaDailyTaskMeditation int32 = 69
	// wuFaMeditationBookItem 冥想书卷 (对应 field_type 3)
	wuFaMeditationBookItem int32 = 8641
	// wuFaMeditationBambooItem 冥想竹简 (对应 field_type 1)
	wuFaMeditationBambooItem int32 = 8358
	// wuFaMeditationXuanItem 冥想玄简 (对应 field_type 7)
	wuFaMeditationXuanItem int32 = 8700
	// maxWuFaDrawsPerRun 单轮最多感悟次数 (每日任务上限 20)
	maxWuFaDrawsPerRun = 20
)

// wuFaItemFields 冥想道具与秘馆类型映射 (线上实测)。
var wuFaItemFields = []struct {
	fieldType int32
	itemID    int32
}{
	{3, wuFaMeditationBookItem},
	{1, wuFaMeditationBambooItem},
	{7, wuFaMeditationXuanItem},
}

// selectWuFaItemDraw 选择可用冥想道具对应的秘馆，优先书卷→竹简→玄简。
func selectWuFaItemDraw(panel *protocol.WuFaPanel, blocked ...map[int32]bool) (int32, uint8, bool) {
	skip := map[int32]bool(nil)
	if len(blocked) > 0 {
		skip = blocked[0]
	}
	for _, m := range wuFaItemFields {
		if skip[m.fieldType] {
			continue
		}
		if hasWuFaItem(panel, m.itemID) {
			return m.fieldType, protocol.WuFaDrawOne, true
		}
	}
	return 0, 0, false
}

// hasWuFaItem 判断面板资源中指定道具是否有余量。
func hasWuFaItem(panel *protocol.WuFaPanel, itemID int32) bool {
	if panel == nil {
		return false
	}
	for _, item := range panel.Items {
		if item.ItemID == itemID {
			return item.ItemNum > 0
		}
	}
	return false
}

// selectWuFaFreeDraw 从面板中挑选免费次数最多的秘馆与抽卡类型。
//
// 优先使用免费十连，其次免费单抽；同等次数下选择更高 field_type 的秘馆。
// 无任何免费次数时返回 ok=false，由调用方跳过抽卡，避免消耗元宝或感悟道具。
func selectWuFaFreeDraw(panel *protocol.WuFaPanel, blocked ...map[int32]bool) (fieldType int32, drawType uint8, ok bool) {
	if panel == nil {
		return 0, 0, false
	}
	skip := map[int32]bool(nil)
	if len(blocked) > 0 {
		skip = blocked[0]
	}

	bestTenField, bestTenCount := int32(0), int32(0)
	bestOneField, bestOneCount := int32(0), int32(0)
	for _, field := range panel.Fields {
		if skip[field.FieldType] {
			continue
		}
		if field.LeftFreeTenCount > 0 &&
			(field.LeftFreeTenCount > bestTenCount || (field.LeftFreeTenCount == bestTenCount && field.FieldType > bestTenField)) {
			bestTenField, bestTenCount = field.FieldType, field.LeftFreeTenCount
		}
		if field.LeftFreeOneCount > 0 &&
			(field.LeftFreeOneCount > bestOneCount || (field.LeftFreeOneCount == bestOneCount && field.FieldType > bestOneField)) {
			bestOneField, bestOneCount = field.FieldType, field.LeftFreeOneCount
		}
	}

	if bestTenCount > 0 {
		return bestTenField, protocol.WuFaDrawTen, true
	}
	if bestOneCount > 0 {
		return bestOneField, protocol.WuFaDrawOne, true
	}
	return 0, 0, false
}

// classifyWuFaDrawResult 将官方悟法结果码映射为调度器可识别的业务语义。
//
// 成功与幂等状态（已领取/已拥有/已满级）返回 nil；
// 次数类失败触发次日熔断，背包满触发短冷却，其余条件不满足触发条件熔断，
// 未知或协议错误返回普通错误以终止本轮，避免盲目重发。
func classifyWuFaDrawResult(code uint8) error {
	switch code {
	case protocol.WuFaResultSuccess,
		protocol.WuFaResultOverGet,
		protocol.WuFaResultIsBuy,
		protocol.WuFaResultIsHave,
		protocol.WuFaResultFullGrade:
		return nil
	case protocol.WuFaResultCountLimit,
		protocol.WuFaResultNotEnoughCount,
		protocol.WuFaResultWeekNumLimit,
		protocol.WuFaResultNoEnoughDrawTimes:
		return scheduler.ErrAttemptsExhausted
	case protocol.WuFaResultItemPackFull:
		return scheduler.ErrBagFull
	case protocol.WuFaResultFunctionNotOpen:
		// 功能可能稍后开启，按活动开启时间重试而不是等 24 小时。
		return scheduler.NewCooldown(time.Hour, "悟法领域功能未开启")
	case protocol.WuFaResultNotEnoughIngot,
		protocol.WuFaResultNotEnoughResourse,
		protocol.WuFaResultNotEnoughResource,
		protocol.WuFaResultNotEnoughRecharge:
		return scheduler.ErrConditionNotMet
	default:
		return fmt.Errorf("悟法领域感悟失败: result=%d (%s)", code, protocol.WuFaResultText(code))
	}
}
