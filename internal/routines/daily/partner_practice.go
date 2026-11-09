// 本文件实现幻境修炼 (Mod_PartnerPractice_Base, Module 306) 日常自动化。
//
// 流程：get_partner_practice_info(0) 读取当前伙伴 → start_practice(2) 开始修炼；
// 若返回 FINISHED 则调用 stop_practice(4) 结算免费收益。
// 永不调用 speed_practice(3)/hire_partner(11)/clean_CD(12) 等付费动作。
package daily

import (
	"context"
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
			slog.Info("幻境修炼当前伙伴", "role_id", session.RoleID(), "partner_role_id", info.PartnerRoleID)

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
				slog.Info("幻境修炼其他伙伴修炼中，稍后重试", "role_id", session.RoleID())
				return scheduler.NewCooldown(2*time.Hour, "其他伙伴修炼中")
			case protocol.PartnerPracticeFuncNoOpen:
				slog.Info("幻境修炼功能未开放，稍后重试", "role_id", session.RoleID())
				return scheduler.NewCooldown(2*time.Hour, "幻境修炼未开放")
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
