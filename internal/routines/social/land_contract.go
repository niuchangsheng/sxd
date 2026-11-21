// 本文件实现灵契之地挑战 (Mod_LandContract_Base, Module 613)。
//
// 抓包基准: /tmp/lingqizhidi.pcapng（2026-11-15，qq s0）：
//
//	613/1 面板 → 613/8 fight_boss（无参数）→ 战报
//
// 用户实测每日 5 次挑战（点击开始挑战 → 确认阵容 → 挑战）。
// 结果码：3 成功继续；8 次数不足、9 非挑战时间、10 冷却、4 背包满等结束。
// 严禁调用 613/4 buy_fight_num（元宝购买次数）。
package social

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// maxLandContractFights 单轮挑战的安全上限。
//
// 每日免费次数由服务端 NoFightCount 决定（实测 5 次），这里只保留一个
// 远高于正常次数的上限，避免服务端次数异常时死循环；正常流程会一直
// 打到服务端返回次数不足。
const maxLandContractFights = 10

// landContractFailureThreshold 连续多少场战斗失败后结束本轮。
// 按用户要求「挑战副本需 N 次失败才退出」，不再第一次失败就放弃。
const landContractFailureThreshold = 3

// NewLandContractRoutine 构造灵契之地挑战任务。
func NewLandContractRoutine() scheduler.ActivityRoutine {
	const callTimeout = 10 * time.Second
	const fightTimeout = 35 * time.Second
	return scheduler.NewBaseRoutine(
		"land_contract",
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
					slog.Info("灵契之地协议无回包", "role_id", session.RoleID(),
						"action", act, "err", err)
					return nil
				}
				return resp
			}

			panelResp := call(protocol.BuildLandContractPanelPacket(),
				protocol.ActionIDLandContractPanel, callTimeout)
			if panelResp == nil {
				return scheduler.NewCooldown(30*time.Minute, "灵契之地面板无回包")
			}
			panel, err := protocol.ParseLandContractPanel(panelResp.Payload)
			if err != nil {
				slog.Warn("解析灵契之地面板失败，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(10*time.Minute, "灵契之地面板解析失败")
			}
			slog.Info("灵契之地面板", "role_id", session.RoleID(),
				"values", panel.Values, "exp", panel.Exp)

			fights := 0
			failures := 0
			for fights < maxLandContractFights && ctx.Err() == nil {
				fightResp := call(protocol.BuildLandContractFightPacket(),
					protocol.ActionIDLandContractFight, fightTimeout)
				if fightResp == nil {
					return scheduler.NewCooldown(10*time.Minute, "灵契之地挑战无回包")
				}
				code, fErr := protocol.ParseLandContractResult(fightResp.Payload)
				if fErr != nil {
					return scheduler.NewCooldown(10*time.Minute, "灵契之地挑战回包解析失败")
				}
				switch code {
				case protocol.LandContractSuccess:
					fights++
					failures = 0
					slog.Info("灵契之地挑战成功", "role_id", session.RoleID(), "fights", fights)
				case protocol.LandContractPackFull:
					slog.Info("灵契之地背包已满，停止挑战", "role_id", session.RoleID(), "fights", fights)
					return scheduler.ErrBagFull
				case protocol.LandContractNoFightCount:
					slog.Info("灵契之地挑战次数不足，结束本轮", "role_id", session.RoleID(), "fights", fights)
					return nil
				case protocol.LandContractNotFightTime:
					slog.Info("灵契之地非挑战时间，稍后重试", "role_id", session.RoleID())
					return scheduler.NewCooldown(30*time.Minute, "灵契之地非挑战时间")
				case protocol.LandContractCDTime:
					slog.Info("灵契之地挑战冷却中，稍后重试", "role_id", session.RoleID(), "fights", fights)
					return scheduler.NewCooldown(5*time.Minute, "灵契之地挑战冷却")
				case protocol.LandContractFaild, protocol.LandContractFailed:
					// 战斗失败按 N 次熔断：单场失败继续挑战，连续失败达到阈值才退出。
					failures++
					if failures >= landContractFailureThreshold {
						slog.Info("灵契之地连续多场战斗失败，结束本轮",
							"role_id", session.RoleID(), "fights", fights, "failures", failures)
						return nil
					}
					slog.Info("灵契之地挑战失败，继续重试",
						"role_id", session.RoleID(), "fights", fights, "failures", failures)
					continue
				case protocol.LandContractNotFinish:
					slog.Info("灵契之地挑战未完成，结束本轮",
						"role_id", session.RoleID(), "fights", fights, "result", code)
					return nil
				case protocol.LandContractNoIngot:
					slog.Info("灵契之地需要元宝，自动化不主动消耗",
						"role_id", session.RoleID(), "fights", fights)
					return nil
				default:
					slog.Info("灵契之地挑战结束", "role_id", session.RoleID(),
						"fights", fights, "result", code,
						"result_text", protocol.LandContractResultText(code))
					return nil
				}
			}
			slog.Info("灵契之地本轮完成", "role_id", session.RoleID(), "fights", fights)
			return nil
		},
	)
}
