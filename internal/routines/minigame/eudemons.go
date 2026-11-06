// Package minigame 提供吉星高照、仙旅奇缘答题、喜从天降、刮刮卡、钓鱼等益智趣味活动。
//
// 本文件实现幻兽经脉激活 (Mod_Eudemons_Base, Module 693) 自动化。
package minigame

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// maxEudemonsActivations 单轮最多激活的经脉数量。
const maxEudemonsActivations = 20

// NewEudemonsRoutine 构造幻兽日常维护任务。
//
// 官方 com.datas.EudemonsData.panel_info 返回经脉激活状态；未激活的经脉可直接
// 调用 activation_meridians 激活，消耗幻兽灵石等游戏资源，不消耗元宝。
// 山海幻兽入侵 (Mod 696) 由独立的 huanshou_invade 任务处理。
func NewEudemonsRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"eudemons",
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
			slog.Info("正在执行益智任务: 幻兽经脉维护", "role_id", session.RoleID())

			// 幻兽经脉属于山海幻兽玩法，先查功能开放列表，未开放时不发送业务请求。
			open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDEudemons)
			if fErr != nil {
				slog.Info("幻兽功能开放状态查询失败，跳过", "role_id", session.RoleID(), "err", fErr)
				return nil
			}
			if !open {
				slog.Info("幻兽功能未开放，跳过", "role_id", session.RoleID(), "function_id", protocol.FunctionIDEudemons)
				return nil
			}

			call := func(pkt *protocol.Packet, act uint32) (*protocol.Packet, error) {
				cCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				return session.Call(cCtx, pkt, act)
			}

			resp, err := call(protocol.BuildEudemonsPanelPacket(), protocol.ActionIDEudemonsPanel)
			if err != nil || resp == nil {
				slog.Info("幻兽功能不可用或未开放，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			panel, err := protocol.ParseEudemonsPanelResult(resp.Payload)
			if err != nil {
				slog.Info("解析幻兽面板失败，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			if panel.Result != protocol.EudemonsResultSuccess {
				slog.Info("幻兽面板返回失败", "role_id", session.RoleID(), "result", panel.Result)
				return nil
			}
			slog.Info("幻兽面板",
				"role_id", session.RoleID(),
				"skin_id", panel.SkinID,
				"meridians", len(panel.Meridians),
				"stone_max_level", panel.StoneMaxLevel,
				"total_stone_step", panel.TotalStoneStep,
			)

			activated := 0
			for _, meridian := range panel.Meridians {
				if meridian.IsActive == 1 {
					continue
				}
				if activated >= maxEudemonsActivations {
					break
				}
				if err := ctx.Err(); err != nil {
					return nil
				}
				aResp, aErr := call(
					protocol.BuildEudemonsActivateMeridiansPacket(meridian.ID),
					protocol.ActionIDEudemonsActivateMeridians,
				)
				if aErr != nil || aResp == nil {
					slog.Warn("激活幻兽经脉无回包", "role_id", session.RoleID(), "meridian_id", meridian.ID, "err", aErr)
					return nil
				}
				code, pErr := protocol.ParseEudemonsActionCode(aResp.Payload)
				if pErr != nil {
					slog.Warn("解析幻兽经脉回包失败", "role_id", session.RoleID(), "err", pErr)
					return nil
				}
				switch code {
				case protocol.EudemonsResultSuccess:
					activated++
					slog.Info("幻兽经脉激活成功", "role_id", session.RoleID(), "meridian_id", meridian.ID)
				case protocol.EudemonsResultMeridiansActive:
					slog.Debug("幻兽经脉已激活", "role_id", session.RoleID(), "meridian_id", meridian.ID)
				case protocol.EudemonsResultMeridiansNotActive:
					slog.Info("幻兽经脉前置条件未满足，停止激活", "role_id", session.RoleID(), "meridian_id", meridian.ID)
					return nil
				case protocol.EudemonsResultGemStepNotEnough:
					slog.Info("幻兽宝石阶数不足，停止激活", "role_id", session.RoleID())
					return nil
				case protocol.EudemonsResultFuncNotOpen:
					slog.Info("幻兽功能未开放", "role_id", session.RoleID())
					return nil
				default:
					slog.Info("幻兽经脉激活未成功", "role_id", session.RoleID(), "meridian_id", meridian.ID, "result", code)
					return nil
				}
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(200 * time.Millisecond):
				}
			}
			slog.Info("幻兽巡检完成", "role_id", session.RoleID(), "activated", activated)
			return nil
		},
	)
}
