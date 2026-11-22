// 本文件实现周末狂欢礼 (637) 与周末水果机 (242) 的免费奖励领取。
package daily

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

// maxSundayFruitDraws 单轮水果机抽奖安全上限。
const maxSundayFruitDraws = 50

// NewWeekendEventRoutine 构造周末狂欢礼 + 周末水果机任务。
func NewWeekendEventRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"weekend_event",
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
					slog.Info("周末活动协议无回包", "role_id", session.RoleID(), "action", act, "err", err)
					return nil
				}
				return resp
			}

			// 1. 周末狂欢礼：逐个槽位领取免费奖励。
			if panelResp := call(protocol.BuildWeekendGiftPanelPacket(), protocol.ActionIDWeekendGiftPanel); panelResp != nil {
				slog.Info("周末狂欢礼面板", "role_id", session.RoleID(), "payload_len", len(panelResp.Payload))
				panel, pErr := protocol.ParseWeekendGiftPanel(panelResp.Payload)
				if pErr != nil {
					slog.Warn("解析周末狂欢礼面板失败", "role_id", session.RoleID(), "err", pErr)
					goto fruit
				}
				claimDays := make([]int32, 0, len(panel.Days))
				for _, d := range panel.Days {
					// 只有 can_receive 的日期可领；其余日期请求会被服务端静默丢弃。
					if d.CanReceive != 0 {
						claimDays = append(claimDays, d.Day)
					}
				}
				slog.Info("周末狂欢礼可领取日期", "role_id", session.RoleID(),
					"days", claimDays, "curr_day", panel.CurrDay)
				for _, day := range claimDays {
					if ctx.Err() != nil {
						return nil
					}
					resp := call(protocol.BuildWeekendGiftAwardPacket(day), protocol.ActionIDWeekendGiftAward)
					if resp == nil {
						// 周末狂欢礼不可用时仍继续尝试水果机，两者独立。
						goto fruit
					}
					code, err := protocol.ParseFirstByteResult(resp.Payload, "周末狂欢礼")
					if err != nil {
						slog.Warn("解析周末狂欢礼回包失败", "role_id", session.RoleID(), "err", err)
						goto fruit
					}
					switch code {
					case protocol.WeekendGiftResultSuccess:
						slog.Info("周末狂欢礼领取成功", "role_id", session.RoleID(), "day", day)
					case protocol.WeekendGiftResultHasGet, protocol.WeekendGiftResultCannotGet,
						protocol.WeekendGiftResultDayError:
						slog.Debug("周末狂欢礼暂不可领取", "role_id", session.RoleID(), "day", day, "result", code)
					case protocol.WeekendGiftResultPackFull:
						return scheduler.ErrBagFull
					case protocol.WeekendGiftResultNotOpen:
						slog.Info("周末狂欢礼未开放", "role_id", session.RoleID())
						goto fruit
					case protocol.WeekendGiftResultNotEnoughIngot:
						slog.Info("周末狂欢礼该档需要元宝，跳过", "role_id", session.RoleID(), "day", day)
					default:
						slog.Info("周末狂欢礼领取未成功", "role_id", session.RoleID(), "day", day, "result", code)
					}
				}
			}

		fruit:
			if abort := caller.AbortErr(); abort != nil {
				return abort
			}
			// 2. 周末水果机：仅免费次数。
			activeResp := call(protocol.BuildSundayFruitActivePacket(), protocol.ActionIDSundayFruitActive)
			if activeResp == nil {
				if abort := caller.AbortErr(); abort != nil {
					return abort
				}
				return scheduler.NewCooldown(30*time.Minute, "周末水果机状态无回包")
			}
			active, err := protocol.ParseFirstByteResult(activeResp.Payload, "水果机")
			if err != nil {
				slog.Warn("周末水果机状态解析失败，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(10*time.Minute, "周末水果机状态解析失败")
			}
			if active != protocol.SundayFruitResultYes {
				slog.Info("周末水果机未开启，稍后重试", "role_id", session.RoleID(), "status", active)
				return scheduler.NewCooldown(6*time.Hour, "周末水果机未开启")
			}
			if infoResp := call(protocol.BuildSundayFruitInfoPacket(), protocol.ActionIDSundayFruitInfo); infoResp != nil {
				slog.Info("周末水果机面板", "role_id", session.RoleID(), "payload_len", len(infoResp.Payload))
			} else if abort := caller.AbortErr(); abort != nil {
				return abort
			}
			for draws := 0; draws < maxSundayFruitDraws; draws++ {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				resp := call(protocol.BuildSundayFruitDrawPacket(), protocol.ActionIDSundayFruitDraw)
				if resp == nil {
					if abort := caller.AbortErr(); abort != nil {
						return abort
					}
					slog.Info("周末水果机抽奖无回包，按连续丢包退避",
						"role_id", session.RoleID(), "draws", draws)
					return scheduler.HandleSilentDropCtx(ctx, session, "weekend_event", 3,
						10*time.Minute, "周末水果机抽奖无回包")
				}
				scheduler.ResetSilentDropsCtx(ctx, session, "weekend_event")
				code, err := protocol.ParseFirstByteResult(resp.Payload, "水果机")
				if err != nil {
					slog.Warn("周末水果机抽奖回包解析失败，稍后重试",
						"role_id", session.RoleID(), "err", err)
					return scheduler.NewCooldown(10*time.Minute, "周末水果机抽奖回包解析失败")
				}
				switch code {
				case protocol.SundayFruitResultSuccess:
					slog.Info("周末水果机抽奖成功", "role_id", session.RoleID(), "draw", draws+1)
				case protocol.SundayFruitResultTimeUsed:
					slog.Info("周末水果机免费次数已用尽", "role_id", session.RoleID(), "draws", draws)
					return scheduler.ErrAttemptsExhausted
				case protocol.SundayFruitResultPackFull:
					return scheduler.ErrBagFull
				case protocol.SundayFruitResultActiveNotOpen:
					slog.Info("周末水果机未开放，2 小时后再试", "role_id", session.RoleID())
					return scheduler.NewCooldown(2*time.Hour, "周末水果机未开放")
				default:
					slog.Info("周末水果机抽奖未成功", "role_id", session.RoleID(), "result", code)
					return nil
				}
			}
			return nil
		},
	)
}
