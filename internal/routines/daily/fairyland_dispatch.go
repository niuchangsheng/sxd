// 本文件实现宠物仙域「游历」(Mod_FairylandDispatch_Base, Module 714)。
//
// main_panel(1) 返回非成功码时直接退出；成功时读取 task_panel(2) 并对未完成订单
// 调用 dispatch(6) 派遣游历，随后领取任务奖励。仅使用免费派遣，不购买礼包/特权。
package daily

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

// maxFairylandDispatch 单轮最多派遣数量。
const maxFairylandDispatch = 6

// NewFairylandDispatchRoutine 构造宠物仙域游历任务。
func NewFairylandDispatchRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"fairyland_dispatch",
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
			caller := kit.NewCaller(ctx, session, callTimeout)
			call := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
				resp, err := caller.Call(pkt, act)
				if err != nil {
					slog.Info("宠物仙域协议无回包", "role_id", session.RoleID(), "action", act, "err", err)
					return nil
				}
				return resp
			}

			panelResp := call(protocol.BuildFairylandMainPanelPacket(), protocol.ActionIDFairylandMainPanel)
			variant := "bare"
			var code uint8
			var codeErr error
			if panelResp != nil {
				code, codeErr = protocol.ParseActionFirstByte(panelResp.Payload, "宠物仙域主面板")
			}
			// 裸载荷返回 FAILED 并不等于“本周上限”：2026-11-22 10:40 实测裸
			// 714/1 返回 39 字节失败桩（01 + 38 个 0），用户确认游历时间并未
			// 达上限。失败/无回包时都再试一次带 PrevAct 的变体，只有两个变体
			// 都失败才按普通失败退避，绝不据此睡到周重置。
			if panelResp == nil || codeErr != nil || code != protocol.FairylandSuccess {
				anchor := session.LastAckActionID()
				variant = fmt.Sprintf("prev_act:0x%08X", anchor)
				if retry := call(protocol.BuildFairylandMainPanelWithPrevActPacket(anchor),
					protocol.ActionIDFairylandMainPanel); retry != nil {
					if c, e := protocol.ParseActionFirstByte(retry.Payload, "宠物仙域主面板"); e == nil {
						panelResp, code, codeErr = retry, c, nil
					}
				}
			}
			if panelResp == nil {
				slog.Info("宠物仙域主面板无回包，稍后重试", "role_id", session.RoleID())
				return scheduler.NewCooldown(30*time.Minute, "宠物仙域主面板无回包")
			}
			slog.Info("宠物仙域主面板已响应", "role_id", session.RoleID(),
				"variant", variant, "payload_len", len(panelResp.Payload),
				"payload_hex", hex.EncodeToString(panelResp.Payload))
			if codeErr != nil {
				slog.Warn("宠物仙域主面板解析失败，稍后重试", "role_id", session.RoleID(), "err", codeErr)
				return scheduler.NewCooldown(10*time.Minute, "宠物仙域主面板解析失败")
			}
			if code != protocol.FairylandSuccess {
				slog.Info("宠物仙域主面板返回失败，2 小时后再试", "role_id", session.RoleID(),
					"variant", variant, "result", code,
					"result_text", protocol.FairylandResultText(code))
				return scheduler.NewCooldown(2*time.Hour, "宠物仙域主面板返回失败")
			}

			taskResp := call(protocol.BuildFairylandTaskPanelPacket(), protocol.ActionIDFairylandTaskPanel)
			if taskResp == nil {
				slog.Info("宠物仙域任务面板无回包，稍后重试", "role_id", session.RoleID())
				return scheduler.NewCooldown(30*time.Minute, "宠物仙域任务面板无回包")
			}
			taskCode, tErr := protocol.ParseActionFirstByte(taskResp.Payload, "宠物仙域任务面板")
			if tErr != nil {
				slog.Warn("宠物仙域任务面板解析失败，稍后重试", "role_id", session.RoleID(), "err", tErr)
				return scheduler.NewCooldown(10*time.Minute, "宠物仙域任务面板解析失败")
			}
			if taskCode != protocol.FairylandSuccess {
				slog.Info("宠物仙域任务面板不可用，稍后重试", "role_id", session.RoleID(),
					"result", taskCode, "result_text", protocol.FairylandResultText(taskCode))
				return scheduler.NewCooldown(2*time.Hour, "宠物仙域任务面板不可用")
			}

			dispatched := 0
			for orderID := int32(1); orderID <= maxFairylandDispatch; orderID++ {
				if ctx.Err() != nil {
					return nil
				}
				dResp := call(protocol.BuildFairylandDispatchPacket(orderID), protocol.ActionIDFairylandDispatch)
				if dResp == nil {
					if abort := caller.AbortErr(); abort != nil {
						return abort
					}
					// 派遣被静默丢弃时不能 return nil：未派遣的订单要等到下一个
					// cron 时段才会重试。按连续丢包退避后继续消费剩余订单。
					slog.Info("宠物仙域游历派遣无回包，按连续丢包退避",
						"role_id", session.RoleID(), "order_id", orderID)
					return scheduler.HandleSilentDropCtx(ctx, session, "fairyland_dispatch", 3,
						30*time.Minute, "宠物仙域游历派遣无回包")
				}
				scheduler.ResetSilentDropsCtx(ctx, session, "fairyland_dispatch")
				dCode, dErr := protocol.ParseActionFirstByte(dResp.Payload, "宠物仙域游历")
				if dErr != nil {
					slog.Warn("宠物仙域游历回包解析失败", "role_id", session.RoleID(),
						"order_id", orderID, "err", dErr)
					return scheduler.NewCooldown(10*time.Minute, "宠物仙域游历回包解析失败")
				}
				switch dCode {
				case protocol.FairylandSuccess:
					dispatched++
					slog.Info("宠物仙域游历派遣成功", "role_id", session.RoleID(), "order_id", orderID)
				case protocol.FairylandOrderCompleted, protocol.FairylandIsGet,
					protocol.FairylandEmptyGrid, protocol.FairylandTimesLimited,
					protocol.FairylandFailed:
					slog.Info("宠物仙域游历结束", "role_id", session.RoleID(),
						"order_id", orderID, "result", dCode,
						"result_text", protocol.FairylandResultText(dCode))
					return nil
				case protocol.FairylandNoIngot:
					slog.Info("宠物仙域返回元宝不足，停止以免误消费", "role_id", session.RoleID())
					return nil
				default:
					slog.Info("宠物仙域游历未成功", "role_id", session.RoleID(),
						"order_id", orderID, "result", dCode,
						"result_text", protocol.FairylandResultText(dCode))
					return nil
				}
			}
			slog.Info("宠物仙域游历完成", "role_id", session.RoleID(), "dispatched", dispatched)
			return nil
		},
	)
}

// nextFairylandWeeklyReset 返回下一次可重试时刻（每天 05:05，游戏侧日常重置点）。
//
// 宠物仙域本周累积游历时间上限为 20 小时；达到上限后 714/1 返回 FAILED。
// 2026-11-16（周一）00:26 实测：00:05 之后仍返回上限，说明周重置不是周一
// 00:00/00:05（游戏内常见为 05:00）。改为每天 05:05 低频探测：若确实是周重置，
// 周一 05:05 之后自然恢复；否则每天一次空探测，成本可接受。
func nextFairylandWeeklyReset(now time.Time) time.Time {
	reset := time.Date(now.Year(), now.Month(), now.Day(), 5, 5, 0, 0, now.Location())
	if !now.Before(reset) {
		reset = reset.AddDate(0, 0, 1)
	}
	return reset
}
