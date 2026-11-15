// 本文件实现无尽宝箱 (Mod_EndlessBox_Base, Module 690)。
//
// 用户确认：无尽宝箱只在「宝箱狂欢周」触发（690/4 返回活动起止时间）。
// 活动窗口外只做时间判断并等待，不发送开箱请求。
//
//	690/4 activity → 690/1 面板 → 690/2 开箱（单开/30 连）→ 690/6 领取全部奖励
//
// 严禁调用 690/5 buy_box（元宝购买）；开箱仅消耗已拥有的宝箱。
package social

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// maxEndlessBoxOpens 单轮最多开箱请求数（每种箱子最多一次，1 或 30 连）。
const maxEndlessBoxOpens = 6

// NewEndlessBoxRoutine 构造无尽宝箱开箱任务。
func NewEndlessBoxRoutine() scheduler.ActivityRoutine {
	const callTimeout = 10 * time.Second
	return scheduler.NewBaseRoutine(
		"endless_box",
		"social",
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
				cCtx, cCancel := context.WithTimeout(ctx, callTimeout)
				defer cCancel()
				resp, err := session.Call(cCtx, pkt, act)
				if err != nil {
					slog.Info("无尽宝箱协议无回包", "role_id", session.RoleID(),
						"action", act, "err", err)
					return nil
				}
				return resp
			}

			actResp := call(protocol.BuildEndlessBoxActivityPacket(), protocol.ActionIDEndlessBoxActivity)
			if actResp == nil {
				return scheduler.NewCooldown(30*time.Minute, "无尽宝箱活动面板无回包")
			}
			act, aErr := protocol.ParseEndlessBoxActivity(actResp.Payload)
			if aErr != nil {
				slog.Warn("解析无尽宝箱活动面板失败", "role_id", session.RoleID(), "err", aErr)
				return nil
			}
			now := int32(time.Now().Unix())
			if act.StartTime <= 0 || act.EndTime <= 0 || now < act.StartTime || now > act.EndTime {
				var wait time.Duration
				if act.StartTime > now {
					wait = time.Until(time.Unix(int64(act.StartTime), 0))
					slog.Info("宝箱狂欢周未开启，等待开启", "role_id", session.RoleID(),
						"start", act.StartTime, "end", act.EndTime, "wait", wait)
				} else {
					wait = 2 * time.Hour
					slog.Info("宝箱狂欢周已结束", "role_id", session.RoleID(),
						"start", act.StartTime, "end", act.EndTime)
				}
				return scheduler.NewCooldown(wait, "宝箱狂欢周未开启")
			}
			slog.Info("宝箱狂欢周已开启", "role_id", session.RoleID(),
				"start", act.StartTime, "end", act.EndTime)

			panelResp := call(protocol.BuildEndlessBoxPanelPacket(), protocol.ActionIDEndlessBoxPanel)
			if panelResp == nil {
				return scheduler.NewCooldown(30*time.Minute, "无尽宝箱面板无回包")
			}
			panel, pErr := protocol.ParseEndlessBoxPanel(panelResp.Payload)
			if pErr != nil {
				slog.Warn("解析无尽宝箱面板失败", "role_id", session.RoleID(), "err", pErr)
				return nil
			}
			slog.Info("无尽宝箱面板", "role_id", session.RoleID(),
				"score", panel.Score, "boxes", panel.Boxes, "cost_id", panel.CostID)

			opens := 0
			for _, box := range panel.Boxes {
				if opens >= maxEndlessBoxOpens || ctx.Err() != nil {
					break
				}
				if box.Type <= 0 || box.Num <= 0 {
					continue
				}
				openType := int32(0) // 单开
				if box.Num >= 30 {
					openType = 1 // 30 连
				}
				resp := call(protocol.BuildEndlessBoxOpenPacket(box.Type, openType),
					protocol.ActionIDEndlessBoxOpen)
				if resp == nil {
					return scheduler.NewCooldown(10*time.Minute, "无尽宝箱开箱无回包")
				}
				code, cErr := protocol.ParseEndlessBoxResult(resp.Payload)
				if cErr != nil {
					return scheduler.NewCooldown(10*time.Minute, "无尽宝箱开箱回包解析失败")
				}
				switch code {
				case protocol.EndlessBoxSuccess:
					opens++
					slog.Info("无尽宝箱开箱成功", "role_id", session.RoleID(),
						"box_type", box.Type, "open_type", openType, "owned", box.Num)
				case protocol.EndlessBoxPackFull:
					return scheduler.ErrBagFull
				case protocol.EndlessBoxNotOpen:
					slog.Info("无尽宝箱未开放，结束本轮", "role_id", session.RoleID())
					return scheduler.NewCooldown(2*time.Hour, "无尽宝箱未开放")
				case protocol.EndlessBoxNotEnoughBox, protocol.EndlessBoxNoScore,
					protocol.EndlessBoxLimitNum:
					slog.Info("无尽宝箱无法继续开启", "role_id", session.RoleID(),
						"box_type", box.Type, "result", code,
						"result_text", protocol.EndlessBoxResultText(code))
					continue
				default:
					slog.Info("无尽宝箱开箱未成功", "role_id", session.RoleID(),
						"box_type", box.Type, "result", code,
						"result_text", protocol.EndlessBoxResultText(code))
					continue
				}
			}

			if gainResp := call(protocol.BuildEndlessBoxGainAllPacket(),
				protocol.ActionIDEndlessBoxGainAll); gainResp != nil {
				if code, gErr := protocol.ParseEndlessBoxResult(gainResp.Payload); gErr == nil {
					if code == protocol.EndlessBoxSuccess {
						slog.Info("无尽宝箱奖励领取成功", "role_id", session.RoleID())
					} else {
						slog.Info("无尽宝箱奖励未领取", "role_id", session.RoleID(),
							"result", code, "result_text", protocol.EndlessBoxResultText(code))
					}
				}
			}
			slog.Info("无尽宝箱本轮完成", "role_id", session.RoleID(), "opens", opens)
			return nil
		},
	)
}
