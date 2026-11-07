// Package daily 提供签到、邮件、在线奖励、绑定奖励与日常维护类玩法。
//
// 本文件实现轮回净地每日挂机奖励领取 (Mod_EraRebirthLandAuto_Base, Module 852)，
// 对应每日活跃度任务 70「每日轮回净地领取挂机奖励」(每次 +20)。
//
// 仅调用免费一键领取 one_click_quick_free_back；元宝档 one_click_quick_ingot_back
// 永不调用。
package daily

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// DailyTaskRebirthLand 每日轮回净地领取挂机奖励。
const DailyTaskRebirthLand int32 = 70

// NewRebirthLandRoutine 构造轮回净地挂机奖励领取任务。
func NewRebirthLandRoutine() scheduler.ActivityRoutine {
	const callTimeout = 6 * time.Second
	return scheduler.NewBaseRoutine(
		"rebirth_land",
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
			// 会话重连期间发包必然失败，直接返回等待下次调度。
			if session.State() != client.StateActive {
				return nil
			}

			call := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
				cCtx, cancel := context.WithTimeout(ctx, callTimeout)
				defer cancel()
				resp, err := session.Call(cCtx, pkt, act)
				if err != nil {
					// 会话断开或服务端静默丢弃均表现为无回包，跳过本轮即可。
					slog.Info("轮回净地协议无回包，跳过", "role_id", session.RoleID(),
						"action", act, "err", err)
					return nil
				}
				return resp
			}

			// 1. 活跃度任务 70 未完成时才领取，避免无意义请求。
			actResp := call(protocol.BuildGameAssistantInfoPacket(), protocol.ActionIDGameAssistantInfo)
			if actResp == nil {
				slog.Info("查询每日活跃度失败，跳过轮回净地", "role_id", session.RoleID())
				return nil
			}
			panel, err := protocol.ParseGameAssistantInfo(actResp.Payload)
			if err != nil {
				return err
			}
			if panel.RemainingTimesFor(DailyTaskRebirthLand) <= 0 {
				slog.Info("轮回净地活跃度已完成，跳过", "role_id", session.RoleID())
				return nil
			}

			// 2. 查询渡业台/渡业仙池面板（仅诊断，失败不阻断领取）。
			if poolResp := call(protocol.BuildEraRebirthLandPoolPanelPacket(), protocol.ActionIDEraRebirthLandPoolPanel); poolResp != nil {
				r := protocol.NewReader(poolResp.Payload)
				vals := make([]int32, 0, 6)
				for i := 0; i < 6; i++ {
					v, rErr := r.ReadInt32()
					if rErr != nil {
						break
					}
					vals = append(vals, v)
				}
				slog.Info("轮回净地渡业仙池面板",
					"role_id", session.RoleID(), "payload_len", len(poolResp.Payload), "ints", vals)
			}

			// 3. 领取渡业仙池免费奖励 / 今日奖励 (852/13, 852/14)。
			claim := func(pkt *protocol.Packet, act uint32, label string) (uint8, bool) {
				resp := call(pkt, act)
				if resp == nil {
					slog.Info("轮回净地领取无回包", "role_id", session.RoleID(), "scene", label)
					return 0, false
				}
				code, pErr := protocol.ParseEraRebirthSimpleResult(resp.Payload)
				if pErr != nil {
					slog.Warn("解析轮回净地领取回包失败", "role_id", session.RoleID(), "scene", label, "err", pErr)
					return 0, false
				}
				switch code {
				case protocol.EraRebirthResultSuccess:
					slog.Info("轮回净地渡业仙池奖励领取成功", "role_id", session.RoleID(), "scene", label)
					return code, true
				case protocol.EraRebirthResultPackageFull:
					slog.Warn("轮回净地渡业仙池奖励背包已满", "role_id", session.RoleID(), "scene", label)
					return code, false
				default:
					slog.Info("轮回净地渡业仙池暂无可领取奖励",
						"role_id", session.RoleID(), "scene", label,
						"result", code, "result_text", protocol.EraRebirthResultText(code))
					return code, false
				}
			}
			freeCode, freeOK := claim(protocol.BuildEraRebirthLandFreeAwardPacket(),
				protocol.ActionIDEraRebirthLandFreeAward, "渡业仙池免费奖励")
			if freeOK {
				return nil
			}
			todayCode, todayOK := claim(protocol.BuildEraRebirthLandTodayAwardPacket(),
				protocol.ActionIDEraRebirthLandTodayAward, "渡业仙池今日奖励")
			if todayOK {
				return nil
			}
			if freeCode == protocol.EraRebirthResultPackageFull || todayCode == protocol.EraRebirthResultPackageFull {
				return scheduler.ErrBagFull
			}

			// 4. 兜底：免费一键领取挂机奖励 (852/6)。
			resp := call(protocol.BuildEraRebirthLandFreeBackPacket(), protocol.ActionIDEraRebirthLandFreeBack)
			if resp == nil {
				slog.Info("轮回净地不可用或未开放，跳过", "role_id", session.RoleID())
				return nil
			}
			result, err := protocol.ParseEraRebirthFreeBackResult(resp.Payload)
			if err != nil {
				slog.Warn("解析轮回净地领取回包失败", "role_id", session.RoleID(), "err", err)
				return nil
			}
			switch result.Result {
			case protocol.EraRebirthResultSuccess:
				slog.Info("轮回净地挂机奖励领取成功",
					"role_id", session.RoleID(),
					"rewards", len(result.Rewards))
			case protocol.EraRebirthResultNoCanGetGift, protocol.EraRebirthResultAlreadyGet,
				protocol.EraRebirthResultNoProfit, protocol.EraRebirthResultNoFreeStorage:
				slog.Info("轮回净地暂无可领取奖励",
					"role_id", session.RoleID(),
					"result", result.Result,
					"result_text", protocol.EraRebirthResultText(result.Result))
			case protocol.EraRebirthResultPackageFull:
				return scheduler.ErrBagFull
			default:
				slog.Info("轮回净地领取未成功",
					"role_id", session.RoleID(),
					"result", result.Result,
					"result_text", protocol.EraRebirthResultText(result.Result))
			}
			return nil
		},
	)
}
