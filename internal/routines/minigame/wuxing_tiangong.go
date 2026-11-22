// 本文件实现五行天宫 (Module 453 / 0x01C5) 自动化。
//
// 抓包流程 (/tmp/wuxingtiangong.pcapng):
//
//	453/10 分类面板 → 453/0 主面板 → 453/3 mode=1 一键挑战 → 453/0 刷新
//
// 用户要求（2026-11-14）：不做阵容/role_id 映射；一键挑战后只要存在失败关卡
// 或无回包，直接退出本轮，不再单独重试。
//
// 全程不消耗元宝。
package minigame

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

const wuxingCallTimeout = 15 * time.Second

// wuxingStatePending 面板中「未挑战/待挑战」状态。
//
// 2026-11-22 线上实测：面板 5 个元素中，四个较低关卡 (31/42/45/51) 为 State 3，
// 最高关卡 (57) 为 State 4；挑战后状态由 4 变为 3，说明 3=已通关、4=待挑战。
// 旧实现把 State 3 当作失败并直接跳过，导致该玩法长期不再挑战。
const wuxingStatePending uint8 = 4

// wuxingMaxFailedRounds 连续多少轮一键挑战仍有失败关卡后结束本轮。
// 按用户要求「挑战副本需 N 次失败才退出」，不再第一次失败就放弃。
const wuxingMaxFailedRounds = 3

// NewWuxingTiangongRoutine 构造五行天宫一键挑战任务。
func NewWuxingTiangongRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"wuxing_tiangong",
		"minigame",
		scheduler.ScheduleCron,
		// 与其它长会话易丢包的活动一致，登录后尽早执行。
		scheduler.RoutinePriority(340),
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			call := func(pkt *protocol.Packet, act uint32, timeout time.Duration) *protocol.Packet {
				cCtx, cancel := context.WithTimeout(ctx, timeout)
				defer cancel()
				resp, err := session.Call(cCtx, pkt, act)
				if err != nil {
					return nil
				}
				return resp
			}

			slog.Info("正在执行五行天宫挑战", "role_id", session.RoleID())

			// 1. 分类面板（诊断）。
			if resp := call(protocol.BuildWuxingPanel10Packet(session.LastActionID()),
				protocol.ActionIDWuxingPanel10, wuxingCallTimeout); resp != nil {
				slog.Info("五行天宫分类面板", "role_id", session.RoleID(), "payload_len", len(resp.Payload))
			}

			// 2. 主面板。
			panelResp := call(protocol.BuildWuxingPanelPacket(session.LastActionID()),
				protocol.ActionIDWuxingPanel, wuxingCallTimeout)
			if panelResp == nil {
				slog.Info("五行天宫主面板无回包，结束本轮", "role_id", session.RoleID())
				return nil
			}
			entries, pErr := protocol.ParseWuxingPanel(panelResp.Payload)
			if pErr != nil {
				slog.Warn("五行天宫面板解析失败，结束本轮", "role_id", session.RoleID(), "err", pErr)
				return nil
			}
			slog.Info("五行天宫面板状态",
				"role_id", session.RoleID(), "phase", "挑战前", "entries", entries)

			// 2.5 只有存在「待挑战 (State 4)」关卡时才提交一键挑战；
			// State 3 表示已通关，不能据此跳过。
			pendingBefore := 0
			for _, e := range entries {
				if e.State == wuxingStatePending {
					pendingBefore++
				}
			}
			if pendingBefore == 0 {
				slog.Info("五行天宫全部关卡已通关，无需挑战", "role_id", session.RoleID())
				return nil
			}

			// 3. 一键挑战；失败关卡按 N 轮重试后退出（挑战副本 N 次失败才退出）。
			failedRounds := 0
			for {
				fightResp := call(protocol.BuildWuxingFightPacket(protocol.WuxingFightModeAll, session.LastActionID()),
					protocol.ActionIDWuxingFight, 30*time.Second)
				if fightResp == nil {
					// 一键挑战被静默丢弃时 10 分钟重试只会持续空等 30 秒；改为 1 小时低频探测。
					slog.Info("五行天宫一键挑战无回包，1 小时后再试", "role_id", session.RoleID())
					return scheduler.NewCooldown(time.Hour, "五行天宫一键挑战无回包")
				}
				scheduler.ResetSilentDrops(session, "wuxing_tiangong")
				slog.Info("五行天宫一键挑战已执行",
					"role_id", session.RoleID(), "report_len", len(fightResp.Payload), "round", failedRounds+1)

				// 4. 刷新面板；统计本轮失败关卡。
				afterResp := call(protocol.BuildWuxingPanelPacket(session.LastActionID()),
					protocol.ActionIDWuxingPanel, wuxingCallTimeout)
				if afterResp == nil {
					slog.Info("五行天宫挑战后面板无回包，按连续丢包退避", "role_id", session.RoleID())
					return scheduler.HandleSilentDrop(session, "wuxing_tiangong", 3,
						30*time.Minute, "五行天宫挑战后面板无回包")
				}
				after, aErr := protocol.ParseWuxingPanel(afterResp.Payload)
				if aErr != nil {
					slog.Warn("五行天宫挑战后面板解析失败，稍后重试", "role_id", session.RoleID(), "err", aErr)
					return scheduler.NewCooldown(10*time.Minute, "五行天宫挑战后面板解析失败")
				}
				slog.Info("五行天宫面板状态",
					"role_id", session.RoleID(), "phase", "挑战后", "entries", after)

				pending := 0
				for _, e := range after {
					if e.State == wuxingStatePending {
						pending++
						slog.Info("五行天宫仍有待挑战关卡",
							"role_id", session.RoleID(), "element", e.Element, "stage", e.Stage)
					}
				}
				if pending == 0 {
					slog.Info("五行天宫本轮挑战完成", "role_id", session.RoleID())
					return nil
				}
				failedRounds++
				if failedRounds >= wuxingMaxFailedRounds {
					slog.Info("五行天宫连续多轮仍有待挑战关卡，结束本轮",
						"role_id", session.RoleID(), "failed_rounds", failedRounds, "pending_stages", pending)
					return nil
				}
				slog.Info("五行天宫仍有待挑战关卡，重试一键挑战",
					"role_id", session.RoleID(), "failed_rounds", failedRounds, "pending_stages", pending)
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(2 * time.Second):
				}
			}
		},
	)
}
