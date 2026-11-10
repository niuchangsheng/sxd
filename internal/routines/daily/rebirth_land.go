// Package daily 提供签到、邮件、在线奖励、绑定奖励与日常维护类玩法。
//
// 本文件实现轮回净地 (Mod_EraRebirthLandAuto_Base, Module 852)：
//   - 渡业台抽取 (852/16)：消耗渡业符/活动道具，不消耗元宝；
//   - 渡业仙池免费/今日奖励 (852/13, 852/14)；
//   - 每日活跃度任务 70「每日轮回净地领取挂机奖励」的免费一键领取 (852/6)。
//
// 元宝档 one_click_quick_ingot_back (852/7) 永不调用。
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

// DailyTaskRebirthLand 每日轮回净地领取挂机奖励。
const DailyTaskRebirthLand int32 = 70

// maxAltarDrawsPerRun 单轮渡业台抽取上限，防止服务端异常导致死循环。
// 实际次数由渡业符数量决定，收到非成功结果码即停止。
const maxAltarDrawsPerRun = 50

// NewRebirthLandRoutine 构造轮回净地日常任务。
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

			// 1. 查询渡业台/渡业仙池面板（必须带 PrevAct，官方抓包为 12 字节请求）。
			//    空载荷版本服务端虽回包但不会登记 PrevAct 链，导致后续 852/16 被静默丢弃。
			// 先用 0/2 全量角色资产重建 PrevAct 链：长会话中大量无 PrevAct 的请求会让
			// 服务端的链锚点与本地 lastActionID 脱节，导致 852/16 被静默丢弃。
			// 线上实测 (2026-11-11)：0/2 → 852/12 → 852/16 可稳定成功。
			// 依次尝试候选锚点，直到 0/2 真正被服务端处理（LastAck 变为 0/2）。
			candidates := []uint32{
				session.LastAckActionID(),
				session.LastActionID(),
				protocol.ActionIDPlayerGetInfo,
				protocol.ActionIDPlayerHeartbeat,
				0x00A50000, // Mod 165 扩展模块初始化
				0x00000063, // Mod 0 Act 99 场景进阶
				0x00000048, // Mod 0 Act 72 城镇初始化
			}
			seen := make(map[uint32]bool, len(candidates))
			synced := false
			for _, cand := range candidates {
				if cand == 0 || seen[cand] {
					continue
				}
				seen[cand] = true
				if call(protocol.BuildPlayerGetInfoPacket(int32(cand)), protocol.ActionIDPlayerGetInfo) != nil &&
					session.LastAckActionID() == protocol.ActionIDPlayerGetInfo {
					synced = true
					slog.Debug("轮回净地 PrevAct 链重建成功", "role_id", session.RoleID(),
						"anchor", fmt.Sprintf("0x%08X", cand))
					break
				}
			}
			if !synced {
				slog.Info("轮回净地 PrevAct 链重建未成功，稍后重试", "role_id", session.RoleID())
			}
			anchor := session.LastAckActionID()
			if anchor == 0 {
				anchor = session.LastActionID()
			}
			if poolResp := call(protocol.BuildEraRebirthLandPoolPanelPrevActPacket(anchor),
				protocol.ActionIDEraRebirthLandPoolPanel); poolResp != nil {
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

			// 2. 渡业台抽取 (852/16)：消耗渡业符/活动道具，不消耗元宝。
			//    抓包请求载荷为 [Int type=0][PrevAct]；收到非成功结果码（通常为
			//    渡业符不足）即停止。元宝数量发生变化时视为协议漂移并熔断。
			ingotsBefore := session.GetPlayerState().Ingots
			draws := 0
			var drawCooldown error
			for draws < maxAltarDrawsPerRun && ctx.Err() == nil {
				resp := call(protocol.BuildEraRebirthLandDrawPacket(0, session.LastActionID()),
					protocol.ActionIDEraRebirthLandDraw)
				if resp == nil {
					// 抽取无回包不应阻断仙池领取与挂机奖励：先完成其余步骤，最后再冷却。
					drawCooldown = scheduler.NewCooldown(30*time.Minute, "渡业台抽取无回包")
					break
				}
				res, err := protocol.ParseEraRebirthDrawResult(resp.Payload)
				if err != nil {
					slog.Warn("解析渡业台抽取回包失败", "role_id", session.RoleID(), "err", err)
					break
				}
				if res.Result != protocol.EraRebirthResultSuccess {
					slog.Info("渡业台抽取结束", "role_id", session.RoleID(), "draws", draws,
						"result", res.Result, "result_text", protocol.EraRebirthResultText(res.Result))
					break
				}
				draws++
				slog.Info("渡业台抽取成功", "role_id", session.RoleID(),
					"draws", draws, "first", res.First, "second", res.Second)
			}
			if ingotsAfter := session.GetPlayerState().Ingots; ingotsAfter != ingotsBefore {
				slog.Error("渡业台抽取导致元宝变化，疑似协议漂移，立即熔断",
					"role_id", session.RoleID(), "before", ingotsBefore, "after", ingotsAfter)
				return scheduler.NewCooldown(6*time.Hour, "渡业台抽取导致元宝变化")
			}
			if draws > 0 {
				slog.Info("渡业台本轮抽取完成", "role_id", session.RoleID(), "draws", draws)
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
				return drawCooldown
			}
			todayCode, todayOK := claim(protocol.BuildEraRebirthLandTodayAwardPacket(),
				protocol.ActionIDEraRebirthLandTodayAward, "渡业仙池今日奖励")
			if todayOK {
				return drawCooldown
			}
			if freeCode == protocol.EraRebirthResultPackageFull || todayCode == protocol.EraRebirthResultPackageFull {
				return scheduler.ErrBagFull
			}

			// 4. 活跃度任务 70 未完成时才领取挂机奖励，避免无意义请求。
			actResp := call(protocol.BuildGameAssistantInfoPacket(), protocol.ActionIDGameAssistantInfo)
			if actResp == nil {
				slog.Info("查询每日活跃度失败，稍后重试轮回净地", "role_id", session.RoleID())
				return scheduler.NewCooldown(10*time.Minute, "轮回净地活跃度查询无回包")
			}
			panel, err := protocol.ParseGameAssistantInfo(actResp.Payload)
			if err != nil {
				return err
			}
			if panel.RemainingTimesFor(DailyTaskRebirthLand) <= 0 {
				slog.Info("轮回净地活跃度已完成，跳过", "role_id", session.RoleID())
				return nil
			}

			// 5. 兜底：免费一键领取挂机奖励 (852/6)。
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
			case protocol.EraRebirthResultFuncNotOpen:
				slog.Info("轮回净地功能未开放，稍后重试", "role_id", session.RoleID())
				return scheduler.NewCooldown(30*time.Minute, "轮回净地功能未开放")
			case protocol.EraRebirthResultPackageFull:
				return scheduler.ErrBagFull
			default:
				slog.Info("轮回净地领取未成功",
					"role_id", session.RoleID(),
					"result", result.Result,
					"result_text", protocol.EraRebirthResultText(result.Result))
			}
			return drawCooldown
		},
	)
}
