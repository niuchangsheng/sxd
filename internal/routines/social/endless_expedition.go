// 本文件实现无尽洞天自动挑战 (Mod_EndlessExpedition_Base, Module 797)。
//
// 抓包基准: /tmp/henduo.pcapng（2026-11-14，qq s0）：
//
//	797/20 活动状态 → 797/21 面板（第 2 个 Int 为当前层）
//	→ 797/32 禁选列表(layer) → 797/22 怪物队伍(layer)
//	→ 797/23 挑战(layer) → 重新拉面板，层数变化则继续
//
// 不购买通行证/超级奖励（797/26、29 等），不消耗元宝。
package social

import (
	"context"
	"encoding/hex"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// maxEndlessChallenges 单轮最多挑战层数。
const maxEndlessChallenges = 6

// NewEndlessExpeditionRoutine 构造无尽洞天挑战任务。
func NewEndlessExpeditionRoutine() scheduler.ActivityRoutine {
	const callTimeout = 10 * time.Second
	// 实测成功挑战的回包在 1 秒内返回（01:05 多次），35 秒超时只会让
	// 「服务端静默丢弃挑战」的单轮阻塞 70 秒（含 PrevAct 重试）。降到 15 秒。
	const fightTimeout = 15 * time.Second
	return scheduler.NewBaseRoutine(
		"endless_expedition",
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
				resp, err := session.CallWithPrevActResync(ctx, timeout, pkt, act)
				if err != nil {
					slog.Info("无尽洞天协议无回包", "role_id", session.RoleID(),
						"action", act, "err", err)
					return nil
				}
				return resp
			}

			stateResp := call(protocol.BuildEndlessActivityStatePacket(),
				protocol.ActionIDEndlessActivityState, callTimeout)
			if stateResp == nil {
				return scheduler.NewCooldown(30*time.Minute, "无尽洞天状态无回包")
			}
			state, sErr := protocol.ParseEndlessActivityState(stateResp.Payload)
			if sErr != nil {
				return scheduler.NewCooldown(30*time.Minute, "无尽洞天状态解析失败")
			}
			slog.Info("无尽洞天活动状态", "role_id", session.RoleID(), "state", state)

			panelResp := call(protocol.BuildEndlessPanelPacket(),
				protocol.ActionIDEndlessPanel, callTimeout)
			if panelResp == nil {
				return scheduler.NewCooldown(30*time.Minute, "无尽洞天面板无回包")
			}
			panel, pErr := protocol.ParseEndlessPanel(panelResp.Payload)
			if pErr != nil {
				slog.Warn("解析无尽洞天面板失败，本轮跳过", "role_id", session.RoleID(), "err", pErr)
				return nil
			}
			if panel.Result != protocol.EndlessSuccess {
				slog.Info("无尽洞天不可挑战", "role_id", session.RoleID(),
					"result", panel.Result, "result_text", protocol.EndlessResultText(panel.Result))
				if panel.Result == protocol.EndlessNotOpen {
					return scheduler.NewCooldown(2*time.Hour, "无尽洞天未开放")
				}
				return nil
			}
			layer := panel.Layer()
			slog.Info("无尽洞天当前层", "role_id", session.RoleID(), "layer", layer)
			if layer <= 0 {
				return nil
			}

			challenges := 0
			for challenges < maxEndlessChallenges && ctx.Err() == nil {
				// 禁选列表仅为客户端展示所需，部分账号/层会被服务端静默丢弃，
				// 因此无回包不阻断挑战。
				_ = call(protocol.BuildEndlessBanListPacket(layer),
					protocol.ActionIDEndlessBanList, callTimeout)
				// 怪物队伍信息同样用于客户端展示，部分时段会被静默丢弃；
				// 无回包时仍尝试直接挑战，由 797/23 的结果决定。
				if infoResp := call(protocol.BuildEndlessMonsterTeamInfoPacket(layer),
					protocol.ActionIDEndlessMonsterTeamInfo, callTimeout); infoResp != nil {
					if code, iErr := protocol.ParseEndlessResult(infoResp.Payload); iErr != nil {
						slog.Warn("无尽洞天怪物队伍解析失败", "role_id", session.RoleID(), "err", iErr)
					} else if code != protocol.EndlessSuccess {
						slog.Info("无尽洞天当前层不可挑战，结束本轮", "role_id", session.RoleID(),
							"layer", layer, "result", code, "result_text", protocol.EndlessResultText(code))
						return nil
					}
				}

				fightResp := call(protocol.BuildEndlessChallengePacket(layer),
					protocol.ActionIDEndlessChallenge, fightTimeout)
				if fightResp == nil {
					// 服务端在当日次数/状态耗尽后会静默丢弃挑战请求：连续 3 次无回包
					// 即挂起到次日重置，避免整晚每小时空等。
					return scheduler.HandleSilentDrop(session, "endless_expedition", 3,
						time.Hour, "无尽洞天挑战无回包")
				}
				scheduler.ResetSilentDrops(session, "endless_expedition")
				code, fErr := protocol.ParseEndlessResult(fightResp.Payload)
				if fErr != nil {
					return scheduler.NewCooldown(10*time.Minute, "无尽洞天挑战回包解析失败")
				}
				if code != protocol.EndlessSuccess {
					head := fightResp.Payload
					if len(head) > 32 {
						head = head[:32]
					}
					slog.Info("无尽洞天挑战结束", "role_id", session.RoleID(),
						"layer", layer, "challenges", challenges,
						"result", code, "result_text", protocol.EndlessResultText(code),
						"payload_len", len(fightResp.Payload), "payload_head", hex.EncodeToString(head))
					if code == protocol.EndlessWarCD {
						// 实测冷却 <30s（01:05:30 CD → 01:05:59 可再次挑战），取 45s 保守唤醒。
						return scheduler.NewCooldown(45*time.Second, "无尽洞天战斗冷却")
					}
					return nil
				}
				challenges++
				slog.Info("无尽洞天挑战成功", "role_id", session.RoleID(),
					"layer", layer, "challenges", challenges)

				// 重新拉面板，服务端推进层数后继续。
				refresh := call(protocol.BuildEndlessPanelPacket(),
					protocol.ActionIDEndlessPanel, callTimeout)
				if refresh == nil {
					return nil
				}
				next, rErr := protocol.ParseEndlessPanel(refresh.Payload)
				if rErr != nil || next.Result != protocol.EndlessSuccess || next.Layer() <= 0 {
					return nil
				}
				if next.Layer() == layer {
					// 层数未推进说明本层尚未清空（或仍需继续战斗），继续挑战同一层；
					// 次数/体力耗尽时服务端会返回非成功码，由上面的分支结束。
					slog.Info("无尽洞天本层继续挑战", "role_id", session.RoleID(),
						"layer", layer, "challenges", challenges)
					continue
				}
				slog.Info("无尽洞天进入下一层", "role_id", session.RoleID(),
					"from", layer, "to", next.Layer())
				layer = next.Layer()
			}
			slog.Info("无尽洞天本轮完成", "role_id", session.RoleID(), "challenges", challenges)
			return nil
		},
	)
}
