// 本文件实现纪元秘藏/轮回图录奖励领取 (Mod_EraTreasureOrder_Base, Module 848)。
package minigame

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// NewEraTreasureRoutine 构造纪元秘藏/轮回图录奖励领取任务。
func NewEraTreasureRoutine() scheduler.ActivityRoutine {
	const callTimeout = 10 * time.Second
	return scheduler.NewBaseRoutine(
		"era_treasure",
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
			call := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
				cCtx, cancel := context.WithTimeout(ctx, callTimeout)
				defer cancel()
				resp, err := session.Call(cCtx, pkt, act)
				if err != nil {
					slog.Info("纪元秘藏协议无回包", "role_id", session.RoleID(), "action", act, "err", err)
					return nil
				}
				return resp
			}

			panelResp := call(protocol.BuildEraTreasurePanelPacket(), protocol.ActionIDEraTreasurePanel)
			if panelResp == nil {
				// 面板无回包通常是活动未开启/不在赛季；定时重试而不是等 24 小时。
				slog.Info("纪元秘藏面板无回包，稍后重试", "role_id", session.RoleID())
				return scheduler.NewCooldown(30*time.Minute, "纪元秘藏面板无回包")
			}
			slog.Info("纪元秘藏面板", "role_id", session.RoleID(), "payload_len", len(panelResp.Payload))

			oneKeyResp := call(protocol.BuildEraTreasureOneKeyPacket(), protocol.ActionIDEraTreasureOneKey)
			if oneKeyResp == nil {
				return scheduler.NewCooldown(30*time.Minute, "纪元秘藏一键领取无回包")
			}
			code, err := protocol.ParseEraTreasureResult(oneKeyResp.Payload)
			if err != nil {
				slog.Warn("解析纪元秘藏一键领取回包失败", "role_id", session.RoleID(), "err", err)
				return nil
			}
			if code == 0 {
				slog.Info("纪元秘藏一键领取成功", "role_id", session.RoleID())
			} else {
				// 无奖励/活动状态下 848/6 也会被静默丢弃，不再发送诊断请求。
				slog.Info("纪元秘藏一键领取未成功", "role_id", session.RoleID(), "result", code)
				return nil
			}

			if taskResp := call(protocol.BuildEraTreasureTaskInfoPacket(), protocol.ActionIDEraTreasureTaskInfo); taskResp != nil {
				slog.Info("纪元秘藏任务列表", "role_id", session.RoleID(), "payload_len", len(taskResp.Payload))
			}
			return nil
		},
	)
}
