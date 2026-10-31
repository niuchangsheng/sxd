// Package daily 提供每日签到、日常任务与城镇打坐练功福利自动化逻辑。
//
// @author Ateng
// @since 2026-10-28
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

			// 3. 查询当前现代离线打坐练功进度与即将领取的经验值 (Mod_NewPractice_Base.get_practice_info)
			pendingExp := int64(0)
			practiceInfo, err := session.GetNewPracticeData(ctx)
			if err == nil && practiceInfo != nil {
				pendingExp = practiceInfo.PracticeExp
			} else {
				pendingExp = session.GetPlayerState().PracticeExp
			}

			// 记录领取前的经验快照
			beforeState := session.GetPlayerState()
			slog.Info("即将领取城镇打坐练功经验",
				"role_id", session.RoleID(),
				"pending_exp", pendingExp,
				"current_level", beforeState.Level,
				"current_exp", beforeState.Experience,
			)

			// 4. 上线直接一键结算并领取经验 (Mod_NewPractice_Base.collect_all，无需主动打坐，离线全自动累计)
			var collectResult *protocol.NewPracticeCollectResult
			collectResult, err = session.ClaimNewPracticeExp(ctx)
			if err != nil {
				// 若新打坐接口调用失败，回退兼容老打坐结算
				if legacyErr := session.ClaimTownPracticeExp(ctx); legacyErr != nil {
					return fmt.Errorf("领取城镇打坐练功经验失败: %w (legacy: %v)", err, legacyErr)
				}
			}

			// 等待网络回包与服务端经验同步
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(800 * time.Millisecond):
			}

			// 5. 拉取角色最新状态并输出等级、经验与升级剩余经验
			pInfo, pErr := session.FetchLatestPlayerInfo(ctx)
			afterExp := beforeState.Experience
			afterLevel := beforeState.Level
			maxExp := beforeState.MaxExperience
			roleName := session.RoleName()
			if pErr == nil && pInfo != nil {
				afterExp = pInfo.Experience
				afterLevel = int(pInfo.Level)
				maxExp = pInfo.MaxExperience
				roleName = pInfo.RoleName
			} else {
				st := session.GetPlayerState()
				afterExp = st.Experience
				afterLevel = st.Level
				maxExp = st.MaxExperience
			}

			gainExp := afterExp - beforeState.Experience
			if collectResult != nil && collectResult.GainExp > 0 && gainExp <= 0 {
				gainExp = collectResult.GainExp
			}

			remExp := int64(0)
			if maxExp > afterExp {
				remExp = maxExp - afterExp
			}

			if gainExp > 0 {
				slog.Info("城镇打坐练功经验领取成功",
					"role_id", session.RoleID(),
					"role_name", roleName,
					"pending_exp", pendingExp,
					"gain_exp", gainExp,
					"level", afterLevel,
					"exp", afterExp,
					"max_exp", maxExp,
					"remaining_exp", remExp,
				)
			} else {
				slog.Info("城镇打坐练功经验结算完成 (本次打坐无经验增长: 离线时间过短或前序已领取)",
					"role_id", session.RoleID(),
					"role_name", roleName,
					"pending_exp", pendingExp,
					"gain_exp", 0,
					"level", afterLevel,
					"exp", afterExp,
					"max_exp", maxExp,
					"remaining_exp", remExp,
				)
			}
			return nil
		},
	)
}
