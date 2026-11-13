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

// NewSignInRoutine 构造每日签到任务 (Module 89, Mod_SignIn_Base)。
//
// 官方流程：get_sign_in_status 确认活动开启 → get_sign_in_info 读取签到状态
// → player_sign_in 签到 → get_continue_award 领连签奖励 → get_total_award
// 领取累计签到档位奖励。全部请求均为空载荷，不消耗元宝。
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

			slog.Info("正在执行日常任务: 每日签到", "role_id", session.RoleID())

			// 2. 活动状态：未开启时官方客户端不展示签到入口，直接跳过。
			statusResp, err := callDaily(ctx, session, protocol.BuildSignInStatusPacket(), protocol.ActionIDSignInStatus, 8*time.Second)
			if err != nil {
				// 活动未开放时服务端会静默丢弃状态查询；按暂不可用跳过，由下次 Cron 重试。
				slog.Info("签到活动状态查询失败/未开放，稍后重试",
					"role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(10*time.Minute, "签到活动状态查询无回包")
			}
			status, err := protocol.ParseSignInStatusResult(statusResp.Payload)
			if err != nil {
				return fmt.Errorf("解析签到活动状态失败: %w", err)
			}
			if !status.ActivityOpen {
				slog.Info("签到活动当前未开放，稍后重试",
					"role_id", session.RoleID(),
					"has_award", status.HasAward,
					"raw", []uint8{status.RawA, status.RawB, status.RawC},
				)
				return scheduler.NewCooldown(time.Hour, "签到活动未开放")
			}

			// 3. 签到详情
			infoResp, err := callDaily(ctx, session, protocol.BuildSignInInfoPacket(), protocol.ActionIDSignInInfo, 8*time.Second)
			if err != nil {
				return fmt.Errorf("查询签到详情失败: %w", err)
			}
			info, err := protocol.ParseSignInInfoResult(infoResp.Payload)
			if err != nil {
				return fmt.Errorf("解析签到详情失败: %w", err)
			}
			slog.Info("签到活动详情",
				"role_id", session.RoleID(),
				"continue_state", info.ContinueState,
				"continue_day", info.ContinueDay,
				"total_day", info.TotalDay,
				"sign_state", info.SignState,
				"cur_day", info.CurDay,
				"repair_sign", info.RepairSign,
				"tiers", info.TotalInfo,
			)

			// 4. 执行签到
			if info.SignState != protocol.SignInStateAlreadySignIn {
				actResp, err := callDaily(ctx, session, protocol.BuildSignInPlayerPacket(), protocol.ActionIDSignInPlayer, 8*time.Second)
				if err != nil {
					return fmt.Errorf("每日签到请求失败: %w", err)
				}
				act, err := protocol.ParseSignInActionResult(actResp.Payload)
				if err != nil {
					return fmt.Errorf("解析每日签到回包失败: %w", err)
				}
				switch act.Result {
				case protocol.SignInResultOK:
					slog.Info("每日签到成功", "role_id", session.RoleID(), "total_day", act.TotalDay)
				case protocol.SignInStateAlreadySignIn:
					slog.Info("今日已签到", "role_id", session.RoleID(), "total_day", act.TotalDay)
				case protocol.SignInResultFail:
					slog.Warn("每日签到失败", "role_id", session.RoleID(), "result", act.Result)
				case protocol.SignInResultPackFull:
					return scheduler.ErrBagFull
				case protocol.SignInResultNoIngot:
					slog.Warn("补签元宝不足，跳过", "role_id", session.RoleID(), "result", act.Result)
				default:
					slog.Warn("每日签到返回未知结果", "role_id", session.RoleID(), "result", act.Result)
				}
			} else {
				slog.Info("今日已完成签到，无需重复执行", "role_id", session.RoleID(), "total_day", info.TotalDay)
			}

			// 5. 连签奖励 (state=0 才可领)
			if info.ContinueState == protocol.SignInStateCanGet {
				resp, err := callDaily(ctx, session, protocol.BuildSignInContinuePacket(), protocol.ActionIDSignInContinue, 8*time.Second)
				if err != nil {
					slog.Info("领取连签奖励无应答", "role_id", session.RoleID(), "err", err)
				} else if result, day, pErr := protocol.ParseSignInContinueResult(resp.Payload); pErr != nil {
					slog.Warn("解析连签奖励回包失败", "role_id", session.RoleID(), "err", pErr)
				} else if result == protocol.SignInResultSuccess {
					slog.Info("连签奖励领取成功", "role_id", session.RoleID(), "continue_day", day)
				} else {
					slog.Info("连签奖励未领取", "role_id", session.RoleID(), "result", result, "continue_day", day)
				}
			}

			// 6. 累计签到档位奖励 (state=0 才可领)
			for _, tier := range info.TotalInfo {
				if tier.State != protocol.SignInStateCanGet {
					continue
				}
				resp, err := callDaily(ctx, session, protocol.BuildSignInTotalAwardPacket(tier.Tier), protocol.ActionIDSignInTotalAward, 8*time.Second)
				if err != nil {
					slog.Info("领取累计签到奖励无应答", "role_id", session.RoleID(), "tier", tier.Tier, "err", err)
					continue
				}
				act, pErr := protocol.ParseSignInActionResult(resp.Payload)
				if pErr != nil {
					slog.Warn("解析累计签到奖励回包失败", "role_id", session.RoleID(), "tier", tier.Tier, "err", pErr)
					continue
				}
				switch act.Result {
				case protocol.SignInResultSuccess:
					slog.Info("累计签到奖励领取成功", "role_id", session.RoleID(), "tier", tier.Tier)
				case protocol.SignInResultPackFull:
					return scheduler.ErrBagFull
				default:
					slog.Info("累计签到奖励未领取",
						"role_id", session.RoleID(), "tier", tier.Tier, "result", act.Result)
				}
			}

			return nil
		},
	)
}

// callDaily 发送一条日常协议请求并在超时内等待对应回包。
func callDaily(ctx context.Context, session *client.RoleSession, pkt *protocol.Packet, actionID uint32, timeout time.Duration) (*protocol.Packet, error) {
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return session.Call(callCtx, pkt, actionID)
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

			// 官方 Mod_Assistant_Base (Module 52):
			//   one_key_get_award(Act 3) 按页签类型一键领取所有达标宝箱。
			// 页签类型从 1 开始；不同版本页签数量不同，逐个尝试并在服务端返回
			// NOT_ENOUGH_NUM / HAVE_DONE 时继续下一个类型。
			claimed := 0
			for typeID := int32(1); typeID <= 3; typeID++ {
				if err := ctx.Err(); err != nil {
					return nil
				}
				pkt := protocol.BuildAssistantOneKeyGetAwardPacket(typeID)
				cCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
				resp, err := session.Call(cCtx, pkt, protocol.ActionIDAssistantOneKeyGetAward)
				cancel()
				if err != nil || resp == nil {
					if typeID == 1 {
						slog.Info("小助手功能未开放/无回包，跳过", "role_id", session.RoleID(), "err", err)
						return nil
					}
					continue
				}
				result, awards, pErr := protocol.ParseAssistantOneKeyResult(resp.Payload)
				if pErr != nil {
					slog.Warn("解析小助手一键领取回包失败", "role_id", session.RoleID(), "err", pErr)
					return nil
				}
				switch result {
				case protocol.AssistantResultSuccess:
					claimed++
					slog.Info("小助手日活跃奖励领取成功",
						"role_id", session.RoleID(), "type", typeID, "awards", awards)
				case protocol.AssistantResultHaveDone,
					protocol.AssistantResultNotEnoughNum,
					protocol.AssistantResultNotEnoughVIP:
					// 该页签已领取或未达标，继续下一个页签。
				case protocol.AssistantResultPackFull:
					return scheduler.ErrBagFull
				case protocol.AssistantResultFailed:
					slog.Info("小助手一键领取失败", "role_id", session.RoleID(), "type", typeID)
					return nil
				default:
					slog.Info("小助手一键领取未成功", "role_id", session.RoleID(), "type", typeID, "result", result)
				}
			}
			slog.Info("小助手日活跃奖励巡检完成", "role_id", session.RoleID(), "claimed_types", claimed)
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
			infoCtx, infoCancel := context.WithTimeout(ctx, 8*time.Second)
			infoResp, infoErr := session.Call(infoCtx, infoPkt, protocol.ActionIDEmailGetInfo)
			infoCancel()
			if infoErr != nil || infoResp == nil || len(infoResp.Payload) == 0 {
				slog.Info("邮箱功能未开放/不可用，跳过", "role_id", session.RoleID(), "err", infoErr)
				return nil
			}
			prevAct = protocol.ActionIDEmailGetInfo
			slog.Info("邮箱面板已同步", "role_id", session.RoleID())

			// 4. 一键领取全部邮件附件 (Mod 345, Act 6)
			collectPkt := protocol.BuildEmailOneKeyGetAwardPacket(prevAct)
			callCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
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

			// 0.5 系统/活动奖励 (Module 2/6 列表 -> 2/29 逐项领取)。
			// 与 712 在线礼包状态无关，必须在状态检查前执行，否则活动未开放时整批漏领。
			if cfg.ClaimSystemAwards {
				if err := claimSystemAwards(ctx, session); err != nil {
					if errors.Is(err, scheduler.ErrBagFull) {
						return err
					}
					slog.Warn("系统/活动奖励领取异常，继续尝试在线礼包",
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
				slog.Info("在线礼包当前不可领取: 活动未开放或无可领礼包，稍后重试",
					"role_id", session.RoleID(),
				)
				return scheduler.NewCooldown(2*time.Hour, "在线礼包活动未开放")
			case -2:
				slog.Warn("在线礼包状态尚未同步, 稍后重试",
					"role_id", session.RoleID(),
				)
				return scheduler.NewCooldown(10*time.Minute, "在线礼包状态未同步")
			}

			// 2. 查询下一份礼包预览 (诊断与日志)
			qCtx, qCancel := context.WithTimeout(ctx, 8*time.Second)
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
			cCtx, cCancel := context.WithTimeout(ctx, 8*time.Second)
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
				OpenGiftPacks(ctx, session, giftPackRepo)
				slog.Info("在线礼包领取成功",
					"role_id", session.RoleID(),
					"gift_count", len(claim.Gifts),
					"gifts", claim.Gifts,
					"next_time_stamp", claim.NextTimeStamp,
				)
			case protocol.PlayerAlreadyGet:
				OpenGiftPacks(ctx, session, giftPackRepo)
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

// godWelfareMaxPlayerLevel 天神赐福等级奖励可参与的最高角色等级。
//
// 对齐官方 GodWelfareView.get_god_welfare_info_callback:
// `_ctrl.player.level <= 260 && _ctrl.godWelfare.isGeted == false` 才显示入口。
const godWelfareMaxPlayerLevel = 260

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
			slog.Info("正在执行日常任务: 天神赐福等级奖励领取", "role_id", session.RoleID())

			call := func(pkt *protocol.Packet, act uint32) (*protocol.Packet, error) {
				cCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
				defer cancel()
				return session.Call(cCtx, pkt, act)
			}

			// 1. 查询赐福状态 (Mod 328, Act 0)
			infoResp, err := call(protocol.BuildGodWelfareInfoPacket(), protocol.ActionIDGodWelfareInfo)
			if err != nil || infoResp == nil {
				slog.Info("天神赐福功能未开放/无回包，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			info, pErr := protocol.ParseGodWelfareInfoResult(infoResp.Payload)
			if pErr != nil {
				slog.Warn("解析天神赐福信息失败，跳过", "role_id", session.RoleID(), "err", pErr)
				return nil
			}
			slog.Info("天神赐福状态",
				"role_id", session.RoleID(),
				"level", info.Level,
				"is_geted", info.IsGeted,
				"point", info.Point,
				"max_point", info.MaxPoint,
				"remain_seconds", info.RemainSeconds,
			)

			// 官方 GodWelfareView 仅在 player.level <= 260 且 isGeted=false 时才挂出
			// 活动入口；超出等级或今日赐福已领取时等级奖励不可领，服务端返回
			// FAILURE(6)。提前跳过，避免每轮都发一次必然失败的领取请求。
			pState := session.GetPlayerState()
			if info.IsGeted == 1 || (pState.Level > 0 && pState.Level > godWelfareMaxPlayerLevel) {
				slog.Info("天神赐福当前不可参与，跳过",
					"role_id", session.RoleID(),
					"player_level", pState.Level,
					"is_geted", info.IsGeted,
					"max_player_level", godWelfareMaxPlayerLevel,
				)
				return nil
			}

			// 2. 查询等级奖励列表 (Mod 328, Act 1)
			listResp, err := call(protocol.BuildGodWelfareLevelAwardInfoPacket(), protocol.ActionIDGodWelfareLevelAwardInfo)
			if err != nil || listResp == nil {
				slog.Info("查询天神赐福等级奖励无回包", "role_id", session.RoleID(), "err", err)
				return nil
			}
			awards, pErr := protocol.ParseGodWelfareLevelAwardInfoResult(listResp.Payload)
			if pErr != nil {
				slog.Warn("解析天神赐福等级奖励失败", "role_id", session.RoleID(), "err", pErr)
				return nil
			}

			claimed := 0
			for _, award := range awards {
				if award.IsGet != 0 {
					continue
				}
				if err := ctx.Err(); err != nil {
					return nil
				}
				cResp, cErr := call(protocol.BuildGodWelfareGetAwardPacket(award.Level), protocol.ActionIDGodWelfareGetAward)
				if cErr != nil || cResp == nil {
					slog.Info("领取天神赐福等级奖励无回包", "role_id", session.RoleID(), "level", award.Level, "err", cErr)
					return nil
				}
				code, cpErr := protocol.ParseGodWelfareGetAwardResult(cResp.Payload)
				if cpErr != nil {
					slog.Warn("解析天神赐福领取回包失败", "role_id", session.RoleID(), "err", cpErr)
					return nil
				}
				switch code {
				case protocol.GodWelfareResultSuccess:
					claimed++
					slog.Info("天神赐福等级奖励领取成功", "role_id", session.RoleID(), "level", award.Level)
				case protocol.GodWelfareResultHadGet:
					slog.Debug("天神赐福等级奖励已领取", "role_id", session.RoleID(), "level", award.Level)
				case protocol.GodWelfareResultLevelLimit:
					slog.Info("天神赐福等级不足，停止领取", "role_id", session.RoleID(), "level", award.Level)
					return nil
				case protocol.GodWelfareResultItemPackFull:
					return scheduler.ErrBagFull
				case protocol.GodWelfareResultFailure:
					// 同一活动状态下各等级奖励会一致失败（例如活动已结束或
					// 当日整体奖励已领完），继续逐个尝试只会放大无效请求。
					slog.Info("天神赐福等级奖励领取失败，停止本轮",
						"role_id", session.RoleID(), "level", award.Level, "result", code)
					return nil
				default:
					slog.Info("天神赐福等级奖励未领取", "role_id", session.RoleID(), "level", award.Level, "result", code)
					continue
				}
			}
			slog.Info("天神赐福巡检完成", "role_id", session.RoleID(), "claimed", claimed)
			return nil
		},
	)
}

// NewVIPWelfareRoutine 构造VIP每日免费礼包领取任务 (Module 18, Mod_Vip_Base)。
//
// 只领取 send_gift_is_get=0 且 vip_level <= 当前VIP等级的免费礼包；
// 付费礼包 buy_send_gift 会消耗元宝，本任务永不调用。
func NewVIPWelfareRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"vip_welfare",
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
			slog.Info("正在执行日常任务: VIP每日免费礼包领取", "role_id", session.RoleID())

			infoResp, err := callDaily(ctx, session, protocol.BuildVIPInfoPacket(), protocol.ActionIDVIPInfo, 8*time.Second)
			if err != nil {
				return fmt.Errorf("查询VIP信息失败: %w", err)
			}
			info, err := protocol.ParseVIPInfoResult(infoResp.Payload)
			if err != nil {
				return fmt.Errorf("解析VIP信息失败: %w", err)
			}
			slog.Info("VIP信息",
				"role_id", session.RoleID(),
				"vip_level", info.VIPLevel,
				"money_to_next_level", info.MoneyToNextLevel,
				"gift_levels", len(info.Gifts),
			)

			claimed := 0
			for _, gift := range info.Gifts {
				if int32(gift.Level) > info.VIPLevel || !gift.SendGiftClaimable() {
					continue
				}
				resp, cErr := callDaily(ctx, session, protocol.BuildVIPSendGiftPacket(gift.Level), protocol.ActionIDVIPSendGift, 8*time.Second)
				if cErr != nil {
					slog.Info("VIP礼包领取无应答", "role_id", session.RoleID(), "level", gift.Level, "err", cErr)
					continue
				}
				result, pErr := protocol.ParseVIPGiftResult(resp.Payload)
				if pErr != nil {
					slog.Warn("解析VIP礼包回包失败", "role_id", session.RoleID(), "level", gift.Level, "err", pErr)
					continue
				}
				switch result {
				case protocol.VIPResultSuccess:
					claimed++
					slog.Info("VIP免费礼包领取成功", "role_id", session.RoleID(), "level", gift.Level)
				case protocol.VIPResultPackFull:
					return scheduler.ErrBagFull
				case protocol.VIPResultNotEnoughLevel:
					slog.Debug("VIP等级不足，跳过该档礼包", "role_id", session.RoleID(), "level", gift.Level)
				case protocol.VIPResultNoIngot:
					slog.Warn("VIP礼包返回元宝不足 (免费礼包不应出现)", "role_id", session.RoleID(), "level", gift.Level)
				default:
					slog.Info("VIP免费礼包未领取",
						"role_id", session.RoleID(), "level", gift.Level, "result", result)
				}
			}
			slog.Info("VIP每日礼包领取完成", "role_id", session.RoleID(), "claimed", claimed)
			return nil
		},
	)
}

// NewOnlineRewardRoutine 构造在线奖励一键领取任务 (Module 712, Mod_OnlineReward_Base)。
//
// 官方客户端先查询 main_panel，再按当日在线时长解锁各档奖励并一键领取；
// 领取本身不消耗元宝。最高档实机配置为元宝 ×100。
func NewOnlineRewardRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"online_reward",
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

			panelResp, err := callDaily(ctx, session, protocol.BuildOnlineRewardPanelPacket(), protocol.ActionIDOnlineRewardPanel, 8*time.Second)
			if err != nil {
				// 活动未开放时服务端会静默丢弃面板请求；按“暂不可用”跳过，由 Loop 下次重试。
				slog.Info("在线奖励活动未开放或面板请求被服务端丢弃，跳过",
					"role_id", session.RoleID(), "err", err)
				return nil
			}
			panel, err := protocol.ParseOnlineRewardPanelResult(panelResp.Payload)
			if err != nil {
				return fmt.Errorf("解析在线奖励面板失败: %w", err)
			}
			// 官方 showBtnBack 仅在服务端时间处于 [start_time, end_time] 内展示入口。
			now := time.Now().Unix()
			if panel.StartTime == 0 || panel.EndTime == 0 ||
				now < int64(panel.StartTime) || now > int64(panel.EndTime) {
				slog.Info("Module 712 在线奖励已结束，回退节日活动 218 在线奖励",
					"role_id", session.RoleID(),
					"start_time", panel.StartTime,
					"end_time", panel.EndTime,
					"now", now,
				)
				return claimFestivalOnlineReward(ctx, session)
			}

			claimable := make([]int32, 0, len(panel.Tiers))
			for _, tier := range panel.Tiers {
				if tier.IsGet == 0 && panel.OnlineTime >= tier.NeedOnlineTime {
					claimable = append(claimable, tier.AwardID)
				}
			}
			if len(claimable) == 0 {
				var next int32 = -1
				for _, tier := range panel.Tiers {
					if tier.IsGet == 0 && (next < 0 || tier.NeedOnlineTime < next) {
						next = tier.NeedOnlineTime
					}
				}
				slog.Info("在线奖励暂无可领取档位",
					"role_id", session.RoleID(),
					"online_time", panel.OnlineTime,
					"next_need", next,
				)
				return nil
			}

			resp, err := callDaily(ctx, session, protocol.BuildOnlineRewardOneKeyPacket(), protocol.ActionIDOnlineRewardOneKey, 8*time.Second)
			if err != nil {
				return fmt.Errorf("在线奖励一键领取失败: %w", err)
			}
			result, awards, err := protocol.ParseOnlineRewardOneKeyResult(resp.Payload)
			if err != nil {
				return fmt.Errorf("解析在线奖励领取回包失败: %w", err)
			}
			switch result {
			case protocol.OnlineRewardResultSuccess:
				OpenGiftPacks(ctx, session, giftPackRepo)
				slog.Info("在线奖励领取成功",
					"role_id", session.RoleID(),
					"award_ids", awards,
					"online_time", panel.OnlineTime,
				)
			case protocol.OnlineRewardResultPackFull:
				return scheduler.ErrBagFull
			case protocol.OnlineRewardResultFailed:
				slog.Info("在线奖励领取失败", "role_id", session.RoleID(), "result", result)
			default:
				slog.Warn("在线奖励领取返回未知结果", "role_id", session.RoleID(), "result", result)
			}
			return nil
		},
	)
}

// claimFestivalOnlineReward 走节日活动 218 的在线时长奖励 (秋季嘉年华等复用)。
func claimFestivalOnlineReward(ctx context.Context, session *client.RoleSession) error {
	infoResp, err := callDaily(ctx, session, protocol.BuildFestivalOnlineInfoPacket(), protocol.ActionIDFestivalOnlineInfo, 8*time.Second)
	if err != nil {
		slog.Info("节日在线奖励面板不可用，跳过", "role_id", session.RoleID(), "err", err)
		return nil
	}
	indexes, err := protocol.ParseFestivalOnlineTierIndexes(infoResp.Payload)
	if err != nil {
		return fmt.Errorf("解析节日在线奖励面板失败: %w", err)
	}
	if len(indexes) == 0 {
		slog.Info("节日在线奖励暂无可领取档位", "role_id", session.RoleID())
		return nil
	}
	claimed := 0
	for _, idx := range indexes {
		resp, cErr := callDaily(ctx, session, protocol.BuildFestivalOnlineAwardPacket(idx), protocol.ActionIDFestivalOnlineAward, 8*time.Second)
		if cErr != nil {
			slog.Info("节日在线奖励领取无回包", "role_id", session.RoleID(), "index", idx, "err", cErr)
			return nil
		}
		if len(resp.Payload) == 0 {
			return nil
		}
		code := resp.Payload[0]
		switch code {
		case protocol.FestivalOnlineSuccess:
			claimed++
			slog.Info("节日在线奖励领取成功", "role_id", session.RoleID(), "index", idx)
		case protocol.FestivalOnlineHadGet:
			continue
		case protocol.FestivalOnlinePackFull:
			return scheduler.ErrBagFull
		case protocol.FestivalOnlineNonActivityTime, protocol.FestivalOnlineTimesLimit,
			protocol.FestivalOnlineFunctionNotOpen:
			slog.Info("节日在线奖励当前不可领取", "role_id", session.RoleID(),
				"index", idx, "result", code, "result_text", protocol.FestivalOnlineResultText(code))
			return nil
		default:
			slog.Info("节日在线奖励领取未成功", "role_id", session.RoleID(),
				"index", idx, "result", code, "result_text", protocol.FestivalOnlineResultText(code))
			return nil
		}
	}
	slog.Info("节日在线奖励巡检完成", "role_id", session.RoleID(), "claimed", claimed)
	return nil
}

// NewDeviceBindRewardRoutine 构造设备/手机绑定奖励领取任务 (Module 814)。
//
// 仅当账号已完成手机绑定与验证时，领取首绑奖励 (200 元宝) 与月度奖励 (50 元宝)；
// 不代用户发送验证码、提交手机号或执行绑定/解绑操作。
func NewDeviceBindRewardRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"device_bind_reward",
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

			infoResp, err := callDaily(ctx, session, protocol.BuildDeviceBindInfoPacket(), protocol.ActionIDDeviceBindInfo, 8*time.Second)
			if err != nil {
				return fmt.Errorf("查询设备绑定状态失败: %w", err)
			}
			info, err := protocol.ParseDeviceBindInfoResult(infoResp.Payload)
			if err != nil {
				return fmt.Errorf("解析设备绑定状态失败: %w", err)
			}
			slog.Info("设备绑定状态",
				"role_id", session.RoleID(),
				"result", info.Result,
				"is_bind", info.IsBind,
				"need_verify_bind", info.NeedVerifyBind,
				"first_reward", info.FirstReward,
				"monthly_reward", info.MonthlyReward,
			)

			if info.Result != protocol.DeviceBindResultSuccess {
				slog.Info("设备绑定功能不可用，跳过", "role_id", session.RoleID(), "result", info.Result)
				return nil
			}
			if info.IsBind == 0 || info.NeedVerifyBind == 1 {
				slog.Info("账号未完成手机绑定/验证，跳过 (需用户手动绑定)",
					"role_id", session.RoleID(),
					"is_bind", info.IsBind,
					"need_verify_bind", info.NeedVerifyBind,
				)
				return nil
			}

			claim := func(pkt *protocol.Packet, actionID uint32, name string) error {
				resp, cErr := callDaily(ctx, session, pkt, actionID, 8*time.Second)
				if cErr != nil {
					slog.Info(name+"无应答", "role_id", session.RoleID(), "err", cErr)
					return nil
				}
				result, pErr := protocol.ParseDeviceBindRewardResult(resp.Payload)
				if pErr != nil {
					slog.Warn("解析"+name+"回包失败", "role_id", session.RoleID(), "err", pErr)
					return nil
				}
				switch result {
				case protocol.DeviceBindResultSuccess:
					slog.Info(name+"领取成功", "role_id", session.RoleID())
				case protocol.DeviceBindResultAlreadyGet:
					slog.Info(name+"已领取", "role_id", session.RoleID())
				case protocol.DeviceBindResultPackFull:
					return scheduler.ErrBagFull
				case protocol.DeviceBindResultNotVerifyAndBind, protocol.DeviceBindResultNotBound:
					slog.Info(name+"未满足绑定条件", "role_id", session.RoleID(), "result", result)
				default:
					slog.Info(name+"未领取", "role_id", session.RoleID(), "result", result)
				}
				return nil
			}

			if info.FirstReward == 0 {
				if err := claim(protocol.BuildDeviceBindFirstRewardPacket(), protocol.ActionIDDeviceBindFirstReward, "设备首绑奖励"); err != nil {
					return err
				}
			}
			if info.MonthlyReward == 0 {
				if err := claim(protocol.BuildDeviceBindMonthlyRewardPacket(), protocol.ActionIDDeviceBindMonthlyReward, "设备月度奖励"); err != nil {
					return err
				}
			}
			return nil
		},
	)
}
