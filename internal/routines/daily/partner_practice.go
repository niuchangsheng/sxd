// 本文件实现幻境修炼 (Mod_PartnerPractice_Base, Module 306) 日常自动化。
//
// 流程：get_partner_practice_info(0) 读取当前伙伴 → start_practice(2) 开始修炼；
// 若返回 FINISHED 则调用 stop_practice(4) 结算免费收益。
// 永不调用 speed_practice(3)/hire_partner(11)/clean_CD(12) 等付费动作。
package daily

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// NewPartnerPracticeRoutine 构造幻境修炼日常任务。
func NewPartnerPracticeRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"partner_practice",
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
			if open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDPartnerPractice); fErr != nil {
				slog.Info("幻境修炼功能开放状态查询失败，本轮跳过", "role_id", session.RoleID(), "err", fErr)
				return nil
			} else if !open {
				slog.Info("幻境修炼功能未开放，跳过", "role_id", session.RoleID(), "function_id", protocol.FunctionIDPartnerPractice)
				return nil
			}
			call := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
				cCtx, cancel := context.WithTimeout(ctx, callTimeout)
				defer cancel()
				resp, err := session.Call(cCtx, pkt, act)
				if err != nil {
					slog.Info("幻境修炼协议无回包", "role_id", session.RoleID(), "action", act, "err", err)
					return nil
				}
				return resp
			}

			infoResp := call(protocol.BuildPartnerPracticeInfoPacket(), protocol.ActionIDPartnerPracticeInfo)
			if infoResp == nil {
				slog.Info("幻境修炼面板无回包，稍后重试", "role_id", session.RoleID())
				return scheduler.NewCooldown(30*time.Minute, "幻境修炼面板无回包")
			}
			info, err := protocol.ParsePartnerPracticeInfo(infoResp.Payload)
			if err != nil {
				slog.Warn("幻境修炼面板解析失败，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(10*time.Minute, "幻境修炼面板解析失败")
			}
			if info.PartnerRoleID <= 0 {
				slog.Info("幻境修炼未选择伙伴，跳过", "role_id", session.RoleID())
				return nil
			}
			slog.Info("幻境修炼当前伙伴", "role_id", session.RoleID(), "partner_role_id", info.PartnerRoleID,
				"payload_hex", fmt.Sprintf("%x", infoResp.Payload))

			// 用户实测（/tmp/xiulian.pcapng）：当前阶段修炼已满时，客户端直接发送
			// enter_fight(306/5) 进入下一阶段，然后 306/10 取伙伴属性 → 306/9 挑战幻想；
			// 被幻想击败后服务端进入 CD，需要 CD 结束后重试直到击败幻想。
			enterResp := call(protocol.BuildPartnerPracticeEnterFightPacket(), protocol.ActionIDPartnerPracticeEnterFight)
			if enterResp != nil {
				enterCode, eErr := protocol.ParsePartnerPracticeResult(enterResp.Payload)
				if eErr == nil {
					// 无条件落结果码：线上长期卡在 11(其他伙伴修炼中) 时，enter_fight 的
					// 返回码是判断「修炼是否已满/是否已结束」的唯一证据，不能只在成功的
					// 分支里记录。
					slog.Info("幻境修炼进入战斗结果", "role_id", session.RoleID(),
						"result", enterCode, "result_text", protocol.PartnerPracticeResultText(enterCode))
					switch enterCode {
					case protocol.PartnerPracticeSuccess:
						_ = call(protocol.BuildPartnerPracticeRoleWarAttrPacket(info.PartnerRoleID),
							protocol.ActionIDPartnerPracticeRoleAttr)
						fCtx, fCancel := context.WithTimeout(ctx, 60*time.Second)
						fightResp, fCallErr := session.Call(fCtx,
							protocol.BuildPartnerPracticeFightPacket(), protocol.ActionIDPartnerPracticeFight)
						fCancel()
						if fCallErr != nil {
							fightResp = nil
						}
						if fightResp == nil {
							return scheduler.NewCooldown(10*time.Minute, "幻境修炼挑战幻想无回包")
						}
						fightCode, fErr := protocol.ParsePartnerPracticeResult(fightResp.Payload)
						if fErr == nil {
							slog.Info("幻境修炼挑战幻想完成", "role_id", session.RoleID(),
								"result", fightCode, "result_text", protocol.PartnerPracticeResultText(fightCode),
								"battle_bytes", len(fightResp.Payload))
						}
						// 战斗后恢复修炼：胜利可继续修炼；失败/冷却由服务端返回 CD。
						again := call(protocol.BuildPartnerPracticeStartPacket(), protocol.ActionIDPartnerPracticeStart)
						if again == nil {
							return scheduler.NewCooldown(10*time.Minute, "幻境修炼恢复开始无回包")
						}
						againCode, aErr := protocol.ParsePartnerPracticeResult(again.Payload)
						if aErr != nil {
							return scheduler.NewCooldown(10*time.Minute, "幻境修炼恢复开始解析失败")
						}
						switch againCode {
						case protocol.PartnerPracticeSuccess, protocol.PartnerPracticeSame:
							slog.Info("幻境修炼已恢复", "role_id", session.RoleID(), "result", againCode)
							return nil
						case protocol.PartnerPracticeCDTime, protocol.PartnerPracticeTimeLimit:
							slog.Info("幻境修炼挑战幻想失败进入 CD，稍后重试", "role_id", session.RoleID(),
								"result", againCode, "result_text", protocol.PartnerPracticeResultText(againCode))
							return scheduler.NewCooldown(10*time.Minute, "幻境修炼幻想 CD")
						case protocol.PartnerPracticeFinished:
							slog.Info("幻境修炼阶段已推进，下一轮继续", "role_id", session.RoleID())
							return scheduler.NewCooldown(10*time.Minute, "幻境修炼阶段推进")
						default:
							slog.Info("幻境修炼恢复未成功，稍后重试", "role_id", session.RoleID(),
								"result", againCode, "result_text", protocol.PartnerPracticeResultText(againCode))
							return scheduler.NewCooldown(10*time.Minute, "幻境修炼恢复未成功")
						}
					case protocol.PartnerPracticeCDTime, protocol.PartnerPracticeTimeLimit:
						slog.Info("幻境修炼挑战幻想 CD 中，稍后重试", "role_id", session.RoleID(),
							"result", enterCode, "result_text", protocol.PartnerPracticeResultText(enterCode))
						return scheduler.NewCooldown(10*time.Minute, "幻境修炼幻想 CD")
					}
				}
			}

			startResp := call(protocol.BuildPartnerPracticeStartPacket(), protocol.ActionIDPartnerPracticeStart)
			if startResp == nil {
				slog.Info("幻境修炼开始无回包，稍后重试", "role_id", session.RoleID())
				return scheduler.NewCooldown(30*time.Minute, "幻境修炼开始无回包")
			}
			code, err := protocol.ParsePartnerPracticeResult(startResp.Payload)
			if err != nil {
				slog.Warn("幻境修炼开始回包解析失败", "role_id", session.RoleID(), "err", err)
				return nil
			}
			switch code {
			case protocol.PartnerPracticeSuccess, protocol.PartnerPracticeSame:
				slog.Info("幻境修炼已开始", "role_id", session.RoleID(), "result", code)
				return nil
			case protocol.PartnerPracticeFinished:
				// 修炼已完成：进入镜像战斗领取收益 (306/5 -> 306/9)。
				enterResp := call(protocol.BuildPartnerPracticeEnterFightPacket(), protocol.ActionIDPartnerPracticeEnterFight)
				if enterResp == nil {
					slog.Info("幻境修炼进入战斗无回包，稍后重试", "role_id", session.RoleID())
					return scheduler.NewCooldown(10*time.Minute, "幻境修炼进入战斗无回包")
				}
				enterCode, eErr := protocol.ParsePartnerPracticeResult(enterResp.Payload)
				if eErr != nil {
					return nil
				}
				if enterCode != protocol.PartnerPracticeSuccess {
					slog.Info("幻境修炼进入战斗未成功", "role_id", session.RoleID(),
						"result", enterCode, "result_text", protocol.PartnerPracticeResultText(enterCode))
					return nil
				}
				fightResp := call(protocol.BuildPartnerPracticeFightPacket(), protocol.ActionIDPartnerPracticeFight)
				if fightResp == nil {
					slog.Info("幻境修炼战斗无回包，稍后重试", "role_id", session.RoleID())
					return scheduler.NewCooldown(10*time.Minute, "幻境修炼战斗无回包")
				}
				fightCode, fErr := protocol.ParsePartnerPracticeResult(fightResp.Payload)
				if fErr != nil {
					return nil
				}
				if fightCode == protocol.PartnerPracticeSuccess {
					slog.Info("幻境修炼镜像战斗完成", "role_id", session.RoleID(),
						"battle_bytes", len(fightResp.Payload))
				} else {
					slog.Info("幻境修炼镜像战斗未成功", "role_id", session.RoleID(),
						"result", fightCode, "result_text", protocol.PartnerPracticeResultText(fightCode))
				}
				// 尝试重新开始，保证持续累积。
				if again := call(protocol.BuildPartnerPracticeStartPacket(), protocol.ActionIDPartnerPracticeStart); again != nil {
					if againCode, aErr := protocol.ParsePartnerPracticeResult(again.Payload); aErr == nil {
						slog.Info("幻境修炼重新开始", "role_id", session.RoleID(),
							"result", againCode, "result_text", protocol.PartnerPracticeResultText(againCode))
					}
				}
				return nil
			case protocol.PartnerPracticeOtherPractice:
				slog.Info("幻境修炼当前无法开始，稍后重试", "role_id", session.RoleID(),
					"result", code, "result_text", protocol.PartnerPracticeResultText(code))
				return scheduler.NewCooldown(2*time.Hour, "幻境修炼当前无法开始")
			case protocol.PartnerPracticeFuncNoOpen:
				slog.Info("幻境修炼功能未开放，稍后重试", "role_id", session.RoleID())
				return scheduler.NewCooldown(2*time.Hour, "幻境修炼未开放")
			case protocol.PartnerPracticeFailure:
				// 用户确认（2026-11-11）：游戏内点击「开始修炼」提示“已在修炼中”，
				// 服务端即返回 10。等待修炼完成后再结算，避免频繁重复请求。
				slog.Info("幻境修炼已在修炼中，等待完成", "role_id", session.RoleID())
				return scheduler.NewCooldown(2*time.Hour, "幻境修炼已在修炼中")
			case protocol.PartnerPracticeCDTime, protocol.PartnerPracticeTimeLimit:
				slog.Info("幻境修炼冷却/时间限制中，稍后重试", "role_id", session.RoleID(),
					"result", code, "result_text", protocol.PartnerPracticeResultText(code))
				return scheduler.NewCooldown(30*time.Minute, "幻境修炼冷却中")
			default:
				slog.Info("幻境修炼开始未成功", "role_id", session.RoleID(),
					"result", code, "result_text", protocol.PartnerPracticeResultText(code))
				return nil
			}
		},
	)
}
