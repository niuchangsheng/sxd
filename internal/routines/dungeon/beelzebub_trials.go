// Package dungeon 提供神仙道副本与试炼类自动化的业务实现。
//
// @author Ateng
// @since 2026-10-28
package dungeon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// BeelzebubTrialsConfig 魔王试炼自动化策略配置。
type BeelzebubTrialsConfig struct {
	// PreferBossID 优先挑战的魔王编号 (0 表示自动选择最高难度魔王)
	PreferBossID int32 `json:"prefer_boss_id"`
	// AutoMoralAward 是否自动领取道行等级宝箱奖励
	AutoMoralAward bool `json:"auto_moral_award"`
	// MinLevel 开启等级门槛 (默认 70 级开放魔王试炼)
	MinLevel int `json:"min_level"`
}

// DefaultBeelzebubTrialsConfig 默认魔王试炼配置。
func DefaultBeelzebubTrialsConfig() BeelzebubTrialsConfig {
	return BeelzebubTrialsConfig{
		PreferBossID:   0, // 自动选择当前可打的最高魔王
		AutoMoralAward: true,
		MinLevel:       0, // 默认 0，由服务端资格检查 (check_can_challenge) 动态决策
	}
}

// NewBeelzebubTrialsRoutine 构造魔王试炼自动化挑战与道行领取任务。
func NewBeelzebubTrialsRoutine(cfg BeelzebubTrialsConfig) scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"beelzebub_trials",
		"dungeon",
		scheduler.ScheduleCron,
		scheduler.PriorityHigh,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}

			// 1. 等级门槛卫语句检查
			pState := session.GetPlayerState()
			if cfg.MinLevel > 0 && pState.Level > 0 && pState.Level < cfg.MinLevel {
				slog.Info("角色等级未达魔王试炼开放门槛，跳过试炼",
					"role_id", session.RoleID(),
					"current_level", pState.Level,
					"required_level", cfg.MinLevel,
				)
				return nil
			}

			// 2. 拟人防封随机抖动
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			// 功能开放检查 (Mod 0 Act 6 会话缓存)：未开放时服务端会静默丢弃 Module 57 请求。
			open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDBeelzebubTrials)
			if fErr != nil {
				slog.Info("魔王试炼功能开放状态查询失败，本轮跳过",
					"role_id", session.RoleID(), "error", fErr)
				return nil
			}
			if !open {
				slog.Info("魔王试炼功能未开放，跳过",
					"role_id", session.RoleID(), "function_id", protocol.FunctionIDBeelzebubTrials)
				return nil
			}

			// 开启时间面板只用于诊断日志：实机 s0/s1 的 State/时间段含义不一致
			// （同一角色不同登录会话可能返回历史窗口），不能据此跳过玩法；
			// 真正的可用性以 check_is_can_challenge / 面板关卡状态为准。
			if openTime, oErr := session.BeelzebubTrialsGetOpenTime(ctx); oErr == nil {
				slog.Debug("魔王试炼开启时间面板",
					"role_id", session.RoleID(),
					"state", openTime.State,
					"windows", openTime.Windows,
					"in_activity", openTime.InActivity,
					"all_death", openTime.AllDeath)
			}

			slog.Info("正在执行副本任务: 魔王试炼自动挑战与道行领取", "role_id", session.RoleID())

			// 3. 检查今日挑战资格
			canChallenge, err := session.BeelzebubTrialsCheckCanChallenge(ctx)
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					// 服务端静默丢弃资格查询（常见于刚切场景/重连后），本轮跳过。
					slog.Info("检查魔王试炼挑战状态无回包，本轮跳过",
						"role_id", session.RoleID(), "error", err)
					return nil
				}
				slog.Warn("检查魔王试炼挑战状态失败", "role_id", session.RoleID(), "error", err)
				return nil
			}

			if !canChallenge {
				slog.Info("今日魔王试炼挑战次数已耗尽或未开放", "role_id", session.RoleID())
				// 即使没有挑战次数，也检查是否有未领取的道行奖励
				if cfg.AutoMoralAward {
					claimMoralAwardSafely(ctx, session)
				}
				return nil
			}

			// 4. 遍历可挑战魔王列表，逐关执行组队、助战镜像配置、战斗与结算闭环
			warResults, err := session.BeelzebubTrialsAutoChallengeAll(ctx, cfg.PreferBossID)
			if err != nil {
				if errors.Is(err, client.ErrBeelzebubTrialsNoResponse) {
					// 已根因定位为 s0 服务端账号/版本限制（详见 docs/protocol-spec.md 4.12），
					// 换载荷/换关卡/换会话均无法恢复；延长冷却，减少无效请求。
					slog.Info("魔王试炼建队被服务端拒绝（账号/版本限制），稍后重试",
						"role_id", session.RoleID())
					return scheduler.NewCooldown(4*time.Hour, "魔王试炼建队被服务端拒绝")
				}
				if errors.Is(err, context.DeadlineExceeded) {
					slog.Info("魔王试炼请求超时/被服务端丢弃，稍后重试", "role_id", session.RoleID(), "error", err)
					return scheduler.NewCooldown(30*time.Minute, "魔王试炼请求被丢弃")
				}
				return fmt.Errorf("魔王试炼自动挑战流程执行失败: %w", err)
			}

			if len(warResults) == 0 {
				slog.Info("魔王试炼暂无可获得奖励的关卡或今日挑战次数已耗尽", "role_id", session.RoleID())
			} else {
				succeeded := 0
				for _, warRes := range warResults {
					if warRes != nil && warRes.Result == protocol.BeelzebubTrialsSuccess {
						succeeded++
					}
				}
				slog.Info("魔王试炼所有可挑战关卡已逐关完成",
					"role_id", session.RoleID(),
					"total", len(warResults),
					"success", succeeded,
				)
			}

			// 5. 战斗结算后尝试领取道行等级宝箱
			if cfg.AutoMoralAward {
				claimMoralAwardSafely(ctx, session)
			}

			return nil
		},
	)
}

// claimMoralAwardSafely 检查并安全领取道行经验等级宝箱奖励。
func claimMoralAwardSafely(ctx context.Context, session *client.RoleSession) {
	moral, err := session.BeelzebubTrialsOpenMoral(ctx)
	if err != nil {
		slog.Debug("获取魔王试炼道行信息失败", "role_id", session.RoleID(), "error", err)
		return
	}

	if moral != nil && moral.CanClaim && moral.Level > 0 {
		res, err := session.BeelzebubTrialsGetMoralAward(ctx, moral.Level)
		if err != nil {
			slog.Warn("领取魔王试炼道行等级奖励失败", "role_id", session.RoleID(), "moral_lv", moral.Level, "error", err)
			return
		}
		if res == protocol.BeelzebubTrialsSuccess {
			slog.Info("已成功领用魔王试炼道行等级宝箱奖励", "role_id", session.RoleID(), "moral_lv", moral.Level)
		}
	}
}
