// Package minigame 提供吉星高照、仙旅奇缘答题、喜从天降、刮刮卡、钓鱼等益智趣味活动。
//
// @author Ateng
// @since 2026-10-08
package minigame

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/qa"
	"sxd-pie-ng/internal/scheduler"
)

// maxLuckyStarRolls 单次调度的最大投掷轮数，防止服务端异常未扣减次数时死循环。
const maxLuckyStarRolls = 10

// NewFactionLuckyStarRoutine 构造帮派吉星高照扔骰子任务。
//
// 协议对齐 2026 客户端 Mod_Faction_Base:
//   - Action 45 faction_roll_cake_info 查询战绩与剩余次数
//   - Action 44 faction_roll_cake_remain_times 查询剩余次数 (信息查询失败时兜底)
//   - Action 46 roll_cake 投掷骰子
//
// 个人吉星高照见 NewLuckyStarRoutine (Mod_RollCake_Base, module 38)。
func NewFactionLuckyStarRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"faction_lucky_star",
		"minigame",
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
			if session.IsFactionFeatureUnavailable() {
				slog.Info("帮派模块对当前角色不可用，跳过帮派吉星高照", "role_id", session.RoleID())
				return nil
			}
			slog.Info("正在执行益智任务: 帮派吉星高照摇骰子", "role_id", session.RoleID())

			// 1. 先查询剩余投掷次数 (Mod 10, Act 44)。
			// 该动作在未加入帮派的角色上依然正常回包（实测 s0 返回 0），
			// 是唯一可用于提前判断“今日还有没有次数”的轻量状态查询；
			// 战绩面板 (Act 45) 会携带帮派成员列表，无帮派角色上会被服务端静默丢弃，
			// 因此不能把面板查询放在最前面。
			remainPkt := protocol.BuildFactionRollCakeRemainTimesPacket(protocol.ActionIDTownEnter)
			rCtx, rCancel := context.WithTimeout(ctx, 3*time.Second)
			remainResp, remainErr := session.Call(rCtx, remainPkt, protocol.ActionIDFactionRollCakeRemainTimes)
			rCancel()

			var remain int8
			remainKnown := false
			if remainErr == nil && remainResp != nil {
				if parsed, err := protocol.ParseFactionRollCakeRemainTimesResult(remainResp.Payload); err != nil {
					slog.Warn("解析吉星高照剩余次数回包失败，尝试查询战绩面板", "role_id", session.RoleID(), "err", err)
				} else {
					remain = parsed
					remainKnown = true
				}
			} else {
				slog.Warn("查询吉星高照剩余次数失败，尝试查询战绩面板", "role_id", session.RoleID(), "err", remainErr)
			}

			if remainKnown && remain <= 0 {
				slog.Info("今日帮派吉星高照投掷次数已用尽", "role_id", session.RoleID())
				return nil
			}

			// 2. 查询战绩面板 (Mod 10, Act 45)：仅用于日志与 44 失败时的兜底。
			prevAct := protocol.ActionIDFactionRollCakeRemainTimes
			infoPkt := protocol.BuildFactionRollCakeInfoPacket(protocol.ActionIDTownEnter)
			infoCtx, infoCancel := context.WithTimeout(ctx, 3*time.Second)
			infoResp, infoErr := session.Call(infoCtx, infoPkt, protocol.ActionIDFactionRollCakeInfo)
			infoCancel()
			if infoErr == nil && infoResp != nil {
				if info, err := protocol.ParseFactionRollCakeInfoResult(infoResp.Payload); err != nil {
					slog.Warn("解析吉星高照战绩回包失败", "role_id", session.RoleID(), "err", err)
				} else {
					slog.Info("吉星高照当前战绩",
						"role_id", session.RoleID(),
						"week_best_score", info.WeekBestScore,
						"today_best_score", info.TodayBestScore,
						"remain_times", info.RemainTimes,
						"faction_score", info.FactionScore,
						"member_count", len(info.Members),
					)
					if !remainKnown {
						remain = info.RemainTimes
						remainKnown = true
						prevAct = protocol.ActionIDFactionRollCakeInfo
					}
				}
			} else {
				slog.Warn("查询吉星高照战绩失败", "role_id", session.RoleID(), "err", infoErr)
			}

			if !remainKnown {
				if errors.Is(remainErr, context.DeadlineExceeded) || errors.Is(infoErr, context.DeadlineExceeded) {
					// 两个只读动作都没有任何回包：判定帮派模块不可用并熔断后续帮派任务。
					session.MarkFactionFeatureUnavailable()
					slog.Info("帮派吉星高照剩余次数与战绩面板均无任何回包，判定帮派模块不可用（角色可能未加入帮派），跳过本轮帮派任务",
						"role_id", session.RoleID())
				} else {
					slog.Warn("无法确定帮派吉星高照剩余次数，跳过本轮", "role_id", session.RoleID())
				}
				return nil
			}

			if remain <= 0 {
				slog.Info("今日帮派吉星高照投掷次数已用尽", "role_id", session.RoleID())
				return nil
			}

			// 3. 循环投掷直至次数用尽 (Mod 10, Act 46)
			rolls := 0
			for remain > 0 && rolls < maxLuckyStarRolls {
				if err := ctx.Err(); err != nil {
					slog.Info("吉星高照投掷已取消，提前退出", "role_id", session.RoleID(), "err", err)
					return nil
				}

				rollPkt := protocol.BuildFactionRollCakePacket(prevAct)
				rollCtx, rollCancel := context.WithTimeout(ctx, 3*time.Second)
				rollResp, err := session.Call(rollCtx, rollPkt, protocol.ActionIDFactionRollCake)
				rollCancel()
				if err != nil || rollResp == nil {
					slog.Warn("吉星高照投掷未收到回包", "role_id", session.RoleID(), "err", err)
					return nil
				}

				res, err := protocol.ParseFactionRollCakeResult(rollResp.Payload)
				if err != nil {
					slog.Warn("解析吉星高照投掷回包失败", "role_id", session.RoleID(), "err", err)
					return nil
				}

				if res.Msg != protocol.RollCakeResultSuccess {
					switch res.Msg {
					case protocol.RollCakeResultNoTimes:
						slog.Info("今日吉星高照投掷次数已用尽", "role_id", session.RoleID())
					case protocol.RollCakeResultAwarding:
						slog.Info("吉星高照正在结算排名奖励，稍后再试", "role_id", session.RoleID())
					default:
						slog.Warn("吉星高照投掷被服务端拒绝", "role_id", session.RoleID(), "result", res.Msg)
					}
					break
				}

				rolls++
				slog.Info("吉星高照投掷成功",
					"role_id", session.RoleID(),
					"roll", rolls,
					"reward_type", res.Type,
					"score", res.Score,
					"today_score", res.TodayScore,
					"dice", res.Dice,
					"remain_times", res.RemainTimes,
				)

				// 熔断保护: 服务端未扣减次数时立即退出，避免死循环。
				if res.RemainTimes >= remain {
					slog.Warn("吉星高照投掷次数未扣减，熔断退出",
						"role_id", session.RoleID(),
						"before_times", remain,
						"after_times", res.RemainTimes,
					)
					remain = 0
					break
				}
				remain = res.RemainTimes
				prevAct = protocol.ActionIDFactionRollCake

				if remain > 0 && jitter != nil {
					_ = jitter.Wait(ctx)
				}
			}

			slog.Info("吉星高照摇骰子完成", "role_id", session.RoleID(), "rolls", rolls, "remain_times", remain)
			return nil
		},
	)
}

// NewTravelEventRoutine 构造仙旅奇缘事件自动抉择任务。
//
// 协议对齐 2026 官方微端 Mod_TravelEvent_Base (Module 21, 仙旅奇缘):
//   - Action 0 get_activity_info 查询今日已参与次数/总次数与冷却
//   - Action 1 get_event_and_answer 拉取随机事件与两个分支选项
//   - Action 2 answer_travel_event 提交分支答案并结算奖励
func NewTravelEventRoutine(qaEngine qa.Engine) scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"travel_event",
		"minigame",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}

			// 1. 拟人防封随机抖动
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			slog.Info("正在执行益智任务: 仙旅奇缘事件抉择", "role_id", session.RoleID())

			// 2. 发送查询奇缘状态请求
			infoPkt, err := protocol.BuildFateGetInfoPacket(protocol.FateGetInfoRequest{
				PrevAct: protocol.ActionIDTownEnter,
			})
			if err != nil {
				return fmt.Errorf("构造仙旅奇缘状态查询封包失败: %w", err)
			}

			infoCtx, infoCancel := context.WithTimeout(ctx, 3*time.Second)
			defer infoCancel()

			respInfoPkt, err := session.Call(infoCtx, infoPkt, protocol.ActionIDFateGetInfo)
			if err != nil {
				return fmt.Errorf("查询仙旅奇缘状态失败: %w", err)
			}

			infoRes, err := protocol.ParseFateGetInfoResult(respInfoPkt.Payload)
			if err != nil {
				return fmt.Errorf("解析仙旅奇缘状态回包失败: %w", err)
			}

			remaining := infoRes.RemainingTimes()
			slog.Info("仙旅奇缘今日进度",
				"role_id", session.RoleID(),
				"join_count", infoRes.JoinCount,
				"total_count", infoRes.TotalCount,
				"remaining", remaining,
				"cd_timer", infoRes.CdTimer,
			)
			if remaining <= 0 {
				slog.Info("今日仙旅奇缘次数已耗尽", "role_id", session.RoleID())
				return nil
			}
			if infoRes.CdTimer > 0 {
				slog.Info("仙旅奇缘事件冷却中，本轮跳过", "role_id", session.RoleID(), "cd_timer", infoRes.CdTimer)
				return nil
			}

			rounds := int(remaining)
			for i := 0; i < rounds; i++ {
				if ctx.Err() != nil {
					slog.Info("仙旅奇缘已取消，提前退出", "role_id", session.RoleID())
					return nil
				}

				// 3. 发送拉取当前奇缘事件题目请求
				questionPkt, err := protocol.BuildFateQuestionPacket(protocol.FateQuestionRequest{
					PrevAct: protocol.ActionIDFateGetInfo,
				})
				if err != nil {
					return fmt.Errorf("构造仙旅奇缘题目查询封包失败: %w", err)
				}

				qCtx, qCancel := context.WithTimeout(ctx, 3*time.Second)
				respQPkt, err := session.Call(qCtx, questionPkt, protocol.ActionIDFateGetQuestion)
				qCancel()
				if err != nil {
					return fmt.Errorf("拉取仙旅奇缘题目失败: %w", err)
				}

				qRes, err := protocol.ParseFateQuestionResult(respQPkt.Payload)
				if err != nil {
					return fmt.Errorf("解析仙旅奇缘题目回包失败: %w", err)
				}
				if qRes.Msg != protocol.FateResultSuccess {
					switch qRes.Msg {
					case protocol.FateResultCDTime:
						slog.Info("仙旅奇缘事件冷却中，本轮跳过", "role_id", session.RoleID())
					case protocol.FateResultReachMaxCount:
						slog.Info("今日仙旅奇缘次数已耗尽", "role_id", session.RoleID())
					default:
						slog.Warn("仙旅奇缘事件拉取被服务端拒绝", "role_id", session.RoleID(), "result", qRes.Msg)
					}
					return nil
				}
				if len(qRes.Options) == 0 {
					slog.Warn("仙旅奇缘事件未下发任何分支选项", "role_id", session.RoleID(), "event_id", qRes.EventID)
					return nil
				}

				// 4. 若挂载题库引擎，执行收益权重决策
				chosen := qRes.Options[0]
				bestBranch := 1
				if qaEngine != nil {
					branch, entry, found := qaEngine.MatchFate(qRes.Story)
					if found && entry != nil {
						bestBranch = branch
						_, reason := qa.DecideBestBranch(entry)
						slog.Info("仙旅奇缘题库检索与决策命中",
							"story", qRes.Story,
							"best_branch", bestBranch,
							"decision_reason", reason,
						)
					}
				}
				if bestBranch == 2 && len(qRes.Options) >= 2 {
					chosen = qRes.Options[1]
				}

				// 5. 提交答案选项请求 (线上仅接受 8 字节 eventId + answerId)
				answerPkt, err := protocol.BuildFateAnswerPacket(protocol.FateAnswerRequest{
					EventID:  qRes.EventID,
					AnswerID: chosen.AnswerID,
				})
				if err != nil {
					return fmt.Errorf("构造仙旅奇缘提交答案封包失败: %w", err)
				}

				ansCtx, ansCancel := context.WithTimeout(ctx, 5*time.Second)
				ansRespPkt, err := session.Call(ansCtx, answerPkt, protocol.ActionIDFateAnswer)
				ansCancel()
				if err != nil {
					return fmt.Errorf("提交仙旅奇缘答案失败: %w", err)
				}

				ansRes, err := protocol.ParseFateAnswerResult(ansRespPkt.Payload)
				if err != nil {
					return fmt.Errorf("解析仙旅奇缘结算回包失败: %w", err)
				}
				slog.Info("仙旅奇缘事件抉择完成",
					"role_id", session.RoleID(),
					"event_id", qRes.EventID,
					"answer_id", chosen.AnswerID,
					"answer_sign", chosen.Sign,
					"result_message", ansRes.Message,
					"awards", formatFateAwards(ansRes.Awards),
					"join_count", ansRes.Info.JoinCount,
					"total_count", ansRes.Info.TotalCount,
				)

				// 熔断保护: 次数未推进或进入冷却时立即退出，避免无意义空转。
				if ansRes.Info.JoinCount <= infoRes.JoinCount {
					slog.Warn("仙旅奇缘参与次数未推进，熔断退出",
						"role_id", session.RoleID(),
						"before_join_count", infoRes.JoinCount,
						"after_join_count", ansRes.Info.JoinCount,
					)
					return nil
				}
				infoRes = &ansRes.Info
				if infoRes.CdTimer > 0 {
					slog.Info("仙旅奇缘事件进入冷却，等待下个恢复窗口",
						"role_id", session.RoleID(),
						"cd_timer", infoRes.CdTimer,
					)
					return nil
				}
				if infoRes.RemainingTimes() <= 0 {
					break
				}
				if jitter != nil {
					_ = jitter.Wait(ctx)
				}
			}

			return nil
		},
	)
}

// formatFateAwards 将结算奖励明细格式化为便于排查的日志文本。
func formatFateAwards(awards []protocol.FateAward) string {
	if len(awards) == 0 {
		return "无"
	}
	typeNames := map[uint8]string{
		protocol.FateAwardCoin:  "铜钱",
		protocol.FateAwardExp:   "经验",
		protocol.FateAwardFame:  "声望",
		protocol.FateAwardPower: "体力",
		protocol.FateAwardSkill: "阅历",
	}
	parts := make([]string, 0, len(awards))
	for _, award := range awards {
		if award.Value == 0 {
			continue
		}
		name := typeNames[award.Type]
		if name == "" {
			name = fmt.Sprintf("类型%d", award.Type)
		}
		parts = append(parts, fmt.Sprintf("%s%+d", name, award.Value))
	}
	if len(parts) == 0 {
		return "无"
	}
	return strings.Join(parts, ",")
}

// NewPartnerGuessRoutine 构造伙伴猜猜看趣味活动。
// NewPartnerGuessRoutine 构造伙伴猜猜看任务 (Module 369, Mod_GuessPartner_Base)。
//
// 按剩余次数开局：先用完免费刮图次数，再提交候选中的第一个答案，
// 按 RIGHT/WRONG 分流。不消耗元宝。
func NewPartnerGuessRoutine() scheduler.ActivityRoutine {
	const callTimeout = 3 * time.Second
	return scheduler.NewBaseRoutine(
		"partner_guess",
		"minigame",
		scheduler.ScheduleCron,
		scheduler.PriorityLow,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			iCtx, iCancel := context.WithTimeout(ctx, callTimeout)
			infoResp, err := session.Call(iCtx, protocol.BuildGuessPartnerInfoPacket(), protocol.ActionIDGuessPartnerInfo)
			iCancel()
			if err != nil {
				slog.Info("伙伴猜猜看功能不可用，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			times, err := protocol.ParseGuessPartnerInfoResult(infoResp.Payload)
			if err != nil {
				return fmt.Errorf("解析伙伴猜猜看次数失败: %w", err)
			}
			if times <= 0 {
				slog.Info("伙伴猜猜看今日次数已用尽", "role_id", session.RoleID())
				return nil
			}
			if times > 5 { // 单轮上限，避免异常次数导致长时间运行
				times = 5
			}

			right, wrong := 0, 0
			for round := int32(0); round < times; round++ {
				sCtx, sCancel := context.WithTimeout(ctx, callTimeout)
				startResp, sErr := session.Call(sCtx, protocol.BuildGuessPartnerStartPacket(), protocol.ActionIDGuessPartnerStart)
				sCancel()
				if sErr != nil {
					slog.Warn("伙伴猜猜看开局无应答", "role_id", session.RoleID(), "err", sErr)
					break
				}
				rd, pErr := protocol.ParseGuessPartnerStartResult(startResp.Payload)
				if pErr != nil {
					slog.Warn("解析伙伴猜猜看开局回包失败", "role_id", session.RoleID(), "err", pErr)
					break
				}
				if rd.Result == protocol.GuessPartnerResultPackFull {
					return scheduler.ErrBagFull
				}
				if rd.Result != protocol.GuessPartnerResultSuccess || len(rd.RoleIDs) == 0 {
					slog.Info("伙伴猜猜看开局未成功", "role_id", session.RoleID(), "result", rd.Result)
					break
				}

				// 用完免费刮图次数
				remain := rd.RemainScrapeTimes
				for remain > 0 {
					scCtx, scCancel := context.WithTimeout(ctx, callTimeout)
					scResp, scErr := session.Call(scCtx, protocol.BuildGuessPartnerScrapePacket(), protocol.ActionIDGuessPartnerScrape)
					scCancel()
					if scErr != nil {
						break
					}
					_, next, e := protocol.ParseGuessPartnerScrapeResult(scResp.Payload)
					if e != nil {
						break
					}
					if next >= remain {
						break
					}
					remain = next
				}

				gCtx, gCancel := context.WithTimeout(ctx, callTimeout)
				gResp, gErr := session.Call(gCtx, protocol.BuildGuessPartnerGuessPacket(rd.RoleIDs[0]), protocol.ActionIDGuessPartnerGuess)
				gCancel()
				if gErr != nil {
					slog.Warn("伙伴猜猜看作答无应答", "role_id", session.RoleID(), "err", gErr)
					break
				}
				gr, gpErr := protocol.ParseGuessPartnerGuessResult(gResp.Payload)
				if gpErr != nil {
					slog.Warn("解析伙伴猜猜看作答回包失败", "role_id", session.RoleID(), "err", gpErr)
					break
				}
				switch gr.Result {
				case protocol.GuessPartnerResultRight:
					right++
					slog.Info("伙伴猜猜看猜对", "role_id", session.RoleID(), "role_id_guess", rd.RoleIDs[0],
						"item_id", gr.Award.ItemID, "count", gr.Award.Count)
					return nil // 猜对即完成本日轮次
				case protocol.GuessPartnerResultWrong:
					wrong++
					slog.Info("伙伴猜猜看猜错", "role_id", session.RoleID(), "role_id_guess", rd.RoleIDs[0])
					if gr.RemainGuessTimes <= 0 {
						return nil
					}
				case protocol.GuessPartnerResultPackFull:
					return scheduler.ErrBagFull
				default:
					slog.Info("伙伴猜猜看作答未成功", "role_id", session.RoleID(), "result", gr.Result)
					return nil
				}
			}
			slog.Info("伙伴猜猜看完成", "role_id", session.RoleID(), "right", right, "wrong", wrong)
			return nil
		},
	)
}

// NewFishingRoutine 构造自动钓鱼与命格整理任务。
// NewFishingRoutine 构造自动钓鱼任务 (Module 217, Mod_Fish_Base)。
//
// 先查询鱼钩数量，再逐个下钩直到鱼钩耗尽或背包/命格位不足；
// 只使用免费鱼钩，不消耗元宝。
func NewFishingRoutine() scheduler.ActivityRoutine {
	const callTimeout = 3 * time.Second
	const fishInterval = 200 * time.Millisecond
	return scheduler.NewBaseRoutine(
		"fishing",
		"minigame",
		scheduler.ScheduleCron,
		scheduler.PriorityLow,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			// 1. 查询鱼钩数量；为 0 时说明免费次数已用尽。
			hCtx, hCancel := context.WithTimeout(ctx, callTimeout)
			hooksResp, err := session.Call(hCtx, protocol.BuildFishHooksPacket(), protocol.ActionIDFishHooks)
			hCancel()
			if err != nil {
				return fmt.Errorf("查询鱼钩数量失败: %w", err)
			}
			hooks, err := protocol.ParseFishHooksResult(hooksResp.Payload)
			if err != nil {
				return fmt.Errorf("解析鱼钩数量失败: %w", err)
			}
			if hooks <= 0 {
				slog.Info("今日鱼钩已用尽，跳过钓鱼", "role_id", session.RoleID())
				return nil
			}
			slog.Info("开始自动钓鱼", "role_id", session.RoleID(), "fishhooks", hooks)

			caught := 0
			for i := int32(0); i < hooks; i++ {
				fCtx, fCancel := context.WithTimeout(ctx, callTimeout)
				resp, cErr := session.Call(fCtx, protocol.BuildFishDoPacket(), protocol.ActionIDFishDo)
				fCancel()
				if cErr != nil {
					slog.Warn("钓鱼无应答", "role_id", session.RoleID(), "index", i+1, "err", cErr)
					break
				}
				result, pErr := protocol.ParseFishDoResult(resp.Payload)
				if pErr != nil {
					slog.Warn("解析钓鱼回包失败", "role_id", session.RoleID(), "err", pErr)
					break
				}
				switch result.Result {
				case protocol.FishResultSuccess:
					caught++
					slog.Info("钓鱼成功",
						"role_id", session.RoleID(),
						"index", i+1,
						"item_id", result.ItemID,
						"number", result.Number,
					)
				case protocol.FishResultNotEnoughFishhook:
					slog.Info("鱼钩已用尽", "role_id", session.RoleID(), "caught", caught)
					i = hooks
				case protocol.FishResultNotEnoughFateGrid,
					protocol.FishResultItemPackFull,
					protocol.FishResultFiveElementsPackFull,
					protocol.FishResultDragonballNoFreeGrid,
					protocol.FishResultCanJuanPackFull:
					slog.Warn("钓鱼奖励因背包/命格位不足未能领取",
						"role_id", session.RoleID(), "result", result.Result)
					i = hooks
				default:
					slog.Warn("钓鱼返回未知结果", "role_id", session.RoleID(), "result", result.Result)
					i = hooks
				}
				if i+1 < hooks {
					select {
					case <-ctx.Done():
						return nil
					case <-time.After(fishInterval):
					}
				}
			}
			slog.Info("自动钓鱼完成", "role_id", session.RoleID(), "caught", caught)
			return nil
		},
	)
}

// NewScratchCardRoutine 构造幸运刮刮卡福利任务 (Module 185, Mod_ScratchCard_Base)。
//
// 只使用每日免费刮卡次数；结果可能为积分/元宝/形象卡/普通道具。
// 次数不足、背包已满、刮卡失败时立即收手，不消耗任何元宝。
func NewScratchCardRoutine() scheduler.ActivityRoutine {
	const callTimeout = 3 * time.Second
	const shaveInterval = 300 * time.Millisecond
	return scheduler.NewBaseRoutine(
		"scratch_card",
		"minigame",
		scheduler.ScheduleCron,
		scheduler.PriorityLow,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			// 1. 查询活动状态与剩余免费次数
			iCtx, iCancel := context.WithTimeout(ctx, callTimeout)
			infoResp, err := session.Call(iCtx, protocol.BuildScratchCardInfoPacket(), protocol.ActionIDScratchCardInfo)
			iCancel()
			if err != nil {
				return fmt.Errorf("查询刮刮卡状态失败: %w", err)
			}
			info, err := protocol.ParseScratchCardInfoResult(infoResp.Payload)
			if err != nil {
				return fmt.Errorf("解析刮刮卡状态失败: %w", err)
			}
			if info.InActivity == 0 || info.CardTimes == 0 {
				slog.Info("刮刮卡活动未开放或次数已用尽，跳过",
					"role_id", session.RoleID(),
					"in_activity", info.InActivity,
					"active_degree", info.ActiveDegree,
					"card_times", info.CardTimes,
				)
				return nil
			}
			slog.Info("开始刮刮卡",
				"role_id", session.RoleID(),
				"card_times", info.CardTimes,
				"active_degree", info.ActiveDegree,
			)

			// 2. 用完全部免费次数
			shaved, ingotHits := 0, 0
			for i := 0; i < int(info.CardTimes); i++ {
				sCtx, sCancel := context.WithTimeout(ctx, callTimeout)
				resp, cErr := session.Call(sCtx, protocol.BuildScratchCardShavePacket(), protocol.ActionIDScratchCardShave)
				sCancel()
				if cErr != nil {
					slog.Warn("刮卡无应答", "role_id", session.RoleID(), "index", i+1, "err", cErr)
					break
				}
				result, pErr := protocol.ParseScratchCardShaveResult(resp.Payload)
				if pErr != nil {
					slog.Warn("解析刮卡回包失败", "role_id", session.RoleID(), "index", i+1, "err", pErr)
					break
				}
				switch result.Result {
				case protocol.ScratchCardResultSuccess,
					protocol.ScratchCardResultPoint,
					protocol.ScratchCardResultImage,
					protocol.ScratchCardResultAward:
					shaved++
					slog.Info("刮卡成功",
						"role_id", session.RoleID(),
						"index", i+1,
						"result", result.Result,
						"item_id", result.ItemID,
					)
				case protocol.ScratchCardResultIngot:
					shaved++
					ingotHits++
					slog.Info("刮出元宝", "role_id", session.RoleID(), "index", i+1, "item_id", result.ItemID)
				case protocol.ScratchCardResultPackFull,
					protocol.ScratchCardResultItemPackFull,
					protocol.ScratchCardResultFatePackFull:
					slog.Warn("刮卡奖励因背包已满未能领取", "role_id", session.RoleID(), "result", result.Result)
					return scheduler.ErrBagFull
				case protocol.ScratchCardResultNotEnoughTimes:
					slog.Info("刮卡次数已用尽", "role_id", session.RoleID(), "index", i+1)
					i = int(info.CardTimes)
				case protocol.ScratchCardResultFailed,
					protocol.ScratchCardResultNotExistCard:
					slog.Warn("刮卡失败", "role_id", session.RoleID(), "result", result.Result)
					i = int(info.CardTimes)
				default:
					slog.Warn("刮卡返回未知结果", "role_id", session.RoleID(), "result", result.Result)
					i = int(info.CardTimes)
				}
				if i+1 < int(info.CardTimes) {
					select {
					case <-ctx.Done():
						return nil
					case <-time.After(shaveInterval):
					}
				}
			}
			slog.Info("刮刮卡任务完成", "role_id", session.RoleID(), "shaved", shaved, "ingot_hits", ingotHits)
			return nil
		},
	)
}

// NewIceCaveRoutine 构造一键冰窟探险任务。
// NewIceCaveRoutine 构造一键冰窟探险任务 (Module 393, Mod_IceCaveTreasure_Base)。
//
// 先查询活动面板：无应答/未开放时安全跳过；开放时按剩余免费锤子逐块敲击，
// 活跃度不足/完成/背包满时停止；最后在冰晶足够时兑换奖励。
// 不调用 buy_hammer (消耗元宝)。
func NewIceCaveRoutine() scheduler.ActivityRoutine {
	const callTimeout = 3 * time.Second
	return scheduler.NewBaseRoutine(
		"ice_cave",
		"minigame",
		scheduler.ScheduleCron,
		scheduler.PriorityLow,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			// 1. 活动/功能检测：无应答视为未开放。
			iCtx, iCancel := context.WithTimeout(ctx, callTimeout)
			infoResp, err := session.Call(iCtx, protocol.BuildIceCaveInfoPacket(), protocol.ActionIDIceCaveInfo)
			iCancel()
			if err != nil {
				slog.Info("冰窟探险当前不可用/未开放，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			info, err := protocol.ParseIceCaveInfoResult(infoResp.Payload)
			if err != nil {
				return fmt.Errorf("解析冰窟面板失败: %w", err)
			}
			slog.Info("冰窟探险状态",
				"role_id", session.RoleID(),
				"slots", len(info.Slots),
				"hammers", info.HammerNum,
				"active_num", info.ActiveNum,
				"active_limit", info.ActiveLimit,
				"gold", info.GoldNum,
				"gift_count", info.GiftCount,
				"gift_limit", info.GiftLimit,
			)
			if info.HammerNum <= 0 {
				slog.Info("冰窟锤子已用尽，跳过", "role_id", session.RoleID())
				return nil
			}
			if info.ActiveLimit > 0 && info.ActiveNum < info.ActiveLimit {
				slog.Info("活跃度不足，跳过冰窟探险",
					"role_id", session.RoleID(),
					"active_num", info.ActiveNum,
					"active_limit", info.ActiveLimit,
				)
				return nil
			}

			// 2. 逐块敲击未开启的冰面。
			hits := int32(0)
			for _, slot := range info.Slots {
				if hits >= info.HammerNum {
					break
				}
				if slot.State != 0 {
					continue
				}
				hCtx, hCancel := context.WithTimeout(ctx, callTimeout)
				resp, cErr := session.Call(hCtx, protocol.BuildIceCaveHitPacket(slot.Ice, slot.IsIron), protocol.ActionIDIceCaveHit)
				hCancel()
				if cErr != nil {
					slog.Warn("冰窟敲击无应答", "role_id", session.RoleID(), "ice", slot.Ice, "err", cErr)
					break
				}
				result, pErr := protocol.ParseIceCaveHitResult(resp.Payload)
				if pErr != nil {
					slog.Warn("解析冰窟敲击回包失败", "role_id", session.RoleID(), "err", pErr)
					break
				}
				switch result.Result {
				case protocol.IceCaveResultSuccess:
					hits++
					slog.Info("冰窟敲击成功", "role_id", session.RoleID(), "ice", slot.Ice, "awards", result.AwardItems)
				case protocol.IceCaveResultCompleted:
					slog.Info("冰窟探险已完成", "role_id", session.RoleID(), "completed_awards", result.CompletedItems)
					hits = info.HammerNum
				case protocol.IceCaveResultItemPackFull:
					return scheduler.ErrBagFull
				case protocol.IceCaveResultNotEnoughActive:
					slog.Info("冰窟活跃度不足，结束敲击", "role_id", session.RoleID())
					hits = info.HammerNum
				case protocol.IceCaveResultFuncNotOpen,
					protocol.IceCaveResultNonActivityTime,
					protocol.IceCaveResultNotActivityTime:
					slog.Info("冰窟活动未开放/非活动时间，结束", "role_id", session.RoleID(), "result", result.Result)
					hits = info.HammerNum
				default:
					slog.Warn("冰窟敲击返回未知结果", "role_id", session.RoleID(), "result", result.Result)
					hits = info.HammerNum
				}
			}
			slog.Info("冰窟敲击完成", "role_id", session.RoleID(), "hits", hits)

			// 3. 冰晶足够时兑换奖励。
			if info.GiftGold > 0 && info.GoldNum >= info.GiftGold && info.GiftCount < info.GiftLimit {
				gCtx, gCancel := context.WithTimeout(ctx, callTimeout)
				gResp, gErr := session.Call(gCtx, protocol.BuildIceCaveGetGiftPacket(), protocol.ActionIDIceCaveGetGift)
				gCancel()
				if gErr != nil {
					slog.Warn("冰窟奖励兑换无应答", "role_id", session.RoleID(), "err", gErr)
					return nil
				}
				result, pErr := protocol.ParseIceCaveGiftResult(gResp.Payload)
				if pErr != nil {
					slog.Warn("解析冰窟兑换回包失败", "role_id", session.RoleID(), "err", pErr)
					return nil
				}
				switch result {
				case protocol.IceCaveResultSuccess:
					slog.Info("冰窟奖励兑换成功", "role_id", session.RoleID(), "item_id", info.GiftItem)
				case protocol.IceCaveResultItemPackFull:
					return scheduler.ErrBagFull
				case protocol.IceCaveResultAlreadyGet:
					slog.Info("冰窟奖励已兑换", "role_id", session.RoleID())
				case protocol.IceCaveResultNotEnoughGold:
					slog.Info("冰晶不足，跳过兑换", "role_id", session.RoleID(), "gold", info.GoldNum)
				default:
					slog.Info("冰窟奖励未兑换", "role_id", session.RoleID(), "result", result)
				}
			}
			return nil
		},
	)
}

// NewImperialExamRoutine 构造金榜题名会试自动答题任务。
// NewImperialExamRoutine 构造金榜题名状态检测任务 (Module 237)。
//
// 先查询活动时间与阶段；活动未开始/已结束/已关闭时跳过。
// 题目内容由客户端资源 SWF 提供，当前缺少可靠题库来源，因此只检测状态、不提交答案。
func NewImperialExamRoutine(qaEngine qa.Engine) scheduler.ActivityRoutine {
	const callTimeout = 3 * time.Second
	return scheduler.NewBaseRoutine(
		"imperial_exam",
		"minigame",
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

			stCtx, stCancel := context.WithTimeout(ctx, callTimeout)
			statusResp, err := session.Call(stCtx, protocol.BuildImperialExamStatusPacket(), protocol.ActionIDImperialExamStatus)
			stCancel()
			if err != nil {
				slog.Info("金榜题名功能不可用，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			status, err := protocol.ParseImperialExamStatusResult(statusResp.Payload)
			if err != nil {
				return fmt.Errorf("解析金榜题名活动状态失败: %w", err)
			}
			if status.Status != protocol.ImperialExamActivityStart {
				slog.Info("金榜题名当前非进行中阶段，跳过",
					"role_id", session.RoleID(),
					"status", status.Status,
					"start_time", status.StartTime,
					"end_time", status.EndTime,
				)
				return nil
			}

			answered, right, wrong := 0, 0, 0
			for round := 0; round < 100; round++ {
				qCtx, qCancel := context.WithTimeout(ctx, callTimeout)
				qResp, qErr := session.Call(qCtx, protocol.BuildImperialExamStartPacket(), protocol.ActionIDImperialExamStart)
				qCancel()
				if qErr != nil {
					slog.Warn("金榜题名开始答题无应答", "role_id", session.RoleID(), "err", qErr)
					break
				}
				q, pErr := protocol.ParseImperialExamQuestionResult(qResp.Payload)
				if pErr != nil {
					slog.Warn("解析金榜题名题目回包失败", "role_id", session.RoleID(), "err", pErr)
					break
				}
				switch q.Result {
				case protocol.ImperialExamAnswerOver:
					slog.Info("金榜题名今日答题已结束", "role_id", session.RoleID(), "answered", answered, "right", right, "wrong", wrong)
					return nil
				case protocol.ImperialExamResultNonActivityTime:
					slog.Info("金榜题名已过活动时间，结束", "role_id", session.RoleID())
					return nil
				case protocol.ImperialExamResultSuccess:
					// 继续
				default:
					slog.Info("金榜题名开始答题未成功", "role_id", session.RoleID(), "result", q.Result)
					return nil
				}

				cipher, ok := protocol.LookupExamQuestion(q.QuestionID)
				if !ok {
					slog.Warn("金榜题名题库缺少题目，结束", "role_id", session.RoleID(), "question_id", q.QuestionID)
					return nil
				}
				question, dErr := protocol.DecryptExamString(cipher.Question, q.DecipherKey)
				if dErr != nil {
					slog.Warn("金榜题名题干解密失败", "role_id", session.RoleID(), "question_id", q.QuestionID, "err", dErr)
					return nil
				}
				var options [4]string
				for i, c := range []string{cipher.AnswerA, cipher.AnswerB, cipher.AnswerC, cipher.AnswerD} {
					if c == "" {
						continue
					}
					if options[i], dErr = protocol.DecryptExamString(c, q.DecipherKey); dErr != nil {
						slog.Debug("金榜题名选项解密失败", "role_id", session.RoleID(), "index", i, "err", dErr)
					}
				}

				knownAnswer, confidence := "", 0.0
				if qaEngine != nil {
					if ans, conf, found := qaEngine.Match(question); found {
						knownAnswer, confidence = ans, conf
					}
				}
				idx := protocol.ChooseExamOption(options, knownAnswer)
				seq := protocol.ImperialExamSequenceA + uint8(idx)

				sCtx, sCancel := context.WithTimeout(ctx, 5*time.Second)
				sResp, sErr := session.Call(sCtx, protocol.BuildImperialExamSubmitPacket(q.CurAnswerSeq, seq), protocol.ActionIDImperialExamSubmit)
				sCancel()
				if sErr != nil {
					slog.Warn("金榜题名提交答案无应答", "role_id", session.RoleID(), "err", sErr)
					break
				}
				sub, spErr := protocol.ParseImperialExamSubmitResult(sResp.Payload)
				if spErr != nil {
					slog.Warn("解析金榜题名提交回包失败", "role_id", session.RoleID(), "err", spErr)
					break
				}
				answered++
				if sub.RightSequence == seq {
					right++
				} else {
					wrong++
				}
				slog.Info("金榜题名答题",
					"role_id", session.RoleID(),
					"question_id", q.QuestionID,
					"question", question,
					"chosen", string(rune('A'+idx)),
					"right_sequence", sub.RightSequence,
					"known_answer", knownAnswer,
					"confidence", confidence,
				)
				switch sub.Result {
				case protocol.ImperialExamResultSuccess:
					// 继续下一题
				case protocol.ImperialExamAnswerOver:
					slog.Info("金榜题名今日答题已结束", "role_id", session.RoleID(), "answered", answered, "right", right, "wrong", wrong)
					return nil
				case protocol.ImperialExamResultNonActivityTime:
					slog.Info("金榜题名已过活动时间，结束", "role_id", session.RoleID())
					return nil
				case protocol.ImperialExamResultSubmitTimeOut:
					slog.Warn("金榜题名提交超时，重新开始", "role_id", session.RoleID())
					continue
				default:
					slog.Info("金榜题名提交未成功", "role_id", session.RoleID(), "result", sub.Result)
					return nil
				}
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(300 * time.Millisecond):
				}
			}
			slog.Info("金榜题名答题巡检完成", "role_id", session.RoleID(), "answered", answered, "right", right, "wrong", wrong)
			return nil
		},
	)
}

// NewEraTravelRoutine 构造山河纪元游历掷骰与奇遇领奖任务 (Feature 10: 山河游历)。
func NewEraTravelRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"era_travel",
		"minigame",
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
			slog.Info("正在执行益智任务: 山河纪元游历掷骰与寻宝", "role_id", session.RoleID())

			// 1. 查询游历状态 (Mod 850, Act 4)
			statePkt := protocol.BuildEraTravelGetStatePacket(protocol.ActionIDTownEnter)
			sCtx, sCancel := context.WithTimeout(ctx, 2*time.Second)
			resp, err := session.Call(sCtx, statePkt, protocol.ActionIDEraTravelGetState)
			sCancel()

			if err != nil || resp == nil || len(resp.Payload) == 0 {
				slog.Info("山河游历功能未开放/不可用，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}

			// 2. 掷骰移动 (Mod 850, Act 0)
			rollPkt := protocol.BuildEraTravelRollDicePacket(1, protocol.ActionIDEraTravelGetState)
			rCtx, rCancel := context.WithTimeout(ctx, 2*time.Second)
			_, _ = session.Call(rCtx, rollPkt, protocol.ActionIDEraTravelRollDice)
			rCancel()

			// 3. 奇遇画卷事件结算 (Mod 635, Act 0)
			eventPkt := protocol.NewPacket(protocol.ActionIDEraEventSettle, protocol.NewWriter().WriteUint32(protocol.ActionIDEraTravelRollDice).Bytes())
			eCtx, eCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
			_, _ = session.Call(eCtx, eventPkt, protocol.ActionIDEraEventSettle)
			eCancel()

			slog.Info("山河游历掷骰与事件结算完成", "role_id", session.RoleID())
			return nil
		},
	)
}

// NewLingDaoRoutine 构造灵道初解任务领悟任务 (Feature 11: 灵道初解)。
func NewLingDaoRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"lingdao_practice",
		"minigame",
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
			slog.Info("正在执行益智任务: 灵道初解修炼与任务领悟", "role_id", session.RoleID())

			// 1. 查询灵道修炼进度 (Mod 658, Act 0)
			statusPkt := protocol.BuildLingDaoGetStatusPacket(protocol.ActionIDTownEnter)
			sCtx, sCancel := context.WithTimeout(ctx, 2*time.Second)
			resp, err := session.Call(sCtx, statusPkt, protocol.ActionIDLingDaoGetStatus)
			sCancel()

			if err != nil || resp == nil || len(resp.Payload) == 0 {
				slog.Info("灵道初解功能未开放/不可用，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}

			// 2. 提交初解任务领悟 (Mod 711, Act 0)
			compPkt := protocol.BuildLingDaoComprehendPacket(protocol.ActionIDLingDaoGetStatus)
			cCtx, cCancel := context.WithTimeout(ctx, 2*time.Second)
			_, _ = session.Call(cCtx, compPkt, protocol.ActionIDLingDaoComprehend)
			cCancel()

			slog.Info("灵道初解阶段领悟完成", "role_id", session.RoleID())
			return nil
		},
	)
}

// NewHuanshouRoutine 构造山海幻兽入侵挑战与扫荡任务 (Feature 12: 山海幻兽)。
func NewHuanshouRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"huanshou_invade",
		"minigame",
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
			slog.Info("正在执行益智任务: 山海幻兽入侵推图与扫荡", "role_id", session.RoleID())

			// 0. 先通过官方功能开放列表判定门槛，未开放时不再发送会被静默丢弃的业务请求。
			open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDEudemons)
			if fErr != nil {
				slog.Info("山海幻兽功能开放状态查询失败，跳过", "role_id", session.RoleID(), "err", fErr)
				return nil
			}
			if !open {
				slog.Info("山海幻兽功能未开放，跳过", "role_id", session.RoleID(), "function_id", protocol.FunctionIDEudemons)
				return nil
			}

			// 1. 查询幻兽面板 (Mod 701, Act 0)
			panelPkt := protocol.BuildHuanshouPanelPacket(protocol.ActionIDTownEnter)
			pCtx, pCancel := context.WithTimeout(ctx, 2*time.Second)
			panelResp, pErr := session.Call(pCtx, panelPkt, protocol.ActionIDHuanshouPanel)
			pCancel()
			if pErr != nil || panelResp == nil || len(panelResp.Payload) == 0 {
				slog.Info("山海幻兽功能未开放/不可用，跳过", "role_id", session.RoleID(), "err", pErr)
				return nil
			}

			// 2. 选定入侵章节并挑战 (Mod 696, Act 1 & 2)
			chapPkt := protocol.NewPacket(protocol.ActionIDHuanshouInvadeChapter, protocol.NewWriter().WriteUint32(protocol.ActionIDHuanshouPanel).Bytes())
			cCtx, cCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
			_, _ = session.Call(cCtx, chapPkt, protocol.ActionIDHuanshouInvadeChapter)
			cCancel()

			chalPkt := protocol.BuildHuanshouInvadeChallengePacket(protocol.ActionIDHuanshouInvadeChapter)
			chCtx, chCancel := context.WithTimeout(ctx, 2*time.Second)
			_, _ = session.Call(chCtx, chalPkt, protocol.ActionIDHuanshouInvadeChallenge)
			chCancel()

			// 3. 极速秒完成扫荡 (Mod 696, Act 3)
			sweepPkt := protocol.BuildHuanshouInvadeSweepPacket(protocol.ActionIDHuanshouInvadeChallenge)
			swCtx, swCancel := context.WithTimeout(ctx, 2*time.Second)
			_, _ = session.Call(swCtx, sweepPkt, protocol.ActionIDHuanshouInvadeSweep)
			swCancel()

			// 4. 逐关扫荡入侵节点 (Mod 696, Act 4: 节点 1~8 依据抓包实采流)
			prevAct := protocol.ActionIDHuanshouInvadeSweep
			var clearedNodes int
			for nodeID := int32(1); nodeID <= 8; nodeID++ {
				nodePkt := protocol.BuildHuanshouInvadeNodePacket(nodeID, prevAct)
				nCtx, nCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
				nResp, nErr := session.Call(nCtx, nodePkt, protocol.ActionIDHuanshouInvadeSettle)
				nCancel()
				if nErr == nil && nResp != nil {
					clearedNodes++
				}
				prevAct = protocol.ActionIDHuanshouInvadeSettle
				time.Sleep(30 * time.Millisecond)
			}

			slog.Info("山海幻兽入侵扫荡轮次完成", "role_id", session.RoleID(), "cleared_nodes", clearedNodes)
			return nil
		},
	)
}

// NewWuFaRoutine 构造悟法领域感悟抽卡与技能配置任务 (Feature 14: 悟法领域)。
//
// 协议对齐原版 Main.swf Mod_WuFaField_Base:
//   - Action 0 panel_info 查询秘馆、资源与免费次数
//   - Action 1 draw 感悟抽卡，请求 [Int field_type, UByte draw_type]
//
// 仅使用剩余免费次数，不主动消耗元宝或感悟道具备用次数；
// 服务端返回的业务失败码全部解析并按语义熔断。
func NewWuFaRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"wufa_field",
		"minigame",
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

			// 0. 原生探测功能开放状态 (Mod_Player_Base.check_function_open)。
			// 未解锁模块服务端会静默丢弃业务请求，先查询可避免空等超时。
			chkCtx, chkCancel := context.WithTimeout(ctx, 2*time.Second)
			open, chkErr := session.CheckFunctionOpen(chkCtx, protocol.FunctionIDWuFaField)
			chkCancel()
			if chkErr != nil {
				slog.Warn("查询悟法领域功能开放状态失败，跳过", "role_id", session.RoleID(), "err", chkErr)
				return nil
			}
			if !open {
				slog.Info("悟法领域功能未开启，跳过", "role_id", session.RoleID())
				return nil
			}
			slog.Info("正在执行益智任务: 悟法领域技能感悟与领悟", "role_id", session.RoleID())

			// 1. 查询悟法领域面板 (Mod 658, Act 0)。
			// 官方面板回包没有 result_code，只能通过结构完整性校验判断是否可信。
			infoPkt := protocol.BuildWuFaFieldInfoPacket(protocol.ActionIDTownEnter)
			iCtx, iCancel := context.WithTimeout(ctx, 2*time.Second)
			infoResp, err := session.Call(iCtx, infoPkt, protocol.ActionIDWuFaFieldInfo)
			iCancel()
			if err != nil || infoResp == nil {
				slog.Warn("查询悟法领域面板未收到回包", "role_id", session.RoleID(), "err", err)
				return nil
			}

			panel, err := protocol.ParseWuFaPanel(infoResp.Payload)
			if err != nil {
				slog.Warn("解析悟法领域面板回包失败",
					"role_id", session.RoleID(),
					"err", err,
					"payload_hex", fmt.Sprintf("%x", infoResp.Payload),
				)
				return fmt.Errorf("解析悟法领域面板回包失败: %w", err)
			}

			fieldType, drawType, ok := selectWuFaFreeDraw(panel)
			if !ok {
				slog.Info("悟法领域当前无免费感悟次数，跳过本次抽卡",
					"role_id", session.RoleID(),
					"rest_times", panel.RestTimes,
				)
				return nil
			}

			// 2. 感悟抽卡 (Mod 658, Act 1)，仅使用免费次数。
			drawPkt := protocol.BuildWuFaDrawCardPacket(fieldType, drawType)
			dCtx, dCancel := context.WithTimeout(ctx, 2*time.Second)
			drawResp, err := session.Call(dCtx, drawPkt, protocol.ActionIDWuFaDrawCard)
			dCancel()
			if err != nil || drawResp == nil {
				slog.Warn("悟法领域感悟抽卡未收到回包",
					"role_id", session.RoleID(),
					"field_type", fieldType,
					"draw_type", drawType,
					"err", err,
				)
				return nil
			}

			result, err := protocol.ParseWuFaDrawResult(drawResp.Payload)
			if err != nil {
				slog.Warn("解析悟法感悟抽卡回包失败",
					"role_id", session.RoleID(),
					"err", err,
					"payload_hex", fmt.Sprintf("%x", drawResp.Payload),
				)
				return fmt.Errorf("解析悟法感悟抽卡回包失败: %w", err)
			}

			if result.Result != protocol.WuFaResultSuccess {
				err = classifyWuFaDrawResult(result.Result)
				if err == nil {
					slog.Info("悟法领域感悟抽卡无需执行",
						"role_id", session.RoleID(),
						"field_type", fieldType,
						"draw_type", drawType,
						"result", result.Result,
						"result_text", protocol.WuFaResultText(result.Result),
						"rest_times", result.RestTimes,
					)
					return nil
				}
				slog.Warn("悟法领域感悟抽卡返回业务失败",
					"role_id", session.RoleID(),
					"field_type", fieldType,
					"draw_type", drawType,
					"result", result.Result,
					"result_text", protocol.WuFaResultText(result.Result),
					"rest_times", result.RestTimes,
				)
				return err
			}

			slog.Info("悟法领域技能感悟成功",
				"role_id", session.RoleID(),
				"field_type", fieldType,
				"draw_type", drawType,
				"award_count", len(result.Awards),
				"rest_times", result.RestTimes,
			)
			return nil
		},
	)
}

// selectWuFaFreeDraw 从面板中挑选免费次数最多的秘馆与抽卡类型。
//
// 优先使用免费十连，其次免费单抽；同等次数下选择更高 field_type 的秘馆。
// 无任何免费次数时返回 ok=false，由调用方跳过抽卡，避免消耗元宝或感悟道具。
func selectWuFaFreeDraw(panel *protocol.WuFaPanel) (fieldType int32, drawType uint8, ok bool) {
	if panel == nil {
		return 0, 0, false
	}

	bestTenField, bestTenCount := int32(0), int32(0)
	bestOneField, bestOneCount := int32(0), int32(0)
	for _, field := range panel.Fields {
		if field.LeftFreeTenCount > 0 &&
			(field.LeftFreeTenCount > bestTenCount || (field.LeftFreeTenCount == bestTenCount && field.FieldType > bestTenField)) {
			bestTenField, bestTenCount = field.FieldType, field.LeftFreeTenCount
		}
		if field.LeftFreeOneCount > 0 &&
			(field.LeftFreeOneCount > bestOneCount || (field.LeftFreeOneCount == bestOneCount && field.FieldType > bestOneField)) {
			bestOneField, bestOneCount = field.FieldType, field.LeftFreeOneCount
		}
	}

	if bestTenCount > 0 {
		return bestTenField, protocol.WuFaDrawTen, true
	}
	if bestOneCount > 0 {
		return bestOneField, protocol.WuFaDrawOne, true
	}
	return 0, 0, false
}

// classifyWuFaDrawResult 将官方悟法结果码映射为调度器可识别的业务语义。
//
// 成功与幂等状态（已领取/已拥有/已满级）返回 nil；
// 次数类失败触发次日熔断，背包满触发短冷却，其余条件不满足触发条件熔断，
// 未知或协议错误返回普通错误以终止本轮，避免盲目重发。
func classifyWuFaDrawResult(code uint8) error {
	switch code {
	case protocol.WuFaResultSuccess,
		protocol.WuFaResultOverGet,
		protocol.WuFaResultIsBuy,
		protocol.WuFaResultIsHave,
		protocol.WuFaResultFullGrade:
		return nil
	case protocol.WuFaResultCountLimit,
		protocol.WuFaResultNotEnoughCount,
		protocol.WuFaResultWeekNumLimit,
		protocol.WuFaResultNoEnoughDrawTimes:
		return scheduler.ErrAttemptsExhausted
	case protocol.WuFaResultItemPackFull:
		return scheduler.ErrBagFull
	case protocol.WuFaResultNotEnoughIngot,
		protocol.WuFaResultNotEnoughResourse,
		protocol.WuFaResultNotEnoughResource,
		protocol.WuFaResultNotEnoughRecharge,
		protocol.WuFaResultFunctionNotOpen:
		return scheduler.ErrConditionNotMet
	default:
		return fmt.Errorf("悟法领域感悟失败: result=%d (%s)", code, protocol.WuFaResultText(code))
	}
}

// NewMasterPracticeRoutine 构造宗师修行挑战与突破任务 (Feature 15: 宗师修行)。
func NewMasterPracticeRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"master_practice",
		"minigame",
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
			slog.Info("正在执行益智任务: 宗师修行试炼与境界突破", "role_id", session.RoleID())

			call := func(pkt *protocol.Packet, act uint32) (*protocol.Packet, error) {
				cCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
				defer cancel()
				return session.Call(cCtx, pkt, act)
			}

			// 1. 查询宗师修行主面板 (Mod 665, Act 1)
			resp, err := call(protocol.BuildMasterPracticePanelPacket(protocol.ActionIDTownEnter), protocol.ActionIDMasterPracticePanel)
			if err != nil || resp == nil {
				slog.Info("宗师修行功能未开放/不可用，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			panel, err := protocol.ParseMasterPracticePanelResult(resp.Payload)
			if err != nil {
				slog.Info("解析宗师修行面板失败，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			if panel.Result != protocol.MasterPracticeResultSuccess {
				slog.Info("宗师修行面板返回失败", "role_id", session.RoleID(), "result", panel.Result)
				return nil
			}
			slog.Info("宗师修行面板",
				"role_id", session.RoleID(),
				"today_challenge_times", panel.TodayChallengeTimes,
				"jobs", len(panel.Jobs),
			)

			for _, job := range panel.Jobs {
				if err := ctx.Err(); err != nil {
					return nil
				}
				challengedToday := make(map[int32]bool, len(job.TodayChallenge))
				for _, s := range job.TodayChallenge {
					challengedToday[s] = true
				}
				for star := int32(1); star <= 3; star++ {
					if challengedToday[star] {
						continue
					}
					cResp, cErr := call(
						protocol.BuildMasterPracticeChallengePacket(uint32(job.JobID), uint32(star), 0),
						protocol.ActionIDMasterPracticeChallenge,
					)
					if cErr != nil || cResp == nil {
						slog.Warn("宗师修行挑战无回包", "role_id", session.RoleID(), "job_id", job.JobID, "star", star, "err", cErr)
						break
					}
					code, pErr := protocol.ParseMasterPracticeActionCode(cResp.Payload)
					if pErr != nil {
						slog.Warn("解析宗师修行挑战回包失败", "role_id", session.RoleID(), "err", pErr)
						break
					}
					switch code {
					case protocol.MasterPracticeResultSuccess:
						slog.Info("宗师修行挑战成功",
							"role_id", session.RoleID(), "job_id", job.JobID, "star", star)
					case protocol.MasterPracticeResultNoChallengeTimes:
						slog.Info("宗师修行今日挑战次数已用尽", "role_id", session.RoleID())
						return nil
					case protocol.MasterPracticeResultIsChallenged:
						break
					case protocol.MasterPracticeResultMaxStars, protocol.MasterPracticeResultLessStars:
						star = 3
					case protocol.MasterPracticeResultJobError, protocol.MasterPracticeResultConditionLimited,
						protocol.MasterPracticeResultIsLocked, protocol.MasterPracticeResultFailed:
						slog.Info("宗师修行挑战未成功",
							"role_id", session.RoleID(), "job_id", job.JobID, "star", star, "result", code)
						star = 3
					default:
						slog.Info("宗师修行挑战未成功",
							"role_id", session.RoleID(), "job_id", job.JobID, "star", star, "result", code)
						star = 3
					}
					select {
					case <-ctx.Done():
						return nil
					case <-time.After(200 * time.Millisecond):
					}
				}

				// 2. 境界突破 (Mod 665, Act 8)。冷却中或星数不足时服务端会返回对应结果码。
				if job.TotalStars <= 0 {
					continue
				}
				bResp, bErr := call(
					protocol.BuildMasterPracticeStepUpPacket(uint32(job.JobID), 0),
					protocol.ActionIDMasterPracticeStepUp,
				)
				if bErr != nil || bResp == nil {
					slog.Warn("宗师境界突破无回包", "role_id", session.RoleID(), "job_id", job.JobID, "err", bErr)
					continue
				}
				code, pErr := protocol.ParseMasterPracticeActionCode(bResp.Payload)
				if pErr != nil {
					slog.Warn("解析宗师突破回包失败", "role_id", session.RoleID(), "err", pErr)
					continue
				}
				switch code {
				case protocol.MasterPracticeResultSuccess:
					slog.Info("宗师境界突破成功", "role_id", session.RoleID(), "job_id", job.JobID)
				case protocol.MasterPracticeResultStepUpCD:
					slog.Info("宗师境界突破冷却中", "role_id", session.RoleID(), "job_id", job.JobID)
				case protocol.MasterPracticeResultLessStars:
					slog.Info("宗师境界突破星数不足", "role_id", session.RoleID(), "job_id", job.JobID)
				case protocol.MasterPracticeResultTopStep:
					slog.Info("宗师境界已达最高", "role_id", session.RoleID(), "job_id", job.JobID)
				default:
					slog.Info("宗师境界突破未成功", "role_id", session.RoleID(), "job_id", job.JobID, "result", code)
				}
			}
			slog.Info("宗师修行巡检完成", "role_id", session.RoleID())
			return nil
		},
	)
}

// AwardSelectorFunc 允许注入自定义交互选择器（例如命令行交互、Web交互回调或自动化测试桩）。
type AwardSelectorFunc func(ctx context.Context, pool *protocol.TreasureHousePool, dict dictionary.Repository) ([]protocol.TreasureAwardSelect, error)

// treasureHousePriority 天帝宝库调度优先级：高于 PriorityHigh (100)，
// 必须在本轮任何会产生队伍/战场上下文的玩法之前执行。
const treasureHousePriority scheduler.RoutinePriority = 110

// TreasureHouseConfig 天帝宝库自选大奖与开启抽奖配置。
type TreasureHouseConfig struct {
	// DictRepo 静态数据库字典仓储 (用于解析大奖条目道具名及说明)
	DictRepo dictionary.Repository
	// Interactive 是否开启终端交互式输入自选大奖
	Interactive bool
	// In 用户交互输入流，默认为 os.Stdin
	In io.Reader
	// Out 交互提示输出流，默认为 os.Stdout
	Out io.Writer
	// Selector 自定义大奖选择逻辑 (优先级高于终端输入)
	Selector AwardSelectorFunc
	// Awards 预设三档自选大奖列表 (非交互模式或交互跳过/默认时的值)
	Awards []protocol.TreasureAwardSelect
	// MaxDrawTimes 单次调度最大抽奖次数 (默认 10)
	MaxDrawTimes int
}

// DefaultTreasureHouseConfig 返回默认天帝宝库三档大奖配置 (对齐抓包基准: 50, 25, 42)。
func DefaultTreasureHouseConfig() TreasureHouseConfig {
	return TreasureHouseConfig{
		Awards: []protocol.TreasureAwardSelect{
			{TierID: 1, ItemID: 50},
			{TierID: 2, ItemID: 25},
			{TierID: 3, ItemID: 42},
		},
		MaxDrawTimes: 10,
	}
}

// formatAwardEntry 将候选道具条目 ID 格式化为直观的中文名称与数量描述。
func formatAwardEntry(id int32, awardMap map[int]*dictionary.WishpoolAward, dict dictionary.Repository) string {
	if awardMap != nil {
		if a, ok := awardMap[int(id)]; ok && a != nil {
			name := a.ItemName
			if name == "" {
				name = fmt.Sprintf("条目%d", a.ID)
			}
			desc := ""
			if a.Description != "" {
				desc = " (" + a.Description + ")"
			}
			if a.ItemNum > 1 {
				return fmt.Sprintf("%s x%d (ID: %d)%s", name, a.ItemNum, id, desc)
			}
			return fmt.Sprintf("%s (ID: %d)%s", name, id, desc)
		}
	}
	if dict != nil {
		if item, err := dict.GetItem(int(id)); err == nil && item != nil {
			return fmt.Sprintf("%s (道具ID: %d)", item.Name, id)
		}
	}
	return fmt.Sprintf("条目 ID: %d", id)
}

// readLineWithTimeout 携带超时与上下文感知地读取一行输入。
func readLineWithTimeout(ctx context.Context, reader *bufio.Reader, timeout time.Duration) (string, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := reader.ReadString('\n')
		ch <- result{line: line, err: err}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timer.C:
		return "", errors.New("输入等待超时")
	case res := <-ch:
		if res.err != nil && res.line == "" {
			return "", res.err
		}
		return strings.TrimSpace(res.line), nil
	}
}

// promptUserAwardSelection 在控制台打印当前服务端刷新出的各档候选大奖池并引导用户交互选择。
func promptUserAwardSelection(
	ctx context.Context,
	in io.Reader,
	out io.Writer,
	pool *protocol.TreasureHousePool,
	dict dictionary.Repository,
	defaultAwards []protocol.TreasureAwardSelect,
) []protocol.TreasureAwardSelect {
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}

	var awardMap map[int]*dictionary.WishpoolAward
	if dict != nil {
		awardMap, _ = dict.GetWishpoolAwards()
	}

	defaultMap := make(map[int]int32)
	for _, a := range defaultAwards {
		defaultMap[int(a.TierID)] = a.ItemID
	}
	if defaultMap[1] == 0 {
		defaultMap[1] = 50
	}
	if defaultMap[2] == 0 {
		defaultMap[2] = 25
	}
	if defaultMap[3] == 0 {
		defaultMap[3] = 42
	}

	fmt.Fprintln(out, "\n====================【天帝宝库 当前候选大奖池】====================")
	fmt.Fprintf(out, "服务端最新候选奖池刷新成功，共解析出 %d 项可选道具：\n", pool.TotalCount)

	printTierOptions := func(tier int, items []int32, defID int32) {
		defName := formatAwardEntry(defID, awardMap, dict)
		fmt.Fprintf(out, "\n【第 %d 档大奖候选列表】 (直接回车默认: %s):\n", tier, defName)
		for idx, itemID := range items {
			entryName := formatAwardEntry(itemID, awardMap, dict)
			fmt.Fprintf(out, "  [%2d] %s\n", idx+1, entryName)
		}
	}

	printTierOptions(1, pool.Tier1Items, defaultMap[1])
	printTierOptions(2, pool.Tier2Items, defaultMap[2])
	printTierOptions(3, pool.Tier3Items, defaultMap[3])
	fmt.Fprintln(out, "=================================================================")

	reader := bufio.NewReader(in)

	selectTier := func(tier int, items []int32, defID int32) int32 {
		defName := formatAwardEntry(defID, awardMap, dict)
		fmt.Fprintf(out, ">> 请选择【第 %d 档大奖】(输入 1-%d 序号、道具ID或名称, 直接回车默认选 [%s]): ", tier, len(items), defName)

		input, err := readLineWithTimeout(ctx, reader, 60*time.Second)
		if err != nil || input == "" {
			fmt.Fprintf(out, "已采纳默认设定: %s\n", defName)
			return defID
		}

		cleanInput := strings.TrimSpace(input)
		lower := strings.ToLower(cleanInput)

		// 1. 若以 id: 或 # 前缀指定道具 ID (如 id:14, #14)
		if strings.HasPrefix(lower, "id:") || strings.HasPrefix(lower, "id ") || strings.HasPrefix(lower, "#") {
			raw := strings.TrimLeft(lower, "id:# ")
			var targetID int32
			if _, err := fmt.Sscanf(raw, "%d", &targetID); err == nil {
				for _, itm := range items {
					if itm == targetID {
						fmt.Fprintf(out, "成功选定第 %d 档 (按ID): %s\n", tier, formatAwardEntry(itm, awardMap, dict))
						return itm
					}
				}
			}
		}

		// 2. 按纯数字解析
		var val int32
		if _, scanErr := fmt.Sscanf(cleanInput, "%d", &val); scanErr == nil {
			// 若数字在 1..len(items) 范围内，优先作为菜单项序号
			if val >= 1 && int(val) <= len(items) {
				picked := items[val-1]
				fmt.Fprintf(out, "成功选定第 %d 档 (序号 %d): %s\n", tier, val, formatAwardEntry(picked, awardMap, dict))
				return picked
			}

			// 若数字超出菜单序号范围，匹配条目 ID (例如 50, 25, 42)
			for _, itm := range items {
				if itm == val {
					fmt.Fprintf(out, "成功选定第 %d 档 (道具ID %d): %s\n", tier, val, formatAwardEntry(itm, awardMap, dict))
					return itm
				}
			}
		}

		// 3. 按道具名称匹配 (如输入 "鸿蒙紫气"、"血脉精华")
		for _, itm := range items {
			entryName := formatAwardEntry(itm, awardMap, dict)
			if strings.Contains(entryName, cleanInput) {
				fmt.Fprintf(out, "成功选定第 %d 档 (名称匹配): %s\n", tier, entryName)
				return itm
			}
		}

		fmt.Fprintf(out, "输入的编号不在本档候选池中，已采纳默认设定: %s\n", defName)
		return defID
	}

	t1 := selectTier(1, pool.Tier1Items, defaultMap[1])
	t2 := selectTier(2, pool.Tier2Items, defaultMap[2])
	t3 := selectTier(3, pool.Tier3Items, defaultMap[3])

	return []protocol.TreasureAwardSelect{
		{TierID: 1, ItemID: t1},
		{TierID: 2, ItemID: t2},
		{TierID: 3, ItemID: t3},
	}
}

// NewTreasureHouseRoutine 构造天帝宝库自选与开启抽奖任务 (Feature 16: 天帝宝库)。
// 支持先向服务端拉取刷新候选道具，再通过用户交互输入或自定义选择器选定一档、二档、三档大奖。
func NewTreasureHouseRoutine(cfgs ...TreasureHouseConfig) scheduler.ActivityRoutine {
	cfg := DefaultTreasureHouseConfig()
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	if len(cfg.Awards) == 0 {
		cfg.Awards = DefaultTreasureHouseConfig().Awards
	}
	if cfg.MaxDrawTimes <= 0 {
		cfg.MaxDrawTimes = 10
	}

	return scheduler.NewBaseRoutine(
		"treasure_house",
		"minigame",
		scheduler.ScheduleCron,
		// 天帝宝库的 set_use_free_flag (Act 4) 会严格校验角色的城镇上下文：
		// 若本轮先执行过魔王试炼等会创建队伍/战场的玩法，服务端会保留战场状态并
		// 静默丢弃 Act 4（实机 2026-11-06）。因此把宝库安排在每日任务批次最前，
		// 高于 PriorityHigh，确保设置免费标记与抽奖链路安全。
		treasureHousePriority,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			slog.Info("正在执行益智任务: 天帝宝库自选与开启抽奖",
				"role_id", session.RoleID(),
				"interactive", cfg.Interactive,
				"awards_count", len(cfg.Awards),
				"max_draw_times", cfg.MaxDrawTimes,
			)

			// 先查功能开放列表；未开放时不再发送会被静默丢弃的 594 业务请求。
			open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDTreasureHouse)
			if fErr != nil {
				slog.Info("天帝宝库功能开放状态查询失败，跳过", "role_id", session.RoleID(), "err", fErr)
				return nil
			}
			if !open {
				slog.Info("天帝宝库功能未开放，跳过", "role_id", session.RoleID(), "function_id", protocol.FunctionIDTreasureHouse)
				return nil
			}

			prevAct := session.LastActionID()
			if prevAct == 0 {
				prevAct = protocol.ActionIDTownEnter
			}
			call := func(pkt *protocol.Packet, action uint32, timeout time.Duration) *protocol.Packet {
				cctx, ccancel := context.WithTimeout(ctx, timeout)
				defer ccancel()
				resp, cErr := session.Call(cctx, pkt, action)
				if cErr != nil || resp == nil {
					slog.Warn("天帝宝库请求无回包",
						"action", fmt.Sprintf("0x%08X", action), "err", cErr)
					return nil
				}
				prevAct = action
				return resp
			}

			// 1. 主面板 (Mod 594, Act 1): 校验 result_code 并读取免费钥匙/当前自选
			mainResp := call(protocol.BuildTreasureHouseMainPanelPacket(prevAct),
				protocol.ActionIDTreasureHouseMainPanel, 2*time.Second)
			if mainResp == nil {
				return nil
			}
			mainInfo, pErr := protocol.ParseTreasureHouseMainPanelResult(mainResp.Payload)
			if pErr != nil {
				slog.Warn("解析天帝宝库主面板失败", "err", pErr)
				return nil
			}
			if mainInfo.Result != protocol.TreasureHouseResultSuccess {
				slog.Warn("天帝宝库主面板返回失败",
					"result", mainInfo.Result,
					"meaning", protocol.TreasureHouseResultName(mainInfo.Result))
				return nil
			}
			slog.Info("天帝宝库主面板",
				"role_id", session.RoleID(),
				"free_item_num", mainInfo.FreeItemNum,
				"use_free_flag", mainInfo.UseFreeFlag,
				"left_can_get_free", mainInfo.LeftCanGetFree,
				"total_times", mainInfo.TotalTimes,
				"wish_list", mainInfo.WishList)

			// 2. 领取今日免费钥匙 (Mod 594, Act 8)
			if mainInfo.LeftCanGetFree > 0 {
				freeResp := call(protocol.BuildTreasureHouseGetFreeItemPacket(prevAct),
					protocol.ActionIDTreasureHouseGetFreeItem, 2*time.Second)
				if freeResp != nil {
					code, cErr := protocol.ParseTreasureHouseResultCode(freeResp.Payload)
					switch {
					case cErr != nil:
						slog.Warn("解析领取免费钥匙回包失败", "err", cErr)
					case code != protocol.TreasureHouseResultSuccess:
						slog.Warn("领取免费钥匙失败",
							"result", code,
							"meaning", protocol.TreasureHouseResultName(code))
					default:
						slog.Info("已领取今日免费钥匙", "left_can_get_free", mainInfo.LeftCanGetFree)
						if refresh := call(protocol.BuildTreasureHouseMainPanelPacket(prevAct),
							protocol.ActionIDTreasureHouseMainPanel, 2*time.Second); refresh != nil {
							if mi, e := protocol.ParseTreasureHouseMainPanelResult(refresh.Payload); e == nil && mi.Result == protocol.TreasureHouseResultSuccess {
								mainInfo = mi
							}
						}
					}
				}
			}

			// 3. 自选面板 (Mod 594, Act 2): 只有 open_flag=1 的候选才能被服务端接受
			panelResp := call(protocol.BuildTreasureHouseInfoPacket(prevAct),
				protocol.ActionIDTreasureHouseSelectPanel, 2*time.Second)
			if panelResp == nil {
				return nil
			}
			panel, pErr := protocol.ParseTreasureHouseSelectPanelResult(panelResp.Payload)
			if pErr != nil {
				slog.Warn("解析天帝宝库自选面板失败", "err", pErr)
				return nil
			}
			if panel.Result != protocol.TreasureHouseResultSuccess {
				slog.Warn("天帝宝库自选面板返回失败",
					"result", panel.Result,
					"meaning", protocol.TreasureHouseResultName(panel.Result))
				return nil
			}

			offered := offeredAwardPool(panel)
			selectedAwards := resolveTreasureAwards(cfg.Awards, mainInfo.WishList, offered)
			slog.Info("天帝宝库当前可选大奖",
				"role_id", session.RoleID(),
				"tier1_options", offered.Tier1Items,
				"tier2_options", offered.Tier2Items,
				"tier3_options", offered.Tier3Items,
				"resolved_awards", selectedAwards)

			// 交互选奖：若配置了 Selector 则优先调用，否则若开启 Interactive 则通过交互式终端输入
			if cfg.Selector != nil {
				if chosen, cErr := cfg.Selector(ctx, offered, cfg.DictRepo); cErr == nil && len(chosen) >= 3 {
					selectedAwards = chosen
				}
			} else if cfg.Interactive {
				selectedAwards = promptUserAwardSelection(ctx, cfg.In, cfg.Out, offered, cfg.DictRepo, selectedAwards)
			}

			// 4. 设定三档自选大奖 (Mod 594, Act 3): 必须解析 result_code
			selResp := call(protocol.BuildTreasureHouseSelectPacket(selectedAwards, prevAct),
				protocol.ActionIDTreasureHouseSetAward, 2*time.Second)
			if selResp == nil {
				return nil
			}
			code, cErr := protocol.ParseTreasureHouseResultCode(selResp.Payload)
			if cErr != nil {
				slog.Warn("解析设定自选大奖回包失败", "err", cErr)
				return nil
			}
			if code != protocol.TreasureHouseResultSuccess {
				slog.Warn("设定天帝宝库自选大奖失败",
					"result", code,
					"meaning", protocol.TreasureHouseResultName(code),
					"awards", selectedAwards)
				return nil
			}
			slog.Info("天帝宝库自选大奖设定成功", "awards", selectedAwards)

			// 5. 设置优先使用免费次数 (Mod 594, Act 4)。
			// 该标记决定抽奖是否优先消耗免费道具；设置失败时直接放弃抽奖，避免误扣元宝。
			flagResp := call(protocol.BuildTreasureHouseUseFreeFlagPacket(1, prevAct),
				protocol.ActionIDTreasureHouseSetUseFreeFlag, 1500*time.Millisecond)
			if flagResp == nil {
				slog.Warn("设置免费次数优先无回包，跳过抽奖以免误消耗元宝")
				return nil
			}
			flagCode, fErr := protocol.ParseTreasureHouseResultCode(flagResp.Payload)
			if fErr != nil {
				slog.Warn("解析设置免费次数优先回包失败，跳过抽奖", "err", fErr)
				return nil
			}
			if flagCode != protocol.TreasureHouseResultSuccess {
				slog.Warn("设置免费次数优先失败，跳过抽奖以免误消耗元宝",
					"result", flagCode,
					"meaning", protocol.TreasureHouseResultName(flagCode))
				return nil
			}

			// 6. 消耗免费钥匙抽奖 (Mod 594, Act 6): 每次必须解析 result_code，失败立即熔断
			drawLimit := cfg.MaxDrawTimes
			if int(mainInfo.FreeItemNum) < drawLimit {
				drawLimit = int(mainInfo.FreeItemNum)
			}
			if drawLimit <= 0 {
				slog.Info("天帝宝库无免费钥匙，跳过抽奖以免消耗元宝",
					"role_id", session.RoleID(),
					"free_item_num", mainInfo.FreeItemNum)
				return nil
			}

			drawCount := 0
			for i := 0; i < drawLimit; i++ {
				drawResp := call(protocol.BuildTreasureHouseDrawPacket(prevAct),
					protocol.ActionIDTreasureHouseLottery, 2*time.Second)
				if drawResp == nil {
					break
				}
				lottery, lErr := protocol.ParseTreasureHouseLotteryResult(drawResp.Payload)
				if lErr != nil {
					slog.Warn("解析天帝宝库抽奖回包失败", "err", lErr)
					break
				}
				if lottery.Result != protocol.TreasureHouseResultSuccess {
					slog.Warn("天帝宝库抽奖失败，停止后续抽奖",
						"result", lottery.Result,
						"meaning", protocol.TreasureHouseResultName(lottery.Result),
						"draw_index", i+1)
					break
				}
				drawCount++
				slog.Info("天帝宝库抽奖成功",
					"award_id", lottery.AwardID,
					"draw_index", i+1)
				time.Sleep(50 * time.Millisecond)
			}

			// 7. 抽取结束后刷新主面板同步剩余钥匙与累计次数
			if refresh := call(protocol.BuildTreasureHouseMainPanelPacket(prevAct),
				protocol.ActionIDTreasureHouseMainPanel, 1500*time.Millisecond); refresh != nil {
				if mi, e := protocol.ParseTreasureHouseMainPanelResult(refresh.Payload); e == nil && mi.Result == protocol.TreasureHouseResultSuccess {
					slog.Info("天帝宝库抽奖后状态",
						"free_item_num", mi.FreeItemNum,
						"total_times", mi.TotalTimes,
						"wish_list", mi.WishList)
				}
			}

			slog.Info("天帝宝库抽奖流程完成", "role_id", session.RoleID(), "draw_count", drawCount)
			return nil
		},
	)
}

// offeredAwardPool 将自选面板中 open_flag=1 的候选大奖整理为三档奖池。
// 服务端只接受当前开放的候选，配置里未开放的道具会被 set_award 拒绝。
func offeredAwardPool(panel *protocol.TreasureHouseSelectPanel) *protocol.TreasureHousePool {
	pool := &protocol.TreasureHousePool{}
	if panel == nil {
		return pool
	}
	for _, q := range panel.Qualities {
		for _, c := range q.Awards {
			if c.OpenFlag == 0 {
				continue
			}
			switch q.Quality {
			case 1:
				pool.Tier1Items = append(pool.Tier1Items, c.AwardID)
			case 2:
				pool.Tier2Items = append(pool.Tier2Items, c.AwardID)
			case 3:
				pool.Tier3Items = append(pool.Tier3Items, c.AwardID)
			default:
				continue
			}
			pool.TotalCount++
		}
	}
	return pool
}

// containsAwardID 判断道具条目 ID 是否在候选列表中。
func containsAwardID(items []int32, id int32) bool {
	for _, item := range items {
		if item == id {
			return true
		}
	}
	return false
}

// resolveTreasureAwards 解析最终三档自选大奖:
// 优先采用配置值；若配置值不在当前开放候选中，则回退到主面板当前自选；最后回退到该档首个开放候选。
func resolveTreasureAwards(configured, current []protocol.TreasureAwardSelect, offered *protocol.TreasureHousePool) []protocol.TreasureAwardSelect {
	configuredMap := make(map[int32]int32, len(configured))
	for _, a := range configured {
		configuredMap[a.TierID] = a.ItemID
	}
	currentMap := make(map[int32]int32, len(current))
	for _, a := range current {
		currentMap[a.TierID] = a.ItemID
	}

	tiers := []struct {
		tier    int32
		options []int32
	}{
		{1, offered.Tier1Items},
		{2, offered.Tier2Items},
		{3, offered.Tier3Items},
	}

	resolved := make([]protocol.TreasureAwardSelect, 0, len(tiers))
	for _, t := range tiers {
		id := configuredMap[t.tier]
		if len(t.options) > 0 && !containsAwardID(t.options, id) {
			if cur := currentMap[t.tier]; containsAwardID(t.options, cur) {
				id = cur
			} else {
				id = t.options[0]
			}
		}
		if id == 0 && len(t.options) > 0 {
			id = t.options[0]
		}
		resolved = append(resolved, protocol.TreasureAwardSelect{TierID: t.tier, ItemID: id})
	}
	return resolved
}

// --------------------------------------------------------------------------------
// 喜从天降 / 五福临门 (Mod_FindImmortal_Base, Module 68)
// --------------------------------------------------------------------------------

// NewFindImmortalRoutine 构造喜从天降画龙鱼任务 (只走免费流程，不消耗元宝)。
//
// 协议对齐 2026 客户端 Mod_FindImmortal_Base:
//   - Action 1 open_find_immortal 查询面板
//   - Action 2 start_find_immortal 召唤龙鱼 (消耗龙鱼令)
//   - Action 4 pickup_award 收取龙鱼奖励
//   - Action 5 get_xun_xian_ling_number 查询龙鱼令
//
// 流程固定为：查询龙鱼令数量 -> 串行召唤并领取 -> 直到龙鱼令耗尽。
// 不调用 goad (吹龙鱼) 与 exchange_xian_ling (声望兑换)，因此不会消耗任何资源。
func NewFindImmortalRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"find_immortal",
		"minigame",
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
			slog.Info("正在执行益智任务: 喜从天降画龙鱼 (免费流程)", "role_id", session.RoleID())

			executeFindImmortalFish(ctx, session, jitter)
			return nil
		},
	)
}

// executeFindImmortalFish 执行龙鱼召唤与免费奖励领取闭环。
func executeFindImmortalFish(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) {
	prevAct := session.LastActionID()

	// 1. 查询面板，优先处理上次未收取的龙鱼 (Mod 68, Act 1)
	panelPkt := protocol.BuildFindImmortalOpenPacket(prevAct)
	pCtx, pCancel := context.WithTimeout(ctx, 3*time.Second)
	pResp, pErr := session.Call(pCtx, panelPkt, protocol.ActionIDFindImmortalOpen)
	pCancel()
	if pErr != nil || pResp == nil {
		slog.Warn("查询喜从天降面板未收到回包", "role_id", session.RoleID(), "err", pErr)
		return
	}
	panel, err := protocol.ParseFindImmortalPanelResult(pResp.Payload)
	if err != nil {
		slog.Warn("解析喜从天降面板回包失败", "role_id", session.RoleID(), "err", err)
		return
	}
	prevAct = protocol.ActionIDFindImmortalOpen

	// 2. 查询龙鱼令数量 (Mod 68, Act 5)。
	// 查询失败时不再回退使用面板 can_buy_number，避免把“可购买次数”误判为免费龙鱼令而触发消费。
	tokens, ok := queryFindImmortalTokens(ctx, session, prevAct)
	if ok {
		prevAct = protocol.ActionIDFindImmortalGetXunXianLing
	} else {
		tokens = 0
		slog.Warn("喜从天降龙鱼令查询失败，本轮回跳过召唤新龙鱼", "role_id", session.RoleID())
	}

	slog.Info("喜从天降当前面板",
		"role_id", session.RoleID(),
		"can_buy_number", panel.CanBuyNumber,
		"state", panel.State,
		"god_id", panel.GodID,
		"total_count", panel.TotalCount,
		"xun_xian_ling", tokens,
	)

	// 优先收掉上次遗留的龙鱼；只领取当前奖励，不吹龙鱼。
	if panel.State > 0 {
		pickupFindImmortal(ctx, session, &prevAct)
	}
	if tokens <= 0 {
		slog.Info("喜从天降龙鱼令不足，跳过召唤新龙鱼", "role_id", session.RoleID())
		return
	}

	// 3. 串行召唤并直接领取龙鱼奖励，直到龙鱼令耗尽 (Mod 68, Act 2 + Act 4)。
	// 每枚龙鱼令至多支撑一次召唤，用初始数量做硬上限，防止异常回包导致死循环。
	initialTokens := tokens
	rounds := 0
	for tokens > 0 && rounds < int(initialTokens) {
		if err := ctx.Err(); err != nil {
			slog.Info("喜从天降任务已取消，提前退出", "role_id", session.RoleID(), "err", err)
			return
		}
		if jitter != nil {
			_ = jitter.Wait(ctx)
		}

		beforeTokens := tokens
		start, sErr := summonFindImmortalFish(ctx, session, prevAct)
		if sErr != nil {
			slog.Warn("喜从天降召唤龙鱼未收到回包或解析失败", "role_id", session.RoleID(), "err", sErr)
			return
		}
		prevAct = protocol.ActionIDFindImmortalStart

		// GOADING 表示服务端仍存在未领取的龙鱼 (例如上一轮熔断遗留)，
		// 先收取清空后再重试一次召唤。
		if start.Result == protocol.FindImmortalResultGoading {
			slog.Warn("喜从天降存在未领取的龙鱼，先收取清空后重试召唤", "role_id", session.RoleID())
			pickupFindImmortal(ctx, session, &prevAct)
			start, sErr = summonFindImmortalFish(ctx, session, prevAct)
			if sErr != nil {
				slog.Warn("喜从天降清空遗留龙鱼后重试召唤失败", "role_id", session.RoleID(), "err", sErr)
				return
			}
			prevAct = protocol.ActionIDFindImmortalStart
		}

		if start.Result != protocol.FindImmortalResultSuccess {
			logFindImmortalResult("召唤龙鱼", session.RoleID(), start.Result)
			break
		}

		slog.Info("喜从天降召唤龙鱼成功",
			"role_id", session.RoleID(),
			"state", start.State,
			"god_id", start.GodID,
			"remain_number", start.RemainNumber,
			"total_count", start.TotalCount,
		)

		// 线上抓包确认 start 回包中的 remain_number 是“剩余吹鱼次数”(随鱼随机，
		// 12:57 回包为 4 而同轮 Act 5 查询为 0)，与龙鱼令余量无关，不能作为
		// 循环依据；每轮召唤成功后统一回查 Act 5 取得权威余量。
		refreshed, refreshOK := queryFindImmortalTokens(ctx, session, prevAct)
		if refreshOK {
			prevAct = protocol.ActionIDFindImmortalGetXunXianLing
		}
		nextTokens, progressed := resolveFindImmortalRemain(beforeTokens, refreshed, refreshOK)
		if !progressed {
			slog.Warn("喜从天降召唤后龙鱼令未按预期递减，熔断退出",
				"role_id", session.RoleID(),
				"before_xun_xian_ling", beforeTokens,
				"queried_xun_xian_ling", refreshed,
				"query_ok", refreshOK,
			)
		}

		// 已成功召唤的龙鱼必须领取，避免回包异常时把龙鱼遗留在面板上。
		pickupFindImmortal(ctx, session, &prevAct)
		rounds++

		tokens = nextTokens
		if !progressed {
			break
		}
	}

	slog.Info("喜从天降画龙鱼流程完成",
		"role_id", session.RoleID(),
		"initial_xun_xian_ling", initialTokens,
		"rounds", rounds,
		"remain_xun_xian_ling", tokens,
	)
}

// summonFindImmortalFish 发送一次召唤龙鱼请求 (Mod 68, Act 2) 并解析业务结果码。
func summonFindImmortalFish(ctx context.Context, session *client.RoleSession, prevAct uint32) (*protocol.FindImmortalStartResult, error) {
	pkt := protocol.BuildFindImmortalStartPacket(prevAct)
	sCtx, sCancel := context.WithTimeout(ctx, 3*time.Second)
	defer sCancel()

	resp, err := session.Call(sCtx, pkt, protocol.ActionIDFindImmortalStart)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("召唤龙鱼未收到回包")
	}
	start, parseErr := protocol.ParseFindImmortalStartResult(resp.Payload)
	if parseErr != nil {
		return nil, parseErr
	}
	return start, nil
}

// pickupFindImmortal 收取当前龙鱼奖励 (Mod 68, Act 4)，不调用 any 元宝消耗接口。
func pickupFindImmortal(ctx context.Context, session *client.RoleSession, prevAct *uint32) {
	if err := ctx.Err(); err != nil {
		return
	}
	pickupPkt := protocol.BuildFindImmortalPickupAwardPacket(*prevAct)
	kCtx, kCancel := context.WithTimeout(ctx, 3*time.Second)
	kResp, kErr := session.Call(kCtx, pickupPkt, protocol.ActionIDFindImmortalPickupAward)
	kCancel()
	*prevAct = protocol.ActionIDFindImmortalPickupAward
	if kErr != nil || kResp == nil {
		slog.Warn("喜从天降收取龙鱼奖励未收到回包", "role_id", session.RoleID(), "err", kErr)
		return
	}
	pickup, err := protocol.ParseFindImmortalPickupResult(kResp.Payload)
	if err != nil {
		slog.Warn("解析喜从天降收取龙鱼奖励回包失败", "role_id", session.RoleID(), "err", err)
		return
	}
	if pickup.Result != protocol.FindImmortalResultSuccess {
		logFindImmortalResult("收取龙鱼奖励", session.RoleID(), pickup.Result)
		return
	}
	slog.Info("喜从天降收取龙鱼奖励成功",
		"role_id", session.RoleID(),
		"award_fame", pickup.Award.Fame,
		"award_skill", pickup.Award.Skill,
		"award_cultivation", pickup.Award.Cultivation,
		"award_state_point", pickup.Award.StatePoint,
		"award_yu_pai", pickup.Award.YuPai,
		"cultivation", pickup.Cultivation,
	)
}

// resolveFindImmortalRemain 依据召唤后的龙鱼令回查结果裁决本轮是否继续召唤。
//
// start_find_immortal 回包中的 remain_number 属于“剩余吹鱼次数”一类随鱼随机
// 的值，与龙鱼令余量无关 (线上抓包: 召唤前查询 1，召唤后回包 3，同轮回查 0)，
// 因此必须使用 get_xun_xian_ling_number 的权威回查结果。回查失败或余量未递减
// 时返回召唤前数量并停止继续召唤，交由调用方熔断退出。
func resolveFindImmortalRemain(before, refreshed int16, refreshedOK bool) (int16, bool) {
	if !refreshedOK || refreshed < 0 {
		return before, false
	}
	return refreshed, refreshed < before
}

// queryFindImmortalTokens 查询龙鱼令数量 (Mod 68, Act 5)。
func queryFindImmortalTokens(ctx context.Context, session *client.RoleSession, prevAct uint32) (int16, bool) {
	pkt := protocol.BuildFindImmortalGetXunXianLingPacket(prevAct)
	qCtx, qCancel := context.WithTimeout(ctx, 2*time.Second)
	resp, err := session.Call(qCtx, pkt, protocol.ActionIDFindImmortalGetXunXianLing)
	qCancel()
	if err != nil || resp == nil {
		slog.Warn("查询喜从天降龙鱼令数量未收到回包", "role_id", session.RoleID(), "err", err)
		return 0, false
	}
	tokens, err := protocol.ParseFindImmortalXunXianLingResult(resp.Payload)
	if err != nil {
		slog.Warn("解析喜从天降龙鱼令数量回包失败", "role_id", session.RoleID(), "err", err)
		return 0, false
	}
	return tokens, true
}

// NewFiveBlessingsRoutine 构造独立的「五福临门」免费祈福任务。
//
// 五福临门 (客户端 FindImmortal2) 与喜从天降共用 Module 68，但属于两个独立活动。
// 本任务硬编码只使用每日免费祈福次数：start_bless -> end_bless，
// 不调用 continue_bless (继续翻牌) 与 five_blessings_change_lucky (一键变金)，
// 因此不会消耗任何元宝。
func NewFiveBlessingsRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"five_blessings",
		"minigame",
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
			slog.Info("正在执行益智任务: 五福临门免费祈福", "role_id", session.RoleID())

			executeFiveBlessings(ctx, session, jitter)
			return nil
		},
	)
}

// executeFiveBlessings 执行五福临门免费次数与既有奖励领取闭环。
func executeFiveBlessings(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) {
	prevAct := session.LastActionID()

	// 1. 检查五福临门是否开启 (Mod 68, Act 14)
	flagPkt := protocol.BuildFindImmortalIsFiveBlessingsOpenPacket(prevAct)
	fCtx, fCancel := context.WithTimeout(ctx, 2*time.Second)
	fResp, fErr := session.Call(fCtx, flagPkt, protocol.ActionIDFindImmortalIsFiveBlessingsOpen)
	fCancel()
	if fErr != nil || fResp == nil {
		slog.Debug("查询五福临门开启状态未收到回包", "role_id", session.RoleID(), "err", fErr)
		return
	}
	open, err := protocol.ParseFiveBlessingsOpenFlagResult(fResp.Payload)
	if err != nil {
		slog.Warn("解析五福临门开启状态回包失败", "role_id", session.RoleID(), "err", err)
		return
	}
	if !open {
		slog.Debug("五福临门活动未开启", "role_id", session.RoleID())
		return
	}
	prevAct = protocol.ActionIDFindImmortalIsFiveBlessingsOpen

	// 2. 查询祈福面板 (Mod 68, Act 10)
	infoPkt := protocol.BuildFindImmortalOpenFiveBlessingsPacket(prevAct)
	iCtx, iCancel := context.WithTimeout(ctx, 3*time.Second)
	iResp, iErr := session.Call(iCtx, infoPkt, protocol.ActionIDFindImmortalOpenFiveBlessings)
	iCancel()
	if iErr != nil || iResp == nil {
		slog.Warn("查询五福临门面板未收到回包", "role_id", session.RoleID(), "err", iErr)
		return
	}
	info, err := protocol.ParseFiveBlessingsInfoResult(iResp.Payload)
	if err != nil {
		slog.Warn("解析五福临门面板回包失败", "role_id", session.RoleID(), "err", err)
		return
	}
	prevAct = protocol.ActionIDFindImmortalOpenFiveBlessings
	slog.Info("五福临门当前面板",
		"role_id", session.RoleID(),
		"bless_number", info.BlessNumber,
		"stage", info.Stage,
		"pending_awards", len(info.Awards),
		"total_count", info.TotalCount,
	)

	// 先领取上次遗留的祈福奖励。
	if len(info.Awards) > 0 {
		endFiveBlessings(ctx, session, prevAct, "领取遗留祈福奖励")
		prevAct = protocol.ActionIDFindImmortalEndBless
	}
	if info.BlessNumber <= 0 {
		slog.Info("五福临门祈福次数已用尽", "role_id", session.RoleID())
		return
	}

	// 3. 逐次开始祈福并立即领取 (Mod 68, Act 11 + Act 13)
	rounds := 0
	for info.BlessNumber > 0 {
		if err := ctx.Err(); err != nil {
			slog.Info("五福临门任务已取消，提前退出", "role_id", session.RoleID(), "err", err)
			return
		}
		if jitter != nil {
			_ = jitter.Wait(ctx)
		}

		startPkt := protocol.BuildFindImmortalStartBlessPacket(prevAct)
		sCtx, sCancel := context.WithTimeout(ctx, 3*time.Second)
		sResp, sErr := session.Call(sCtx, startPkt, protocol.ActionIDFindImmortalStartBless)
		sCancel()
		prevAct = protocol.ActionIDFindImmortalStartBless
		if sErr != nil || sResp == nil {
			slog.Warn("五福临门开始祈福请求未收到回包", "role_id", session.RoleID(), "err", sErr)
			return
		}
		start, err := protocol.ParseFiveBlessingsActionResult(sResp.Payload)
		if err != nil {
			slog.Warn("解析五福临门开始祈福回包失败", "role_id", session.RoleID(), "err", err)
			return
		}
		if start.Result != protocol.FindImmortalResultSuccess {
			if start.Result == protocol.FindImmortalResultAlreadyStart {
				// 服务端仍有一次未结算的祈福，先结束并领取，交由下一轮调度继续。
				endFiveBlessings(ctx, session, prevAct, "结算未完成祈福")
			} else {
				logFindImmortalResult("开始五福临门祈福", session.RoleID(), start.Result)
			}
			return
		}
		info.BlessNumber--
		rounds++

		endFiveBlessings(ctx, session, prevAct, "领取五福临门奖励")
		prevAct = protocol.ActionIDFindImmortalEndBless

		if info.BlessNumber > 0 && jitter != nil {
			_ = jitter.Wait(ctx)
		}
	}

	slog.Info("五福临门流程完成", "role_id", session.RoleID(), "rounds", rounds, "remain_bless_number", info.BlessNumber)
}

// endFiveBlessings 结束当前祈福并领取奖励 (Mod 68, Act 13)。
func endFiveBlessings(ctx context.Context, session *client.RoleSession, prevAct uint32, scene string) {
	endPkt := protocol.BuildFindImmortalEndBlessPacket(prevAct)
	eCtx, eCancel := context.WithTimeout(ctx, 3*time.Second)
	eResp, eErr := session.Call(eCtx, endPkt, protocol.ActionIDFindImmortalEndBless)
	eCancel()
	if eErr != nil || eResp == nil {
		slog.Warn("五福临门结束祈福请求未收到回包", "role_id", session.RoleID(), "scene", scene, "err", eErr)
		return
	}
	end, err := protocol.ParseFiveBlessingsEndResult(eResp.Payload)
	if err != nil {
		slog.Warn("解析五福临门结束祈福回包失败", "role_id", session.RoleID(), "scene", scene, "err", err)
		return
	}
	if end.Result != protocol.FindImmortalResultSuccess {
		logFindImmortalResult(scene, session.RoleID(), end.Result)
		return
	}
	slog.Info("五福临门奖励领取成功",
		"role_id", session.RoleID(),
		"scene", scene,
		"award_count", len(end.Awards),
	)
}

// logFindImmortalResult 输出喜从天降/五福临门业务失败码的中文语义。
func logFindImmortalResult(scene string, roleID string, result uint8) {
	switch result {
	case protocol.FindImmortalResultNotEnoughCoin:
		slog.Info("喜从天降铜钱不足", "role_id", roleID, "scene", scene, "result", result)
	case protocol.FindImmortalResultHaveNotNumber:
		slog.Info("喜从天降龙鱼不足", "role_id", roleID, "scene", scene, "result", result)
	case protocol.FindImmortalResultGoading:
		slog.Info("喜从天降正在吹龙鱼", "role_id", roleID, "scene", scene, "result", result)
	case protocol.FindImmortalResultNotEnoughIngot:
		slog.Info("喜从天降元宝不足", "role_id", roleID, "scene", scene, "result", result)
	case protocol.FindImmortalResultFailed:
		slog.Info("喜从天降吹龙鱼失败", "role_id", roleID, "scene", scene, "result", result)
	case protocol.FindImmortalResultHaveNotFind:
		slog.Info("喜从天降还未寻找到仙迹", "role_id", roleID, "scene", scene, "result", result)
	case protocol.FindImmortalResultNoEnoughFame:
		slog.Info("喜从天降声望不足", "role_id", roleID, "scene", scene, "result", result)
	case protocol.FindImmortalResultNotEnoughNumber:
		slog.Info("五福临门祈福次数不足", "role_id", roleID, "scene", scene, "result", result)
	case protocol.FindImmortalResultAlreadyStart:
		slog.Info("五福临门已经开始祈福", "role_id", roleID, "scene", scene, "result", result)
	case protocol.FindImmortalResultNotStart:
		slog.Info("五福临门尚未开始祈福", "role_id", roleID, "scene", scene, "result", result)
	default:
		slog.Warn("喜从天降/五福临门操作被服务端拒绝", "role_id", roleID, "scene", scene, "result", result)
	}
}

// NewAskFairyRoutine 构造每日问灵任务 (Module 503, Mod_AskFairy_Base)。
//
// 每日免费问灵一次；服务端下发随机奖励候选，优先选择元宝 (item_id=3345)，
// 否则选第一项。同时领取可领的回归奖励与问灵等级奖励。全程不消耗元宝。
func NewAskFairyRoutine() scheduler.ActivityRoutine {
	const callTimeout = 3 * time.Second
	return scheduler.NewBaseRoutine(
		"ask_fairy",
		"minigame",
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

			fetchInfo := func() (*protocol.AskFairyInfo, error) {
				cCtx, cancel := context.WithTimeout(ctx, callTimeout)
				resp, err := session.Call(cCtx, protocol.BuildAskFairyInfoPacket(), protocol.ActionIDAskFairyInfo)
				cancel()
				if err != nil {
					return nil, err
				}
				return protocol.ParseAskFairyInfoResult(resp.Payload)
			}
			callAction := func(pkt *protocol.Packet, actionID uint32, name string) (uint8, error) {
				cCtx, cancel := context.WithTimeout(ctx, callTimeout)
				resp, err := session.Call(cCtx, pkt, actionID)
				cancel()
				if err != nil {
					return 0, err
				}
				result, pErr := protocol.ParseAskFairyActionResult(resp.Payload)
				if pErr != nil {
					return 0, pErr
				}
				_ = name
				return result, nil
			}

			info, err := fetchInfo()
			if err != nil {
				return fmt.Errorf("查询问灵状态失败: %w", err)
			}
			if info.LeftDay <= 0 {
				slog.Info("问灵活动已结束，跳过", "role_id", session.RoleID(), "left_day", info.LeftDay)
				return nil
			}
			slog.Info("问灵状态",
				"role_id", session.RoleID(),
				"has_gain_award", info.HasGainAward,
				"max_ask_num", info.MaxAskNum,
				"has_ask_num", info.HasAskNum,
				"cur_level", info.CurLevel,
				"left_day", info.LeftDay,
				"random_awards", info.RandomAwards,
			)

			// 回归奖励 (已领过返回 HAS_GAIN_RECALL=2)
			if info.HasGainAward == 0 {
				result, err := callAction(protocol.BuildAskFairyRecallAwardPacket(), protocol.ActionIDAskFairyRecallAward, "回归奖励")
				if err != nil {
					slog.Warn("领取回归奖励失败", "role_id", session.RoleID(), "err", err)
				} else if result == protocol.AskFairyResultPackFull {
					return scheduler.ErrBagFull
				} else {
					slog.Info("回归奖励领取结果", "role_id", session.RoleID(), "result", result)
				}
			}

			// 问灵等级奖励 (登录天数不足返回 9，属正常)
			if result, err := callAction(protocol.BuildAskFairyLevelAwardPacket(), protocol.ActionIDAskFairyLevelAward, "问灵等级奖励"); err != nil {
				slog.Warn("领取问灵等级奖励失败", "role_id", session.RoleID(), "err", err)
			} else if result == protocol.AskFairyResultSuccess {
				slog.Info("问灵等级奖励领取成功", "role_id", session.RoleID(), "cur_level", info.CurLevel)
			} else {
				slog.Debug("问灵等级奖励未领取", "role_id", session.RoleID(), "result", result, "cur_level", info.CurLevel)
			}

			// 每日问灵
			if info.HasAskNum >= info.MaxAskNum {
				slog.Info("今日问灵次数已用尽", "role_id", session.RoleID(), "has_ask", info.HasAskNum, "max_ask", info.MaxAskNum)
				return nil
			}
			result, err := callAction(protocol.BuildAskFairyDoPacket(), protocol.ActionIDAskFairyDo, "问灵")
			if err != nil {
				return fmt.Errorf("问灵请求失败: %w", err)
			}
			switch result {
			case protocol.AskFairyResultSuccess:
				slog.Info("问灵成功", "role_id", session.RoleID())
			case protocol.AskFairyResultHasAsk, protocol.AskFairyResultCountLimit:
				slog.Info("今日问灵次数已用尽", "role_id", session.RoleID(), "result", result)
				return nil
			case protocol.AskFairyResultPackFull, protocol.AskFairyResultLinkFatePackFull:
				return scheduler.ErrBagFull
			default:
				slog.Warn("问灵失败", "role_id", session.RoleID(), "result", result)
				return nil
			}

			// 重新拉取随机奖励候选，优先选择元宝
			info, err = fetchInfo()
			if err != nil {
				return fmt.Errorf("问灵后刷新状态失败: %w", err)
			}
			if len(info.RandomAwards) == 0 {
				slog.Warn("问灵后未下发随机奖励候选", "role_id", session.RoleID())
				return nil
			}
			chosen := info.RandomAwards[0]
			for _, a := range info.RandomAwards {
				if a.ItemID == 3345 { // 元宝
					chosen = a
					break
				}
			}
			result, err = callAction(protocol.BuildAskFairyGainPacket(chosen.AwardID), protocol.ActionIDAskFairyGain, "问灵奖励")
			if err != nil {
				return fmt.Errorf("领取问灵奖励失败: %w", err)
			}
			switch result {
			case protocol.AskFairyResultSuccess:
				slog.Info("问灵奖励领取成功",
					"role_id", session.RoleID(),
					"award_id", chosen.AwardID,
					"item_id", chosen.ItemID,
					"item_count", chosen.ItemCount,
				)
			case protocol.AskFairyResultPackFull, protocol.AskFairyResultLinkFatePackFull:
				return scheduler.ErrBagFull
			default:
				slog.Info("问灵奖励未领取", "role_id", session.RoleID(), "result", result)
			}
			return nil
		},
	)
}

// NewGoldenTouchStoneRoutine 构造点金石任务 (Module 551, Mod_GoldenTouchStone_Base)。
//
// 先查询点金石状态：待初始化时初始化；待分享/帮助不足时跳过；可点化时读取面板并点化一次。
// 不消耗元宝。
func NewGoldenTouchStoneRoutine() scheduler.ActivityRoutine {
	const callTimeout = 3 * time.Second
	return scheduler.NewBaseRoutine(
		"golden_touch_stone",
		"minigame",
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

			call := func(pkt *protocol.Packet, act uint32) (*protocol.Packet, error) {
				cCtx, cancel := context.WithTimeout(ctx, callTimeout)
				defer cancel()
				return session.Call(cCtx, pkt, act)
			}

			statusResp, err := call(protocol.BuildGoldenTouchStoneStatusPacket(), protocol.ActionIDGoldenTouchStoneStatus)
			if err != nil || statusResp == nil || len(statusResp.Payload) == 0 {
				slog.Info("点金石功能未开放/不可用，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			status, err := protocol.ParseGoldenTouchStoneStatusResult(statusResp.Payload)
			if err != nil {
				return fmt.Errorf("解析点金石状态失败: %w", err)
			}

			if status == protocol.GoldenTouchStoneStatusInit {
				initResp, iErr := call(protocol.BuildGoldenTouchStoneInitPacket(), protocol.ActionIDGoldenTouchStoneInit)
				if iErr != nil || initResp == nil || len(initResp.Payload) == 0 {
					slog.Warn("点金石初始化无应答", "role_id", session.RoleID(), "err", iErr)
					return nil
				}
				slog.Info("点金石初始化成功", "role_id", session.RoleID())
				statusResp, err = call(protocol.BuildGoldenTouchStoneStatusPacket(), protocol.ActionIDGoldenTouchStoneStatus)
				if err != nil || statusResp == nil || len(statusResp.Payload) == 0 {
					return nil
				}
				status, err = protocol.ParseGoldenTouchStoneStatusResult(statusResp.Payload)
				if err != nil {
					return nil
				}
			}

			switch status {
			case protocol.GoldenTouchStoneStatusShare:
				slog.Info("点金石当前待分享/帮助，跳过（需好友协助）", "role_id", session.RoleID())
				return nil
			case protocol.GoldenTouchStoneStatusTouch:
				// 继续
			default:
				slog.Info("点金石状态未知，跳过", "role_id", session.RoleID(), "status", status)
				return nil
			}

			panelResp, err := call(protocol.BuildGoldenTouchStoneTouchPanelPacket(), protocol.ActionIDGoldenTouchStoneTouchPanel)
			if err != nil || panelResp == nil {
				slog.Info("点金石点化面板无应答，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			if panel, pErr := protocol.ParseGoldenTouchStoneTouchPanelResult(panelResp.Payload); pErr == nil {
				slog.Info("点金石面板",
					"role_id", session.RoleID(),
					"level", panel.Level,
					"exp", panel.Exp,
					"this_week_luck", panel.ThisWeekLuckValue,
					"today_touch_num", panel.TodayTouchNum,
				)
			}

			touchResp, err := call(protocol.BuildGoldenTouchStoneTouchPacket(), protocol.ActionIDGoldenTouchStoneTouch)
			if err != nil || touchResp == nil {
				slog.Warn("点金石点化无应答", "role_id", session.RoleID(), "err", err)
				return nil
			}
			result, exp, luck, pErr := protocol.ParseGoldenTouchStoneTouchResult(touchResp.Payload)
			if pErr != nil {
				slog.Warn("解析点金石点化回包失败", "role_id", session.RoleID(), "err", pErr)
				return nil
			}
			switch result {
			case protocol.GoldenTouchStoneResultSuccess:
				slog.Info("点金石点化成功", "role_id", session.RoleID(), "exp", exp, "luck", luck)
			case protocol.GoldenTouchStoneResultNotEnoughHelp:
				slog.Info("点金石帮助人数不足，跳过", "role_id", session.RoleID())
			case protocol.GoldenTouchStoneResultNotEnoughCount:
				slog.Info("点金石今日次数已用尽", "role_id", session.RoleID())
			case protocol.GoldenTouchStoneResultHasHelp:
				slog.Info("点金石已帮助过", "role_id", session.RoleID())
			default:
				slog.Info("点金石点化未成功", "role_id", session.RoleID(), "result", result)
			}
			return nil
		},
	)
}

// NewForbiddenBookRoutine 构造禁地之书探索任务 (Module 199, Mod_Awake_Base)。
//
// 优先使用各区域免费探索次数；免费次数用尽且仅需铜钱时，按铜钱兑换少量次数继续探索。
// 永不使用元宝兑换；功能未开放/元宝付费路径直接跳过。
func NewForbiddenBookRoutine() scheduler.ActivityRoutine {
	const callTimeout = 3 * time.Second
	return scheduler.NewBaseRoutine(
		"forbidden_book",
		"minigame",
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

			call := func(pkt *protocol.Packet, act uint32) (*protocol.Packet, error) {
				cCtx, cancel := context.WithTimeout(ctx, callTimeout)
				defer cancel()
				return session.Call(cCtx, pkt, act)
			}
			fetchInfo := func() (*protocol.ForbiddenBookInfo, error) {
				resp, err := call(protocol.BuildForbiddenBookInfoPacket(), protocol.ActionIDForbiddenBookInfo)
				if err != nil {
					return nil, err
				}
				return protocol.ParseForbiddenBookInfoResult(resp.Payload)
			}

			info, err := fetchInfo()
			if err != nil || info == nil || len(info.Areas) == 0 {
				slog.Info("禁地之书功能未开放/无可用区域，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			slog.Info("禁地之书面板",
				"role_id", session.RoleID(),
				"areas", len(info.Areas),
				"consum_score", info.ConsumScore,
			)

			explored := 0
			exchanges := 0
			exploreArea := func(area protocol.ForbiddenBookArea) {
				for area.CurNumber > 0 && explored < 30 {
					resp, eErr := call(protocol.BuildForbiddenBookExplorePacket(area.ForbiddenType, area.ExploreType), protocol.ActionIDForbiddenBookExplore)
					if eErr != nil {
						slog.Warn("禁地之书探索无应答", "role_id", session.RoleID(), "type", area.ForbiddenType, "err", eErr)
						return
					}
					result, pErr := protocol.ParseForbiddenBookExploreResult(resp.Payload)
					if pErr != nil {
						slog.Warn("解析禁地之书探索回包失败", "role_id", session.RoleID(), "err", pErr)
						return
					}
					if result != protocol.AwakeResultSuccess {
						slog.Info("禁地之书探索未成功", "role_id", session.RoleID(), "type", area.ForbiddenType, "result", result)
						return
					}
					explored++
					area.CurNumber--
					slog.Info("禁地之书探索成功", "role_id", session.RoleID(), "type", area.ForbiddenType, "remaining", area.CurNumber)
					select {
					case <-ctx.Done():
						return
					case <-time.After(200 * time.Millisecond):
					}
				}
			}

			// 1. 免费次数优先。
			for _, area := range info.Areas {
				exploreArea(area)
			}

			// 2. 免费次数用尽后，仅兑换纯铜钱档位的探索次数。
			// 线上 s0 实测: Act 13 返回 18/19/20 号区域价格，其中 19/20 号
			// cost_ingot=0、cost_coin=50/100；18 号同时需要元宝，自动化严格跳过。
			exResp, exErr := call(protocol.BuildForbiddenBookExchangeInfoPacket(), protocol.ActionIDForbiddenBookExchangeInfo)
			if exErr != nil || exResp == nil {
				slog.Info("禁地之书兑换价格不可用，跳过铜钱兑换", "role_id", session.RoleID(), "err", exErr)
				slog.Info("禁地之书巡检完成", "role_id", session.RoleID(), "explored", explored, "coin_exchanges", exchanges)
				return nil
			}
			priceList, pErr := protocol.ParseForbiddenBookExchangeInfoResult(exResp.Payload)
			if pErr != nil {
				slog.Warn("解析禁地之书兑换价格失败", "role_id", session.RoleID(), "err", pErr)
				slog.Info("禁地之书巡检完成", "role_id", session.RoleID(), "explored", explored, "coin_exchanges", exchanges)
				return nil
			}

			for _, price := range priceList {
				if price.CostIngot > 0 || price.CostCoin <= 0 || price.Count <= 0 {
					continue
				}
				exchangedThisType := 0
				for i := int16(0); i < price.Count && exchanges < 10; i++ {
					if err := ctx.Err(); err != nil {
						return nil
					}
					coins := session.GetPlayerState().Coins
					if coins < int64(price.CostCoin) {
						slog.Info("铜钱不足，停止禁地之书兑换", "role_id", session.RoleID(),
							"coins", coins, "cost_coin", price.CostCoin)
						break
					}
					resp, xErr := call(protocol.BuildForbiddenBookExchangePacket(price.ForbiddenType), protocol.ActionIDForbiddenBookExchange)
					if xErr != nil {
						slog.Warn("禁地之书铜钱兑换无应答", "role_id", session.RoleID(), "type", price.ForbiddenType, "err", xErr)
						break
					}
					result, pErr := protocol.ParseForbiddenBookExchangeResult(resp.Payload)
					if pErr != nil {
						slog.Warn("解析禁地之书铜钱兑换回包失败", "role_id", session.RoleID(), "err", pErr)
						break
					}
					if result != protocol.AwakeResultSuccess {
						slog.Info("禁地之书铜钱兑换未成功", "role_id", session.RoleID(),
							"type", price.ForbiddenType, "result", result)
						break
					}
					exchanges++
					exchangedThisType++
					session.UpdatePlayerState(func(ps *client.PlayerState) {
						ps.Coins -= int64(price.CostCoin)
					})
					slog.Info("禁地之书铜钱兑换成功", "role_id", session.RoleID(),
						"type", price.ForbiddenType, "cost_coin", price.CostCoin)
				}

				// 兑换次数不会体现在 panel 的 cur_number 上（线上实测兑换后仍为 0），
				// 必须主动下发 explore 请求使用兑换次数，由服务端返回结果码熔断。
				refreshed, rErr := fetchInfo()
				if rErr != nil || refreshed == nil {
					continue
				}
				var target protocol.ForbiddenBookArea
				found := false
				for _, area := range refreshed.Areas {
					if area.ForbiddenType != price.ForbiddenType {
						continue
					}
					// 兑换得到的次数只能用于不消耗元宝的探索入口。
					// 线上实测多次探索入口 cost_ingot>0，误用会直接扣元宝；
					// 因此这里严格要求 cost_ingot=0，并优先单次探索。
					if area.CostIngot != 0 {
						continue
					}
					if !found || area.ExploreType == protocol.ForbiddenBookOnce {
						target = area
						found = true
					}
				}
				if !found {
					slog.Info("禁地之书兑换区域无免元宝探索入口，跳过探索", "role_id", session.RoleID(),
						"type", price.ForbiddenType)
					continue
				}
				for n := 0; n < exchangedThisType && explored < 30; n++ {
					if err := ctx.Err(); err != nil {
						return nil
					}
					eResp, eErr := call(
						protocol.BuildForbiddenBookExplorePacket(target.ForbiddenType, target.ExploreType),
						protocol.ActionIDForbiddenBookExplore,
					)
					if eErr != nil {
						slog.Warn("禁地之书兑换次数探索无应答", "role_id", session.RoleID(), "type", target.ForbiddenType, "err", eErr)
						break
					}
					eResult, pErr := protocol.ParseForbiddenBookExploreResult(eResp.Payload)
					if pErr != nil {
						slog.Warn("解析禁地之书兑换次数探索回包失败", "role_id", session.RoleID(), "err", pErr)
						break
					}
					if eResult != protocol.AwakeResultSuccess {
						slog.Info("禁地之书兑换次数探索未成功", "role_id", session.RoleID(),
							"type", target.ForbiddenType, "result", eResult)
						break
					}
					explored++
					slog.Info("禁地之书兑换次数探索成功", "role_id", session.RoleID(), "type", target.ForbiddenType)
					select {
					case <-ctx.Done():
						return nil
					case <-time.After(200 * time.Millisecond):
					}
				}
			}

			slog.Info("禁地之书巡检完成", "role_id", session.RoleID(), "explored", explored, "coin_exchanges", exchanges)
			return nil
		},
	)
}
