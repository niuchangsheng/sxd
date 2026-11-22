// 本文件实现修真境界「一键领取礼包」(Mod_XiuzhenRealm_Base, Module 552)。
//
// 抓包基准: /tmp/xiuzhen.pcapng（2026-11-22，qq s0）：
//
//	552/1 main_panel      []  → 主面板
//	552/4 get_step        []
//	552/5 get_extra_award []  → 领取礼包，回包含奖励列表
//
// 抓包奖励: 8808 青铜宝箱 ×5、5852 一品神器包 ×1。
// 结果码: 0 成功、1 已领取、2 境界点不足、3 条件未满足、4 背包已满。
// 不消耗元宝。
package daily

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

// xiuzhenRealmRewardDetail 将奖励列表格式化为 "item:num" 便于日志排查。
func xiuzhenRealmRewardDetail(rewards []protocol.XiuZhenRealmReward) string {
	out := ""
	for i, it := range rewards {
		if i > 0 {
			out += ","
		}
		out += fmt.Sprintf("%d:%d", it.ItemID, it.Num)
	}
	return out
}

// NewXiuZhenRealmAwardRoutine 构造修真境界礼包领取任务。
func NewXiuZhenRealmAwardRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"xiuzhen_realm_award",
		"daily",
		scheduler.ScheduleCron,
		// 与其他礼包类任务一样，登录后优先执行，避免长会话后段被静默丢弃。
		scheduler.PhasePriority(scheduler.PhaseEarlyBattle, 15),
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			caller := kit.NewCaller(ctx, session, callTimeout)
			call := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
				resp, err := caller.CallResync(pkt, act)
				if err != nil {
					slog.Info("修真境界协议无回包", "role_id", session.RoleID(),
						"action", act, "err", err,
						"recent_rx", fmt.Sprintf("%X", session.RecentRxActions()))
					return nil
				}
				return resp
			}

			// 先打开主面板；面板被静默丢弃不阻断领取，以 552/5 结果码为准。
			if resp := call(protocol.BuildXiuZhenRealmMainPacket(),
				protocol.ActionIDXiuZhenRealmMain); resp != nil {
				if code, err := protocol.ParseXiuZhenRealmMainResult(resp.Payload); err == nil {
					slog.Info("修真境界主面板", "role_id", session.RoleID(),
						"result", code, "result_text", protocol.XiuZhenRealmResultText(code))
				}
			} else if abort := caller.AbortErr(); abort != nil {
				return abort
			} else if session.State() != client.StateActive {
				return client.ErrNotConnected
			}

			resp := call(protocol.BuildXiuZhenRealmExtraAwardPacket(),
				protocol.ActionIDXiuZhenRealmExtraAward)
			if resp == nil {
				if abort := caller.AbortErr(); abort != nil {
					return abort
				}
				if session.State() != client.StateActive {
					return client.ErrNotConnected
				}
				return scheduler.HandleSilentDropCtx(ctx, session, "xiuzhen_realm_award", 3,
					30*time.Minute, "修真境界礼包领取无回包")
			}
			scheduler.ResetSilentDropsCtx(ctx, session, "xiuzhen_realm_award")

			res, err := protocol.ParseXiuZhenRealmResult(resp.Payload)
			if err != nil {
				return scheduler.NewCooldown(10*time.Minute, "修真境界礼包回包解析失败")
			}
			switch res.Result {
			case protocol.XiuZhenRealmSuccess:
				slog.Info("修真境界礼包领取成功", "role_id", session.RoleID(),
					"rewards", xiuzhenRealmRewardDetail(res.Rewards))
				// 礼包奖励随境界点累积可重复领取（实测 20 分钟后再次领取成功），
				// 2 小时后再检查，避免 Cron 直接等到次日。
				return scheduler.NewCooldown(2*time.Hour, "修真境界礼包已领取，2 小时后再检查")
			case protocol.XiuZhenRealmIsGet:
				slog.Info("修真境界礼包今日已领取，等待次日重置", "role_id", session.RoleID())
				return scheduler.NewCooldownUntil(scheduler.NextDailyReset(time.Now()),
					"修真境界礼包今日已领取")
			case protocol.XiuZhenRealmLessPoint:
				slog.Info("修真境界点不足，6 小时后再检查", "role_id", session.RoleID())
				return scheduler.NewCooldown(6*time.Hour, "修真境界点不足")
			case protocol.XiuZhenRealmCondLimit:
				slog.Info("修真境界条件未满足，6 小时后再检查", "role_id", session.RoleID())
				return scheduler.NewCooldown(6*time.Hour, "修真境界条件未满足")
			case protocol.XiuZhenRealmItemPackFull:
				slog.Info("修真境界礼包领取失败：背包已满", "role_id", session.RoleID())
				return scheduler.ErrBagFull
			case protocol.XiuZhenRealmNoAward:
				slog.Info("修真境界暂无可领取奖励，1 小时后再检查", "role_id", session.RoleID())
				return scheduler.NewCooldown(time.Hour, "修真境界暂无可领取奖励")
			default:
				slog.Info("修真境界礼包领取未成功", "role_id", session.RoleID(),
					"result", res.Result, "result_text", protocol.XiuZhenRealmResultText(res.Result))
				return scheduler.NewCooldown(time.Hour, "修真境界礼包领取结果异常")
			}
		},
	)
}
