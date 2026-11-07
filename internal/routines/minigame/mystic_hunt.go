// Package minigame 提供仙幽猎境等玩法自动化。
//
// 本文件实现仙幽猎境每日进入 (Mod_MysticCapture_Base, Module 762)，
// 对应每日活跃度任务 73「每日进入仙幽猎境」(每次 +20)。
//
// 线上实测：
//   - 762 请求不需要 PrevAct，空载荷即可；
//   - `is_open_hunt_entrance (762/5)` 返回 0 表示入口当前关闭（按活动时段开放），
//     此时 `click_enter_hunt/enter_hunt` 会被服务端静默丢弃；
//   - 入口开放时 `enter_hunt (762/1)` 免费进入，不消耗元宝/体力。
package minigame

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// DailyTaskMysticHunt 每日进入仙幽猎境任务编号。
const DailyTaskMysticHunt int32 = 73

// NewMysticHuntRoutine 构造仙幽猎境每日进入任务。
func NewMysticHuntRoutine() scheduler.ActivityRoutine {
	const callTimeout = 10 * time.Second
	return scheduler.NewBaseRoutine(
		"mystic_hunt",
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

			call := func(pkt *protocol.Packet, act uint32, timeout time.Duration) *protocol.Packet {
				cCtx, cancel := context.WithTimeout(ctx, timeout)
				defer cancel()
				resp, err := session.Call(cCtx, pkt, act)
				if err != nil {
					return nil
				}
				return resp
			}

			// 1. 活跃度任务 73 已完成则跳过。
			actResp := call(protocol.BuildGameAssistantInfoPacket(), protocol.ActionIDGameAssistantInfo, callTimeout)
			if actResp == nil {
				slog.Info("仙幽猎境: 查询每日活跃度失败，跳过", "role_id", session.RoleID())
				return nil
			}
			panel, err := protocol.ParseGameAssistantInfo(actResp.Payload)
			if err != nil {
				return fmt.Errorf("解析每日活跃度面板失败: %w", err)
			}
			if panel.RemainingTimesFor(DailyTaskMysticHunt) <= 0 {
				slog.Info("仙幽猎境活跃度已完成，跳过", "role_id", session.RoleID())
				return nil
			}

			// 2. 入口开放检查 (按活动时段开放，关闭时后续请求会被静默丢弃)。
			openResp := call(protocol.BuildMysticHuntEntrancePacket(), protocol.ActionIDMysticHuntEntranceOpen, callTimeout)
			if openResp == nil {
				slog.Info("仙幽猎境入口状态无回包，跳过", "role_id", session.RoleID())
				return nil
			}
			open, err := protocol.ParseMysticHuntEntranceResult(openResp.Payload)
			if err != nil {
				return fmt.Errorf("解析仙幽猎境入口状态失败: %w", err)
			}
			if open == 0 {
				slog.Info("仙幽猎境入口当前未开放，跳过", "role_id", session.RoleID())
				return nil
			}

			// 3. 进入猎境 (免费)。
			enterResp := call(protocol.BuildMysticHuntEnterPacket(), protocol.ActionIDMysticHuntEnter, 15*time.Second)
			if enterResp == nil {
				slog.Info("仙幽猎境进入无回包，跳过", "role_id", session.RoleID())
				return nil
			}
			result, err := protocol.ParseMysticHuntEnterResult(enterResp.Payload)
			if err != nil {
				return fmt.Errorf("解析仙幽猎境进入回包失败: %w", err)
			}
			switch result {
			case protocol.MysticHuntResultSuccess:
				slog.Info("仙幽猎境进入成功", "role_id", session.RoleID())
			case protocol.MysticHuntResultAlreadyExit:
				slog.Info("仙幽猎境已在猎境中/无需重复进入", "role_id", session.RoleID())
			case protocol.MysticHuntResultNoTimes:
				slog.Info("仙幽猎境进入次数不足，跳过", "role_id", session.RoleID())
			default:
				slog.Warn("仙幽猎境进入失败", "role_id", session.RoleID(), "result", result)
			}
			return nil
		},
	)
}
