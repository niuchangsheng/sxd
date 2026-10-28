// Package daily 提供每日签到、日常任务与城镇打坐练功福利自动化逻辑。
//
// @author Ateng
// @since 2026-10-28
package daily

import (
	"context"
	"fmt"
	"log/slog"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/scheduler"
)

// NewTownPracticeRoutine 构造普通城镇打坐练功经验领取任务。
// 现代版机制：神仙道角色离线会自动进入打坐，上线后直接结算并领取累积经验即可。
func NewTownPracticeRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"town_practice",
		"daily",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}

			// 1. 前置卫语句检查: 是否进入城镇场景
			pState := session.GetPlayerState()
			if pState.TownID <= 0 {
				slog.Debug("角色尚未进入城镇场景，跳过打坐练功经验领取", "role_id", session.RoleID())
				return nil
			}

			// 2. 拟人防封随机抖动
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			slog.Info("正在执行日常任务: 领取离线城镇打坐练功经验", "role_id", session.RoleID())

			// 3. 上线直接一键结算并领取经验 (无需繁杂的手动打坐循环，离线自动打坐)
			if err := session.ClaimTownPracticeExp(ctx); err != nil {
				return fmt.Errorf("领取城镇打坐练功经验失败: %w", err)
			}

			slog.Info("已成功触发城镇打坐练功经验领取", "role_id", session.RoleID())
			return nil
		},
	)
}
