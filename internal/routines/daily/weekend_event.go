// 本文件实现周末狂欢礼 (637) 与周末水果机 (242) 的免费奖励领取。
package daily

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
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
			call := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
				cCtx, cancel := context.WithTimeout(ctx, callTimeout)
				defer cancel()
				resp, err := session.Call(cCtx, pkt, act)
				if err != nil {
					slog.Info("周末活动协议无回包", "role_id", session.RoleID(), "action", act, "err", err)
					return nil
				}
				return resp
			}

			// 1. 周末狂欢礼：逐个槽位领取免费奖励。
			if panelResp := call(protocol.BuildWeekendGiftPanelPacket(), protocol.ActionIDWeekendGiftPanel); panelResp != nil {
				slog.Info("周末狂欢礼面板", "role_id", session.RoleID(), "payload_len", len(panelResp.Payload))
				for idx := int32(0); idx < 4; idx++ {
					if ctx.Err() != nil {
						return nil
					}
					resp := call(protocol.BuildWeekendGiftAwardPacket(idx), protocol.ActionIDWeekendGiftAward)
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
						slog.Info("周末狂欢礼领取成功", "role_id", session.RoleID(), "index", idx)
					case protocol.WeekendGiftResultHasGet, protocol.WeekendGiftResultCannotGet,
						protocol.WeekendGiftResultDayError:
						slog.Debug("周末狂欢礼暂不可领取", "role_id", session.RoleID(), "index", idx, "result", code)
					case protocol.WeekendGiftResultPackFull:
						return scheduler.ErrBagFull
					case protocol.WeekendGiftResultNotOpen:
						slog.Info("周末狂欢礼未开放", "role_id", session.RoleID())
						goto fruit
					case protocol.WeekendGiftResultNotEnoughIngot:
						slog.Info("周末狂欢礼该档需要元宝，跳过", "role_id", session.RoleID(), "index", idx)
					default:
						slog.Info("周末狂欢礼领取未成功", "role_id", session.RoleID(), "index", idx, "result", code)
					}
				}
			}

		fruit:
			// 2. 周末水果机：仅免费次数。
			activeResp := call(protocol.BuildSundayFruitActivePacket(), protocol.ActionIDSundayFruitActive)
			if activeResp == nil {
				return nil
			}
			active, err := protocol.ParseFirstByteResult(activeResp.Payload, "水果机")
			if err != nil {
				return nil
			}
			if active != protocol.SundayFruitResultYes {
				slog.Info("周末水果机未开启", "role_id", session.RoleID(), "status", active)
				return nil
			}
			if infoResp := call(protocol.BuildSundayFruitInfoPacket(), protocol.ActionIDSundayFruitInfo); infoResp != nil {
				slog.Info("周末水果机面板", "role_id", session.RoleID(), "payload_len", len(infoResp.Payload))
			}
			for draws := 0; draws < maxSundayFruitDraws; draws++ {
				if ctx.Err() != nil {
					return nil
				}
				resp := call(protocol.BuildSundayFruitDrawPacket(), protocol.ActionIDSundayFruitDraw)
				if resp == nil {
					return nil
				}
				code, err := protocol.ParseFirstByteResult(resp.Payload, "水果机")
				if err != nil {
					return nil
				}
				switch code {
				case protocol.SundayFruitResultSuccess:
					slog.Info("周末水果机抽奖成功", "role_id", session.RoleID(), "draw", draws+1)
				case protocol.SundayFruitResultTimeUsed:
					slog.Info("周末水果机免费次数已用尽", "role_id", session.RoleID(), "draws", draws)
					return nil
				case protocol.SundayFruitResultPackFull:
					return scheduler.ErrBagFull
				case protocol.SundayFruitResultActiveNotOpen:
					slog.Info("周末水果机未开放", "role_id", session.RoleID())
					return nil
				default:
					slog.Info("周末水果机抽奖未成功", "role_id", session.RoleID(), "result", code)
					return nil
				}
			}
			return nil
		},
	)
}
