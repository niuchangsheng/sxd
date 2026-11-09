// 本文件实现法相页面「造物佛手 / 守护魔兽」试炼 (Mod_ExtremeTrial_Base, Module 721)。
//
// 流程：main_panel_info(1) 判断开放 → monster_config_info(6) 只读记录守护魔兽配置
// → task_panel_info(2) 领取已完成任务 → score_award_panel_info(4) 领取积分奖励
// → start_challenge(16) 发起一次免费挑战。
// 仅调用面板/领奖/挑战，不修改魔兽属性、不购买次数、不消耗元宝。
package minigame

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// NewFaxiangTrialRoutine 构造法相试炼（造物佛手/守护魔兽）日常任务。
func NewFaxiangTrialRoutine() scheduler.ActivityRoutine {
	const callTimeout = 10 * time.Second
	return scheduler.NewBaseRoutine(
		"faxiang_trial",
		"minigame",
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
					slog.Info("法相试炼协议无回包", "role_id", session.RoleID(), "action", act, "err", err)
					return nil
				}
				return resp
			}

			panelResp := call(protocol.BuildFaxiangTrialPanelPacket(), protocol.ActionIDFaxiangTrialPanel)
			if panelResp == nil {
				slog.Info("法相试炼面板无回包，稍后重试", "role_id", session.RoleID())
				return scheduler.NewCooldown(30*time.Minute, "法相试炼面板无回包")
			}
			code, err := protocol.ParseActionFirstByte(panelResp.Payload, "法相试炼")
			if err != nil {
				slog.Warn("法相试炼面板解析失败，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(10*time.Minute, "法相试炼面板解析失败")
			}
			if code != protocol.FaxiangTrialSuccess {
				slog.Info("法相试炼未开放，稍后重试", "role_id", session.RoleID(),
					"result", code, "result_text", protocol.FaxiangTrialResultText(code))
				return scheduler.NewCooldown(2*time.Hour, "法相试炼未开放")
			}

			// 只读记录守护魔兽配置，便于后续实现造物佛手参数。
			if cfgResp := call(protocol.BuildFaxiangTrialMonsterConfigPacket(), protocol.ActionIDFaxiangTrialMonsterConfig); cfgResp != nil {
				slog.Info("法相试炼守护魔兽配置", "role_id", session.RoleID(), "payload_len", len(cfgResp.Payload))
			}

			// 1. 任务奖励。
			if taskResp := call(protocol.BuildFaxiangTrialTaskPanelPacket(), protocol.ActionIDFaxiangTrialTaskPanel); taskResp != nil {
				_, tasks, tErr := protocol.ParseFaxiangTrialTasks(taskResp.Payload)
				if tErr != nil {
					slog.Warn("法相试炼任务解析失败", "role_id", session.RoleID(), "err", tErr)
				}
				for _, task := range tasks {
					if ctx.Err() != nil {
						return nil
					}
					if task.Progress <= 0 || task.State != 0 {
						continue
					}
					awardResp := call(protocol.BuildFaxiangTrialTaskAwardPacket(task.TaskID), protocol.ActionIDFaxiangTrialTaskAward)
					if awardResp == nil {
						continue
					}
					aCode, aErr := protocol.ParseActionFirstByte(awardResp.Payload, "法相试炼任务奖励")
					if aErr != nil {
						continue
					}
					switch aCode {
					case protocol.FaxiangTrialSuccess:
						slog.Info("法相试炼任务奖励领取成功", "role_id", session.RoleID(), "task_id", task.TaskID)
					case protocol.FaxiangTrialPackFull:
						return scheduler.ErrBagFull
					default:
						slog.Debug("法相试炼任务奖励未领取", "role_id", session.RoleID(),
							"task_id", task.TaskID, "result", aCode)
					}
				}
			}

			// 2. 积分奖励。
			if scoreResp := call(protocol.BuildFaxiangTrialScorePanelPacket(), protocol.ActionIDFaxiangTrialScorePanel); scoreResp != nil {
				ids, sErr := protocol.ParseFaxiangTrialScoreAwardIDs(scoreResp.Payload)
				if sErr != nil {
					slog.Warn("法相试炼积分面板解析失败", "role_id", session.RoleID(), "err", sErr)
				}
				for _, id := range ids {
					awardResp := call(protocol.BuildFaxiangTrialScoreAwardPacket(id), protocol.ActionIDFaxiangTrialScoreAward)
					if awardResp == nil {
						continue
					}
					aCode, aErr := protocol.ParseActionFirstByte(awardResp.Payload, "法相试炼积分奖励")
					if aErr != nil {
						continue
					}
					switch aCode {
					case protocol.FaxiangTrialSuccess:
						slog.Info("法相试炼积分奖励领取成功", "role_id", session.RoleID(), "award_id", id)
					case protocol.FaxiangTrialPackFull:
						return scheduler.ErrBagFull
					default:
						slog.Debug("法相试炼积分奖励未领取", "role_id", session.RoleID(),
							"award_id", id, "result", aCode)
					}
				}
			}

			// 3. 免费挑战一次。
			challengeResp := call(protocol.BuildFaxiangTrialStartChallengePacket(), protocol.ActionIDFaxiangTrialStartChallenge)
			if challengeResp == nil {
				return nil
			}
			cCode, cErr := protocol.ParseActionFirstByte(challengeResp.Payload, "法相试炼挑战")
			if cErr != nil {
				return nil
			}
			switch cCode {
			case protocol.FaxiangTrialSuccess:
				slog.Info("法相试炼挑战完成", "role_id", session.RoleID())
			case protocol.FaxiangTrialNoTimes, protocol.FaxiangTrialTimesLimited,
				protocol.FaxiangTrialLowScore, protocol.FaxiangTrialNoEnoughScore:
				slog.Info("法相试炼挑战结束", "role_id", session.RoleID(),
					"result", cCode, "result_text", protocol.FaxiangTrialResultText(cCode))
			case protocol.FaxiangTrialPackFull:
				return scheduler.ErrBagFull
			default:
				slog.Info("法相试炼挑战未成功", "role_id", session.RoleID(),
					"result", cCode, "result_text", protocol.FaxiangTrialResultText(cCode))
			}
			return nil
		},
	)
}
