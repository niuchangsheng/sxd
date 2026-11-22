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
	"fmt"
	"log/slog"
	"sort"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// maxSoulHuntBattles 单轮最多挑战次数（防止异常循环）。
const maxSoulHuntBattles = 8

// nextSoulHuntActivityTime 根据主面板 step_pairs 推算下一轮开放时间。
//
// 抓包实测 step_pairs 为 [轮次ID, 时间戳] 成对出现，每对是「本轮结束/下一轮
// 开始」（如 2026-11-22 23:59:30、2026-11-23 00:00:00）。取未来最近的两个
// 时间戳中的后一个作为下一轮开始时刻；只有一个时取该时刻。
func nextSoulHuntActivityTime(panel *protocol.SoulHuntMainPanel, now time.Time) time.Time {
	if panel == nil {
		return time.Time{}
	}
	var times []time.Time
	for _, p := range panel.StepPairs {
		if p[1] <= 0 {
			continue
		}
		t := time.Unix(int64(p[1]), 0)
		if t.After(now) {
			times = append(times, t)
		}
	}
	if len(times) == 0 {
		return time.Time{}
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	if len(times) >= 2 {
		return times[1]
	}
	return times[0]
}

// NewSoulHuntRoutine 构造灵域猎妖挑战任务。
func NewSoulHuntRoutine() scheduler.ActivityRoutine {
	const callTimeout = 10 * time.Second
	// 实测成功挑战的回包在 1 秒内返回（00:43 连续三场），30 秒超时只会放大
	// 「服务端静默丢弃」时的空等；降到 15 秒。
	const fightTimeout = 15 * time.Second
	return scheduler.NewBaseRoutine(
		"soul_hunt",
		"social",
		scheduler.ScheduleCron,
		// 669/3 严格校验城镇/战场上下文：实测排在宝库/五行/无量等之后时
		// 会被静默丢弃，而登录后第一条业务请求则正常回包。排在宝库(450)之后、
		// 所有其它战斗玩法之前。
		scheduler.RoutinePriority(449),
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			// 战斗结束后回到城镇，避免把战斗上下文留给后续 453/3、669/3 等
			// 严格校验城镇状态的玩法。
			defer func() {
				if ctx.Err() == nil && session.ReturnToTown(ctx, 5*time.Second) {
					slog.Info("灵域猎妖结束，已返回城镇场景", "role_id", session.RoleID())
				}
			}()
			call := func(pkt *protocol.Packet, act uint32, timeout time.Duration) *protocol.Packet {
				resp, err := session.CallWithPrevActResync(ctx, timeout, pkt, act)
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
			head := panelResp.Payload
			if len(head) > 160 {
				head = head[:160]
			}
			slog.Info("灵域猎妖主面板", "role_id", session.RoleID(), "step", step,
				"values", panel.Values, "step_pairs", panel.StepPairs, "tail", panel.Tail,
				"payload_hex", fmt.Sprintf("%X", head))
			if step <= 0 {
				slog.Info("灵域猎妖当前无可挑战关卡", "role_id", session.RoleID())
				return nil
			}

			fights := 0
			for fights < maxSoulHuntBattles && ctx.Err() == nil {
				challengeResp := call(protocol.BuildSoulHuntChallengePanelPacket(step),
					protocol.ActionIDSoulHuntChallengePanel, callTimeout)
				if challengeResp == nil {
					// 当日挑战次数耗尽后服务端会静默丢弃关卡面板；连续 3 次即挂起
					// 到次日重置，避免每 10 分钟反复空等。
					return scheduler.HandleSilentDrop(session, "soul_hunt", 3,
						10*time.Minute, "灵域猎妖关卡面板无回包")
				}
				// 关卡面板成功不代表挑战动作可用（当日次数耗尽时服务端可能只丢 669/5）。
				// 计数只在挑战请求真正回包后清零，否则每轮都会被面板成功重置，
				// 永远达不到 3 次熔断阈值（与飞升灌注/万古神墟同类缺陷）。
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
					return scheduler.HandleSilentDrop(session, "soul_hunt", 3,
						10*time.Minute, "灵域猎妖挑战无回包")
				}
				scheduler.ResetSilentDrops(session, "soul_hunt")
				code, pErr := protocol.ParseSoulHuntResult(fightResp.Payload)
				if pErr != nil {
					return scheduler.NewCooldown(10*time.Minute, "灵域猎妖挑战回包解析失败")
				}
				if code != protocol.SoulHuntSuccess {
					if code == protocol.SoulHuntNoChallengeTime {
						if next := nextSoulHuntActivityTime(panel, time.Now()); !next.IsZero() {
							slog.Info("灵域猎妖挑战次数不足，按下一轮开放时间唤醒",
								"role_id", session.RoleID(), "step", step, "fights", fights,
								"next_open", next.Format(time.RFC3339))
							return scheduler.NewCooldownUntil(next, "灵域猎妖挑战次数不足")
						}
						return scheduler.NewCooldownUntil(scheduler.NextDailyReset(time.Now()),
							"灵域猎妖挑战次数不足，等待次日重置")
					}
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
