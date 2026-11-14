// Package daily 提供签到、邮件、在线奖励、绑定奖励与日常维护类玩法。
//
// 本文件实现轮回净地 (Mod_EraRebirthLandAuto_Base, Module 852)：
//   - 渡业台抽取 (852/16)：消耗渡业符/活动道具，不消耗元宝；
//   - 渡业仙池免费/今日奖励 (852/13, 852/14)；
//   - 每日活跃度任务 70「每日轮回净地领取挂机奖励」的免费一键领取 (852/6)。
//
// 元宝档 one_click_quick_ingot_back (852/7) 永不调用。
package daily

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// DailyTaskRebirthLand 每日轮回净地领取挂机奖励。
const DailyTaskRebirthLand int32 = 70

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
			if _, attempted := altarAttempted.Load(session); !attempted {
				_, drawCooldown = runAltarDraws(ctx, session, call, true)
				altarAttempted.Store(session, true)
			}

			// 3. 领取渡业仙池免费奖励 / 今日奖励 (852/13, 852/14)。
			claim := func(pkt *protocol.Packet, act uint32, label string) (uint8, bool) {
				resp := call(pkt, act)
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

			// 4. 活跃度任务 70 未完成时才领取挂机奖励，避免无意义请求。
			actResp := call(protocol.BuildGameAssistantInfoPacket(), protocol.ActionIDGameAssistantInfo)
			if actResp == nil {
				slog.Info("查询每日活跃度失败，稍后重试轮回净地", "role_id", session.RoleID())
				return scheduler.NewCooldown(10*time.Minute, "轮回净地活跃度查询无回包")
			}
			panel, err := protocol.ParseGameAssistantInfo(actResp.Payload)
			if err != nil {
				return err
			}
			if panel.RemainingTimesFor(DailyTaskRebirthLand) <= 0 {
				slog.Info("轮回净地活跃度已完成，跳过", "role_id", session.RoleID())
				return nil
			}

			// 5. 兜底：免费一键领取挂机奖励 (852/6)。
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
	if session == nil {
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
	// 会话激活后服务端场景/动作链仍需短暂稳定；过早发包会被静默丢弃。
	select {
	case <-ctx.Done():
		return
	case <-time.After(time.Second):
	}
	const callTimeout = 8 * time.Second
	call := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
		cCtx, cancel := context.WithTimeout(ctx, callTimeout)
		defer cancel()
		resp, err := session.Call(cCtx, pkt, act)
		if err != nil {
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
	}
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
	// 依次尝试候选锚点，直到 0/2 真正被服务端处理（LastAck 变为 0/2）。
	if doResync {
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
		synced := false
		for _, cand := range candidates {
			if cand == 0 || seen[cand] {
				continue
			}
			seen[cand] = true
			if call(protocol.BuildPlayerGetInfoPacket(int32(cand)), protocol.ActionIDPlayerGetInfo) != nil &&
				session.LastAckActionID() == protocol.ActionIDPlayerGetInfo {
				synced = true
				slog.Debug("轮回净地 PrevAct 链重建成功", "role_id", session.RoleID(),
					"anchor", fmt.Sprintf("0x%08X", cand))
				break
			}
		}
		if !synced {
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
	if poolResp := call(protocol.BuildEraRebirthLandPoolPanelPrevActPacket(anchor),
		protocol.ActionIDEraRebirthLandPoolPanel); poolResp != nil {
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
		resp := call(drawPkt, protocol.ActionIDEraRebirthLandDraw)
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
