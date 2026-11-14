// 本文件实现灵域猎妖自动挑战 (Mod_SoulHunt_Base, Module 669)。
//
// 抓包基准: /tmp/henduo.pcapng（2026-11-14，qq s0）：
//
//	669/1 主面板（第 2 个 Int 为当前关卡）→ 669/3 关卡面板(step)
//	→ 669/5 start_challenge(step) → 战报
//
// 仅使用免费挑战次数；不调用 buy_challenge_times(669/4)，不消耗元宝。
// 结果码按 Mod_SoulHunt_Base 分流：5=挑战次数不足、6=战斗中等直接结束。
package social

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// maxSoulHuntBattles 单轮最多挑战次数（防止异常循环）。
const maxSoulHuntBattles = 8

// NewSoulHuntRoutine 构造灵域猎妖挑战任务。
func NewSoulHuntRoutine() scheduler.ActivityRoutine {
	const callTimeout = 10 * time.Second
	const fightTimeout = 30 * time.Second
	return scheduler.NewBaseRoutine(
		"soul_hunt",
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
			call := func(pkt *protocol.Packet, act uint32, timeout time.Duration) *protocol.Packet {
				cCtx, cCancel := context.WithTimeout(ctx, timeout)
				defer cCancel()
				resp, err := session.Call(cCtx, pkt, act)
				if err != nil {
					slog.Info("灵域猎妖协议无回包", "role_id", session.RoleID(),
						"action", act, "err", err)
					return nil
				}
				return resp
			}

			panelResp := call(protocol.BuildSoulHuntMainPanelPacket(),
				protocol.ActionIDSoulHuntMainPanel, callTimeout)
			if panelResp == nil {
				return scheduler.NewCooldown(30*time.Minute, "灵域猎妖主面板无回包")
			}
			panel, err := protocol.ParseSoulHuntMainPanel(panelResp.Payload)
			if err != nil {
				slog.Warn("解析灵域猎妖主面板失败，本轮跳过",
					"role_id", session.RoleID(), "err", err)
				return nil
			}
			if panel.Result != protocol.SoulHuntSuccess {
				slog.Info("灵域猎妖不可挑战", "role_id", session.RoleID(),
					"result", panel.Result, "result_text", protocol.SoulHuntResultText(panel.Result))
				if panel.Result == protocol.SoulHuntFuncNotOpen {
					return scheduler.NewCooldown(2*time.Hour, "灵域猎妖未开放")
				}
				return nil
			}
			step := panel.CurrentStep()
			slog.Info("灵域猎妖主面板", "role_id", session.RoleID(), "step", step)
			if step <= 0 {
				slog.Info("灵域猎妖当前无可挑战关卡", "role_id", session.RoleID())
				return nil
			}

			fights := 0
			for fights < maxSoulHuntBattles && ctx.Err() == nil {
				challengeResp := call(protocol.BuildSoulHuntChallengePanelPacket(step),
					protocol.ActionIDSoulHuntChallengePanel, callTimeout)
				if challengeResp == nil {
					return scheduler.NewCooldown(10*time.Minute, "灵域猎妖关卡面板无回包")
				}
				if code, pErr := protocol.ParseSoulHuntResult(challengeResp.Payload); pErr != nil {
					return scheduler.NewCooldown(10*time.Minute, "灵域猎妖关卡面板解析失败")
				} else if code != protocol.SoulHuntSuccess {
					slog.Info("灵域猎妖关卡不可挑战，结束本轮", "role_id", session.RoleID(),
						"step", step, "result", code, "result_text", protocol.SoulHuntResultText(code))
					return nil
				}

				fightResp := call(protocol.BuildSoulHuntStartChallengePacket(step),
					protocol.ActionIDSoulHuntStartChallenge, fightTimeout)
				if fightResp == nil {
					return scheduler.NewCooldown(10*time.Minute, "灵域猎妖挑战无回包")
				}
				code, pErr := protocol.ParseSoulHuntResult(fightResp.Payload)
				if pErr != nil {
					return scheduler.NewCooldown(10*time.Minute, "灵域猎妖挑战回包解析失败")
				}
				if code != protocol.SoulHuntSuccess {
					slog.Info("灵域猎妖挑战结束", "role_id", session.RoleID(),
						"step", step, "fights", fights,
						"result", code, "result_text", protocol.SoulHuntResultText(code))
					return nil
				}
				fights++
				slog.Info("灵域猎妖挑战成功", "role_id", session.RoleID(),
					"step", step, "fights", fights)

				// 刷新主面板，关卡可能在胜利后推进。
				refresh := call(protocol.BuildSoulHuntMainPanelPacket(),
					protocol.ActionIDSoulHuntMainPanel, callTimeout)
				if refresh == nil {
					return nil
				}
				next, rErr := protocol.ParseSoulHuntMainPanel(refresh.Payload)
				if rErr != nil || next.Result != protocol.SoulHuntSuccess || next.CurrentStep() <= 0 {
					return nil
				}
				if next.CurrentStep() != step {
					step = next.CurrentStep()
					slog.Info("灵域猎妖进入下一关", "role_id", session.RoleID(), "step", step)
				}
			}
			slog.Info("灵域猎妖本轮完成", "role_id", session.RoleID(), "fights", fights)
			return nil
		},
	)
}