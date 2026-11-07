// 本文件实现宠物庭院灵符召唤 (Mod_CourtyardPet_Base, Module 216)。
//
// 仅使用 call(NORMAL=0) 消耗灵符；call(INGOT=1) 永久禁用。
package daily

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// maxCourtyardPetCallsPerRun 单轮召唤安全上限。
const maxCourtyardPetCallsPerRun = 200

// NewCourtyardPetRoutine 构造宠物灵符召唤任务。
func NewCourtyardPetRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"courtyard_pet",
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
					slog.Info("宠物庭院协议无回包", "role_id", session.RoleID(), "action", act, "err", err)
					return nil
				}
				return resp
			}
			query := func() *protocol.CourtyardPetPlayerInfo {
				resp := call(protocol.BuildCourtyardPetInfoPacket(), protocol.ActionIDCourtyardPetInfo)
				if resp == nil {
					return nil
				}
				info, err := protocol.ParseCourtyardPetInfo(resp.Payload)
				if err != nil {
					slog.Warn("解析宠物庭院信息失败", "role_id", session.RoleID(), "err", err)
					return nil
				}
				return info
			}

			info := query()
			if info == nil {
				slog.Info("宠物庭院功能不可用或未开放，跳过", "role_id", session.RoleID())
				return nil
			}
			remain := info.TalismanCount
			slog.Info("宠物庭院灵符状态", "role_id", session.RoleID(), "talisman", remain)
			if remain <= 0 {
				slog.Info("宠物庭院无灵符，跳过召唤", "role_id", session.RoleID())
				return nil
			}

			calls := 0
			for remain > 0 && calls < maxCourtyardPetCallsPerRun {
				if ctx.Err() != nil {
					return nil
				}
				resp := call(protocol.BuildCourtyardPetCallPacket(protocol.CourtyardPetUseNormal),
					protocol.ActionIDCourtyardPetCall)
				if resp == nil {
					return nil
				}
				code, err := protocol.ParseCourtyardPetResultCode(resp.Payload)
				if err != nil {
					slog.Warn("解析宠物召唤回包失败", "role_id", session.RoleID(), "err", err)
					return nil
				}
				switch code {
				case protocol.CourtyardPetResultSuccess:
					calls++
					slog.Info("灵符召唤宠物成功", "role_id", session.RoleID(), "calls", calls)
				case protocol.CourtyardPetResultTimesUsedUp, protocol.CourtyardPetResultNotEnoughCount,
					protocol.CourtyardPetResultReachMax, protocol.CourtyardPetResultNoEnoughPet:
					slog.Info("灵符召唤停止", "role_id", session.RoleID(),
						"result", code, "result_text", protocol.CourtyardPetResultText(code))
					return nil
				case protocol.CourtyardPetResultNoEnoughIngot:
					slog.Warn("灵符召唤返回元宝不足，停止", "role_id", session.RoleID())
					return nil
				default:
					slog.Info("灵符召唤未成功", "role_id", session.RoleID(),
						"result", code, "result_text", protocol.CourtyardPetResultText(code))
					return nil
				}

				next := query()
				if next == nil {
					return nil
				}
				if next.TalismanCount >= remain {
					slog.Warn("宠物庭院灵符数量未递减，熔断退出",
						"role_id", session.RoleID(), "before", remain, "after", next.TalismanCount)
					return nil
				}
				remain = next.TalismanCount
				if jitter != nil {
					_ = jitter.Wait(ctx)
				}
			}
			slog.Info("宠物灵符召唤巡检完成", "role_id", session.RoleID(), "calls", calls, "remain", remain)
			return nil
		},
	)
}
