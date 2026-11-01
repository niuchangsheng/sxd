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

// NewOnlineGiftRoutine 构造在线成长礼包领取任务 (Feature 1: 领取在线时长福利)。
//
// 协议基准 (原版客户端 Main.swf Mod_Player_Base.cinit / ActivitiesView):
//   - get_player_current_online_gift -> Module 0, Action 31 (0x0000001F), 请求体为空
//   - player_get_online_gift         -> Module 0, Action 30 (0x0000001E), 请求体为空
//
// 服务端按在线时长滚动发放礼包，客户端由服务器下发的 isGetOnlineGift / onlineGiftTime
// 决定「立即领取」按钮是否出现；机器人侧以固定周期轮询查询并尝试领取。
// 领取结果码: 84 成功, 87 未到时间, 88 背包已满。
func NewOnlineGiftRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"online_gift",
		"daily",
		scheduler.ScheduleLoop,
		scheduler.PriorityNormal,
		10*time.Minute,
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
			slog.Info("正在执行日常任务: 在线成长礼包领取", "role_id", session.RoleID())

			// 1. 查询当前礼包状态 (仅用于诊断与日志, 不阻断领取)
			qCtx, qCancel := context.WithTimeout(ctx, 3*time.Second)
			qResp, qErr := session.Call(qCtx, protocol.BuildOnlineGiftCurrentPacket(), protocol.ActionIDPlayerGetCurrentOnlineGift)
			qCancel()
			if qErr != nil {
				slog.Warn("在线礼包状态查询失败", "role_id", session.RoleID(), "err", qErr)
			} else if qResp != nil {
				if cur, err := protocol.ParseOnlineGiftCurrentResult(qResp.Payload); err != nil {
					slog.Warn("在线礼包状态回包解析失败", "role_id", session.RoleID(), "err", err)
				} else {
					slog.Info("在线礼包当前状态",
						"role_id", session.RoleID(),
						"time_stamp", cur.TimeStamp,
						"gift_count", len(cur.Gifts),
						"gifts", cur.Gifts,
					)
				}
			}

			// 2. 发起领取 (原版客户端点击「立即领取」的协议路径)
			cCtx, cCancel := context.WithTimeout(ctx, 5*time.Second)
			resp, err := session.Call(cCtx, protocol.BuildOnlineGiftClaimPacket(), protocol.ActionIDPlayerGetOnlineGift)
			cCancel()
			if err != nil {
				return fmt.Errorf("daily: 在线礼包领取请求失败: %w", err)
			}
			claim, err := protocol.ParseOnlineGiftClaimResult(resp.Payload)
			if err != nil {
				return fmt.Errorf("daily: 在线礼包领取回包解析失败: %w", err)
			}

			switch claim.Result {
			case protocol.PlayerActionSuccess:
				slog.Info("在线礼包领取成功",
					"role_id", session.RoleID(),
					"gift_count", len(claim.Gifts),
					"gifts", claim.Gifts,
					"next_time_stamp", claim.NextTimeStamp,
				)
			case protocol.PlayerActionFailed:
				slog.Info("在线礼包尚未到领取时间",
					"role_id", session.RoleID(),
					"result", claim.Result,
					"next_time_stamp", claim.NextTimeStamp,
				)
			case protocol.PlayerPackFull:
				slog.Warn("在线礼包领取失败: 背包格子不足",
					"role_id", session.RoleID(),
					"result", claim.Result,
					"next_time_stamp", claim.NextTimeStamp,
				)
				return scheduler.ErrBagFull
			default:
				slog.Info("在线礼包领取返回未识别状态",
					"role_id", session.RoleID(),
					"result", claim.Result,
					"next_time_stamp", claim.NextTimeStamp,
				)
			}
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

