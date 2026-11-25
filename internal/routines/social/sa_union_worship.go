// 本文件实现圣盟膜拜 (Mod_SaUnionWorship_Base, Module 419)。
//
// 仅使用 COIN=0 铜钱/免费档；INGOT=1 元宝档永久禁用。
package social

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
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
			caller := kit.NewCaller(ctx, session, callTimeout)
			call := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
				resp, err := caller.Call(pkt, act)
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
			} else if abort := caller.AbortErr(); abort != nil {
				return abort
			}
			// 与仙盟上香/万古膜拜相同的场景写操作：先复位城镇再膜拜；
			// 仅当写请求在老会话上被静默丢弃时按需重建一次，避免今日已膜拜时平白断连。
			if !session.ReturnToTown(ctx, 5*time.Second) {
				slog.Info("圣盟膜拜前返回城镇失败，继续尝试", "role_id", session.RoleID())
			}
			resp := call(protocol.BuildSaUnionWorshipPacket(protocol.SaUnionWorshipUseCoin), protocol.ActionIDSaUnionWorship)
			if resp == nil && caller.AbortErr() == nil {
				if since := session.ConnectionSince(); !since.IsZero() && time.Since(since) > client.HealthySessionAge {
					if err := session.EnsureFreshConnection(ctx, client.HealthySessionAge); err != nil {
						return err
					}
					_ = call(protocol.BuildSaUnionWorshipPanelPacketWithPrev(session.LastActionID()), protocol.ActionIDSaUnionWorshipPanel)
					_ = session.ReturnToTown(ctx, 5*time.Second)
					resp = call(protocol.BuildSaUnionWorshipPacket(protocol.SaUnionWorshipUseCoin), protocol.ActionIDSaUnionWorship)
				}
			}
			if resp == nil {
				if abort := caller.AbortErr(); abort != nil {
					return abort
				}
				return scheduler.NewCooldown(30*time.Minute, "圣盟膜拜无回包")
			}
			code, err := protocol.ParseSaUnionWorshipResult(resp.Payload)
			if err != nil {
				slog.Warn("解析圣盟膜拜回包失败", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(30*time.Minute, "圣盟膜拜回包解析失败")
			}
			switch code {
			case protocol.SaUnionWorshipResultSuccess:
				scheduler.ResetSilentDropsCtx(ctx, session, "sa_union_worship")
				slog.Info("圣盟膜拜成功", "role_id", session.RoleID())
			case protocol.SaUnionWorshipResultPackFull:
				return scheduler.ErrBagFull
			case protocol.SaUnionWorshipResultJoinToday, protocol.SaUnionWorshipResultIsVoted,
				protocol.SaUnionWorshipResultNoTime:
				scheduler.ResetSilentDropsCtx(ctx, session, "sa_union_worship")
				slog.Info("圣盟膜拜今日已完成", "role_id", session.RoleID(),
					"result", code, "result_text", protocol.SaUnionWorshipResultText(code))
			default:
				// 有明确结果码说明请求已被受理，属业务失败（如当前无膜拜目标），
				// 不能计入静默丢包退避。
				slog.Info("圣盟膜拜未成功，稍后重试", "role_id", session.RoleID(),
					"result", code, "result_text", protocol.SaUnionWorshipResultText(code))
				return scheduler.NewCooldown(time.Hour, "圣盟膜拜业务失败")
			}
			return nil
		},
	)
}
