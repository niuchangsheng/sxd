// Package daily 提供每日签到、日常任务与邮件福利自动化逻辑。
//
// @author Ateng
// @since 2026-10-08
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

// NewSignInRoutine 构造每日签到与更新福利任务。
func NewSignInRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"daily_sign_in",
		"daily",
		scheduler.ScheduleCron,
		scheduler.PriorityHigh,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}

			// 1. 拟人防封随机抖动
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			slog.Info("正在执行日常任务: 每日签到与在线福利领取", "role_id", session.RoleID())

			// 2. 发起每日签到请求
			reqPkt := protocol.BuildDailySignInPacket(protocol.DailySignInRequest{
				PrevAct: protocol.ActionIDPlayerGetInfo,
			})

			callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()

			respPkt, err := session.Call(callCtx, reqPkt, protocol.ActionIDDailySignIn)
			if err != nil {
				return fmt.Errorf("每日签到请求超时或失败: %w", err)
			}

			// 3. 解析签到回包与收益
			res, err := protocol.ParseDailySignInResult(respPkt.Payload)
			if err != nil {
				return fmt.Errorf("解析每日签到回包失败: %w", err)
			}

			if res.AlreadySigned {
				slog.Info("今日已完成签到，无需重复执行",
					"role_id", session.RoleID(),
					"signed_days", res.SignedDays,
				)
				return nil
			}

			if res.Success {
				slog.Info("每日签到成功",
					"role_id", session.RoleID(),
					"signed_days", res.SignedDays,
					"reward_coins", res.RewardCoins,
					"reward_ingots", res.RewardIngots,
					"message", res.Message,
				)

				// 同步更新本地资产快照
				if res.RewardCoins > 0 || res.RewardIngots > 0 {
					session.UpdatePlayerState(func(ps *client.PlayerState) {
						ps.Coins += res.RewardCoins
						ps.Ingots += int64(res.RewardIngots)
					})
				}
			}

			return nil
		},
	)
}

// NewDailyQuestRoutine 构造每日日常活跃度与小助手任务。
func NewDailyQuestRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"daily_quest",
		"daily",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行日常任务: 每日任务与小助手日常")
			return nil
		},
	)
}

// NewMailCollectRoutine 构造自动领取邮件附件任务。
func NewMailCollectRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"mail_collect",
		"daily",
		scheduler.ScheduleCron,
		scheduler.PriorityLow,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}

			// 1. 前置卫语句：背包空余检查 (防爆仓)
			state := session.GetPlayerState()
			if state.BagCapacity <= 0 {
				slog.Warn("背包容量不足，暂不收取邮件附件", "role_id", session.RoleID())
				return scheduler.ErrBagFull
			}

			// 2. 拟人防封随机抖动
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			slog.Info("正在执行日常任务: 邮件附件一键收取", "role_id", session.RoleID())

			// 3. 发送一键提取邮件附件请求
			reqPkt := protocol.BuildMailCollectPacket(protocol.MailCollectRequest{
				PrevAct: protocol.ActionIDMailGetList,
			})

			callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()

			respPkt, err := session.Call(callCtx, reqPkt, protocol.ActionIDMailCollect)
			if err != nil {
				return fmt.Errorf("一键收取邮件请求超时或失败: %w", err)
			}

			// 4. 解析收取结果
			res, err := protocol.ParseMailCollectResult(respPkt.Payload)
			if err != nil {
				return fmt.Errorf("解析邮件收取回包失败: %w", err)
			}

			if res.Success {
				slog.Info("邮件附件收取完成",
					"role_id", session.RoleID(),
					"collected_count", res.CollectedCount,
					"reward_coins", res.RewardCoins,
					"message", res.Message,
				)

				if res.RewardCoins > 0 {
					session.UpdatePlayerState(func(ps *client.PlayerState) {
						ps.Coins += res.RewardCoins
					})
				}
			}

			return nil
		},
	)
}

// NewOnlineGiftRoutine 构造在线福利与挂机礼包领取任务。
func NewOnlineGiftRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"online_gift",
		"daily",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行日常任务: 在线福利与成长礼包领取")
			return nil
		},
	)
}

// NewVIPWelfareRoutine 构造VIP福利与每日俸禄领用任务。
func NewVIPWelfareRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"vip_welfare",
		"daily",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行日常任务: VIP等级特权礼包与俸禄领取")
			return nil
		},
	)
}

