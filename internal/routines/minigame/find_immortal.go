// Package minigame 提供吉星高照、仙旅奇缘答题、喜从天降、刮刮卡、钓鱼等益智趣味活动。
//
// 本文件实现喜从天降、五福临门、问仙、点金石与禁地之书自动化。
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

// NewFindImmortalRoutine 构造喜从天降画龙鱼任务 (只走免费流程，不消耗元宝)。
//
// 协议对齐 2026 客户端 Mod_FindImmortal_Base:
//   - Action 1 open_find_immortal 查询面板
//   - Action 2 start_find_immortal 召唤龙鱼 (消耗龙鱼令)
//   - Action 4 pickup_award 收取龙鱼奖励
//   - Action 5 get_xun_xian_ling_number 查询龙鱼令
//
// 流程固定为：查询龙鱼令数量 -> 串行召唤并领取 -> 直到龙鱼令耗尽。
// 不调用 goad (吹龙鱼) 与 exchange_xian_ling (声望兑换)，因此不会消耗任何资源。
func NewFindImmortalRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"find_immortal",
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
			slog.Info("正在执行益智任务: 喜从天降画龙鱼 (免费流程)", "role_id", session.RoleID())

			executeFindImmortalFish(ctx, session, jitter)
			return nil
		},
	)
}

// executeFindImmortalFish 执行龙鱼召唤与免费奖励领取闭环。
func executeFindImmortalFish(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) {
	prevAct := session.LastActionID()

	// 1. 查询面板，优先处理上次未收取的龙鱼 (Mod 68, Act 1)
	panelPkt := protocol.BuildFindImmortalOpenPacket(prevAct)
	pCtx, pCancel := context.WithTimeout(ctx, 8*time.Second)
	pResp, pErr := session.Call(pCtx, panelPkt, protocol.ActionIDFindImmortalOpen)
	pCancel()
	if pErr != nil || pResp == nil {
		slog.Info("查询喜从天降面板未收到回包", "role_id", session.RoleID(), "err", pErr)
		return
	}
	panel, err := protocol.ParseFindImmortalPanelResult(pResp.Payload)
	if err != nil {
		slog.Warn("解析喜从天降面板回包失败", "role_id", session.RoleID(), "err", err)
		return
	}
	prevAct = protocol.ActionIDFindImmortalOpen

	// 2. 查询龙鱼令数量 (Mod 68, Act 5)。
	// 查询失败时不再回退使用面板 can_buy_number，避免把“可购买次数”误判为免费龙鱼令而触发消费。
	tokens, ok := queryFindImmortalTokens(ctx, session, prevAct)
	if ok {
		prevAct = protocol.ActionIDFindImmortalGetXunXianLing
	} else {
		tokens = 0
		slog.Warn("喜从天降龙鱼令查询失败，本轮回跳过召唤新龙鱼", "role_id", session.RoleID())
	}

	slog.Info("喜从天降当前面板",
		"role_id", session.RoleID(),
		"can_buy_number", panel.CanBuyNumber,
		"state", panel.State,
		"god_id", panel.GodID,
		"total_count", panel.TotalCount,
		"xun_xian_ling", tokens,
	)

	// 优先收掉上次遗留的龙鱼；只领取当前奖励，不吹龙鱼。
	if panel.State > 0 {
		pickupFindImmortal(ctx, session, &prevAct)
	}
	if tokens <= 0 {
		slog.Info("喜从天降龙鱼令不足，跳过召唤新龙鱼", "role_id", session.RoleID())
		return
	}

	// 3. 串行召唤并直接领取龙鱼奖励，直到龙鱼令耗尽 (Mod 68, Act 2 + Act 4)。
	// 每枚龙鱼令至多支撑一次召唤，用初始数量做硬上限，防止异常回包导致死循环。
	initialTokens := tokens
	rounds := 0
	for tokens > 0 && rounds < int(initialTokens) {
		if err := ctx.Err(); err != nil {
			slog.Info("喜从天降任务已取消，提前退出", "role_id", session.RoleID(), "err", err)
			return
		}
		if jitter != nil {
			_ = jitter.Wait(ctx)
		}

		beforeTokens := tokens
		start, sErr := summonFindImmortalFish(ctx, session, prevAct)
		if sErr != nil {
			slog.Info("喜从天降召唤龙鱼未收到回包或解析失败", "role_id", session.RoleID(), "err", sErr)
			return
		}
		prevAct = protocol.ActionIDFindImmortalStart

		// GOADING 表示服务端仍存在未领取的龙鱼 (例如上一轮熔断遗留)，
		// 先收取清空后再重试一次召唤。
		if start.Result == protocol.FindImmortalResultGoading {
			slog.Warn("喜从天降存在未领取的龙鱼，先收取清空后重试召唤", "role_id", session.RoleID())
			pickupFindImmortal(ctx, session, &prevAct)
			start, sErr = summonFindImmortalFish(ctx, session, prevAct)
			if sErr != nil {
				slog.Warn("喜从天降清空遗留龙鱼后重试召唤失败", "role_id", session.RoleID(), "err", sErr)
				return
			}
			prevAct = protocol.ActionIDFindImmortalStart
		}

		if start.Result != protocol.FindImmortalResultSuccess {
			logFindImmortalResult("召唤龙鱼", session.RoleID(), start.Result)
			break
		}

		slog.Info("喜从天降召唤龙鱼成功",
			"role_id", session.RoleID(),
			"state", start.State,
			"god_id", start.GodID,
			"remain_number", start.RemainNumber,
			"total_count", start.TotalCount,
		)

		// 线上抓包确认 start 回包中的 remain_number 是“剩余吹鱼次数”(随鱼随机，
		// 12:57 回包为 4 而同轮 Act 5 查询为 0)，与龙鱼令余量无关，不能作为
		// 循环依据；每轮召唤成功后统一回查 Act 5 取得权威余量。
		refreshed, refreshOK := queryFindImmortalTokens(ctx, session, prevAct)
		if refreshOK {
			prevAct = protocol.ActionIDFindImmortalGetXunXianLing
		}
		nextTokens, progressed := resolveFindImmortalRemain(beforeTokens, refreshed, refreshOK)
		if !progressed {
			slog.Warn("喜从天降召唤后龙鱼令未按预期递减，熔断退出",
				"role_id", session.RoleID(),
				"before_xun_xian_ling", beforeTokens,
				"queried_xun_xian_ling", refreshed,
				"query_ok", refreshOK,
			)
		}

		// 已成功召唤的龙鱼必须领取，避免回包异常时把龙鱼遗留在面板上。
		pickupFindImmortal(ctx, session, &prevAct)
		rounds++

		tokens = nextTokens
		if !progressed {
			break
		}
	}

	slog.Info("喜从天降画龙鱼流程完成",
		"role_id", session.RoleID(),
		"initial_xun_xian_ling", initialTokens,
		"rounds", rounds,
		"remain_xun_xian_ling", tokens,
	)
}

// summonFindImmortalFish 发送一次召唤龙鱼请求 (Mod 68, Act 2) 并解析业务结果码。
func summonFindImmortalFish(ctx context.Context, session *client.RoleSession, prevAct uint32) (*protocol.FindImmortalStartResult, error) {
	pkt := protocol.BuildFindImmortalStartPacket(prevAct)
	sCtx, sCancel := context.WithTimeout(ctx, 8*time.Second)
	defer sCancel()

	resp, err := session.Call(sCtx, pkt, protocol.ActionIDFindImmortalStart)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("召唤龙鱼未收到回包")
	}
	start, parseErr := protocol.ParseFindImmortalStartResult(resp.Payload)
	if parseErr != nil {
		return nil, parseErr
	}
	return start, nil
}

// pickupFindImmortal 收取当前龙鱼奖励 (Mod 68, Act 4)，不调用 any 元宝消耗接口。
func pickupFindImmortal(ctx context.Context, session *client.RoleSession, prevAct *uint32) {
	if err := ctx.Err(); err != nil {
		return
	}
	pickupPkt := protocol.BuildFindImmortalPickupAwardPacket(*prevAct)
	kCtx, kCancel := context.WithTimeout(ctx, 8*time.Second)
	kResp, kErr := session.Call(kCtx, pickupPkt, protocol.ActionIDFindImmortalPickupAward)
	kCancel()
	*prevAct = protocol.ActionIDFindImmortalPickupAward
	if kErr != nil || kResp == nil {
		slog.Info("喜从天降收取龙鱼奖励未收到回包", "role_id", session.RoleID(), "err", kErr)
		return
	}
	pickup, err := protocol.ParseFindImmortalPickupResult(kResp.Payload)
	if err != nil {
		slog.Warn("解析喜从天降收取龙鱼奖励回包失败", "role_id", session.RoleID(), "err", err)
		return
	}
	if pickup.Result != protocol.FindImmortalResultSuccess {
		logFindImmortalResult("收取龙鱼奖励", session.RoleID(), pickup.Result)
		return
	}
	slog.Info("喜从天降收取龙鱼奖励成功",
		"role_id", session.RoleID(),
		"award_fame", pickup.Award.Fame,
		"award_skill", pickup.Award.Skill,
		"award_cultivation", pickup.Award.Cultivation,
		"award_state_point", pickup.Award.StatePoint,
		"award_yu_pai", pickup.Award.YuPai,
		"cultivation", pickup.Cultivation,
	)
}

// resolveFindImmortalRemain 依据召唤后的龙鱼令回查结果裁决本轮是否继续召唤。
//
// start_find_immortal 回包中的 remain_number 属于“剩余吹鱼次数”一类随鱼随机
// 的值，与龙鱼令余量无关 (线上抓包: 召唤前查询 1，召唤后回包 3，同轮回查 0)，
// 因此必须使用 get_xun_xian_ling_number 的权威回查结果。回查失败或余量未递减
// 时返回召唤前数量并停止继续召唤，交由调用方熔断退出。
func resolveFindImmortalRemain(before, refreshed int16, refreshedOK bool) (int16, bool) {
	if !refreshedOK || refreshed < 0 {
		return before, false
	}
	return refreshed, refreshed < before
}

// queryFindImmortalTokens 查询龙鱼令数量 (Mod 68, Act 5)。
func queryFindImmortalTokens(ctx context.Context, session *client.RoleSession, prevAct uint32) (int16, bool) {
	pkt := protocol.BuildFindImmortalGetXunXianLingPacket(prevAct)
	qCtx, qCancel := context.WithTimeout(ctx, 8*time.Second)
	resp, err := session.Call(qCtx, pkt, protocol.ActionIDFindImmortalGetXunXianLing)
	qCancel()
	if err != nil || resp == nil {
		slog.Info("查询喜从天降龙鱼令数量未收到回包", "role_id", session.RoleID(), "err", err)
		return 0, false
	}
	tokens, err := protocol.ParseFindImmortalXunXianLingResult(resp.Payload)
	if err != nil {
		slog.Warn("解析喜从天降龙鱼令数量回包失败", "role_id", session.RoleID(), "err", err)
		return 0, false
	}
	return tokens, true
}

// NewFiveBlessingsRoutine 构造独立的「五福临门」免费祈福任务。
//
// 五福临门 (客户端 FindImmortal2) 与喜从天降共用 Module 68，但属于两个独立活动。
// 本任务硬编码只使用每日免费祈福次数：start_bless -> end_bless，
// 不调用 continue_bless (继续翻牌) 与 five_blessings_change_lucky (一键变金)，
// 因此不会消耗任何元宝。
func NewFiveBlessingsRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"five_blessings",
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
			if open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDFiveBlessings); fErr != nil {
				slog.Info("五福临门功能开放状态查询失败，本轮跳过", "role_id", session.RoleID(), "err", fErr)
				return nil
			} else if !open {
				slog.Info("五福临门功能未开放，跳过", "role_id", session.RoleID(), "function_id", protocol.FunctionIDFiveBlessings)
				return nil
			}
			slog.Info("正在执行益智任务: 五福临门免费祈福", "role_id", session.RoleID())

			return executeFiveBlessings(ctx, session, jitter)
		},
	)
}

// executeFiveBlessings 执行五福临门免费次数与既有奖励领取闭环。
func executeFiveBlessings(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) (runErr error) {
	prevAct := session.LastActionID()

	// 1. 不再调用 Act 14 is_five_blessings_open。
	//
	// 审计文档（docs/research/s0-daily-activity-1200-audit.md 第 7 节）与线上
	// 2026-11-07 21:42 实测均确认：Act 14 在活动实际开启时仍返回 12(NO)，
	// 且该动作会污染同一会话的动作链，紧随其后的 start_bless 被静默丢弃
	// (context deadline exceeded)。因此这里直接跳过 Act 14，以 Act 10 面板
	// bless_number 作为唯一门禁。
	prevAct = session.LastActionID()

	// 2. 查询祈福面板 (Mod 68, Act 10)
	infoPkt := protocol.BuildFindImmortalOpenFiveBlessingsPacket(prevAct)
	iCtx, iCancel := context.WithTimeout(ctx, 8*time.Second)
	iResp, iErr := session.Call(iCtx, infoPkt, protocol.ActionIDFindImmortalOpenFiveBlessings)
	iCancel()
	if iErr != nil || iResp == nil {
		slog.Info("查询五福临门面板未收到回包，稍后重试", "role_id", session.RoleID(), "err", iErr)
		return scheduler.NewCooldown(10*time.Minute, "五福临门面板无回包")
	}
	info, err := protocol.ParseFiveBlessingsInfoResult(iResp.Payload)
	if err != nil {
		slog.Warn("解析五福临门面板回包失败", "role_id", session.RoleID(), "err", err)
		return
	}
	prevAct = protocol.ActionIDFindImmortalOpenFiveBlessings
	slog.Info("五福临门当前面板",
		"role_id", session.RoleID(),
		"bless_number", info.BlessNumber,
		"stage", info.Stage,
		"pending_awards", len(info.Awards),
		"total_count", info.TotalCount,
	)

	// 先领取上次遗留的祈福奖励。
	if len(info.Awards) > 0 {
		endFiveBlessings(ctx, session, prevAct, "领取遗留祈福奖励")
		prevAct = protocol.ActionIDFindImmortalEndBless
	}
	if info.BlessNumber <= 0 {
		slog.Info("五福临门祈福次数已用尽", "role_id", session.RoleID())
		return
	}

	// 3. 逐次开始祈福并立即领取 (Mod 68, Act 11 + Act 13)
	rounds := 0
	for info.BlessNumber > 0 {
		if err := ctx.Err(); err != nil {
			slog.Info("五福临门任务已取消，提前退出", "role_id", session.RoleID(), "err", err)
			return
		}
		if jitter != nil {
			_ = jitter.Wait(ctx)
		}

		startPkt := protocol.BuildFindImmortalStartBlessPacket(prevAct)
		sCtx, sCancel := context.WithTimeout(ctx, 8*time.Second)
		sResp, sErr := session.Call(sCtx, startPkt, protocol.ActionIDFindImmortalStartBless)
		sCancel()
		prevAct = protocol.ActionIDFindImmortalStartBless
		if sErr != nil || sResp == nil {
			slog.Info("五福临门开始祈福请求未收到回包，稍后重试", "role_id", session.RoleID(), "err", sErr)
			return scheduler.NewCooldown(30*time.Minute, "五福临门开始祈福无回包")
		}
		start, err := protocol.ParseFiveBlessingsActionResult(sResp.Payload)
		if err != nil {
			slog.Warn("解析五福临门开始祈福回包失败", "role_id", session.RoleID(), "err", err)
			return
		}
		if start.Result != protocol.FindImmortalResultSuccess {
			if start.Result == protocol.FindImmortalResultAlreadyStart {
				// 服务端仍有一次未结算的祈福，先结束并领取，交由下一轮调度继续。
				endFiveBlessings(ctx, session, prevAct, "结算未完成祈福")
			} else {
				logFindImmortalResult("开始五福临门祈福", session.RoleID(), start.Result)
			}
			return
		}
		info.BlessNumber--
		rounds++

		endFiveBlessings(ctx, session, prevAct, "领取五福临门奖励")
		prevAct = protocol.ActionIDFindImmortalEndBless

		if info.BlessNumber > 0 && jitter != nil {
			_ = jitter.Wait(ctx)
		}
	}

	slog.Info("五福临门流程完成", "role_id", session.RoleID(), "rounds", rounds, "remain_bless_number", info.BlessNumber)
	return
}

// endFiveBlessings 结束当前祈福并领取奖励 (Mod 68, Act 13)。
func endFiveBlessings(ctx context.Context, session *client.RoleSession, prevAct uint32, scene string) {
	endPkt := protocol.BuildFindImmortalEndBlessPacket(prevAct)
	eCtx, eCancel := context.WithTimeout(ctx, 8*time.Second)
	eResp, eErr := session.Call(eCtx, endPkt, protocol.ActionIDFindImmortalEndBless)
	eCancel()
	if eErr != nil || eResp == nil {
		slog.Info("五福临门结束祈福请求未收到回包", "role_id", session.RoleID(), "scene", scene, "err", eErr)
		return
	}
	end, err := protocol.ParseFiveBlessingsEndResult(eResp.Payload)
	if err != nil {
		slog.Warn("解析五福临门结束祈福回包失败", "role_id", session.RoleID(), "scene", scene, "err", err)
		return
	}
	if end.Result != protocol.FindImmortalResultSuccess {
		logFindImmortalResult(scene, session.RoleID(), end.Result)
		return
	}
	slog.Info("五福临门奖励领取成功",
		"role_id", session.RoleID(),
		"scene", scene,
		"award_count", len(end.Awards),
	)
}

// logFindImmortalResult 输出喜从天降/五福临门业务失败码的中文语义。
func logFindImmortalResult(scene string, roleID string, result uint8) {
	switch result {
	case protocol.FindImmortalResultNotEnoughCoin:
		slog.Info("喜从天降铜钱不足", "role_id", roleID, "scene", scene, "result", result)
	case protocol.FindImmortalResultHaveNotNumber:
		slog.Info("喜从天降龙鱼不足", "role_id", roleID, "scene", scene, "result", result)
	case protocol.FindImmortalResultGoading:
		slog.Info("喜从天降正在吹龙鱼", "role_id", roleID, "scene", scene, "result", result)
	case protocol.FindImmortalResultNotEnoughIngot:
		slog.Info("喜从天降元宝不足", "role_id", roleID, "scene", scene, "result", result)
	case protocol.FindImmortalResultFailed:
		slog.Info("喜从天降吹龙鱼失败", "role_id", roleID, "scene", scene, "result", result)
	case protocol.FindImmortalResultHaveNotFind:
		slog.Info("喜从天降还未寻找到仙迹", "role_id", roleID, "scene", scene, "result", result)
	case protocol.FindImmortalResultNoEnoughFame:
		slog.Info("喜从天降声望不足", "role_id", roleID, "scene", scene, "result", result)
	case protocol.FindImmortalResultNotEnoughNumber:
		slog.Info("五福临门祈福次数不足", "role_id", roleID, "scene", scene, "result", result)
	case protocol.FindImmortalResultAlreadyStart:
		slog.Info("五福临门已经开始祈福", "role_id", roleID, "scene", scene, "result", result)
	case protocol.FindImmortalResultNotStart:
		slog.Info("五福临门尚未开始祈福", "role_id", roleID, "scene", scene, "result", result)
	default:
		slog.Warn("喜从天降/五福临门操作被服务端拒绝", "role_id", roleID, "scene", scene, "result", result)
	}
}

// NewAskFairyRoutine 构造每日问灵任务 (Module 503, Mod_AskFairy_Base)。
//
// 每日免费问灵一次；服务端下发随机奖励候选，优先选择元宝 (item_id=3345)，
// 否则选第一项。同时领取可领的回归奖励与问灵等级奖励。全程不消耗元宝。
func NewAskFairyRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"ask_fairy",
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

			fetchInfo := func() (*protocol.AskFairyInfo, error) {
				cCtx, cancel := context.WithTimeout(ctx, callTimeout)
				resp, err := session.Call(cCtx, protocol.BuildAskFairyInfoPacket(), protocol.ActionIDAskFairyInfo)
				cancel()
				if err != nil {
					return nil, err
				}
				return protocol.ParseAskFairyInfoResult(resp.Payload)
			}
			callAction := func(pkt *protocol.Packet, actionID uint32, name string) (uint8, error) {
				cCtx, cancel := context.WithTimeout(ctx, callTimeout)
				resp, err := session.Call(cCtx, pkt, actionID)
				cancel()
				if err != nil {
					return 0, err
				}
				result, pErr := protocol.ParseAskFairyActionResult(resp.Payload)
				if pErr != nil {
					return 0, pErr
				}
				_ = name
				return result, nil
			}

			info, err := fetchInfo()
			if err != nil {
				// 活动未开放/已结束时服务端会静默丢弃信息查询；稍后重试而非等 24 小时。
				slog.Info("问灵活动未开放或状态查询被服务端丢弃，稍后重试",
					"role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(30*time.Minute, "问灵状态查询无回包")
			}
			if info.LeftDay <= 0 {
				slog.Info("问灵活动已结束，跳过", "role_id", session.RoleID(), "left_day", info.LeftDay)
				return nil
			}
			slog.Info("问灵状态",
				"role_id", session.RoleID(),
				"has_gain_award", info.HasGainAward,
				"max_ask_num", info.MaxAskNum,
				"has_ask_num", info.HasAskNum,
				"cur_level", info.CurLevel,
				"left_day", info.LeftDay,
				"random_awards", info.RandomAwards,
			)

			// 回归奖励 (已领过返回 HAS_GAIN_RECALL=2)
			if info.HasGainAward == 0 {
				result, err := callAction(protocol.BuildAskFairyRecallAwardPacket(), protocol.ActionIDAskFairyRecallAward, "回归奖励")
				if err != nil {
					slog.Warn("领取回归奖励失败", "role_id", session.RoleID(), "err", err)
				} else if result == protocol.AskFairyResultPackFull {
					return scheduler.ErrBagFull
				} else {
					slog.Info("回归奖励领取结果", "role_id", session.RoleID(), "result", result)
				}
			}

			// 问灵等级奖励 (登录天数不足返回 9，属正常)
			if result, err := callAction(protocol.BuildAskFairyLevelAwardPacket(), protocol.ActionIDAskFairyLevelAward, "问灵等级奖励"); err != nil {
				slog.Warn("领取问灵等级奖励失败", "role_id", session.RoleID(), "err", err)
			} else if result == protocol.AskFairyResultSuccess {
				slog.Info("问灵等级奖励领取成功", "role_id", session.RoleID(), "cur_level", info.CurLevel)
			} else {
				slog.Debug("问灵等级奖励未领取", "role_id", session.RoleID(), "result", result, "cur_level", info.CurLevel)
			}

			// 每日问灵
			if info.HasAskNum >= info.MaxAskNum {
				slog.Info("今日问灵次数已用尽", "role_id", session.RoleID(), "has_ask", info.HasAskNum, "max_ask", info.MaxAskNum)
				return nil
			}
			result, err := callAction(protocol.BuildAskFairyDoPacket(), protocol.ActionIDAskFairyDo, "问灵")
			if err != nil {
				slog.Info("问灵请求无回包，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(30*time.Minute, "问灵请求无回包")
			}
			switch result {
			case protocol.AskFairyResultSuccess:
				slog.Info("问灵成功", "role_id", session.RoleID())
			case protocol.AskFairyResultHasAsk, protocol.AskFairyResultCountLimit:
				slog.Info("今日问灵次数已用尽", "role_id", session.RoleID(), "result", result)
				return nil
			case protocol.AskFairyResultPackFull, protocol.AskFairyResultLinkFatePackFull:
				return scheduler.ErrBagFull
			default:
				// 线上该账号持续返回 FAILED(3)（面板显示今日问灵 0/1 次、
				// 官方客户端同样是空请求），判定为服务端资格限制：2 小时后再试，
				// 避免每轮批次都重复请求并刷 WARN。
				slog.Warn("问灵失败", "role_id", session.RoleID(), "result", result)
				return scheduler.NewCooldown(2*time.Hour, "问灵失败")
			}

			// 重新拉取随机奖励候选，优先选择元宝
			info, err = fetchInfo()
			if err != nil {
				slog.Info("问灵后刷新状态无回包，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(10*time.Minute, "问灵后刷新状态无回包")
			}
			if len(info.RandomAwards) == 0 {
				slog.Warn("问灵后未下发随机奖励候选", "role_id", session.RoleID())
				return nil
			}
			chosen := info.RandomAwards[0]
			for _, a := range info.RandomAwards {
				if a.ItemID == 3345 { // 元宝
					chosen = a
					break
				}
			}
			result, err = callAction(protocol.BuildAskFairyGainPacket(chosen.AwardID), protocol.ActionIDAskFairyGain, "问灵奖励")
			if err != nil {
				return fmt.Errorf("领取问灵奖励失败: %w", err)
			}
			switch result {
			case protocol.AskFairyResultSuccess:
				slog.Info("问灵奖励领取成功",
					"role_id", session.RoleID(),
					"award_id", chosen.AwardID,
					"item_id", chosen.ItemID,
					"item_count", chosen.ItemCount,
				)
			case protocol.AskFairyResultPackFull, protocol.AskFairyResultLinkFatePackFull:
				return scheduler.ErrBagFull
			default:
				slog.Info("问灵奖励未领取", "role_id", session.RoleID(), "result", result)
			}
			return nil
		},
	)
}

// NewGoldenTouchStoneRoutine 构造点金石任务 (Module 551, Mod_GoldenTouchStone_Base)。
//
// 先查询点金石状态：待初始化时初始化；待分享/帮助不足时跳过；可点化时读取面板并点化一次。
// 不消耗元宝。
func NewGoldenTouchStoneRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"golden_touch_stone",
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
			call := func(pkt *protocol.Packet, act uint32) (*protocol.Packet, error) {
				return caller.Call(pkt, act)
			}

			queryStatus := func() (uint8, error) {
				statusResp, qErr := call(protocol.BuildGoldenTouchStoneStatusPacket(), protocol.ActionIDGoldenTouchStoneStatus)
				if qErr != nil || statusResp == nil || len(statusResp.Payload) == 0 {
					return 0, qErr
				}
				return protocol.ParseGoldenTouchStoneStatusResult(statusResp.Payload)
			}

			status, err := queryStatus()
			if err != nil {
				if abortErr, ok := kit.IsAbortErr(err); ok {
					return abortErr
				}
				slog.Info("点金石功能未开放/不可用，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(30*time.Minute, "点金石状态查询无回包")
			}

			if status == protocol.GoldenTouchStoneStatusInit {
				initResp, iErr := call(protocol.BuildGoldenTouchStoneInitPacket(), protocol.ActionIDGoldenTouchStoneInit)
				if iErr != nil || initResp == nil || len(initResp.Payload) == 0 {
					if abortErr, ok := kit.IsAbortErr(iErr); ok {
						return abortErr
					}
					slog.Info("点金石初始化无应答，稍后重试", "role_id", session.RoleID(), "err", iErr)
					return scheduler.NewCooldown(30*time.Minute, "点金石初始化无回包")
				}
				initResult := initResp.Payload[0]
				if initResult != protocol.GoldenTouchStoneResultSuccess {
					// 线上 init_player_panel 返回 FAIL(4) 时状态会一直停在 INIT_STATUS，
					// 继续轮询没有意义；按冷却重试，避免整日错过。
					slog.Info("点金石初始化未成功，稍后重试",
						"role_id", session.RoleID(), "result", initResult)
					return scheduler.NewCooldown(time.Hour, "点金石初始化失败")
				}
				slog.Info("点金石初始化完成，等待状态刷新", "role_id", session.RoleID())
				// 初始化由服务端异步落库，立即查询经常仍返回 INIT_STATUS；
				// 官方客户端是剧情结束后再打开面板，这里短暂轮询几次。
				for i := 0; i < 5; i++ {
					select {
					case <-ctx.Done():
						return nil
					case <-time.After(300 * time.Millisecond):
					}
					next, qErr := queryStatus()
					if qErr == nil {
						status = next
					}
					if qErr == nil && status != protocol.GoldenTouchStoneStatusInit {
						break
					}
				}
			}

			switch status {
			case protocol.GoldenTouchStoneStatusShare:
				slog.Info("点金石当前待分享/帮助，跳过（需好友协助）", "role_id", session.RoleID())
				return nil
			case protocol.GoldenTouchStoneStatusTouch:
				// 继续
			default:
				slog.Info("点金石状态未就绪，稍后重试", "role_id", session.RoleID(), "status", status)
				return scheduler.NewCooldown(30*time.Minute, "点金石状态未就绪")
			}

			panelResp, err := call(protocol.BuildGoldenTouchStoneTouchPanelPacket(), protocol.ActionIDGoldenTouchStoneTouchPanel)
			if err != nil || panelResp == nil {
				slog.Info("点金石点化面板无应答，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			if panel, pErr := protocol.ParseGoldenTouchStoneTouchPanelResult(panelResp.Payload); pErr == nil {
				slog.Info("点金石面板",
					"role_id", session.RoleID(),
					"level", panel.Level,
					"exp", panel.Exp,
					"this_week_luck", panel.ThisWeekLuckValue,
					"today_touch_num", panel.TodayTouchNum,
				)
			}

			touchResp, err := call(protocol.BuildGoldenTouchStoneTouchPacket(), protocol.ActionIDGoldenTouchStoneTouch)
			if err != nil || touchResp == nil {
				slog.Info("点金石点化无应答", "role_id", session.RoleID(), "err", err)
				return nil
			}
			result, exp, luck, pErr := protocol.ParseGoldenTouchStoneTouchResult(touchResp.Payload)
			if pErr != nil {
				slog.Warn("解析点金石点化回包失败", "role_id", session.RoleID(), "err", pErr)
				return nil
			}
			switch result {
			case protocol.GoldenTouchStoneResultSuccess:
				slog.Info("点金石点化成功", "role_id", session.RoleID(), "exp", exp, "luck", luck)
			case protocol.GoldenTouchStoneResultNotEnoughHelp:
				slog.Info("点金石帮助人数不足，跳过", "role_id", session.RoleID())
			case protocol.GoldenTouchStoneResultNotEnoughCount:
				slog.Info("点金石今日次数已用尽", "role_id", session.RoleID())
			case protocol.GoldenTouchStoneResultHasHelp:
				slog.Info("点金石已帮助过", "role_id", session.RoleID())
			default:
				slog.Info("点金石点化未成功", "role_id", session.RoleID(), "result", result)
			}
			return nil
		},
	)
}

// NewForbiddenBookRoutine 构造禁地之书探索任务 (Module 199, Mod_Awake_Base)。
//
// 优先使用各区域免费探索次数；免费次数用尽且仅需铜钱时，按铜钱兑换少量次数继续探索。
// 永不使用元宝兑换；功能未开放/元宝付费路径直接跳过。
func NewForbiddenBookRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"forbidden_book",
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

			// 功能未开放时服务端会静默丢弃 Module 199 请求，先用 Action 6 缓存判定。
			open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDAwakeForbiddenBook)
			if fErr != nil {
				if abortErr, ok := kit.IsAbortErr(fErr); ok {
					return abortErr
				}
				slog.Info("禁地之书功能开放状态查询失败，跳过",
					"role_id", session.RoleID(), "err", fErr)
				return nil
			}
			if !open {
				slog.Info("禁地之书功能未开放，跳过",
					"role_id", session.RoleID(), "function_id", protocol.FunctionIDAwakeForbiddenBook)
				return nil
			}

			caller := kit.NewCaller(ctx, session, callTimeout)
			call := func(pkt *protocol.Packet, act uint32) (*protocol.Packet, error) {
				return caller.Call(pkt, act)
			}
			fetchInfo := func() (*protocol.ForbiddenBookInfo, error) {
				resp, err := call(protocol.BuildForbiddenBookInfoPacket(), protocol.ActionIDForbiddenBookInfo)
				if err != nil {
					return nil, err
				}
				return protocol.ParseForbiddenBookInfoResult(resp.Payload)
			}

			info, err := fetchInfo()
			if err != nil || info == nil || len(info.Areas) == 0 {
				if abortErr, ok := kit.IsAbortErr(err); ok {
					return abortErr
				}
				slog.Info("禁地之书功能未开放/无可用区域，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			slog.Info("禁地之书面板",
				"role_id", session.RoleID(),
				"areas", len(info.Areas),
				"consum_score", info.ConsumScore,
			)

			explored := 0
			exchanges := 0
			exploreArea := func(area protocol.ForbiddenBookArea) {
				for area.CurNumber > 0 && explored < 30 {
					resp, eErr := call(protocol.BuildForbiddenBookExplorePacket(area.ForbiddenType, area.ExploreType), protocol.ActionIDForbiddenBookExplore)
					if eErr != nil {
						slog.Info("禁地之书探索无应答", "role_id", session.RoleID(), "type", area.ForbiddenType, "err", eErr)
						return
					}
					result, pErr := protocol.ParseForbiddenBookExploreResult(resp.Payload)
					if pErr != nil {
						slog.Warn("解析禁地之书探索回包失败", "role_id", session.RoleID(), "err", pErr)
						return
					}
					if result != protocol.AwakeResultSuccess {
						slog.Info("禁地之书探索未成功", "role_id", session.RoleID(), "type", area.ForbiddenType, "result", result)
						return
					}
					explored++
					area.CurNumber--
					slog.Info("禁地之书探索成功", "role_id", session.RoleID(), "type", area.ForbiddenType, "remaining", area.CurNumber)
					select {
					case <-ctx.Done():
						return
					case <-time.After(200 * time.Millisecond):
					}
				}
			}

			// 1. 免费次数优先。
			//    线上实测：面板同时下发"单次免费"与"多次元宝"两种入口，后者
			//    cur_number>0 且 cost_ingot=300；若不加门禁直接探索会消耗元宝。
			//    因此这里只探索 cost_ingot=0 且 cost_coin=0 的纯免费入口。
			for _, area := range info.Areas {
				if area.CostIngot > 0 || area.CostCoin > 0 {
					slog.Info("禁地之书跳过计费探索入口",
						"role_id", session.RoleID(),
						"type", area.ForbiddenType,
						"explore_type", area.ExploreType,
						"cost_ingot", area.CostIngot,
						"cost_coin", area.CostCoin,
						"cur_number", area.CurNumber,
					)
					continue
				}
				exploreArea(area)
			}

			// 2. 免费次数用尽后，仅兑换纯铜钱档位的探索次数。
			// 线上 s0 实测: Act 13 返回 18/19/20 号区域价格，其中 19/20 号
			// cost_ingot=0、cost_coin=50/100；18 号同时需要元宝，自动化严格跳过。
			exResp, exErr := call(protocol.BuildForbiddenBookExchangeInfoPacket(), protocol.ActionIDForbiddenBookExchangeInfo)
			if exErr != nil || exResp == nil {
				slog.Info("禁地之书兑换价格不可用，跳过铜钱兑换", "role_id", session.RoleID(), "err", exErr)
				slog.Info("禁地之书巡检完成", "role_id", session.RoleID(), "explored", explored, "coin_exchanges", exchanges)
				return nil
			}
			priceList, pErr := protocol.ParseForbiddenBookExchangeInfoResult(exResp.Payload)
			if pErr != nil {
				slog.Warn("解析禁地之书兑换价格失败", "role_id", session.RoleID(), "err", pErr)
				slog.Info("禁地之书巡检完成", "role_id", session.RoleID(), "explored", explored, "coin_exchanges", exchanges)
				return nil
			}

			for _, price := range priceList {
				if price.CostIngot > 0 || price.CostCoin <= 0 || price.Count <= 0 {
					continue
				}
				exchangedThisType := 0
				current := price
				for i := int16(0); i < price.Count && exchanges < 10; i++ {
					if err := ctx.Err(); err != nil {
						return nil
					}
					// 每次兑换前重新读取价格：服务端可能在首兑后把同类型
					// 切换为元宝计价，复用旧价格会直接消耗元宝。
					if exchangedThisType > 0 {
						freshResp, fErr := call(protocol.BuildForbiddenBookExchangeInfoPacket(),
							protocol.ActionIDForbiddenBookExchangeInfo)
						if fErr != nil || freshResp == nil {
							slog.Info("禁地之书兑换价格刷新无应答，停止兑换",
								"role_id", session.RoleID(), "type", price.ForbiddenType, "err", fErr)
							break
						}
						freshList, fpErr := protocol.ParseForbiddenBookExchangeInfoResult(freshResp.Payload)
						if fpErr != nil {
							slog.Warn("禁地之书兑换价格刷新解析失败，停止兑换",
								"role_id", session.RoleID(), "err", fpErr)
							break
						}
						found := false
						for _, fp := range freshList {
							if fp.ForbiddenType == price.ForbiddenType {
								current = fp
								found = true
								break
							}
						}
						if !found || current.CostIngot > 0 || current.CostCoin <= 0 || current.Count <= 0 {
							slog.Info("禁地之书兑换已切换为元宝档或售罄，停止兑换",
								"role_id", session.RoleID(), "type", price.ForbiddenType,
								"cost_ingot", current.CostIngot, "cost_coin", current.CostCoin,
								"count", current.Count)
							break
						}
					}
					coins := session.GetPlayerState().Coins
					if coins < int64(current.CostCoin) {
						slog.Info("铜钱不足，停止禁地之书兑换", "role_id", session.RoleID(),
							"coins", coins, "cost_coin", current.CostCoin)
						break
					}
					resp, xErr := call(protocol.BuildForbiddenBookExchangePacket(price.ForbiddenType), protocol.ActionIDForbiddenBookExchange)
					if xErr != nil {
						slog.Info("禁地之书铜钱兑换无应答", "role_id", session.RoleID(), "type", price.ForbiddenType, "err", xErr)
						break
					}
					result, pErr := protocol.ParseForbiddenBookExchangeResult(resp.Payload)
					if pErr != nil {
						slog.Warn("解析禁地之书铜钱兑换回包失败", "role_id", session.RoleID(), "err", pErr)
						break
					}
					if result != protocol.AwakeResultSuccess {
						slog.Info("禁地之书铜钱兑换未成功", "role_id", session.RoleID(),
							"type", price.ForbiddenType, "result", result)
						break
					}
					exchanges++
					exchangedThisType++
					session.UpdatePlayerState(func(ps *client.PlayerState) {
						ps.Coins -= int64(current.CostCoin)
					})
					slog.Info("禁地之书铜钱兑换成功", "role_id", session.RoleID(),
						"type", price.ForbiddenType, "cost_coin", current.CostCoin)
				}

				// 兑换次数不会体现在 panel 的 cur_number 上（线上实测兑换后仍为 0），
				// 必须主动下发 explore 请求使用兑换次数，由服务端返回结果码熔断。
				refreshed, rErr := fetchInfo()
				if rErr != nil || refreshed == nil {
					continue
				}
				var target protocol.ForbiddenBookArea
				found := false
				for _, area := range refreshed.Areas {
					if area.ForbiddenType != price.ForbiddenType {
						continue
					}
					// 兑换得到的次数只能用于不消耗元宝的探索入口。
					// 线上实测多次探索入口 cost_ingot>0，误用会直接扣元宝；
					// 因此这里严格要求 cost_ingot=0，并优先单次探索。
					if area.CostIngot != 0 {
						continue
					}
					if !found || area.ExploreType == protocol.ForbiddenBookOnce {
						target = area
						found = true
					}
				}
				if !found {
					slog.Info("禁地之书兑换区域无免元宝探索入口，跳过探索", "role_id", session.RoleID(),
						"type", price.ForbiddenType)
					continue
				}
				for n := 0; n < exchangedThisType && explored < 30; n++ {
					if err := ctx.Err(); err != nil {
						return nil
					}
					eResp, eErr := call(
						protocol.BuildForbiddenBookExplorePacket(target.ForbiddenType, target.ExploreType),
						protocol.ActionIDForbiddenBookExplore,
					)
					if eErr != nil {
						slog.Info("禁地之书兑换次数探索无应答", "role_id", session.RoleID(), "type", target.ForbiddenType, "err", eErr)
						break
					}
					eResult, pErr := protocol.ParseForbiddenBookExploreResult(eResp.Payload)
					if pErr != nil {
						slog.Warn("解析禁地之书兑换次数探索回包失败", "role_id", session.RoleID(), "err", pErr)
						break
					}
					if eResult != protocol.AwakeResultSuccess {
						slog.Info("禁地之书兑换次数探索未成功", "role_id", session.RoleID(),
							"type", target.ForbiddenType, "result", eResult)
						break
					}
					explored++
					slog.Info("禁地之书兑换次数探索成功", "role_id", session.RoleID(), "type", target.ForbiddenType)
					select {
					case <-ctx.Done():
						return nil
					case <-time.After(200 * time.Millisecond):
					}
				}
			}

			slog.Info("禁地之书巡检完成", "role_id", session.RoleID(), "explored", explored, "coin_exchanges", exchanges)
			return nil
		},
	)
}
