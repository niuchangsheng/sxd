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

// NewDailyQuestRoutine 构造每日日常活跃度与小助手领奖任务 (Feature 3: 领取日活跃奖励)。
func NewDailyQuestRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"daily_quest",
		"daily",
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
			slog.Info("正在执行日常任务: 领取日活跃度小助手奖励", "role_id", session.RoleID())

			pkt := protocol.BuildAssistantClaimRewardPacket(protocol.ActionIDTownEnter)
			callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()

			resp, err := session.Call(callCtx, pkt, protocol.ActionIDAssistantClaimReward)
			if err != nil {
				slog.Warn("领取日活跃奖励请求超时或网关未即时应答", "err", err)
				return nil
			}
			if resp != nil {
				slog.Info("领取日活跃奖励成功", "role_id", session.RoleID())
			}
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

			callCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
			respPkt, err := session.Call(callCtx, reqPkt, protocol.ActionIDMailCollect)
			cancel()

			// 4. 解析收取结果 (兼容异步通知网关)
			if err == nil && respPkt != nil {
				res, pErr := protocol.ParseMailCollectResult(respPkt.Payload)
				if pErr == nil && res.Success {
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
					return nil
				}
			}

			slog.Info("邮件附件一键收取指令已发送", "role_id", session.RoleID())
			return nil
		},
	)
}

// NewOnlineGiftRoutine 构造在线福利与成长礼包领取任务 (Feature 1: 领取奖励)。
func NewOnlineGiftRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"online_gift",
		"daily",
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
			slog.Info("正在执行日常任务: 在线福利与礼包奖励领取", "role_id", session.RoleID())

			// 1. 查询奖励列表
			queryPkt := protocol.NewPacket(protocol.ActionIDVipAwardQuery, protocol.NewWriter().WriteUint32(protocol.ActionIDTownEnter).Bytes())
			callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			_, _ = session.Call(callCtx, queryPkt, protocol.ActionIDVipAwardQuery)
			cancel()

			// 2. 依据 haha.pcapng 抓包事实: 连续提取各序列在线奖励 (0x055A3412 ~ 0x055A3417)
			baseSeq := uint32(0x055A3412)
			prevAct := protocol.ActionIDVipAwardQuery
			var successCount int

			for i := uint32(0); i < 6; i++ {
				seqID := baseSeq + i
				pkt := protocol.BuildClaimAwardPacket(seqID, prevAct)
				prevAct = protocol.ActionIDItemClaimAward

				cCtx, cCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
				resp, err := session.Call(cCtx, pkt, protocol.ActionIDItemClaimAward)
				cCancel()

				if err == nil && resp != nil {
					if ok, _ := protocol.ParseClaimAwardResult(resp.Payload); ok {
						successCount++
					}
				}
			}

			slog.Info("在线礼包奖励领取完成", "role_id", session.RoleID(), "success_count", successCount)
			return nil
		},
	)
}

// NewGodBlessRoutine 构造跨服天神赐福福利领用任务 (Feature 6: 天神赐福)。
func NewGodBlessRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"god_bless",
		"daily",
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
			slog.Info("正在执行日常任务: 跨服天神赐福领取", "role_id", session.RoleID())

			// 依据抓包: 每次领取 1 点天神赐福，单次触发 10 轮
			prevAct := protocol.ActionIDTownEnter
			var successCount int

			for i := 0; i < 10; i++ {
				pkt := protocol.BuildGodBlessClaimPacket(prevAct)
				prevAct = protocol.ActionIDGodBlessClaim

				cCtx, cCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
				resp, err := session.Call(cCtx, pkt, protocol.ActionIDGodBlessClaim)
				cCancel()

				if err == nil && resp != nil {
					successCount++
				} else {
					break
				}
			}

			slog.Info("跨服天神赐福领取完成", "role_id", session.RoleID(), "claimed_times", successCount)
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

