// Package pvp 提供竞技场、仙界竞技场、神魔大战、取经劫镖、世界 Boss 与跨服天梯等对抗玩法。
package pvp

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

// NewGodsAndDemonsRoutine 构造神魔大战限时活动任务。
// NewGodsAndDemonsRoutine 构造神魔大战自动报名任务 (Module 118)。
//
// 先查询大战阶段：仅报名期 (SIGN_UP) 发起报名；未开启或已进入赛程阶段时跳过。
// 不调用清冷却/购买挑战次数等消耗元宝接口。
func NewGodsAndDemonsRoutine() scheduler.ActivityRoutine {
	const callTimeout = 3 * time.Second
	return scheduler.NewBaseRoutine(
		"gods_and_demons",
		"pvp",
		scheduler.ScheduleCron,
		scheduler.PriorityHigh,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			sCtx, sCancel := context.WithTimeout(ctx, callTimeout)
			statusResp, err := session.Call(sCtx, protocol.BuildGodWarStatusPacket(), protocol.ActionIDGodWarStatus)
			sCancel()
			if err != nil {
				slog.Info("神魔大战功能不可用，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(30*time.Minute, "神魔大战状态查询无回包")
			}
			status, err := protocol.ParseGodWarStatusResult(statusResp.Payload)
			if err != nil {
				return fmt.Errorf("解析神魔大战阶段失败: %w", err)
			}
			switch status.Status {
			case protocol.GodWarStatusClose:
				slog.Info("神魔大战未开启，稍后重试", "role_id", session.RoleID())
				return scheduler.NewCooldown(time.Hour, "神魔大战未开启")
			case protocol.GodWarStatusSignUp:
				// 报名期，继续
			default:
				slog.Info("神魔大战当前非报名期，稍后重试",
					"role_id", session.RoleID(),
					"status", status.Status,
					"race_count", status.RaceCount,
				)
				return scheduler.NewCooldown(time.Hour, "神魔大战非报名期")
			}

			rCtx, rCancel := context.WithTimeout(ctx, callTimeout)
			signResp, err := session.Call(rCtx, protocol.BuildGodWarSignUpPacket(), protocol.ActionIDGodWarSignUp)
			rCancel()
			if err != nil {
				return fmt.Errorf("神魔大战报名失败: %w", err)
			}
			result, err := protocol.ParseGodWarSignUpResult(signResp.Payload)
			if err != nil {
				return fmt.Errorf("解析神魔大战报名回包失败: %w", err)
			}
			if result == protocol.GodWarResultSuccess {
				slog.Info("神魔大战报名成功", "role_id", session.RoleID())
			} else {
				slog.Info("神魔大战报名未成功", "role_id", session.RoleID(), "result", result)
			}
			return nil
		},
	)
}

// NewCaravanHijackRoutine 构造劫镖任务 (Feature 7: 劫镖)。
//
// 官方 Mod_RobMoney_Base 流程：
//
//	get_rob_money_info (Act 0) 读取目标状态 → rob (Act 2) 劫镖
//
// 目标状态首字节: 0=NOT_SEARCH 尚未搜索，1=SEARCHED 已有可劫目标。
// 搜索 (Act 1) 在"重搜"场景会消耗 30 元宝，自动化无法确认服务端是否收费，
// 因此本任务只对已存在的目标发起劫镖；未搜索时直接跳过，绝不调用 search。
// 旧实现把 Act 12 (open_panel) 当成劫镖、把 Act 2 (rob) 当成结算，且丢弃结果码，
// 现一并修正为严格解析 rob 结果码。
func NewCaravanHijackRoutine() scheduler.ActivityRoutine {
	const callTimeout = 5 * time.Second
	return scheduler.NewBaseRoutine(
		"caravan_hijack",
		"pvp",
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
			slog.Info("正在执行对抗任务: 劫镖", "role_id", session.RoleID())

			caller := kit.NewCaller(ctx, session, callTimeout)
			call := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
				resp, err := caller.Call(pkt, act)
				if err != nil {
					slog.Info("劫镖协议无回包", "role_id", session.RoleID(),
						"action", fmt.Sprintf("0x%08X", act), "err", err)
					return nil
				}
				return resp
			}

			// 1. 读取劫镖面板状态。
			infoResp := call(protocol.BuildCaravanGetDetailPacket(session.LastActionID()), protocol.ActionIDCaravanGetDetail)
			if infoResp == nil {
				if abort := caller.AbortErr(); abort != nil {
					return abort
				}
				slog.Info("劫镖功能不可用或未开放，稍后重试", "role_id", session.RoleID())
				return scheduler.NewCooldown(30*time.Minute, "劫镖面板无回包")
			}
			state, err := protocol.ParseCaravanInfoResultCode(infoResp.Payload)
			if err != nil {
				slog.Warn("解析劫镖面板失败，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(10*time.Minute, "劫镖面板解析失败")
			}
			if state != protocol.CaravanStateSearched {
				if state != protocol.CaravanStateNotSearch {
					slog.Info("劫镖面板返回未知状态，跳过", "role_id", session.RoleID(), "state", state)
					return nil
				}
				// 官方流程：未勘察时先点“开始勘察” search (314/1)，成功后再劫镖。
				// 每日首次勘察为免费档；若服务端返回元宝不足/勘察次数不足则立即停止，
				// 绝不调用 buy_rob_times (314/3)。
				searchResp := call(protocol.BuildCaravanSearchPacket(session.LastActionID()), protocol.ActionIDCaravanGetList)
				if searchResp == nil {
					slog.Info("劫镖开始勘察无回包，稍后重试", "role_id", session.RoleID())
					return scheduler.NewCooldown(30*time.Minute, "劫镖勘察无回包")
				}
				searchRes, sErr := protocol.ParseCaravanSearchResult(searchResp.Payload)
				if sErr != nil {
					slog.Warn("解析劫镖勘察回包失败", "role_id", session.RoleID(), "err", sErr)
					return nil
				}
				switch searchRes.Result {
				case protocol.CaravanResultSuccess:
					slog.Info("劫镖开始勘察成功",
						"role_id", session.RoleID(), "targets", searchRes.Targets)
				case protocol.CaravanResultNotEnoughIngot, protocol.CaravanResultNotEnoughBuyTimes:
					slog.Info("劫镖勘察需要付费次数/元宝，自动化停止",
						"role_id", session.RoleID(), "result", searchRes.Result,
						"result_text", protocol.CaravanResultText(searchRes.Result))
					return nil
				default:
					slog.Info("劫镖开始勘察未成功",
						"role_id", session.RoleID(), "result", searchRes.Result,
						"result_text", protocol.CaravanResultText(searchRes.Result))
					return nil
				}

				// 勘察成功后重新读取面板，确认 state=SEARCHED。
				infoResp = call(protocol.BuildCaravanGetDetailPacket(protocol.ActionIDCaravanGetList),
					protocol.ActionIDCaravanGetDetail)
				if infoResp == nil {
					slog.Info("劫镖勘察后读取面板无回包，稍后重试", "role_id", session.RoleID())
					return scheduler.NewCooldown(10*time.Minute, "劫镖勘察后面板无回包")
				}
				state, err = protocol.ParseCaravanInfoResultCode(infoResp.Payload)
				if err != nil {
					slog.Warn("解析劫镖面板失败", "role_id", session.RoleID(), "err", err)
					return nil
				}
				if state != protocol.CaravanStateSearched {
					slog.Info("劫镖勘察后仍无可劫目标，跳过",
						"role_id", session.RoleID(), "state", state)
					return nil
				}
			}

			// 2. 对已搜索到的目标发起劫镖。
			robResp := call(protocol.BuildCaravanRobPacket(protocol.ActionIDCaravanGetDetail), protocol.ActionIDCaravanRob)
			if robResp == nil {
				return nil
			}
			res, err := protocol.ParseCaravanRobResult(robResp.Payload)
			if err != nil {
				slog.Warn("解析劫镖回包失败", "role_id", session.RoleID(), "err", err)
				return nil
			}
			switch res.Result {
			case protocol.CaravanResultSuccess:
				slog.Info("劫镖成功",
					"role_id", session.RoleID(),
					"award_type", res.Award[0],
					"award_value", res.Award[1])
			case protocol.CaravanResultNotEnoughTimes:
				slog.Info("今日劫镖次数已用尽", "role_id", session.RoleID())
			case protocol.CaravanResultFailure:
				slog.Info("劫镖战斗失败", "role_id", session.RoleID())
			default:
				slog.Info("劫镖未成功",
					"role_id", session.RoleID(),
					"result", res.Result,
					"result_text", protocol.CaravanResultText(res.Result))
			}
			return nil
		},
	)
}
