// 本文件实现纪元造化秘境 (Mod_EraCreationRealm_Base, Module 866) 日常自动化。
//
// 官方流程：get_panel_info(1) → explore(6) 探索 → fight_event(7) 取事件 →
// fight(10) 挑战事件 → fight_award_panel(11)/choose_fight_award(12) 领奖。
// 仅调用免费探索/挑战入口，不调用 active_privilege 等付费动作。
package minigame

import (
	"context"
	"encoding/hex"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// maxEraCreationFights 单轮最多挑战事件数。
const maxEraCreationFights = 5

// NewEraCreationRoutine 构造纪元造化秘境日常任务。
func NewEraCreationRoutine() scheduler.ActivityRoutine {
	const callTimeout = 15 * time.Second
	return scheduler.NewBaseRoutine(
		"era_creation",
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
			if open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDEraCreationRealm); fErr != nil {
				slog.Info("造化秘境功能开放状态查询失败，本轮跳过", "role_id", session.RoleID(), "err", fErr)
				return nil
			} else if !open {
				slog.Info("造化秘境功能未开放，跳过", "role_id", session.RoleID(), "function_id", protocol.FunctionIDEraCreationRealm)
				return nil
			}
			call := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
				cCtx, cancel := context.WithTimeout(ctx, callTimeout)
				defer cancel()
				resp, err := session.Call(cCtx, pkt, act)
				if err != nil {
					slog.Info("造化秘境协议无回包", "role_id", session.RoleID(), "action", act, "err", err)
					return nil
				}
				return resp
			}

			panelResp := call(protocol.BuildEraCreationPanelPacket(), protocol.ActionIDEraCreationPanel)
			if panelResp == nil {
				return scheduler.NewCooldown(time.Hour, "造化秘境面板无回包")
			}
			panelCode, err := protocol.ParseActionFirstByte(panelResp.Payload, "造化秘境面板")
			if err != nil {
				slog.Warn("造化秘境面板解析失败", "role_id", session.RoleID(), "err", err)
				return nil
			}
			if panelCode != protocol.EraCreationSuccess {
				slog.Info("造化秘境面板不可用", "role_id", session.RoleID(),
					"result", panelCode, "result_text", protocol.EraCreationResultText(panelCode))
				return nil
			}

			// 1. 先探索，获得可挑战事件。
			exploreResp := call(protocol.BuildEraCreationExplorePacket(), protocol.ActionIDEraCreationExplore)
			if exploreResp == nil {
				return scheduler.NewCooldown(time.Hour, "造化秘境探索无回包")
			}
			explore, err := protocol.ParseEraCreationExplore(exploreResp.Payload)
			if err != nil {
				slog.Warn("造化秘境探索回包解析失败", "role_id", session.RoleID(), "err", err)
				return nil
			}
			switch explore.Result {
			case protocol.EraCreationSuccess:
				slog.Info("造化秘境探索成功", "role_id", session.RoleID(), "values", explore.Values)
			case protocol.EraCreationNoExploreNum:
				slog.Info("造化秘境探索次数不足，继续检查已有事件", "role_id", session.RoleID())
			default:
				slog.Info("造化秘境探索未成功", "role_id", session.RoleID(),
					"result", explore.Result, "result_text", protocol.EraCreationResultText(explore.Result))
			}

			// 2. 查询可挑战事件。
			eventResp := call(protocol.BuildEraCreationFightEventPacket(), protocol.ActionIDEraCreationFightEvent)
			if eventResp == nil {
				return scheduler.NewCooldown(time.Hour, "造化秘境事件无回包")
			}
			events, err := protocol.ParseEraCreationFightEvent(eventResp.Payload)
			if err != nil {
				slog.Warn("造化秘境事件回包解析失败", "role_id", session.RoleID(), "err", err)
				return nil
			}
			if events.Result != protocol.EraCreationSuccess {
				slog.Info("造化秘境事件查询未成功", "role_id", session.RoleID(),
					"result", events.Result, "result_text", protocol.EraCreationResultText(events.Result))
			} else {
				slog.Info("造化秘境可挑战事件", "role_id", session.RoleID(), "event_ids", events.IDs)
			}

			// 2.5 布阵面板：官方 flow 为 fight_event(7) → fight_deploy(8) →
			// deploy(9) → fight(10)。此前直接发 fight(10) 一直被静默丢弃，
			// 先查询布阵面板并记录回包，确认是否必须布阵才能挑战。
			if deployResp := call(protocol.BuildEraCreationFightDeployPacket(),
				protocol.ActionIDEraCreationFightDeploy); deployResp != nil {
				head := deployResp.Payload
				if len(head) > 64 {
					head = head[:64]
				}
				slog.Info("造化秘境布阵面板", "role_id", session.RoleID(),
					"len", len(deployResp.Payload),
					"result", head[0],
					"hex", hex.EncodeToString(head))
			} else {
				slog.Info("造化秘境布阵面板无回包", "role_id", session.RoleID())
			}

			// 3. 逐个挑战事件。
			fights := 0
			for _, eventID := range events.IDs {
				if ctx.Err() != nil || fights >= maxEraCreationFights {
					break
				}
				fightResp := call(protocol.BuildEraCreationFightPacket(eventID), protocol.ActionIDEraCreationFight)
				if fightResp == nil {
					// 当日次数结束后服务端会静默丢弃挑战：连续 3 次挂起（最长 6 小时）。
					return scheduler.HandleSilentDrop(session, "era_creation", 3,
						time.Hour, "造化秘境挑战无回包")
				}
				scheduler.ResetSilentDrops(session, "era_creation")
				fights++
				code, fErr := protocol.ParseActionFirstByte(fightResp.Payload, "造化秘境挑战")
				if fErr != nil {
					slog.Warn("造化秘境挑战回包解析失败", "role_id", session.RoleID(), "err", fErr)
					break
				}
				switch code {
				case protocol.EraCreationSuccess:
					slog.Info("造化秘境挑战成功", "role_id", session.RoleID(), "event_id", eventID)
				case protocol.EraCreationWrongID, protocol.EraCreationNoFightAward,
					protocol.EraCreationMaxFightEvent, protocol.EraCreationNoExploreNum:
					slog.Info("造化秘境挑战结束", "role_id", session.RoleID(),
						"event_id", eventID, "result", code,
						"result_text", protocol.EraCreationResultText(code))
					goto award
				default:
					slog.Info("造化秘境挑战未成功", "role_id", session.RoleID(),
						"event_id", eventID, "result", code,
						"result_text", protocol.EraCreationResultText(code))
					goto award
				}
			}

		award:
			// 4. 领取战斗奖励。
			awardResp := call(protocol.BuildEraCreationAwardPanelPacket(), protocol.ActionIDEraCreationAwardPanel)
			if awardResp == nil {
				return nil
			}
			awardCode, aErr := protocol.ParseActionFirstByte(awardResp.Payload, "造化秘境奖励面板")
			if aErr != nil {
				slog.Warn("造化秘境奖励面板解析失败", "role_id", session.RoleID(), "err", aErr)
				return nil
			}
			if awardCode != protocol.EraCreationSuccess {
				slog.Info("造化秘境暂无战斗奖励", "role_id", session.RoleID(),
					"result", awardCode, "result_text", protocol.EraCreationResultText(awardCode))
				return nil
			}
			chooseResp := call(protocol.BuildEraCreationChooseAwardPacket(), protocol.ActionIDEraCreationChoose)
			if chooseResp == nil {
				return nil
			}
			chooseCode, cErr := protocol.ParseActionFirstByte(chooseResp.Payload, "造化秘境领取奖励")
			if cErr != nil {
				slog.Warn("造化秘境领取奖励回包解析失败", "role_id", session.RoleID(), "err", cErr)
				return nil
			}
			slog.Info("造化秘境领取奖励结果", "role_id", session.RoleID(),
				"result", chooseCode, "result_text", protocol.EraCreationResultText(chooseCode),
				"fights", fights)
			return nil
		},
	)
}
