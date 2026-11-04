// Package daily 提供每日签到、日常任务与邮件福利自动化逻辑。
//
// @author Ateng
// @since 2026-10-08
package daily

import (
	"context"
	"encoding/hex"
	"errors"
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

// NewMailCollectRoutine 构造邮箱附件一键收取任务。
//
// 协议依据官方客户端 Main.swf 的 Mod_Email_Base (Module 345)：
//   - get_email_info:    Action 0，打开邮箱面板并同步邮件列表
//   - one_key_get_award: Action 6，一键领取全部带附件邮件
//
// 主服请求末尾由客户端框架自动追加 4 字节 PrevAct，因此空请求线格式为
// uint32 PrevAct。先打开邮箱面板再一键收取，以对齐官方客户端的 prevAct 链。
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
				slog.Warn("背包容量不足，暂不收取邮箱附件", "role_id", session.RoleID())
				return scheduler.ErrBagFull
			}

			// 2. 拟人防封随机抖动
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			slog.Info("正在执行日常任务: 邮箱附件一键收取", "role_id", session.RoleID())

			// 3. 打开邮箱面板并同步邮件列表 (Mod 345, Act 0)
			prevAct := protocol.ActionIDTownEnter
			infoPkt := protocol.BuildEmailGetInfoPacket(prevAct)
			infoCtx, infoCancel := context.WithTimeout(ctx, 3*time.Second)
			infoResp, infoErr := session.Call(infoCtx, infoPkt, protocol.ActionIDEmailGetInfo)
			infoCancel()
			if infoErr != nil || infoResp == nil {
				slog.Warn("邮箱面板同步失败，继续尝试一键收取", "role_id", session.RoleID(), "err", infoErr)
			} else {
				prevAct = protocol.ActionIDEmailGetInfo
				slog.Info("邮箱面板已同步", "role_id", session.RoleID())
			}

			// 4. 一键领取全部邮件附件 (Mod 345, Act 6)
			collectPkt := protocol.BuildEmailOneKeyGetAwardPacket(prevAct)
			callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			respPkt, err := session.Call(callCtx, collectPkt, protocol.ActionIDEmailOneKeyGetAward)
			cancel()
			if err != nil {
				return fmt.Errorf("邮箱一键收取请求失败: %w", err)
			}
			if respPkt == nil {
				return fmt.Errorf("邮箱一键收取未收到回包")
			}

			// 5. 解析业务结果码并按语义熔断
			result, err := protocol.ParseEmailOneKeyGetAwardResult(respPkt.Payload)
			if err != nil {
				return fmt.Errorf("解析邮箱一键收取回包失败: %w", err)
			}

			resultText := protocol.EmailResultText(result)
			switch result {
			case protocol.EmailResultSuccess:
				slog.Info("邮箱附件一键收取完成",
					"role_id", session.RoleID(),
					"result", result,
					"result_text", resultText,
				)
			case protocol.EmailResultNoAward, protocol.EmailResultHadGet:
				slog.Info("邮箱暂无可收取附件",
					"role_id", session.RoleID(),
					"result", result,
					"result_text", resultText,
				)
			case protocol.EmailResultItemPackFull,
				protocol.EmailResultFatePackFull,
				protocol.EmailResultLinkFatePackFull,
				protocol.EmailResultDragonballPackFull:
				slog.Warn("邮箱附件收取失败: 背包已满",
					"role_id", session.RoleID(),
					"result", result,
					"result_text", resultText,
				)
				return scheduler.ErrBagFull
			default:
				// 业务确定性失败 (失败/编号无效/非本角色) 不重试，等待下一调度窗口。
				slog.Warn("邮箱附件收取被服务端拒绝",
					"role_id", session.RoleID(),
					"result", result,
					"result_text", resultText,
				)
			}
			return nil
		},
	)
}

// NewOnlineGiftRoutine 构造在线礼包与功能终结礼包领取任务 (Feature 1: 领取奖励)。
//
// 协议基准 (原版客户端 Main.swf):
//   - 在线成长礼包 Mod_Player_Base:
//     get_player_current_online_gift -> Module 0, Action 31 (0x0000001F), 请求体为空
//     player_get_online_gift         -> Module 0, Action 30 (0x0000001E), 请求体为空
//   - 功能终结礼包 Mod_FunctionEnd (吉星高照/喜从天降/灵脉/拜关公/聚灵等玩法终结后的
//     每日补偿礼包):
//     game_function_end_gift       -> Module 127, Action 1 (0x007F0001), 请求体为空
//     get_game_function_end_gift   -> Module 127, Action 2 (0x007F0002), 请求体为 gift_id
//
// 状态机对齐官方客户端 ActivitiesView.onlineGiftActivity:
//   - update_player_data prop 82 (GET_ONLINE_GIFT): -1 无礼包 / 0 倒计时 / 1 可领取;
//   - update_player_data prop 81 (ONLINE_GIFT): 剩余秒数, 客户端换算为绝对时间戳;
//   - 客户端在 -1 时移除活动, 仅在 0/1 时展示「立即领取」。
//
// 领取结果码对齐 Mod_Player_Base: 97 ACTION_SUCCESS, 98 ALREADY_GET,
// 100 ACTION_FAILED (时间未到), 101 PACK_FULL。
//
// 功能终结礼包使用完全独立的 Module 127，与各玩法的活跃协议互不干扰；
// 在线礼包部分保持原有状态机与结果码分流不变。
func NewOnlineGiftRoutine(cfgs ...OnlineGiftConfig) scheduler.ActivityRoutine {
	cfg := DefaultOnlineGiftConfig()
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
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
			slog.Info("正在执行日常任务: 在线礼包与功能终结礼包领取", "role_id", session.RoleID())

			// 0. 功能终结礼包 (吉星高照/喜从天降/灵脉/拜关公/聚灵等)。
			// 独立模块，先于在线礼包状态检查执行，避免在线礼包未开放时漏领。
			if cfg.ClaimFunctionEndGifts {
				if err := claimFunctionEndGifts(ctx, session, cfg); err != nil {
					if errors.Is(err, scheduler.ErrBagFull) {
						return err
					}
					slog.Warn("功能终结礼包领取异常，继续尝试在线礼包",
						"role_id", session.RoleID(),
						"err", err,
					)
				}
			}

			// 1. 读取服务端下发的在线礼包状态 (update_player_data prop 82)。
			// 客户端 ActivitiesView.onlineGiftActivity: -1 时直接移除活动, 不查询也不领取。
			st := session.GetPlayerState()
			switch st.IsGetOnlineGift {
			case -1:
				slog.Info("在线礼包当前不可领取: 活动未开放或无可领礼包",
					"role_id", session.RoleID(),
				)
				return nil
			case -2:
				slog.Warn("在线礼包状态尚未同步, 跳过本轮",
					"role_id", session.RoleID(),
				)
				return nil
			}

			// 2. 查询下一份礼包预览 (诊断与日志)
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

			// 3. 倒计时未结束时客户端不会点击「立即领取」, 此处同样跳过。
			// prop 81 已按客户端语义换算为绝对时间戳。
			if st.IsGetOnlineGift == 0 {
				remaining := st.OnlineGiftTime - time.Now().Unix()
				if remaining > 0 {
					slog.Info("在线礼包倒计时中",
						"role_id", session.RoleID(),
						"remaining_seconds", remaining,
					)
					return nil
				}
			}

			// 4. 发起领取
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
			case protocol.PlayerAlreadyGet:
				slog.Info("在线礼包已领取过",
					"role_id", session.RoleID(),
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
				slog.Warn("在线礼包领取返回未知状态",
					"role_id", session.RoleID(),
					"result", claim.Result,
					"next_time_stamp", claim.NextTimeStamp,
					"payload", hex.EncodeToString(resp.Payload),
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
