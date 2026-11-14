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
			call := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
				cCtx, cancel := context.WithTimeout(ctx, callTimeout)
				defer cancel()
				resp, err := session.Call(cCtx, pkt, act)
				if err != nil {
					slog.Info("宠物仙域协议无回包", "role_id", session.RoleID(), "action", act, "err", err)
					return nil
				}
				return resp
			}

			panelResp := call(protocol.BuildFairylandMainPanelPacket(), protocol.ActionIDFairylandMainPanel)
			variant := "bare"
			if panelResp == nil {
				// 裸载荷被静默丢弃时，改用服务端最后受理的动作号作为显式锚点重试一次。
				anchor := session.LastAckActionID()
				variant = fmt.Sprintf("prev_act:0x%08X", anchor)
				panelResp = call(protocol.BuildFairylandMainPanelWithPrevActPacket(anchor),
					protocol.ActionIDFairylandMainPanel)
			}
			if panelResp == nil {
				slog.Info("宠物仙域主面板无回包，稍后重试", "role_id", session.RoleID())
				return scheduler.NewCooldown(30*time.Minute, "宠物仙域主面板无回包")
			}
			slog.Info("宠物仙域主面板已响应", "role_id", session.RoleID(),
				"variant", variant, "payload_len", len(panelResp.Payload),
				"payload_hex", hex.EncodeToString(panelResp.Payload))
			code, err := protocol.ParseActionFirstByte(panelResp.Payload, "宠物仙域主面板")
			if err != nil {
				slog.Warn("宠物仙域主面板解析失败，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(10*time.Minute, "宠物仙域主面板解析失败")
			}
			if code != protocol.FairylandSuccess {
				// 用户实测开放时间 08:00~20:00；窗口内仍失败则 30 分钟后再试，
				// 窗口外按下一次 08:00 唤醒，避免整点/整小时空轮询。
				wait := fairylandRetryDelay(time.Now())
				slog.Info("宠物仙域未开放，等待开放时间", "role_id", session.RoleID(),
					"result", code, "result_text", protocol.FairylandResultText(code),
					"retry_in", wait)
				return scheduler.NewCooldown(wait, "宠物仙域未开放")
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
					return nil
				}
				dCode, dErr := protocol.ParseActionFirstByte(dResp.Payload, "宠物仙域游历")
				if dErr != nil {
					return nil
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

// fairylandRetryDelay 返回宠物仙域未开放时的重试间隔。
//
// 开放时间 08:00~20:00：窗口内失败按 30 分钟复查；窗口外精确等待次日 08:00。
func fairylandRetryDelay(now time.Time) time.Duration {
	start := time.Date(now.Year(), now.Month(), now.Day(), 8, 0, 0, 0, now.Location())
	end := time.Date(now.Year(), now.Month(), now.Day(), 20, 0, 0, 0, now.Location())
	if now.Before(start) {
		return start.Sub(now)
	}
	if now.Before(end) {
		return 30 * time.Minute
	}
	return start.AddDate(0, 0, 1).Sub(now)
}
