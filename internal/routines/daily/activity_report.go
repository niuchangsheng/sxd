// Package daily 提供签到、邮件、在线奖励、绑定奖励与日常维护类玩法。
//
// 本文件实现每日活跃度汇总上报：读取游戏小助手活跃度面板，输出当前每日/周常
// 活跃度与 1200 达标缺口，便于在运行日志中直接判定目标是否达成。
//
// 该例程只读，不改变任何游戏状态；优先级最低，确保在其他例程之后执行，
// 同时避免活跃度面板请求污染尚未执行的孩子子面板 (Module 312/315)。
package daily

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// dailyActivityTarget 每日活跃度目标 (红包装励门槛)。
const dailyActivityTarget = 1200

// NewDailyActivityReportRoutine 构造每日活跃度汇总任务。
func NewDailyActivityReportRoutine() scheduler.ActivityRoutine {
	const callTimeout = 6 * time.Second
	return scheduler.NewBaseRoutine(
		"activity_report",
		"daily",
		scheduler.ScheduleCron,
		scheduler.PriorityLow,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			cCtx, cancel := context.WithTimeout(ctx, callTimeout)
			resp, err := session.Call(cCtx, protocol.BuildGameAssistantInfoPacket(), protocol.ActionIDGameAssistantInfo)
			cancel()
			if err != nil || resp == nil {
				slog.Warn("查询每日活跃度失败", "role_id", session.RoleID(), "err", err)
				return nil
			}
			panel, err := protocol.ParseGameAssistantInfo(resp.Payload)
			if err != nil {
				return err
			}
			total := panel.DailyTotal()
			gap := dailyActivityTarget - total
			if gap < 0 {
				gap = 0
			}
			slog.Info("每日活跃度汇总",
				"role_id", session.RoleID(),
				"daily_total", total,
				"target", dailyActivityTarget,
				"gap", gap,
				"met", total >= dailyActivityTarget,
			)
			return nil
		},
	)
}
