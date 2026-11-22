// Package pvp 提供竞技场、仙界竞技场、神魔大战、取经劫镖、世界 Boss 与跨服天梯等对抗玩法。
//
// 本文件实现世界 Boss (Mod_WorldBoss_Base) 自动挑战。
package pvp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// NewWorldBossRoutine 构造世界 Boss 自动参战任务 (Module 24, Mod_WorldBoss_Base)。
//
// 每 5 分钟巡检一次 Boss 列表，仅在 state=1 (开战中) 时发起一次免费战斗；
// 战斗回包携带完整战报，只解析首个结果码。永不调用
// clear_death_cd / reset_death_cd / strengthen_combat 等消耗元宝的接口。
// worldBossFightBlocked 世界 Boss 战斗请求超时后的退避。
// 低等级角色挑战高等级 Boss 时服务端可能长时间不结算/直接丢弃，
// 5 分钟巡检会反复超时；按会话退避 30 分钟。
var worldBossFightBlocked sync.Map // *client.RoleSession -> time.Time

// maxWorldBossFightsPerRun 单轮巡检最多参战次数（安全上限，实际以服务端
// MaxTimes 回包为准）。Boss 窗口较短时一次调度内连续打完剩余次数。
const maxWorldBossFightsPerRun = 10

func NewWorldBossRoutine() scheduler.ActivityRoutine {
	const callTimeout = 5 * time.Second
	const fightTimeout = 30 * time.Second
	return scheduler.NewBaseRoutine(
		"world_boss",
		"pvp",
		scheduler.ScheduleLoop,
		scheduler.PriorityHigh,
		5*time.Minute,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if session.State() != client.StateActive {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			// 1. 查询世界 Boss 列表
			lCtx, lCancel := context.WithTimeout(ctx, callTimeout)
			listResp, err := session.Call(lCtx, protocol.BuildWorldBossListPacket(), protocol.ActionIDWorldBossList)
			lCancel()
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					// 5 分钟巡检下，列表查询被静默丢弃会变成每小时 12 次空请求；
					// 连续丢包按静默丢包机制退避（首次 10 分钟，连续 3 次挂起到重置）。
					return scheduler.HandleSilentDropCtx(ctx, session, "world_boss", 3, 10*time.Minute, "世界Boss列表无回包")
				}
				return fmt.Errorf("查询世界Boss列表失败: %w", err)
			}
			bosses, err := protocol.ParseWorldBossListResult(listResp.Payload)
			if err != nil {
				return fmt.Errorf("解析世界Boss列表失败: %w", err)
			}

			var open *protocol.WorldBossEntry
			for i := range bosses {
				if bosses[i].State == protocol.WorldBossStateOpen {
					open = &bosses[i]
					break
				}
			}
			if open == nil {
				slog.Debug("世界Boss当前未开战", "role_id", session.RoleID(), "bosses", len(bosses))
				return nil
			}

			// 2. 发起免费战斗，直到服务端提示次数用尽。
			//    一次调度只打一场会把剩余次数留到下一个 5 分钟巡检，Boss 窗口
			//    较短时可能打不完；这里按回包结果连续参战，MaxTimes 即停。
			slog.Info("世界Boss开战中，发起参战",
				"role_id", session.RoleID(),
				"boss_id", open.BossID,
				"boss_level", open.Level,
				"map_id", open.MapID,
			)
			if until, ok := worldBossFightBlocked.Load(session); ok {
				if t, _ := until.(time.Time); time.Now().Before(t) {
					slog.Debug("世界Boss战斗近期超时，退避期间跳过", "role_id", session.RoleID())
					return nil
				}
			}
			fights := 0
			for fights < maxWorldBossFightsPerRun {
				if ctx.Err() != nil {
					return nil
				}
				fCtx, fCancel := context.WithTimeout(ctx, fightTimeout)
				fightResp, err := session.Call(fCtx, protocol.BuildWorldBossFightPacket(open.BossID), protocol.ActionIDWorldBossFight)
				fCancel()
				if err != nil {
					worldBossFightBlocked.Store(session, time.Now().Add(30*time.Minute))
					slog.Info("世界Boss战斗请求超时，暂停参战 30 分钟",
						"role_id", session.RoleID(), "boss_id", open.BossID, "fights", fights, "err", err)
					return nil
				}
				result, err := protocol.ParseWorldBossFightResult(fightResp.Payload)
				if err != nil {
					return fmt.Errorf("解析世界Boss战斗回包失败: %w", err)
				}
				switch result {
				case protocol.WorldBossResultSuccess:
					fights++
					slog.Info("世界Boss参战成功",
						"role_id", session.RoleID(), "boss_id", open.BossID, "fights", fights)
					select {
					case <-ctx.Done():
						return nil
					case <-time.After(time.Second):
					}
					continue
				case protocol.WorldBossResultNotEnoughIngot:
					slog.Warn("世界Boss参战提示元宝不足 (自动化不主动消耗元宝)",
						"role_id", session.RoleID(), "boss_id", open.BossID, "fights", fights)
					return nil
				case protocol.WorldBossResultMaxTimes:
					// 今日参战次数已满：挂起至次日，避免每次调度都重复发起参战。
					slog.Info("世界Boss今日参战次数已满，今日不再参战",
						"role_id", session.RoleID(), "boss_id", open.BossID, "fights", fights)
					return scheduler.ErrAttemptsExhausted
				default:
					slog.Info("世界Boss参战未成功，结束本轮",
						"role_id", session.RoleID(),
						"boss_id", open.BossID,
						"result", result,
						"reason", worldBossResultName(result),
					)
					return nil
				}
			}
			slog.Info("世界Boss本轮参战达到安全上限，等待下一次巡检",
				"role_id", session.RoleID(), "fights", fights)
			return nil
		},
	)
}

// worldBossResultName 返回世界 Boss 战斗结果码的中文描述。
func worldBossResultName(result uint8) string {
	switch result {
	case protocol.WorldBossResultFailed:
		return "战斗失败"
	case protocol.WorldBossResultNotBegin:
		return "未到开战时间"
	case protocol.WorldBossResultMissionOver:
		return "活动已结束"
	case protocol.WorldBossResultBossDead:
		return "Boss已被击杀"
	case protocol.WorldBossResultDeathStatus:
		return "角色处于死亡状态"
	case protocol.WorldBossResultNotEnoughIngot:
		return "元宝不足"
	case protocol.WorldBossResultMaxTimes:
		return "今日参战次数已满"
	case protocol.WorldBossResultNotEnoughSkill:
		return "技能点不足"
	case protocol.WorldBossResultCombatLimit:
		return "战力不足"
	default:
		return "未知结果"
	}
}
