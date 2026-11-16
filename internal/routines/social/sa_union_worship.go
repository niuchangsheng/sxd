// 本文件实现万古神墟/圣盟膜拜 (Mod_SaUnionWorship_Base, Module 419)。
//
// 仅使用 COIN=0 铜钱/免费档；INGOT=1 元宝档永久禁用。
package social

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// NewSaUnionWorshipRoutine 构造万古神墟膜拜任务。
func NewSaUnionWorshipRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"sa_union_worship",
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
				cCtx, cancel := context.WithTimeout(ctx, callTimeout)
				defer cancel()
				resp, err := session.Call(cCtx, pkt, act)
				if err != nil {
					slog.Info("圣盟膜拜协议无回包", "role_id", session.RoleID(), "action", act, "err", err)
					return nil
				}
				return resp
			}

			if panelResp := call(protocol.BuildSaUnionWorshipPanelPacketWithPrev(session.LastActionID()), protocol.ActionIDSaUnionWorshipPanel); panelResp != nil {
				if vals, vErr := protocol.ParseSaUnionWorshipPanel(panelResp.Payload); vErr != nil {
					slog.Warn("解析圣盟膜拜面板失败", "role_id", session.RoleID(), "err", vErr)
				} else {
					slog.Info("圣盟膜拜面板", "role_id", session.RoleID(),
						"payload_len", len(panelResp.Payload), "values", vals)
				}
			}
			resp := call(protocol.BuildSaUnionWorshipPacket(protocol.SaUnionWorshipUseCoin), protocol.ActionIDSaUnionWorship)
			if resp == nil {
				return scheduler.NewCooldown(30*time.Minute, "圣盟膜拜无回包")
			}
			code, err := protocol.ParseSaUnionWorshipResult(resp.Payload)
			if err != nil {
				slog.Warn("解析圣盟膜拜回包失败", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(30*time.Minute, "圣盟膜拜回包解析失败")
			}
			switch code {
			case protocol.SaUnionWorshipResultSuccess:
				scheduler.ResetSilentDrops(session, "sa_union_worship")
				slog.Info("圣盟膜拜成功", "role_id", session.RoleID())
			case protocol.SaUnionWorshipResultPackFull:
				return scheduler.ErrBagFull
			case protocol.SaUnionWorshipResultJoinToday, protocol.SaUnionWorshipResultIsVoted,
				protocol.SaUnionWorshipResultNoTime:
				scheduler.ResetSilentDrops(session, "sa_union_worship")
				slog.Info("圣盟膜拜今日已完成", "role_id", session.RoleID(),
					"result", code, "result_text", protocol.SaUnionWorshipResultText(code))
			default:
				// 线上长期返回 8「失败」且 419/1 面板 11 个字段全为 0（活动未开放/
				// 圣盟未分配），每小时重试没有意义：连续 3 次后挂起（最长 6 小时）。
				slog.Info("圣盟膜拜未成功，稍后重试", "role_id", session.RoleID(),
					"result", code, "result_text", protocol.SaUnionWorshipResultText(code))
				return scheduler.HandleSilentDrop(session, "sa_union_worship", 3,
					time.Hour, "圣盟膜拜失败")
			}
			return nil
		},
	)
}
