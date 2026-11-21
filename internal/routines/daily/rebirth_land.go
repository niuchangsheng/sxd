// Package daily 提供签到、邮件、在线奖励、绑定奖励与日常维护类玩法。
//
// 本文件实现轮回净地 (Mod_EraRebirthLandAuto_Base, Module 852)：
//   - 渡业台挂机收益 (852/9) 与每日两次免费快速挂机 (852/8)；
//   - 渡业台抽取 (852/16)：消耗渡业符/活动道具，不消耗元宝；
//   - 渡业仙池免费/今日奖励 (852/13, 852/14)；
//   - 每日活跃度任务 70「每日轮回净地领取挂机奖励」的免费一键领取 (852/6)。
//
// 元宝档 one_click_quick_ingot_back (852/7) 与 quick_auto_back (852/5) 永不调用；
// 852/8 免费次数用尽后仍返回成功但会扣 10 元宝，必须先读 852/1 面板确认剩余次数。
package daily

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// DailyTaskRebirthLand 每日轮回净地领取挂机奖励。
const DailyTaskRebirthLand int32 = 70

// altarDrawEnabled 渡业台自动抽取开关。
//
// 2026-11-15 实测：852/16 被静默丢弃后，整个会话的 Mod 4 (enter_town/get_sections)
// 会连续丢包，任务推图完全无法推进；抽取奖励远小于推图收益，因此默认关闭自动抽取，
// 仅保留渡业仙池免费奖励与挂机奖励领取。需要时改回 true 即可恢复。
const altarDrawEnabled = false

// maxAltarDrawsPerRun 单轮渡业台抽取上限，防止服务端异常导致死循环。
// 实际次数由渡业符数量决定，收到非成功结果码即停止。
const maxAltarDrawsPerRun = 50

// NewRebirthLandRoutine 构造轮回净地日常任务。
func NewRebirthLandRoutine() scheduler.ActivityRoutine {
	const callTimeout = 6 * time.Second
	return scheduler.NewBaseRoutine(
		"rebirth_land",
		"daily",
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
			// 会话重连期间发包必然失败，直接返回等待下次调度。
			if session.State() != client.StateActive {
				return nil
			}
			if open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDLunHuiJingDi); fErr != nil {
				slog.Info("轮回净地功能开放状态查询失败，本轮跳过", "role_id", session.RoleID(), "err", fErr)
				return nil
			} else if !open {
				slog.Info("轮回净地功能未开放，跳过", "role_id", session.RoleID(), "function_id", protocol.FunctionIDLunHuiJingDi)
				return nil
			}

			call := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
				cCtx, cancel := context.WithTimeout(ctx, callTimeout)
				defer cancel()
				resp, err := session.Call(cCtx, pkt, act)
				if err != nil {
					// 会话断开或服务端静默丢弃均表现为无回包，跳过本轮即可。
					slog.Info("轮回净地协议无回包，跳过", "role_id", session.RoleID(),
						"action", act, "err", err)
					return nil
				}
				return resp
			}

			// 1~2. 渡业台抽取：登录钩子已在会话激活后第一时间执行过则跳过；
			// 长会话中再执行会被服务端静默丢弃（2026-11-11 实测）。
			var drawCooldown error
			if altarDrawEnabled {
				if _, attempted := altarAttempted.Load(session); !attempted {
					_, drawCooldown = runAltarDraws(ctx, session, call, true)
					altarAttempted.Store(session, true)
				}
			}

			// 3. 渡业台：领取挂机收益 + 两次免费快速挂机 (852/9, 852/8)。
			//    抓包时序 (2026-11-21): 852/1 面板 → 852/9 收益 → 701/0 锚点 → 852/8
			//    → 852/1 刷新；第二次快速挂机重复同一链路。
			if qErr := runAltarProfitAndQuickAuto(ctx, session, call); qErr != nil {
				return qErr
			}

			// 4. 领取渡业仙池免费奖励 / 今日奖励 (852/13, 852/14)。
			claim := func(pkt *protocol.Packet, act uint32, label string) (uint8, bool) {
				resp := call(pkt, act)
				if resp == nil && act == protocol.ActionIDEraRebirthLandTodayAward {
					// 裸载荷被静默丢弃时，改用服务端最后受理动作号重试一次。
					anchor := session.LastAckActionID()
					resp = call(protocol.BuildEraRebirthLandTodayAwardPrevActPacket(anchor), act)
					if resp != nil {
						slog.Info("轮回净地今日奖励 PrevAct 变体已响应",
							"role_id", session.RoleID(), "anchor", fmt.Sprintf("0x%08X", anchor))
					}
				}
				if resp == nil {
					slog.Info("轮回净地领取无回包", "role_id", session.RoleID(), "scene", label)
					return 0, false
				}
				code, pErr := protocol.ParseEraRebirthSimpleResult(resp.Payload)
				if pErr != nil {
					slog.Warn("解析轮回净地领取回包失败", "role_id", session.RoleID(), "scene", label, "err", pErr)
					return 0, false
				}
				switch code {
				case protocol.EraRebirthResultSuccess:
					slog.Info("轮回净地渡业仙池奖励领取成功", "role_id", session.RoleID(), "scene", label)
					return code, true
				case protocol.EraRebirthResultPackageFull:
					slog.Warn("轮回净地渡业仙池奖励背包已满", "role_id", session.RoleID(), "scene", label)
					return code, false
				default:
					slog.Info("轮回净地渡业仙池暂无可领取奖励",
						"role_id", session.RoleID(), "scene", label,
						"result", code, "result_text", protocol.EraRebirthResultText(code))
					return code, false
				}
			}
			freeCode, freeOK := claim(protocol.BuildEraRebirthLandFreeAwardPacket(),
				protocol.ActionIDEraRebirthLandFreeAward, "渡业仙池免费奖励")
			if freeOK {
				return drawCooldown
			}
			todayCode, todayOK := claim(protocol.BuildEraRebirthLandTodayAwardPacket(),
				protocol.ActionIDEraRebirthLandTodayAward, "渡业仙池今日奖励")
			if todayOK {
				return drawCooldown
			}
			if freeCode == protocol.EraRebirthResultPackageFull || todayCode == protocol.EraRebirthResultPackageFull {
				return scheduler.ErrBagFull
			}

			// 5. 活跃度任务 70 未完成时才领取挂机奖励，避免无意义请求。
			actResp := call(protocol.BuildGameAssistantInfoPacket(), protocol.ActionIDGameAssistantInfo)
			if actResp == nil {
				// 单次无回包多为网关瞬断；短暂重试一次再进入较长冷却。
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(500 * time.Millisecond):
				}
				actResp = call(protocol.BuildGameAssistantInfoPacket(), protocol.ActionIDGameAssistantInfo)
			}
			if actResp == nil {
				slog.Info("查询每日活跃度失败，稍后重试轮回净地", "role_id", session.RoleID())
				return scheduler.NewCooldown(30*time.Minute, "轮回净地活跃度查询无回包")
			}
			panel, err := protocol.ParseGameAssistantInfo(actResp.Payload)
			if err != nil {
				return err
			}
			if panel.RemainingTimesFor(DailyTaskRebirthLand) <= 0 {
				slog.Info("轮回净地活跃度已完成，跳过", "role_id", session.RoleID())
				return nil
			}

			// 6. 兜底：免费一键领取挂机奖励 (852/6)。
			resp := call(protocol.BuildEraRebirthLandFreeBackPacket(), protocol.ActionIDEraRebirthLandFreeBack)
			if resp == nil {
				slog.Info("轮回净地不可用或未开放，跳过", "role_id", session.RoleID())
				return nil
			}
			result, err := protocol.ParseEraRebirthFreeBackResult(resp.Payload)
			if err != nil {
				slog.Warn("解析轮回净地领取回包失败", "role_id", session.RoleID(), "err", err)
				return nil
			}
			switch result.Result {
			case protocol.EraRebirthResultSuccess:
				slog.Info("轮回净地挂机奖励领取成功",
					"role_id", session.RoleID(),
					"rewards", len(result.Rewards))
			case protocol.EraRebirthResultNoCanGetGift, protocol.EraRebirthResultAlreadyGet,
				protocol.EraRebirthResultNoProfit, protocol.EraRebirthResultNoFreeStorage:
				slog.Info("轮回净地暂无可领取奖励",
					"role_id", session.RoleID(),
					"result", result.Result,
					"result_text", protocol.EraRebirthResultText(result.Result))
			case protocol.EraRebirthResultFuncNotOpen:
				slog.Info("轮回净地功能未开放，稍后重试", "role_id", session.RoleID())
				return scheduler.NewCooldown(30*time.Minute, "轮回净地功能未开放")
			case protocol.EraRebirthResultPackageFull:
				return scheduler.ErrBagFull
			default:
				slog.Info("轮回净地领取未成功",
					"role_id", session.RoleID(),
					"result", result.Result,
					"result_text", protocol.EraRebirthResultText(result.Result))
			}
			return drawCooldown
		},
	)
}

// altarAttempted 标记本进程内已对某角色执行过渡业台抽取（登录钩子优先）。
var altarAttempted sync.Map

// RunRebirthAltarOnLogin 在会话刚激活、调度器尚未启动时执行渡业台抽取。
//
// 服务端对 852/16 的上下文非常敏感：登录后第一件事执行可稳定成功；
// 经过其他模块请求后再抽取会被静默丢弃（2026-11-11 线上实测）。
// 调用方应在 dispatcher.StartAll 之前调用本函数。
func RunRebirthAltarOnLogin(ctx context.Context, session *client.RoleSession) {
	if session == nil || !altarDrawEnabled {
		return
	}
	// RoleSession.Start 是异步连接：等待状态机进入 Active 后再抢占“第一业务请求”。
	deadline := time.Now().Add(30 * time.Second)
	for session.State() != client.StateActive && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	if session.State() != client.StateActive {
		slog.Info("登录后渡业台等待会话激活超时", "role_id", session.RoleID())
		return
	}
	if _, loaded := altarAttempted.LoadOrStore(session, true); loaded {
		return
	}
	const callTimeout = 8 * time.Second
	call := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
		cCtx, cancel := context.WithTimeout(ctx, callTimeout)
		defer cancel()
		resp, err := session.Call(cCtx, pkt, act)
		if err != nil {
			// 会话正在重连时请求根本没有发出，不计为服务端静默丢包。
			if session.State() != client.StateActive {
				return nil
			}
			slog.Info("登录后渡业台协议无回包", "role_id", session.RoleID(),
				"action", act, "err", err)
			return nil
		}
		return resp
	}
	// 登录钩子处于全新会话，无需 0/2 重同步；实测重同步会把动作链切到
	// 其他上下文，紧随其后的 852/16 抽取被静默丢弃。
	draws, cooldown := runAltarDraws(ctx, session, call, false)
	if draws > 0 {
		slog.Info("登录后渡业台抽取完成", "role_id", session.RoleID(), "draws", draws)
	}
	if cooldown != nil {
		slog.Info("登录后渡业台抽取未完成", "role_id", session.RoleID())
		// 抽取被静默丢弃时服务端 PrevAct 锚点停在上一个动作（通常是 0/2），
		// 而本地 lastActionID 已推进到 852/16；不重新对齐会让随后的
		// enter_town/get_sections 连续丢包（2026-11-15 实测）。
		if resyncPrevActChain(session, call) {
			slog.Info("登录后渡业台丢弃后已重新对齐 PrevAct 链", "role_id", session.RoleID())
		}
		// 会话重连导致登录钩子未完成时撤销标记，让后续定时任务重新尝试，
		// 否则本进程内渡业台将永久跳过。
		altarAttempted.Delete(session)
	}
}

// resyncPrevActChain 用 0/2 全量角色资产重建 PrevAct 链。
//
// 依次尝试候选锚点，直到 0/2 真正被服务端处理（LastAck 变为 0/2）。
func resyncPrevActChain(
	session *client.RoleSession,
	call func(*protocol.Packet, uint32) *protocol.Packet,
) bool {
	candidates := []uint32{
		session.LastAckActionID(),
		session.LastActionID(),
		protocol.ActionIDPlayerGetInfo,
		protocol.ActionIDPlayerHeartbeat,
		0x00A50000, // Mod 165 扩展模块初始化
		0x00000063, // Mod 0 Act 99 场景进阶
		0x00000048, // Mod 0 Act 72 城镇初始化
	}
	seen := make(map[uint32]bool, len(candidates))
	for _, cand := range candidates {
		if cand == 0 || seen[cand] {
			continue
		}
		seen[cand] = true
		if call(protocol.BuildPlayerGetInfoPacket(int32(cand)), protocol.ActionIDPlayerGetInfo) != nil &&
			session.LastAckActionID() == protocol.ActionIDPlayerGetInfo {
			slog.Debug("PrevAct 链重建成功", "role_id", session.RoleID(),
				"anchor", fmt.Sprintf("0x%08X", cand))
			return true
		}
	}
	return false
}

// runAltarDraws 查询渡业仙池面板并循环执行 852/16 抽取。
//
// doResync=true 时会先用 0/2 全量角色资产重建 PrevAct 链（长会话兜底路径）。
func runAltarDraws(
	ctx context.Context,
	session *client.RoleSession,
	call func(*protocol.Packet, uint32) *protocol.Packet,
	doResync bool,
) (int, error) {
	// 先用 0/2 全量角色资产重建 PrevAct 链：长会话中大量无 PrevAct 的请求会让
	// 服务端的链锚点与本地 lastActionID 脱节，导致 852/16 被静默丢弃。
	if doResync {
		if !resyncPrevActChain(session, call) {
			slog.Info("轮回净地 PrevAct 链重建未成功，稍后重试", "role_id", session.RoleID())
		}
	}

	anchor := session.LastAckActionID()
	if anchor == 0 {
		anchor = session.LastActionID()
	}
	slog.Info("轮回净地渡业台请求上下文", "role_id", session.RoleID(),
		"anchor", fmt.Sprintf("0x%08X", anchor),
		"last_action", fmt.Sprintf("0x%08X", session.LastActionID()),
		"last_ack", fmt.Sprintf("0x%08X", session.LastAckActionID()))
	// 面板仅用于诊断；部分账号面板请求会被静默丢弃，但紧随其后的抽取仍可
	// 正常回包。因此面板查询失败时不打印无回包日志，避免误报。
	var poolResp *protocol.Packet
	{
		cCtx, cCancel := context.WithTimeout(ctx, 8*time.Second)
		poolResp, _ = session.Call(cCtx, protocol.BuildEraRebirthLandPoolPanelPrevActPacket(anchor),
			protocol.ActionIDEraRebirthLandPoolPanel)
		cCancel()
	}
	if poolResp != nil {
		r := protocol.NewReader(poolResp.Payload)
		vals := make([]int32, 0, 6)
		for i := 0; i < 6; i++ {
			v, rErr := r.ReadInt32()
			if rErr != nil {
				break
			}
			vals = append(vals, v)
		}
		slog.Info("轮回净地渡业仙池面板",
			"role_id", session.RoleID(), "payload_len", len(poolResp.Payload), "ints", vals)
	}

	ingotsBefore := session.GetPlayerState().Ingots
	draws := 0
	var drawCooldown error
	// 面板与抽取之间、连续抽取之间节流，避免服务端频率限制导致静默丢包。
	time.Sleep(500 * time.Millisecond)
	for draws < maxAltarDrawsPerRun && ctx.Err() == nil {
		drawPkt := protocol.BuildEraRebirthLandDrawPacket(0, session.LastActionID())
		slog.Info("轮回净地渡业台抽取请求", "role_id", session.RoleID(), "draws", draws,
			"prev_act", fmt.Sprintf("0x%08X", session.LastActionID()),
			"payload", fmt.Sprintf("% X", drawPkt.Payload))
		// 抽取请求对 PrevAct 链敏感；首次无回包时按「最后收到的包」重同步重试一次。
		// 852/16 被静默丢弃会污染整个会话的 Mod 4 上下文（2026-11-15 实测：
		// 登录钩子抽取超时后，任务推图的 enter_town/get_sections 连续丢包）。
		dCtx, dCancel := context.WithTimeout(ctx, 8*time.Second)
		resp, rErr := session.CallWithPrevActResync(dCtx, 8*time.Second, drawPkt,
			protocol.ActionIDEraRebirthLandDraw)
		dCancel()
		if rErr != nil {
			slog.Info("轮回净地渡业台抽取无回包", "role_id", session.RoleID(), "err", rErr)
			resp = nil
		}
		if resp == nil {
			// 抽取无回包不应阻断仙池领取与挂机奖励：先完成其余步骤，最后再冷却。
			drawCooldown = scheduler.NewCooldown(30*time.Minute, "渡业台抽取无回包")
			break
		}
		res, err := protocol.ParseEraRebirthDrawResult(resp.Payload)
		if err != nil {
			slog.Warn("解析渡业台抽取回包失败", "role_id", session.RoleID(), "err", err)
			break
		}
		if res.Result != protocol.EraRebirthResultSuccess {
			slog.Info("渡业台抽取结束", "role_id", session.RoleID(), "draws", draws,
				"result", res.Result, "result_text", protocol.EraRebirthResultText(res.Result))
			break
		}
		draws++
		slog.Info("渡业台抽取成功", "role_id", session.RoleID(),
			"draws", draws, "first", res.First, "second", res.Second)
		// 连续抽取之间节流，避免触发服务端频率限制。
		time.Sleep(700 * time.Millisecond)
	}
	if ingotsAfter := session.GetPlayerState().Ingots; ingotsAfter != ingotsBefore {
		slog.Error("渡业台抽取导致元宝变化，疑似协议漂移，立即熔断",
			"role_id", session.RoleID(), "before", ingotsBefore, "after", ingotsAfter)
		return draws, scheduler.NewCooldown(6*time.Hour, "渡业台抽取导致元宝变化")
	}
	if draws > 0 {
		slog.Info("渡业台本轮抽取完成", "role_id", session.RoleID(), "draws", draws)
	}
	return draws, drawCooldown
}

// runAltarProfitAndQuickAuto 完成「渡业台领取挂机收益 + 两次免费快速挂机」。
//
// 抓包时序 (2026-11-21): 852/1 面板 → 852/9 收益 → 852/8 → 852/1 刷新；
// 第二次快速挂机重复。线上实测 701/0 锚点会被静默丢弃，852/8 只需 PrevAct 连续。
// 任何失败码只驱动本阶段分支，不阻断后续渡业仙池领取。
func runAltarProfitAndQuickAuto(
	ctx context.Context,
	session *client.RoleSession,
	call func(*protocol.Packet, uint32) *protocol.Packet,
) error {
	// 1. 面板用于建立 PrevAct 上下文，并读取当日已用免费快速挂机次数。
	//    852/8 在免费次数用尽后仍返回 result=0 但会扣 10 元宝 (2026-11-21 实测)，
	//    因此面板解析失败时必须放弃快速挂机，宁可少领也不能扣元宝。
	freeRemaining := int32(0)
	freeUsedBefore := int32(0)
	paidUsed := int32(0)
	panelOK := false
	if resp := call(protocol.BuildEraRebirthLandPanelPrevActPacket(session.LastActionID()),
		protocol.ActionIDEraRebirthLandPanel); resp != nil {
		if panel, err := protocol.ParseEraRebirthLandPanel(resp.Payload); err != nil {
			slog.Warn("解析轮回净地面板失败，本轮跳过快速挂机",
				"role_id", session.RoleID(), "payload_len", len(resp.Payload), "err", err)
		} else {
			panelOK = true
			freeRemaining = panel.QuickFreeRemaining()
			freeUsedBefore = panel.QuickFreeUsed
			paidUsed = panel.QuickPaidUsed
			slog.Info("轮回净地面板", "role_id", session.RoleID(),
				"idle_seconds", panel.IdleSeconds,
				"idle_cap_seconds", panel.IdleCapSeconds,
				"quick_free_used", panel.QuickFreeUsed,
				"quick_free_remaining", freeRemaining,
				"quick_paid_used", panel.QuickPaidUsed)
		}
	}

	// 2. 领取渡业台挂机收益 (852/9)。
	if resp := call(protocol.BuildEraRebirthLandAutoProfitPrevActPacket(session.LastActionID()),
		protocol.ActionIDEraRebirthLandAutoProfit); resp == nil {
		slog.Info("轮回净地挂机收益无回包，按连续丢包退避", "role_id", session.RoleID())
		return scheduler.HandleSilentDrop(session, "rebirth_land_profit", 3,
			30*time.Minute, "轮回净地挂机收益无回包")
	} else if profit, err := protocol.ParseEraRebirthAutoProfit(resp.Payload); err != nil {
		slog.Warn("解析轮回净地挂机收益失败", "role_id", session.RoleID(), "err", err)
	} else {
		switch profit.Result {
		case protocol.EraRebirthResultSuccess:
			slog.Info("轮回净地挂机收益领取成功", "role_id", session.RoleID(),
				"profit", profit.Profit, "rewards", eraRebirthRewardText(profit.Rewards))
		case protocol.EraRebirthResultNoCanGetGift, protocol.EraRebirthResultNoProfit,
			protocol.EraRebirthResultNoFreeStorage, protocol.EraRebirthResultAlreadyGet:
			slog.Info("轮回净地暂无挂机收益", "role_id", session.RoleID(),
				"result", profit.Result, "result_text", protocol.EraRebirthResultText(profit.Result))
		case protocol.EraRebirthResultPackageFull:
			return scheduler.ErrBagFull
		case protocol.EraRebirthResultFuncNotOpen:
			return scheduler.NewCooldown(30*time.Minute, "轮回净地功能未开放")
		case protocol.EraRebirthResultNotEnoughIngot, protocol.EraRebirthResultNotEnoughTodayIngot:
			// 收益档若开始索要元宝，立即熔断，绝不允许继续调用。
			return scheduler.NewCooldown(6*time.Hour, "轮回净地挂机收益要求元宝，熔断")
		default:
			slog.Info("轮回净地挂机收益未成功", "role_id", session.RoleID(),
				"result", profit.Result, "result_text", protocol.EraRebirthResultText(profit.Result))
		}
	}

	if !panelOK {
		return nil
	}
	return runQuickAuto(ctx, session, call, freeRemaining, freeUsedBefore, paidUsed)
}

// runQuickAuto 触发每日免费快速挂机 (852/8)，只消耗服务端面板显示的剩余免费次数。
//
// 每次成功均校验元宝余额：852/8 是免费档，若余额变化说明协议漂移或误触发付费档，
// 立即 6 小时熔断。
func runQuickAuto(
	ctx context.Context,
	session *client.RoleSession,
	call func(*protocol.Packet, uint32) *protocol.Packet,
	freeRemaining int32,
	freeUsedBefore int32,
	paidUsedBefore int32,
) error {
	if freeRemaining <= 0 {
		slog.Info("轮回净地免费快速挂机次数已用尽，跳过以避免扣元宝", "role_id", session.RoleID())
		return nil
	}
	ingotsBefore := session.GetPlayerState().Ingots
	for i := int32(0); i < freeRemaining && i < protocol.EraRebirthQuickFreeDailyLimit; i++ {
		if ctx.Err() != nil {
			return nil
		}
		// 2026-11-21 线上实测: 官方抓包中的 701/0 get_state 锚点在本服被静默丢弃
		// （每次白等 6 秒），而紧随其后的 852/8 仍能成功。说明 852/8 只要求
		// PrevAct 链连续，不需要 701/0 参与；自动化直接省略该请求。

		resp := call(protocol.BuildEraRebirthLandQuickAutoPacket(session.LastActionID()),
			protocol.ActionIDEraRebirthLandQuickAuto)
		if resp == nil {
			slog.Info("轮回净地快速挂机无回包，按连续丢包退避", "role_id", session.RoleID(),
				"times", i+1)
			return scheduler.HandleSilentDrop(session, "rebirth_land_quick", 3,
				30*time.Minute, "轮回净地快速挂机无回包")
		}
		scheduler.ResetSilentDrops(session, "rebirth_land_quick")
		res, err := protocol.ParseEraRebirthFreeBackResult(resp.Payload)
		if err != nil {
			slog.Warn("解析轮回净地快速挂机回包失败", "role_id", session.RoleID(), "err", err)
			return nil
		}
		switch res.Result {
		case protocol.EraRebirthResultSuccess:
			slog.Info("轮回净地快速挂机成功", "role_id", session.RoleID(),
				"times", i+1, "rewards", eraRebirthRewardText(res.Rewards))
		case protocol.EraRebirthResultNoTimes, protocol.EraRebirthResultNoProfit,
			protocol.EraRebirthResultNoCanGetGift, protocol.EraRebirthResultNoFreeStorage,
			protocol.EraRebirthResultNotEnoughItem, protocol.EraRebirthResultAlreadyGet:
			slog.Info("轮回净地快速挂机结束（免费次数用尽或暂无可领取）",
				"role_id", session.RoleID(), "times", i+1,
				"result", res.Result, "result_text", protocol.EraRebirthResultText(res.Result))
			return nil
		case protocol.EraRebirthResultPackageFull:
			return scheduler.ErrBagFull
		case protocol.EraRebirthResultFuncNotOpen:
			return scheduler.NewCooldown(30*time.Minute, "轮回净地快速挂机功能未开放")
		case protocol.EraRebirthResultNotEnoughIngot, protocol.EraRebirthResultNotEnoughTodayIngot:
			// 852/8 应为免费档；出现元宝不足说明免费次数已耗尽且服务端转向付费，
			// 立即熔断，绝不进入 852/5/852/7 付费链路。
			slog.Error("快速挂机返回元宝不足，疑似免费次数耗尽后转向付费，立即熔断",
				"role_id", session.RoleID(), "result", res.Result)
			return scheduler.NewCooldown(6*time.Hour, "快速挂机免费次数已耗尽")
		default:
			slog.Info("轮回净地快速挂机未成功", "role_id", session.RoleID(),
				"times", i+1, "result", res.Result,
				"result_text", protocol.EraRebirthResultText(res.Result))
			return nil
		}

		// 每次快速挂机后刷新面板，用服务端字段确认免费次数确实被消耗。
		// 若面板解析失败或免费次数没有减少，立即停止，避免下一次落到付费档。
		panelResp := call(protocol.BuildEraRebirthLandPanelPrevActPacket(session.LastActionID()),
			protocol.ActionIDEraRebirthLandPanel)
		if panelResp == nil {
			slog.Warn("快速挂机后刷新面板无回包，按连续丢包退避", "role_id", session.RoleID())
			return scheduler.HandleSilentDrop(session, "rebirth_land_quick", 3,
				30*time.Minute, "快速挂机后刷新面板无回包")
		}
		panel, pErr := protocol.ParseEraRebirthLandPanel(panelResp.Payload)
		if pErr != nil {
			slog.Warn("快速挂机后解析面板失败，停止后续快速挂机",
				"role_id", session.RoleID(), "err", pErr)
			return nil
		}
		if panel.QuickPaidUsed > paidUsedBefore {
			slog.Error("快速挂机误入付费档，已扣元宝，立即熔断",
				"role_id", session.RoleID(),
				"paid_before", paidUsedBefore, "paid_after", panel.QuickPaidUsed)
			return scheduler.NewCooldown(6*time.Hour, "快速挂机误入付费档")
		}
		if want := freeUsedBefore + i + 1; panel.QuickFreeUsed < want {
			slog.Warn("快速挂机后面板免费次数未按预期增加，停止以避免扣元宝",
				"role_id", session.RoleID(), "want_used", want,
				"quick_free_used", panel.QuickFreeUsed)
			return nil
		}
		// 以服务端最新面板为准确认是否还有剩余免费次数：期间若有其他客户端消耗了
		// 免费次数，这里会立即归零并停止，绝不会把下一次调用落到付费档。
		if remaining := panel.QuickFreeRemaining(); remaining <= 0 {
			slog.Info("轮回净地免费快速挂机次数已用尽，停止", "role_id", session.RoleID(),
				"quick_free_used", panel.QuickFreeUsed)
			return nil
		}

		if ingotsAfter := session.GetPlayerState().Ingots; ingotsAfter != ingotsBefore {
			slog.Error("快速挂机导致元宝变化，疑似协议漂移，立即熔断",
				"role_id", session.RoleID(), "before", ingotsBefore, "after", ingotsAfter)
			return scheduler.NewCooldown(6*time.Hour, "快速挂机导致元宝变化")
		}
	}
	return nil
}

// eraRebirthRewardText 将奖励列表格式化为 "item x num, ..." 便于日志检索。
func eraRebirthRewardText(rewards []protocol.EraRebirthReward) string {
	parts := make([]string, 0, len(rewards))
	for _, reward := range rewards {
		parts = append(parts, fmt.Sprintf("%d x %d", reward.ItemID, reward.Num))
	}
	return strings.Join(parts, ", ")
}
