// 本文件实现三界修炼 (Mod_ThreeWorld 529 / 人界修炼 Mod_HumanWorld 531)。
//
// 529 get_status(1) 返回三界状态；人界修炼通过 531 practice_info(12) 读取状态，
// 空闲时 start_practice(13)，已完成时 stop_practice(14) 结算。不使用加速(15)。
package daily

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// NewThreeWorldRoutine 构造三界修炼日常任务。
func NewThreeWorldRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"three_world",
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
					slog.Info("三界修炼协议无回包", "role_id", session.RoleID(), "action", act, "err", err)
					return nil
				}
				return resp
			}

			// 1. 三界总状态（入口可能只在跨服会话可达，失败不阻断人界修炼）。
			// 实测 sub=4 表示 IDLE，此时应开始人界修炼。
			threeWorldSub := int32(-1)
			if statusResp := call(protocol.BuildThreeWorldStatusPacket(), protocol.ActionIDThreeWorldStatus); statusResp != nil {
				st, err := protocol.ParseThreeWorldStatus(statusResp.Payload)
				if err != nil {
					slog.Warn("三界状态解析失败", "role_id", session.RoleID(), "err", err)
				} else {
					threeWorldSub = int32(st.Sub)
					slog.Info("三界修炼状态", "role_id", session.RoleID(),
						"state", st.State, "sub", st.Sub, "values", []int32{st.V1, st.V2, st.V3})
				}
			}

			// 2. 人界修炼状态。
			infoResp := call(protocol.BuildHumanWorldPracticePacket(), protocol.ActionIDHumanWorldPractice)
			if infoResp == nil {
				return nil
			}
			res, vals, err := protocol.ParseHumanWorldPractice(infoResp.Payload)
			if err != nil {
				slog.Warn("人界修炼面板解析失败", "role_id", session.RoleID(), "err", err)
				return nil
			}
			if res != protocol.ThreeWorldSuccess {
				slog.Info("人界修炼不可用", "role_id", session.RoleID(),
					"result", res, "result_text", protocol.ThreeWorldResultText(res))
				return nil
			}
			slog.Info("人界修炼面板", "role_id", session.RoleID(), "values", vals)

			// 优先使用 529 sub 状态：4 IDLE / 5 PRACTICE / 6 COMPLETE；否则回退到 531 面板。
			state := threeWorldSub
			if state != int32(protocol.ThreeWorldIdle) && state != int32(protocol.ThreeWorldPractice) &&
				state != int32(protocol.ThreeWorldComplete) && len(vals) > 3 {
				state = vals[3]
			}
			switch state {
			case int32(protocol.ThreeWorldIdle):
				startResp := call(protocol.BuildHumanWorldStartPacket(), protocol.ActionIDHumanWorldStart)
				if startResp == nil {
					return nil
				}
				code, sErr := protocol.ParseActionFirstByte(startResp.Payload, "人界开始修炼")
				if sErr != nil {
					return nil
				}
				slog.Info("人界开始修炼结果", "role_id", session.RoleID(),
					"result", code, "result_text", protocol.ThreeWorldResultText(code))
			case int32(protocol.ThreeWorldComplete):
				stopResp := call(protocol.BuildHumanWorldStopPacket(), protocol.ActionIDHumanWorldStop)
				if stopResp == nil {
					return nil
				}
				code, sErr := protocol.ParseActionFirstByte(stopResp.Payload, "人界结算修炼")
				if sErr != nil {
					return nil
				}
				slog.Info("人界结算修炼结果", "role_id", session.RoleID(),
					"result", code, "result_text", protocol.ThreeWorldResultText(code))
			default:
				slog.Info("人界修炼状态无需操作", "role_id", session.RoleID(), "state", state)
			}
			return nil
		},
	)
}
