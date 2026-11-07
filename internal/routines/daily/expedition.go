// 本文件实现仙界远征任务 (Mod_ExpeditionTask_Base, Module 430)。
//
// get_info(0) 读取 5 条远征任务 → 对未开始的任务调用 start_task(1) 派遣。
// 仅派遣免费任务，不涉及元宝购买。
package daily

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// NewExpeditionRoutine 构造仙界远征任务日常任务。
func NewExpeditionRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"expedition",
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
					slog.Info("远征任务协议无回包", "role_id", session.RoleID(), "action", act, "err", err)
					return nil
				}
				return resp
			}

			infoResp := call(protocol.BuildExpeditionInfoPacket(), protocol.ActionIDExpeditionInfo)
			if infoResp == nil {
				return nil
			}
			tasks, err := protocol.ParseExpeditionInfo(infoResp.Payload)
			if err != nil {
				slog.Warn("远征任务面板解析失败", "role_id", session.RoleID(), "err", err)
				return nil
			}
			slog.Info("远征任务面板", "role_id", session.RoleID(), "tasks", len(tasks))

			started := 0
			for _, task := range tasks {
				if ctx.Err() != nil {
					return nil
				}
				startResp := call(protocol.BuildExpeditionStartPacket(task.MissionID), protocol.ActionIDExpeditionStart)
				if startResp == nil {
					return nil
				}
				code, sErr := protocol.ParseExpeditionResult(startResp.Payload)
				if sErr != nil {
					slog.Warn("远征任务开始回包解析失败", "role_id", session.RoleID(), "err", sErr)
					continue
				}
				switch code {
				case protocol.ExpeditionSuccess:
					started++
					slog.Info("远征任务已派遣", "role_id", session.RoleID(), "mission_id", task.MissionID)
				case protocol.ExpeditionAlreadyStart:
					slog.Info("远征任务已在派遣中", "role_id", session.RoleID(), "mission_id", task.MissionID)
				case protocol.ExpeditionOverGet, protocol.ExpeditionFailed:
					slog.Info("远征任务不可派遣", "role_id", session.RoleID(),
						"mission_id", task.MissionID, "result", code,
						"result_text", protocol.ExpeditionResultText(code))
				default:
					slog.Info("远征任务开始未成功", "role_id", session.RoleID(),
						"mission_id", task.MissionID, "result", code,
						"result_text", protocol.ExpeditionResultText(code))
				}
			}
			slog.Info("远征任务巡检完成", "role_id", session.RoleID(), "started", started)
			return nil
		},
	)
}
