// 本文件实现灵域猎妖 (Mod_SoulHunt_Base, Module 669) 日常自动化。
//
// 流程：get_activity_state(0) → main_panel(1) → 按面板关卡顺序 challenge_panel(3) 检查 →
// start_challenge(5) 免费挑战。绝不调用 buy_challenge_times(4) 元宝购买次数。
package minigame

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// maxSoulHuntRounds 单轮最多挑战次数，防止服务端异常导致死循环。
const maxSoulHuntRounds = 10

// NewSoulHuntRoutine 构造灵域猎妖日常任务。
func NewSoulHuntRoutine() scheduler.ActivityRoutine {
	const callTimeout = 10 * time.Second
	return scheduler.NewBaseRoutine(
		"soul_hunt",
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
					slog.Info("灵域猎妖协议无回包", "role_id", session.RoleID(), "action", act, "err", err)
					return nil
				}
				return resp
			}

			stateResp := call(protocol.NewPacket(protocol.ActionIDSoulHuntState, nil), protocol.ActionIDSoulHuntState)
			if stateResp == nil {
				slog.Info("灵域猎妖活动状态无回包，稍后重试", "role_id", session.RoleID())
				return scheduler.NewCooldown(30*time.Minute, "灵域猎妖活动状态无回包")
			}
			slog.Info("灵域猎妖活动状态", "role_id", session.RoleID(), "payload_len", len(stateResp.Payload))

			panelResp := call(protocol.NewPacket(protocol.ActionIDSoulHuntMainPanel, nil), protocol.ActionIDSoulHuntMainPanel)
			if panelResp == nil {
				slog.Info("灵域猎妖主面板无回包，稍后重试", "role_id", session.RoleID())
				return scheduler.NewCooldown(30*time.Minute, "灵域猎妖主面板无回包")
			}
			panel, err := protocol.ParseSoulHuntMainPanel(panelResp.Payload)
			if err != nil {
				slog.Warn("灵域猎妖主面板解析失败，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(10*time.Minute, "灵域猎妖主面板解析失败")
			}
			if panel.Result != protocol.SoulHuntSuccess {
				slog.Info("灵域猎妖主面板不可用，稍后重试", "role_id", session.RoleID(),
					"result", panel.Result, "result_text", protocol.SoulHuntResultText(panel.Result))
				return scheduler.NewCooldown(2*time.Hour, "灵域猎妖未开放")
			}
			slog.Info("灵域猎妖主面板", "role_id", session.RoleID(),
				"fields", panel.Ints, "levels", panel.Levels, "times", panel.Times)

			attempted := 0
			for round := 0; round < maxSoulHuntRounds; round++ {
				if ctx.Err() != nil {
					return nil
				}
				// 实测 challenge_panel/start_challenge 接受按 1 起始的关卡序号；
				// 面板下发的 id 列表是解锁时间记录，不能直接作为关卡参数。
				levelID := int32(round + 1)
				challengeResp := call(protocol.BuildSoulHuntChallengePanelPacket(levelID),
					protocol.ActionIDSoulHuntChallengePanel)
				if challengeResp == nil {
					return nil
				}
				challengeCode, cErr := protocol.ParseSoulHuntChallengeResult(challengeResp.Payload)
				if cErr != nil {
					slog.Warn("灵域猎妖关卡面板解析失败", "role_id", session.RoleID(), "err", cErr)
					return nil
				}
				if challengeCode != protocol.SoulHuntSuccess {
					slog.Info("灵域猎妖关卡不可挑战", "role_id", session.RoleID(),
						"level_id", levelID, "result", challengeCode,
						"result_text", protocol.SoulHuntResultText(challengeCode))
					continue
				}

				fightResp := call(protocol.BuildSoulHuntStartChallengePacket(levelID),
					protocol.ActionIDSoulHuntStartChallenge)
				if fightResp == nil {
					return nil
				}
				attempted++
				code, fErr := protocol.ParseSoulHuntChallengeResult(fightResp.Payload)
				if fErr != nil {
					slog.Warn("灵域猎妖挑战回包解析失败", "role_id", session.RoleID(), "err", fErr)
					return nil
				}
				switch code {
				case protocol.SoulHuntSuccess:
					slog.Info("灵域猎妖挑战成功", "role_id", session.RoleID(), "level_id", levelID)
				case protocol.SoulHuntCantChallenge, protocol.SoulHuntNoTime,
					protocol.SoulHuntFuncNotOpen, protocol.SoulHuntUpLevelLimit:
					slog.Info("灵域猎妖挑战结束", "role_id", session.RoleID(),
						"level_id", levelID, "result", code,
						"result_text", protocol.SoulHuntResultText(code))
					return nil
				case protocol.SoulHuntNoIngot:
					slog.Info("灵域猎妖返回元宝不足，停止以免误消费", "role_id", session.RoleID())
					return nil
				default:
					slog.Info("灵域猎妖挑战未成功", "role_id", session.RoleID(),
						"level_id", levelID, "result", code,
						"result_text", protocol.SoulHuntResultText(code))
					return nil
				}
			}
			slog.Info("灵域猎妖本轮结束", "role_id", session.RoleID(), "attempted", attempted)
			return nil
		},
	)
}
