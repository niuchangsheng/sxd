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
			rCtx, rCancel := context.WithTimeout(ctx, 8*time.Second)
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
			infoCtx, infoCancel := context.WithTimeout(ctx, 8*time.Second)
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
				rollCtx, rollCancel := context.WithTimeout(ctx, 8*time.Second)
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

			infoCtx, infoCancel := context.WithTimeout(ctx, 8*time.Second)
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

			// fetchQuestion 拉取当前奇缘题目并校验业务结果码。
			fetchQuestion := func() (*protocol.FateQuestionResult, error) {
				questionPkt, err := protocol.BuildFateQuestionPacket(protocol.FateQuestionRequest{
					PrevAct: protocol.ActionIDFateGetInfo,
				})
				if err != nil {
					return nil, fmt.Errorf("构造仙旅奇缘题目查询封包失败: %w", err)
				}
				qCtx, qCancel := context.WithTimeout(ctx, 8*time.Second)
				defer qCancel()
				respQPkt, err := session.Call(qCtx, questionPkt, protocol.ActionIDFateGetQuestion)
				if err != nil {
					return nil, fmt.Errorf("拉取仙旅奇缘题目失败: %w", err)
				}
				qRes, err := protocol.ParseFateQuestionResult(respQPkt.Payload)
				if err != nil {
					return nil, fmt.Errorf("解析仙旅奇缘题目回包失败: %w", err)
				}
				return qRes, nil
			}
			// rejected 统一记录题目拉取失败语义，调用方随后熔断退出。
			rejected := func(qRes *protocol.FateQuestionResult) {
				switch qRes.Msg {
				case protocol.FateResultCDTime:
					slog.Info("仙旅奇缘事件冷却中，本轮跳过", "role_id", session.RoleID())
				case protocol.FateResultReachMaxCount:
					slog.Info("今日仙旅奇缘次数已耗尽", "role_id", session.RoleID())
				default:
					slog.Warn("仙旅奇缘事件拉取被服务端拒绝", "role_id", session.RoleID(), "result", qRes.Msg)
				}
			}

			rounds := int(remaining)
			for i := 0; i < rounds; i++ {
				if ctx.Err() != nil {
					slog.Info("仙旅奇缘已取消，提前退出", "role_id", session.RoleID())
					return nil
				}

				// 3. 发送拉取当前奇缘事件题目请求
				qRes, err := fetchQuestion()
				if err != nil {
					return err
				}
				if qRes.Msg != protocol.FateResultSuccess {
					rejected(qRes)
					return nil
				}

				// 3.1 每答满一轮 (通常 5 次) 会生成答题宝箱；未开箱前服务端
				// 会静默拒绝新的答案请求 (不回包直至超时)，因此必须先免费开箱。
				for boxAttempts := 0; qRes.TolAnswerTimes > 0 && qRes.CurAnswerTimes >= qRes.TolAnswerTimes; boxAttempts++ {
					if boxAttempts >= 3 {
						slog.Warn("仙旅奇缘答题宝箱开启次数超限，熔断退出",
							"role_id", session.RoleID(),
							"cur_answer_times", qRes.CurAnswerTimes,
							"tol_answer_times", qRes.TolAnswerTimes,
						)
						return nil
					}
					drawPkt, err := protocol.BuildFateLotteryDrawPacket(protocol.ActionIDFateGetQuestion)
					if err != nil {
						return fmt.Errorf("构造仙旅奇缘开宝箱封包失败: %w", err)
					}
					drawCtx, drawCancel := context.WithTimeout(ctx, 8*time.Second)
					drawRespPkt, err := session.Call(drawCtx, drawPkt, protocol.ActionIDFateLotteryDraw)
					drawCancel()
					if err != nil {
						return fmt.Errorf("开启仙旅奇缘答题宝箱失败: %w", err)
					}
					drawRes, err := protocol.ParseFateLotteryDrawResult(drawRespPkt.Payload)
					if err != nil {
						return fmt.Errorf("解析仙旅奇缘开宝箱回包失败: %w", err)
					}
					if drawRes.Msg != protocol.FateResultSuccess {
						slog.Warn("仙旅奇缘答题宝箱开启被服务端拒绝",
							"role_id", session.RoleID(), "result", drawRes.Msg)
						return nil
					}
					slog.Info("仙旅奇缘答题宝箱已开启",
						"role_id", session.RoleID(),
						"awards", formatFateDrawItems(drawRes.Awards),
					)
					qRes, err = fetchQuestion()
					if err != nil {
						return err
					}
					if qRes.Msg != protocol.FateResultSuccess {
						rejected(qRes)
						return nil
					}
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

				ansCtx, ansCancel := context.WithTimeout(ctx, 8*time.Second)
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

// formatFateDrawItems 将答题宝箱道具奖励格式化为便于排查的日志文本。
func formatFateDrawItems(items []protocol.FateDrawItem) string {
	if len(items) == 0 {
		return "无"
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, fmt.Sprintf("%d×%d", item.ItemID, item.Num))
	}
	return strings.Join(parts, ",")
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
	const callTimeout = 8 * time.Second
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
	const callTimeout = 8 * time.Second
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

			// 0. 功能开放检查：未开放时服务端会静默丢弃钓鱼请求。
			open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDFishing)
			if fErr != nil {
				slog.Info("查询钓鱼功能开放状态失败，跳过", "role_id", session.RoleID(), "err", fErr)
				return nil
			}
			if !open {
				slog.Info("钓鱼功能未开放，跳过", "role_id", session.RoleID(),
					"function_id", protocol.FunctionIDFishing)
				return nil
			}

			// 1. 查询鱼钩数量；为 0 时说明免费次数已用尽。
			hCtx, hCancel := context.WithTimeout(ctx, callTimeout)
			hooksResp, err := session.Call(hCtx, protocol.BuildFishHooksPacket(), protocol.ActionIDFishHooks)
			hCancel()
			if err != nil {
				// 功能已开放但查询无回包，通常是服务端状态/场景不满足；跳过本轮。
				slog.Info("查询鱼钩数量失败/服务端未响应，跳过", "role_id", session.RoleID(), "err", err)
				return nil
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
	const callTimeout = 8 * time.Second
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
	const callTimeout = 8 * time.Second
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
	const callTimeout = 8 * time.Second
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

				sCtx, sCancel := context.WithTimeout(ctx, 8*time.Second)
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
			sCtx, sCancel := context.WithTimeout(ctx, 8*time.Second)
			resp, err := session.Call(sCtx, statePkt, protocol.ActionIDEraTravelGetState)
			sCancel()

			if err != nil || resp == nil || len(resp.Payload) == 0 {
				slog.Info("山河游历功能未开放/不可用，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}

			// 2. 掷骰移动 (Mod 850, Act 0)
			rollPkt := protocol.BuildEraTravelRollDicePacket(1, protocol.ActionIDEraTravelGetState)
			rCtx, rCancel := context.WithTimeout(ctx, 8*time.Second)
			_, _ = session.Call(rCtx, rollPkt, protocol.ActionIDEraTravelRollDice)
			rCancel()

			// 3. 奇遇画卷事件结算 (Mod 635, Act 0)
			eventPkt := protocol.NewPacket(protocol.ActionIDEraEventSettle, protocol.NewWriter().WriteUint32(protocol.ActionIDEraTravelRollDice).Bytes())
			eCtx, eCancel := context.WithTimeout(ctx, 8*time.Second)
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
			sCtx, sCancel := context.WithTimeout(ctx, 8*time.Second)
			resp, err := session.Call(sCtx, statusPkt, protocol.ActionIDLingDaoGetStatus)
			sCancel()

			if err != nil || resp == nil || len(resp.Payload) == 0 {
				slog.Info("灵道初解功能未开放/不可用，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}

			// 2. 提交初解任务领悟 (Mod 711, Act 0)
			compPkt := protocol.BuildLingDaoComprehendPacket(protocol.ActionIDLingDaoGetStatus)
			cCtx, cCancel := context.WithTimeout(ctx, 8*time.Second)
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
			pCtx, pCancel := context.WithTimeout(ctx, 8*time.Second)
			panelResp, pErr := session.Call(pCtx, panelPkt, protocol.ActionIDHuanshouPanel)
			pCancel()
			if pErr != nil || panelResp == nil || len(panelResp.Payload) == 0 {
				slog.Info("山海幻兽功能未开放/不可用，跳过", "role_id", session.RoleID(), "err", pErr)
				return nil
			}

			// 2. 选定入侵章节并挑战 (Mod 696, Act 1 & 2)
			chapPkt := protocol.NewPacket(protocol.ActionIDHuanshouInvadeChapter, protocol.NewWriter().WriteUint32(protocol.ActionIDHuanshouPanel).Bytes())
			cCtx, cCancel := context.WithTimeout(ctx, 8*time.Second)
			_, _ = session.Call(cCtx, chapPkt, protocol.ActionIDHuanshouInvadeChapter)
			cCancel()

			chalPkt := protocol.BuildHuanshouInvadeChallengePacket(protocol.ActionIDHuanshouInvadeChapter)
			chCtx, chCancel := context.WithTimeout(ctx, 8*time.Second)
			_, _ = session.Call(chCtx, chalPkt, protocol.ActionIDHuanshouInvadeChallenge)
			chCancel()

			// 3. 极速秒完成扫荡 (Mod 696, Act 3)
			sweepPkt := protocol.BuildHuanshouInvadeSweepPacket(protocol.ActionIDHuanshouInvadeChallenge)
			swCtx, swCancel := context.WithTimeout(ctx, 8*time.Second)
			_, _ = session.Call(swCtx, sweepPkt, protocol.ActionIDHuanshouInvadeSweep)
			swCancel()

			// 4. 逐关扫荡入侵节点 (Mod 696, Act 4: 节点 1~8 依据抓包实采流)
			prevAct := protocol.ActionIDHuanshouInvadeSweep
			var clearedNodes int
			for nodeID := int32(1); nodeID <= 8; nodeID++ {
				nodePkt := protocol.BuildHuanshouInvadeNodePacket(nodeID, prevAct)
				nCtx, nCancel := context.WithTimeout(ctx, 8*time.Second)
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
		// 冥想是每日活跃度的高权重大头 (每次 +10)，提升优先级确保尽早执行。
		scheduler.PriorityHigh,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			// 0. 功能开放检查：使用 Mod 0 Act 6 的会话缓存 (一次查询覆盖整轮日常)。
			// 逐功能的 check_function_open 在部分状态下会被服务端静默丢弃，不能再作为唯一判定。
			open, chkErr := session.IsFunctionOpen(ctx, protocol.FunctionIDWuFaField)
			if chkErr != nil {
				slog.Info("查询悟法领域功能开放状态失败，继续尝试面板查询",
					"role_id", session.RoleID(), "err", chkErr)
			} else if !open {
				slog.Info("悟法领域功能未开启，跳过", "role_id", session.RoleID())
				return nil
			}
			slog.Info("正在执行益智任务: 悟法领域技能感悟与领悟", "role_id", session.RoleID())

			// 1. 查询悟法领域面板 (Mod 658, Act 0)。
			// 官方面板回包没有 result_code，只能通过结构完整性校验判断是否可信。
			infoPkt := protocol.BuildWuFaFieldInfoPacket(protocol.ActionIDTownEnter)
			iCtx, iCancel := context.WithTimeout(ctx, wuFaCallTimeout)
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

			// 2. 读取每日活跃度，确认「悟法领域冥想」任务还有多少次可计入，
			//    避免免费次数/冥想道具在任务满额后被白白消耗。
			actCtx, actCancel := context.WithTimeout(ctx, wuFaCallTimeout)
			actResp, actErr := session.Call(actCtx, protocol.BuildGameAssistantInfoPacket(), protocol.ActionIDGameAssistantInfo)
			actCancel()
			if actErr != nil || actResp == nil {
				slog.Warn("查询每日活跃度失败，跳过悟法领域抽卡",
					"role_id", session.RoleID(), "err", actErr)
				return nil
			}
			actPanel, actErr := protocol.ParseGameAssistantInfo(actResp.Payload)
			if actErr != nil {
				return fmt.Errorf("解析每日活跃度失败: %w", actErr)
			}
			remain := actPanel.RemainingTimesFor(wuFaDailyTaskMeditation)
			if remain <= 0 {
				slog.Info("悟法领域冥想活跃度已完成，跳过抽卡", "role_id", session.RoleID())
				return nil
			}
			if remain > maxWuFaDrawsPerRun {
				remain = maxWuFaDrawsPerRun
			}

			// 3. 逐次感悟：优先免费次数，其次使用已持有的冥想道具。
			//    线上实测道具映射：field 3 <- 冥想书卷 8641，field 1 <- 冥想竹简 8358，
			//    field 7 <- 冥想玄简 8700。全部为非元宝道具，绝不触发元宝档。
			for draw := 0; draw < remain; draw++ {
				if ctx.Err() != nil {
					return nil
				}
				fieldType, drawType, ok := selectWuFaFreeDraw(panel)
				if !ok {
					fieldType, drawType, ok = selectWuFaItemDraw(panel)
					if !ok {
						slog.Info("悟法领域无免费次数且无冥想道具，停止抽卡",
							"role_id", session.RoleID(),
							"rest_times", panel.RestTimes,
						)
						return nil
					}
				}

				drawPkt := protocol.BuildWuFaDrawCardPacket(fieldType, drawType)
				dCtx, dCancel := context.WithTimeout(ctx, wuFaCallTimeout)
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

				// 刷新面板，确保下一轮只使用真实剩余的免费次数或道具。
				refreshPkt := protocol.BuildWuFaFieldInfoPacket(protocol.ActionIDWuFaDrawCard)
				rCtx, rCancel := context.WithTimeout(ctx, wuFaCallTimeout)
				refreshResp, rErr := session.Call(rCtx, refreshPkt, protocol.ActionIDWuFaFieldInfo)
				rCancel()
				if rErr != nil || refreshResp == nil {
					return nil
				}
				refreshed, rErr := protocol.ParseWuFaPanel(refreshResp.Payload)
				if rErr != nil {
					return nil
				}
				panel = refreshed
			}
			return nil
		},
	)
}

// 悟法领域冥想相关常量。
const (
	// wuFaCallTimeout 悟法领域单次协议调用超时。实测网关响应在 2s 上下波动，
	// 过短的超时会把"功能已开放"误判为静默丢弃，导致例程整轮跳过。
	wuFaCallTimeout = 8 * time.Second
	// wuFaDailyTaskMeditation 每日活跃度任务「每日悟法领域冥想」编号
	wuFaDailyTaskMeditation int32 = 69
	// wuFaMeditationBookItem 冥想书卷 (对应 field_type 3)
	wuFaMeditationBookItem int32 = 8641
	// wuFaMeditationBambooItem 冥想竹简 (对应 field_type 1)
	wuFaMeditationBambooItem int32 = 8358
	// wuFaMeditationXuanItem 冥想玄简 (对应 field_type 7)
	wuFaMeditationXuanItem int32 = 8700
	// maxWuFaDrawsPerRun 单轮最多感悟次数 (每日任务上限 20)
	maxWuFaDrawsPerRun = 20
)

// wuFaItemFields 冥想道具与秘馆类型映射 (线上实测)。
var wuFaItemFields = []struct {
	fieldType int32
	itemID    int32
}{
	{3, wuFaMeditationBookItem},
	{1, wuFaMeditationBambooItem},
	{7, wuFaMeditationXuanItem},
}

// selectWuFaItemDraw 选择可用冥想道具对应的秘馆，优先书卷→竹简→玄简。
func selectWuFaItemDraw(panel *protocol.WuFaPanel) (int32, uint8, bool) {
	for _, m := range wuFaItemFields {
		if hasWuFaItem(panel, m.itemID) {
			return m.fieldType, protocol.WuFaDrawOne, true
		}
	}
	return 0, 0, false
}

// hasWuFaItem 判断面板资源中指定道具是否有余量。
func hasWuFaItem(panel *protocol.WuFaPanel, itemID int32) bool {
	if panel == nil {
		return false
	}
	for _, item := range panel.Items {
		if item.ItemID == itemID {
			return item.ItemNum > 0
		}
	}
	return false
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

// MasterPracticeConfig 宗师修行策略配置。
type MasterPracticeConfig struct {
	// DictRepo 静态数据字典，用于把主角/伙伴 role_id 映射为职业 JobId。
	DictRepo dictionary.Repository
	// PracticeOrder 期望修行的职业 JobId 顺序。服务端要求整队同职业；
	// 切换主角神器绝技 (Mod 85 Act 3，映射来自 Act 5) 会改写主角当前 role_id/职业，
	// 因此可依次切换职业并上阵同职业伙伴，逐个方向修行。
	PracticeOrder []int32
	// MaxStarsPerJob 单方向累计星数上限 (默认 30)。
	MaxStarsPerJob int32
	// MaxChallengesPerDay 每日挑战次数上限 (默认 3，线上实测每天 3 次)。
	MaxChallengesPerDay int32
	// AutoNormalizeDeploy 是否自动下阵异职业伙伴、上阵同职业伙伴 (默认 true)。
	AutoNormalizeDeploy bool
	// AllowSwitchStunt 是否允许通过切换主角神器绝技来转换职业 (默认 true)。
	AllowSwitchStunt bool
	// AllowStepUp 是否在挑战后自动升阶 (默认 true)。
	AllowStepUp bool
	// MaxRetriesPerStar 单个星级挑战失败后的重试次数 (默认 2)。
	MaxRetriesPerStar int
	// SwitchJobOnFailure 当前职业重试耗尽仍失败时，是否切换到其他可修行职业 (默认 true)。
	SwitchJobOnFailure bool
}

// DefaultMasterPracticeConfig 返回宗师修行默认策略配置。
func DefaultMasterPracticeConfig() MasterPracticeConfig {
	return MasterPracticeConfig{
		MaxStarsPerJob:      30,
		MaxChallengesPerDay: 3,
		AutoNormalizeDeploy: true,
		AllowSwitchStunt:    true,
		AllowStepUp:         true,
		MaxRetriesPerStar:   2,
		SwitchJobOnFailure:  true,
	}
}

// NewMasterPracticeRoutine 构造宗师修行挑战与突破任务 (Feature 15: 宗师修行)。
func NewMasterPracticeRoutine(cfgs ...MasterPracticeConfig) scheduler.ActivityRoutine {
	cfg := DefaultMasterPracticeConfig()
	if len(cfgs) > 0 {
		cfg = cfgs[0]
		if cfg.MaxStarsPerJob <= 0 {
			cfg.MaxStarsPerJob = 30
		}
		if cfg.MaxChallengesPerDay <= 0 {
			cfg.MaxChallengesPerDay = 3
		}
	}
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

			// 2. 查询神器绝技映射 (职业 -> 已激活绝技) 与当前布阵。
			//   主角通过切换神器绝技转换职业 (Mod 85 Act 3)，映射来自 Mod 85 Act 5；
			//   Mod 85 Act 7 的未激活宗师绝技会被服务端静默丢弃，不能用于切换。
			stunts, stErr := masterPracticeFetchArtifactStunts(call)
			if stErr != nil {
				slog.Warn("宗师修行绝技映射查询失败，跳过", "role_id", session.RoleID(), "err", stErr)
				return nil
			}
			deploy := masterPracticeFetchDeploy(call)
			if deploy == nil {
				slog.Info("宗师修行布阵信息不可用，跳过", "role_id", session.RoleID())
				return nil
			}
			_, currentJob, hasMain := masterPracticeProtagonistJob(deploy, cfg.DictRepo)
			if !hasMain {
				slog.Info("宗师修行无法解析主角当前职业，跳过", "role_id", session.RoleID())
				return nil
			}

			order := masterPracticeBuildOrder(cfg, panel, currentJob, stunts)
			if len(cfg.PracticeOrder) == 0 {
				slog.Info("宗师修行未配置职业顺序，以主角当前职业为主并按需切换备选职业",
					"role_id", session.RoleID(), "job_id", currentJob, "order", order)
			} else {
				slog.Info("宗师修行职业顺序",
					"role_id", session.RoleID(), "order", order, "current_job", currentJob)
			}

			totalChallenged := 0
			switchedAny := false
			var usedToday int32

			for _, requestedJob := range order {
				if err := ctx.Err(); err != nil {
					return nil
				}

				// 每个职业开始前刷新面板，确保星数与今日完成状态是最新的。
				if fresh := masterPracticeFetchPanel(call); fresh != nil {
					panel = fresh
				}
				usedToday = panel.TodayChallengeTimes
				if usedToday >= cfg.MaxChallengesPerDay {
					slog.Info("宗师修行今日挑战次数已用尽，结束本轮",
						"role_id", session.RoleID(),
						"used", usedToday, "limit", cfg.MaxChallengesPerDay)
					return nil
				}
				job, found := masterPracticeJobByID(panel, requestedJob)
				if !found {
					slog.Info("宗师修行方向不在面板中，跳过", "role_id", session.RoleID(), "job_id", requestedJob)
					continue
				}
				if masterPracticeJobFull(job, cfg.MaxStarsPerJob) {
					slog.Info("宗师修行方向累计星数已满，切换下一职业",
						"role_id", session.RoleID(), "job_id", requestedJob, "total_stars", job.TotalStars)
					continue
				}

				// 刷新布阵，读取主角当前职业。
				deploy = masterPracticeFetchDeploy(call)
				if deploy == nil {
					slog.Warn("宗师修行布阵信息刷新失败，跳过该职业", "role_id", session.RoleID(), "job_id", requestedJob)
					continue
				}
				_, currentJob, hasMain = masterPracticeProtagonistJob(deploy, cfg.DictRepo)
				if !hasMain {
					slog.Warn("宗师修行无法解析主角当前职业，跳过该职业", "role_id", session.RoleID(), "job_id", requestedJob)
					continue
				}

				// 今日已完成但尚未满星：先升阶，然后尝试下一候选职业。
				if masterPracticeJobDoneToday(job) {
					if requestedJob == currentJob && cfg.AllowStepUp {
						if refreshed := masterPracticeStepUpJob(requestedJob, cfg, session.RoleID(), call); refreshed != nil {
							panel = refreshed
						}
					}
					slog.Info("宗师修行方向今日已完成，尝试下一职业",
						"role_id", session.RoleID(), "job_id", requestedJob, "total_stars", job.TotalStars)
					continue
				}

				// 3. 切换主角神器绝技 (职业)。成功后服务端会改写主角 role_id。
				switched, swErr := masterPracticeEnsureStunt(ctx, currentJob, requestedJob, stunts, cfg, session.RoleID(), call)
				if swErr != nil {
					slog.Warn("宗师修行切换主角绝技失败，跳过该职业",
						"role_id", session.RoleID(), "job_id", requestedJob, "err", swErr)
					continue
				}
				if switched {
					switchedAny = true
					verified := false
					for attempt := 0; attempt < 2; attempt++ {
						if err := masterPracticeDeployPause(ctx); err != nil {
							return nil
						}
						deploy = masterPracticeFetchDeploy(call)
						if deploy == nil {
							continue
						}
						_, currentJob, hasMain = masterPracticeProtagonistJob(deploy, cfg.DictRepo)
						if hasMain && currentJob == requestedJob {
							verified = true
							break
						}
					}
					if !verified {
						slog.Warn("切换绝技后主角职业未生效，跳过该职业",
							"role_id", session.RoleID(), "expect_job", requestedJob, "actual_job", currentJob)
						continue
					}
				}

				// 4. 调整阵容为整队同职业。JOB_ERROR(11) 的官方文案是
				//   “请上阵相同职业的角色”，因此异职业伙伴必须先下阵。
				if cfg.AutoNormalizeDeploy {
					var normErr error
					deploy, normErr = masterPracticeNormalizeDeploy(ctx, deploy, requestedJob, cfg.DictRepo, call)
					if normErr != nil {
						slog.Warn("宗师修行阵容整理失败，跳过该职业",
							"role_id", session.RoleID(), "job_id", requestedJob, "err", normErr)
						continue
					}
				}
				if ok, reason := masterPracticeDeployMatches(deploy, requestedJob, cfg.DictRepo); !ok {
					slog.Warn("宗师修行阵容未满足整队同职业，跳过该职业",
						"role_id", session.RoleID(), "job_id", requestedJob, "reason", reason)
					continue
				}

				// 5. 挑战尚未完成的星级；每日最多 3 次，用尽前主动停止。
				remaining := cfg.MaxChallengesPerDay - usedToday
				if remaining <= 0 {
					slog.Info("宗师修行今日挑战次数已用尽，结束本轮",
						"role_id", session.RoleID(), "used", usedToday, "limit", cfg.MaxChallengesPerDay)
					return nil
				}
				challenged, full, failed, chErr := masterPracticeChallengeJob(
					ctx, job, remaining, cfg.MaxRetriesPerStar, session.RoleID(), call)
				usedToday += challenged
				totalChallenged += int(challenged)
				if chErr != nil {
					if errors.Is(chErr, scheduler.ErrAttemptsExhausted) {
						slog.Info("宗师修行今日挑战次数已用尽", "role_id", session.RoleID())
					}
					return chErr
				}
				if failed {
					slog.Warn("宗师修行当前职业重试耗尽仍挑战失败，准备切换职业",
						"role_id", session.RoleID(), "job_id", requestedJob, "challenged", challenged)
					if !cfg.SwitchJobOnFailure {
						return nil
					}
					// 刷新面板，下一候选职业据此更新剩余次数与今日进度。
					if fresh := masterPracticeFetchPanel(call); fresh != nil {
						panel = fresh
					}
					continue
				}

				// 6. 刷新面板后升阶；只有当前职业满星才继续修行顺序中的下一个职业。
				if refreshed := masterPracticeStepUpJob(requestedJob, cfg, session.RoleID(), call); refreshed != nil {
					panel = refreshed
					if freshJob, ok := masterPracticeJobByID(panel, requestedJob); ok {
						full = full || masterPracticeJobFull(freshJob, cfg.MaxStarsPerJob)
					}
				}
				if !full {
					slog.Info("宗师修行当前职业尚未满星，本轮结束",
						"role_id", session.RoleID(), "job_id", requestedJob, "challenged", challenged)
					return nil
				}
				slog.Info("宗师修行当前职业已满星，继续下一职业",
					"role_id", session.RoleID(), "job_id", requestedJob)
			}

			if totalChallenged == 0 && !switchedAny {
				slog.Info("宗师修行本轮没有可执行方向",
					"role_id", session.RoleID(), "order", order,
					"reason", "全部方向已满星、今日已完成，或未激活对应神器绝技")
			}
			return nil
		},
	)
}

// masterPracticeFetchArtifactStunts 查询职业 -> 已激活神器绝技映射 (Mod 85 Act 5)。
//
// 注意: Mod 85 Act 7 的“宗师绝技”列表在未激活时 role_stunt_id=0/is_active=0，
// 服务端会静默丢弃这类 use_stunt_skill 请求。切换主角职业必须使用
// Mod 85 Act 5 返回的已激活神器绝技 ID。
func masterPracticeFetchArtifactStunts(call func(*protocol.Packet, uint32) (*protocol.Packet, error)) ([]protocol.LibraryStuntInfo, error) {
	resp, err := call(protocol.BuildLibraryArtifactStuntInfoPacket(), protocol.ActionIDLibraryArtifactStuntInfo)
	if err != nil {
		return nil, fmt.Errorf("查询神器绝技映射无回包: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("查询神器绝技映射无回包")
	}
	infos, pErr := protocol.ParseLibraryStuntInfoResult(resp.Payload)
	if pErr != nil {
		return nil, pErr
	}
	return infos, nil
}

// masterPracticeFindStunt 从神器绝技映射中选择目标职业可用的绝技。
// 优先 is_use=1，其次 is_active=1；未激活的绝技服务端会静默丢弃，不能用于切换。
func masterPracticeFindStunt(stunts []protocol.LibraryStuntInfo, jobID int32) (int32, bool) {
	var fallback int32
	for _, info := range stunts {
		if info.RoleJobID != jobID {
			continue
		}
		stuntID := info.RoleStuntID
		if stuntID <= 0 {
			stuntID = info.NextRoleStuntID
		}
		if stuntID <= 0 {
			continue
		}
		if info.IsUse == 1 {
			return stuntID, true
		}
		if info.IsActive == 1 && fallback == 0 {
			fallback = stuntID
		}
	}
	if fallback > 0 {
		return fallback, true
	}
	return 0, false
}

// masterPracticeEnsureStunt 通过切换主角神器绝技把职业转换为 targetJob。
// 返回 switched=true 表示已发送切换请求并成功。
func masterPracticeEnsureStunt(
	ctx context.Context,
	currentJob, targetJob int32,
	stunts []protocol.LibraryStuntInfo,
	cfg MasterPracticeConfig,
	roleID string,
	call func(*protocol.Packet, uint32) (*protocol.Packet, error),
) (bool, error) {
	if currentJob == targetJob {
		return false, nil
	}
	if !cfg.AllowSwitchStunt {
		return false, fmt.Errorf("主角当前职业 %d != 目标 %d，且配置关闭了绝技切换", currentJob, targetJob)
	}
	stuntID, ok := masterPracticeFindStunt(stunts, targetJob)
	if !ok {
		return false, fmt.Errorf("职业 %d 没有已激活的神器绝技 (请先学习对应绝技)", targetJob)
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	pkt, err := protocol.BuildLibraryUseStuntSkillPacket(stuntID)
	if err != nil {
		return false, err
	}
	resp, err := call(pkt, protocol.ActionIDLibraryUseStuntSkill)
	if err != nil || resp == nil {
		return false, fmt.Errorf("切换神器绝技无回包: %w", err)
	}
	result, curRoleID, curStuntID, pErr := protocol.ParseLibraryUseStuntSkillResult(resp.Payload)
	if pErr != nil {
		return false, pErr
	}
	if result != protocol.LibraryResultSuccess {
		return false, fmt.Errorf("切换神器绝技失败: %s (result=%d)", protocol.LibraryUseStuntResultText(result), result)
	}
	slog.Info("已切换主角神器绝技",
		"role_id", roleID,
		"target_job", targetJob,
		"stunt_id", stuntID,
		"cur_role_id", curRoleID,
		"cur_stunt_id", curStuntID,
	)
	return true, nil
}

// masterPracticeRetryDelay 宗师修行战斗失败后重试同层前的等待。
const masterPracticeRetryDelay = 400 * time.Millisecond

// masterPracticeChallengeJob 挑战指定职业今日尚未完成的星级。
//
// remaining 为今日剩余挑战次数；只有服务端返回 SUCCESS 才扣减一次。
// maxRetries 为单个星级挑战失败 (FAILED) 后的重试次数。
// 返回:
//   - consumed: 本次实际消耗的挑战次数
//   - full: 该方向已累计满星
//   - failed: 重试耗尽仍打不过，调用方可切换其他职业继续
func masterPracticeChallengeJob(
	ctx context.Context,
	job protocol.MasterPracticeJobInfo,
	remaining int32,
	maxRetries int,
	roleID string,
	call func(*protocol.Packet, uint32) (*protocol.Packet, error),
) (consumed int32, full bool, failed bool, err error) {
	challengedToday := make(map[int32]bool, len(job.TodayChallenge))
	for _, star := range job.TodayChallenge {
		challengedToday[star] = true
	}
	if maxRetries < 0 {
		maxRetries = 0
	}

	for star := int32(1); star <= 3; star++ {
		if challengedToday[star] {
			continue
		}
		retries := 0
		for {
			if remaining > 0 && consumed >= remaining {
				slog.Info("宗师修行今日挑战次数已用尽，停止发送挑战",
					"role_id", roleID, "job_id", job.JobID, "remaining", remaining)
				return consumed, false, false, scheduler.ErrAttemptsExhausted
			}
			if ctx.Err() != nil {
				return consumed, false, false, nil
			}
			resp, callErr := call(
				protocol.BuildMasterPracticeChallengePacket(uint32(job.JobID), uint32(star), 0),
				protocol.ActionIDMasterPracticeChallenge,
			)
			if callErr != nil || resp == nil {
				slog.Info("宗师修行挑战无回包", "role_id", roleID, "job_id", job.JobID, "star", star, "err", callErr)
				return consumed, false, false, nil
			}
			code, pErr := protocol.ParseMasterPracticeActionCode(resp.Payload)
			if pErr != nil {
				slog.Warn("解析宗师修行挑战回包失败", "role_id", roleID, "err", pErr)
				return consumed, false, false, nil
			}
			outcome, outcomeErr := classifyMasterPracticeChallengeResult(code)
			switch outcome {
			case masterPracticeChallengeContinue:
				if code == protocol.MasterPracticeResultSuccess {
					consumed++
					slog.Info("宗师修行挑战成功", "role_id", roleID, "job_id", job.JobID, "star", star)
				} else {
					// 今日已挑战过该星级，不再重复消耗全局次数。
					slog.Info("宗师修行该星级今日已挑战", "role_id", roleID, "job_id", job.JobID, "star", star)
				}
			case masterPracticeChallengeFull:
				slog.Info("宗师修行方向累计星数已满", "role_id", roleID, "job_id", job.JobID, "result", code)
				return consumed, true, false, nil
			case masterPracticeChallengeNoTimes:
				return consumed, false, false, scheduler.ErrAttemptsExhausted
			case masterPracticeChallengeJobError:
				slog.Warn("宗师修行职业不匹配 (请上阵相同职业的角色)",
					"role_id", roleID, "job_id", job.JobID, "result", code)
				return consumed, false, false, scheduler.ErrConditionNotMet
			default:
				if code == protocol.MasterPracticeResultFailed {
					if retries < maxRetries {
						retries++
						slog.Warn("宗师修行挑战失败，重试同星级",
							"role_id", roleID, "job_id", job.JobID, "star", star,
							"retry", retries, "max_retries", maxRetries)
						select {
						case <-ctx.Done():
							return consumed, false, false, nil
						case <-time.After(masterPracticeRetryDelay):
						}
						continue
					}
					slog.Warn("宗师修行挑战失败且重试耗尽",
						"role_id", roleID, "job_id", job.JobID, "star", star,
						"retries", retries, "max_retries", maxRetries)
					return consumed, false, true, nil
				}
				if outcomeErr != nil {
					slog.Warn("宗师修行挑战业务失败",
						"role_id", roleID, "job_id", job.JobID, "star", star, "result", code, "err", outcomeErr)
					return consumed, false, false, outcomeErr
				}
				return consumed, false, false, nil
			}
			select {
			case <-ctx.Done():
				return consumed, false, false, nil
			case <-time.After(200 * time.Millisecond):
			}
			break
		}
	}
	return consumed, false, false, nil
}

// masterPracticeBuildOrder 构建本轮职业修行顺序：
// 先按配置/当前职业确定主方向；开启失败切换时，再追加面板中可修行且已激活神器绝技的备选职业。
func masterPracticeBuildOrder(
	cfg MasterPracticeConfig,
	panel *protocol.MasterPracticePanel,
	currentJob int32,
	stunts []protocol.LibraryStuntInfo,
) []int32 {
	seen := make(map[int32]bool)
	order := make([]int32, 0, len(panel.Jobs)+1)
	add := func(jobID int32) {
		if jobID <= 0 || seen[jobID] {
			return
		}
		seen[jobID] = true
		order = append(order, jobID)
	}

	if len(cfg.PracticeOrder) > 0 {
		for _, jobID := range cfg.PracticeOrder {
			add(jobID)
		}
	} else {
		add(currentJob)
	}

	if !cfg.SwitchJobOnFailure || !cfg.AllowSwitchStunt {
		return order
	}
	for _, job := range panel.Jobs {
		if job.JobID == currentJob || seen[job.JobID] {
			continue
		}
		if masterPracticeJobFull(job, cfg.MaxStarsPerJob) || masterPracticeJobDoneToday(job) {
			continue
		}
		if _, ok := masterPracticeFindStunt(stunts, job.JobID); !ok {
			continue
		}
		add(job.JobID)
	}
	return order
}

// masterPracticeStepUpJob 刷新面板并在星数满足时尝试升阶，返回刷新后的面板。
func masterPracticeStepUpJob(
	jobID int32,
	cfg MasterPracticeConfig,
	roleID string,
	call func(*protocol.Packet, uint32) (*protocol.Packet, error),
) *protocol.MasterPracticePanel {
	panel := masterPracticeFetchPanel(call)
	if panel == nil {
		slog.Warn("宗师修行升阶前刷新面板失败", "role_id", roleID, "job_id", jobID)
		return nil
	}
	if !cfg.AllowStepUp {
		return panel
	}
	job, found := masterPracticeJobByID(panel, jobID)
	if !found || job.TotalStars <= 0 {
		return panel
	}
	resp, err := call(
		protocol.BuildMasterPracticeStepUpPacket(uint32(jobID), 0),
		protocol.ActionIDMasterPracticeStepUp,
	)
	if err != nil || resp == nil {
		slog.Warn("宗师境界突破无回包", "role_id", roleID, "job_id", jobID, "err", err)
		return panel
	}
	code, pErr := protocol.ParseMasterPracticeActionCode(resp.Payload)
	if pErr != nil {
		slog.Warn("解析宗师突破回包失败", "role_id", roleID, "err", pErr)
		return panel
	}
	switch code {
	case protocol.MasterPracticeResultSuccess:
		slog.Info("宗师境界突破成功", "role_id", roleID, "job_id", jobID)
	case protocol.MasterPracticeResultStepUpCD:
		slog.Info("宗师境界突破冷却中", "role_id", roleID, "job_id", jobID)
	case protocol.MasterPracticeResultLessStars:
		slog.Info("宗师境界突破星数不足", "role_id", roleID, "job_id", jobID)
	case protocol.MasterPracticeResultLessItem:
		slog.Info("宗师境界突破道具不足", "role_id", roleID, "job_id", jobID)
	case protocol.MasterPracticeResultNotActiveAll:
		slog.Info("宗师境界突破需先激活全部炼魂", "role_id", roleID, "job_id", jobID)
	case protocol.MasterPracticeResultTopStep:
		slog.Info("宗师境界已达最高", "role_id", roleID, "job_id", jobID)
	default:
		slog.Info("宗师境界突破未成功", "role_id", roleID, "job_id", jobID, "result", code)
	}
	return panel
}

// masterPracticeFetchPanel 查询并解析宗师修行主面板。
func masterPracticeFetchPanel(call func(*protocol.Packet, uint32) (*protocol.Packet, error)) *protocol.MasterPracticePanel {
	resp, err := call(protocol.BuildMasterPracticePanelPacket(protocol.ActionIDTownEnter), protocol.ActionIDMasterPracticePanel)
	if err != nil || resp == nil {
		return nil
	}
	panel, pErr := protocol.ParseMasterPracticePanelResult(resp.Payload)
	if pErr != nil || panel.Result != protocol.MasterPracticeResultSuccess {
		return nil
	}
	return panel
}

// masterPracticeFetchDeploy 查询并解析宗师修行布阵信息。
func masterPracticeFetchDeploy(call func(*protocol.Packet, uint32) (*protocol.Packet, error)) *protocol.MasterPracticeDeployInfo {
	resp, err := call(protocol.BuildMasterPracticeDeployInfoPacket(), protocol.ActionIDMasterPracticeDeployInfo)
	if err != nil || resp == nil {
		return nil
	}
	info, pErr := protocol.ParseMasterPracticeDeployInfoResult(resp.Payload)
	if pErr != nil || info.Result != protocol.MasterPracticeResultSuccess {
		return nil
	}
	return info
}

// masterPracticeJobByID 按 JobID 从面板中查找方向。
func masterPracticeJobByID(panel *protocol.MasterPracticePanel, jobID int32) (protocol.MasterPracticeJobInfo, bool) {
	for _, job := range panel.Jobs {
		if job.JobID == jobID {
			return job, true
		}
	}
	return protocol.MasterPracticeJobInfo{}, false
}

// masterPracticeRoleJob 把角色 role_id 映射为职业 JobId；字典缺失时返回 0。
func masterPracticeRoleJob(repo dictionary.Repository, roleID int32) int32 {
	if repo == nil || roleID <= 0 {
		return 0
	}
	rt, err := repo.GetRoleType(int(roleID))
	if err != nil || rt == nil {
		return 0
	}
	return int32(rt.JobID)
}

// masterPracticeProtagonistJob 返回阵容中的主角及其当前 JobId。
func masterPracticeProtagonistJob(deploy *protocol.MasterPracticeDeployInfo, repo dictionary.Repository) (playerRoleID, jobID int32, ok bool) {
	if deploy == nil {
		return 0, 0, false
	}
	for _, role := range deploy.Roles {
		if role.IsMainRole != 1 {
			continue
		}
		job := masterPracticeRoleJob(repo, role.RoleID)
		if job <= 0 {
			return role.PlayerRoleID, 0, false
		}
		return role.PlayerRoleID, job, true
	}
	return 0, 0, false
}

// masterPracticeJobFull 判断方向累计星数是否已达上限。
func masterPracticeJobFull(job protocol.MasterPracticeJobInfo, maxStars int32) bool {
	return maxStars > 0 && job.TotalStars >= maxStars
}

// masterPracticeJobDoneToday 判断方向今日 1~3 星是否均已挑战。
func masterPracticeJobDoneToday(job protocol.MasterPracticeJobInfo) bool {
	var seen [4]bool
	for _, star := range job.TodayChallenge {
		if star >= 1 && star <= 3 {
			seen[star] = true
		}
	}
	return seen[1] && seen[2] && seen[3]
}

// masterPracticeMismatchedPartners 返回需要下阵的异职业伙伴 player_role_id。
// 主角 (IsMainRole==1) 永不下阵。
func masterPracticeMismatchedPartners(deploy *protocol.MasterPracticeDeployInfo, targetJob int32, repo dictionary.Repository) []int32 {
	if deploy == nil {
		return nil
	}
	roleByPlayerID := make(map[int32]protocol.MasterPracticeDeployRole, len(deploy.Roles))
	for _, role := range deploy.Roles {
		roleByPlayerID[role.PlayerRoleID] = role
	}
	var out []int32
	for _, grid := range deploy.Grids {
		if grid.PlayerRoleID == 0 {
			continue
		}
		role, ok := roleByPlayerID[grid.PlayerRoleID]
		if !ok || role.IsMainRole == 1 {
			continue
		}
		if masterPracticeRoleJob(repo, role.RoleID) != targetJob {
			out = append(out, grid.PlayerRoleID)
		}
	}
	return out
}

// masterPracticeHasDeployedPartner 判断阵容中是否已有同职业伙伴。
func masterPracticeHasDeployedPartner(deploy *protocol.MasterPracticeDeployInfo, targetJob int32, repo dictionary.Repository) bool {
	if deploy == nil {
		return false
	}
	roleByPlayerID := make(map[int32]protocol.MasterPracticeDeployRole, len(deploy.Roles))
	for _, role := range deploy.Roles {
		roleByPlayerID[role.PlayerRoleID] = role
	}
	for _, grid := range deploy.Grids {
		if grid.PlayerRoleID == 0 {
			continue
		}
		role, ok := roleByPlayerID[grid.PlayerRoleID]
		if !ok || role.IsMainRole == 1 {
			continue
		}
		if masterPracticeRoleJob(repo, role.RoleID) == targetJob {
			return true
		}
	}
	return false
}

// masterPracticeDeployCandidate 选择一名未上阵的同职业伙伴及空余格位。
// 返回 ok=false 表示没有可上阵的同职业伙伴、当前已有同职业伙伴或没有空位。
func masterPracticeDeployCandidate(deploy *protocol.MasterPracticeDeployInfo, targetJob int32, repo dictionary.Repository) (playerRoleID int32, gridTypeID uint8, ok bool) {
	if deploy == nil {
		return 0, 0, false
	}
	if masterPracticeHasDeployedPartner(deploy, targetJob, repo) {
		return 0, 0, false
	}
	deployed := make(map[int32]bool, len(deploy.Grids))
	empty := make(map[int32]bool, len(deploy.Grids))
	for _, grid := range deploy.Grids {
		if grid.PlayerRoleID != 0 {
			deployed[grid.PlayerRoleID] = true
		} else if isMasterPracticeNormalGrid(grid.GridTypeID) {
			empty[grid.GridTypeID] = true
		}
	}

	for _, role := range deploy.Roles {
		if role.IsMainRole == 1 || deployed[role.PlayerRoleID] {
			continue
		}
		if masterPracticeRoleJob(repo, role.RoleID) == targetJob {
			playerRoleID = role.PlayerRoleID
			break
		}
	}
	if playerRoleID == 0 {
		return 0, 0, false
	}

	// 优先使用 9 号常规格，其次 2~8；不使用 1 号主角位与 51/52 替补位。
	for _, preferred := range []int32{9, 2, 3, 4, 5, 6, 7, 8} {
		if empty[preferred] {
			return playerRoleID, uint8(preferred), true
		}
	}
	return 0, 0, false
}

func isMasterPracticeNormalGrid(gridTypeID int32) bool {
	return gridTypeID >= 1 && gridTypeID <= 9
}

// masterPracticeDeployPause 在连续布阵操作之间注入短间隔，降低触发风控的概率。
func masterPracticeDeployPause(ctx context.Context) error {
	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// masterPracticeDeployMatches 校验所有已上阵角色 (含主角) 是否都与 targetJob 同职业。
func masterPracticeDeployMatches(deploy *protocol.MasterPracticeDeployInfo, targetJob int32, repo dictionary.Repository) (bool, string) {
	if deploy == nil {
		return false, "布阵信息为空"
	}
	roleByPlayerID := make(map[int32]protocol.MasterPracticeDeployRole, len(deploy.Roles))
	for _, role := range deploy.Roles {
		roleByPlayerID[role.PlayerRoleID] = role
	}
	mainDeployed := false
	for _, grid := range deploy.Grids {
		if grid.PlayerRoleID == 0 {
			continue
		}
		role, ok := roleByPlayerID[grid.PlayerRoleID]
		if !ok {
			return false, fmt.Sprintf("格位 %d 的角色 %d 不在角色列表", grid.GridTypeID, grid.PlayerRoleID)
		}
		job := masterPracticeRoleJob(repo, role.RoleID)
		if role.IsMainRole == 1 {
			mainDeployed = true
		}
		if job != targetJob {
			return false, fmt.Sprintf("角色 %d (%s) 职业 %d != 目标 %d", role.PlayerRoleID, role.RoleName, job, targetJob)
		}
	}
	if !mainDeployed {
		return false, "主角未上阵"
	}
	return true, ""
}

// masterPracticeNormalizeDeploy 下阵异职业伙伴，并在需要时上阵一名同职业伙伴。
func masterPracticeNormalizeDeploy(
	ctx context.Context,
	deploy *protocol.MasterPracticeDeployInfo,
	targetJob int32,
	repo dictionary.Repository,
	call func(*protocol.Packet, uint32) (*protocol.Packet, error),
) (*protocol.MasterPracticeDeployInfo, error) {
	mismatched := masterPracticeMismatchedPartners(deploy, targetJob, repo)
	for _, playerRoleID := range mismatched {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		resp, err := call(protocol.BuildMasterPracticeDeployPacket(playerRoleID, 0), protocol.ActionIDMasterPracticeDeploy)
		if err != nil || resp == nil {
			return nil, fmt.Errorf("下阵角色 %d 无回包: %w", playerRoleID, err)
		}
		code, pErr := protocol.ParseMasterPracticeActionCode(resp.Payload)
		if pErr != nil || code != protocol.MasterPracticeResultSuccess {
			return nil, fmt.Errorf("下阵角色 %d 失败: result=%d err=%v", playerRoleID, code, pErr)
		}
		if err := masterPracticeDeployPause(ctx); err != nil {
			return nil, err
		}
	}
	if len(mismatched) > 0 {
		refreshed := masterPracticeFetchDeploy(call)
		if refreshed == nil {
			return nil, fmt.Errorf("下阵后刷新布阵信息失败")
		}
		deploy = refreshed
	}

	if masterPracticeHasDeployedPartner(deploy, targetJob, repo) {
		return deploy, nil
	}
	playerRoleID, gridTypeID, ok := masterPracticeDeployCandidate(deploy, targetJob, repo)
	if !ok {
		// 没有同职业伙伴时仍允许以主角单人阵容挑战；若服务端要求伙伴，
		// 后续挑战会返回 JOB_ERROR 并由调用方熔断。
		return deploy, nil
	}
	resp, err := call(protocol.BuildMasterPracticeDeployPacket(playerRoleID, gridTypeID), protocol.ActionIDMasterPracticeDeploy)
	if err != nil || resp == nil {
		return nil, fmt.Errorf("上阵角色 %d 到格位 %d 无回包: %w", playerRoleID, gridTypeID, err)
	}
	code, pErr := protocol.ParseMasterPracticeActionCode(resp.Payload)
	if pErr != nil || code != protocol.MasterPracticeResultSuccess {
		return nil, fmt.Errorf("上阵角色 %d 到格位 %d 失败: result=%d err=%v", playerRoleID, gridTypeID, code, pErr)
	}
	if err := masterPracticeDeployPause(ctx); err != nil {
		return nil, err
	}
	refreshed := masterPracticeFetchDeploy(call)
	if refreshed == nil {
		return nil, fmt.Errorf("上阵后刷新布阵信息失败")
	}
	return refreshed, nil
}

// masterPracticeChallengeOutcome 挑战回包的业务分类。
type masterPracticeChallengeOutcome int

const (
	// masterPracticeChallengeContinue 已挑战或挑战成功，继续下一星。
	masterPracticeChallengeContinue masterPracticeChallengeOutcome = iota
	// masterPracticeChallengeStop 正常停止当前方向 (如星数不足)。
	masterPracticeChallengeStop
	// masterPracticeChallengeFull 该方向累计星数已满。
	masterPracticeChallengeFull
	// masterPracticeChallengeNoTimes 今日挑战次数用尽。
	masterPracticeChallengeNoTimes
	// masterPracticeChallengeJobError 阵容职业与方向不匹配。
	masterPracticeChallengeJobError
)

// classifyMasterPracticeChallengeResult 将挑战结果码映射为业务动作与调度错误。
func classifyMasterPracticeChallengeResult(code uint8) (masterPracticeChallengeOutcome, error) {
	switch code {
	case protocol.MasterPracticeResultSuccess, protocol.MasterPracticeResultIsChallenged:
		return masterPracticeChallengeContinue, nil
	case protocol.MasterPracticeResultMaxStars:
		return masterPracticeChallengeFull, nil
	case protocol.MasterPracticeResultNoChallengeTimes:
		return masterPracticeChallengeNoTimes, scheduler.ErrAttemptsExhausted
	case protocol.MasterPracticeResultJobError:
		return masterPracticeChallengeJobError, scheduler.ErrConditionNotMet
	case protocol.MasterPracticeResultLessStars, protocol.MasterPracticeResultFailed:
		return masterPracticeChallengeStop, nil
	case protocol.MasterPracticeResultConditionLimited, protocol.MasterPracticeResultIsLocked:
		return masterPracticeChallengeStop, scheduler.ErrConditionNotMet
	default:
		return masterPracticeChallengeStop, fmt.Errorf("宗师修行挑战未知结果码: %d", code)
	}
}

// AwardSelectorFunc 允许注入自定义交互选择器（例如命令行交互、Web交互回调或自动化测试桩）。
type AwardSelectorFunc func(ctx context.Context, pool *protocol.TreasureHousePool, dict dictionary.Repository) ([]protocol.TreasureAwardSelect, error)

// treasureHousePriority 天帝宝库调度优先级：高于孩子日常 (120) 与 PriorityHigh (100)。
// Act 4 set_use_free_flag 会严格校验城镇/战场上下文，服务端在前置状态被污染时会
// 静默丢弃该请求；因此宝库必须排在本轮所有场景/队伍类玩法之前执行。
const treasureHousePriority scheduler.RoutinePriority = 200

// 天帝宝库非交互模式选奖策略。
const (
	// TreasureHouseSelectionRarity 按商品稀有度自动选择 (默认)
	TreasureHouseSelectionRarity = "rarity"
	// TreasureHouseSelectionExplicit 严格使用配置指定的奖励 ID
	TreasureHouseSelectionExplicit = "explicit"
	// TreasureHouseSelectionAuto 依据账号养成进度快照动态计算需求分并选奖
	TreasureHouseSelectionAuto = "auto"
)

// TreasureHouseConfig 天帝宝库自选大奖与开启抽奖配置。
type TreasureHouseConfig struct {
	// DictRepo 静态数据库字典仓储 (用于解析大奖条目道具名及说明)
	DictRepo dictionary.Repository
	// SelectionMode 非交互模式选奖策略: rarity(默认)|explicit
	SelectionMode string
	// PreferAwards 优先奖励条目 ID 列表 (按顺序匹配当前开放候选，命中即选).
	// 用于优先补齐账号当前最缺的资源，例如鸿蒙紫气、血魂石等。
	PreferAwards []int32
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

			// 0.1 不要在 Act4 之前发送任何 Mod 57 (魔王试炼) 请求。
			// 线上实测: 即便是只读的 get_player_team_info 也会让服务端进入
			// 试炼上下文，随后免费优先标记 set_use_free_flag (Act 4) 被静默
			// 丢弃，导致整轮免费抽奖被跳过。存在遗留队伍时由魔王试炼例程
			// 自身在开战前清理，无需在此重复处理。

			prevAct := session.LastActionID()
			if prevAct == 0 {
				prevAct = protocol.ActionIDTownEnter
			}
			call := func(pkt *protocol.Packet, action uint32, timeout time.Duration) *protocol.Packet {
				cctx, ccancel := context.WithTimeout(ctx, timeout)
				defer ccancel()
				resp, cErr := session.Call(cctx, pkt, action)
				if cErr != nil || resp == nil {
					// 调用方会记录具体业务上下文，这里只保留 Debug 级诊断，避免重复 WARN。
					slog.Debug("天帝宝库请求无回包",
						"action", fmt.Sprintf("0x%08X", action), "err", cErr)
					return nil
				}
				prevAct = action
				return resp
			}

			// 1. 主面板 (Mod 594, Act 1): 校验 result_code 并读取免费钥匙/当前自选
			mainResp := call(protocol.BuildTreasureHouseMainPanelPacket(prevAct),
				protocol.ActionIDTreasureHouseMainPanel, 8*time.Second)
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
					protocol.ActionIDTreasureHouseGetFreeItem, 8*time.Second)
				if freeResp != nil {
					code, cErr := protocol.ParseTreasureHouseResultCode(freeResp.Payload)
					switch {
					case cErr != nil:
						slog.Warn("解析领取免费钥匙回包失败", "err", cErr)
					case code == protocol.TreasureHouseResultNoItem:
						// 今日已无免费钥匙可领，属于正常业务状态。
						slog.Info("领取免费钥匙: 无可用道具",
							"result", code,
							"meaning", protocol.TreasureHouseResultName(code))
					case code != protocol.TreasureHouseResultSuccess:
						slog.Warn("领取免费钥匙失败",
							"result", code,
							"meaning", protocol.TreasureHouseResultName(code))
					default:
						slog.Info("已领取今日免费钥匙", "left_can_get_free", mainInfo.LeftCanGetFree)
						if refresh := call(protocol.BuildTreasureHouseMainPanelPacket(prevAct),
							protocol.ActionIDTreasureHouseMainPanel, 8*time.Second); refresh != nil {
							if mi, e := protocol.ParseTreasureHouseMainPanelResult(refresh.Payload); e == nil && mi.Result == protocol.TreasureHouseResultSuccess {
								mainInfo = mi
							}
						}
					}
				}
			}

			// 2.1 设置优先使用免费次数 (Mod 594, Act 4) 必须紧跟在主面板/领钥匙之后、
			// 任何养成面板采集之前发送。线上实测: Mod 57 请求以及动态选奖采集的
			// 养成面板请求都会让服务端进入其他上下文，随后 Act 4 被静默丢弃，
			// 整轮免费抽奖因此被跳过。设置失败时直接放弃抽奖，避免误扣元宝。
			flagResp := call(protocol.BuildTreasureHouseUseFreeFlagPacket(1, prevAct),
				protocol.ActionIDTreasureHouseSetUseFreeFlag, 8*time.Second)
			if flagResp == nil {
				slog.Info("设置免费次数优先无回包，跳过抽奖以免误消耗元宝",
					"role_id", session.RoleID())
				return nil
			}
			flagCode, fErr := protocol.ParseTreasureHouseResultCode(flagResp.Payload)
			if fErr != nil {
				slog.Warn("解析设置免费次数优先回包失败，跳过抽奖", "err", fErr)
				return nil
			}
			if flagCode == protocol.TreasureHouseResultNoFreeItem {
				// 今日无免费钥匙：服务端正常返回该业务码，不是协议失败。
				slog.Info("天帝宝库无免费次数，跳过抽奖以免误消耗元宝",
					"result", flagCode,
					"meaning", protocol.TreasureHouseResultName(flagCode))
				return nil
			}
			if flagCode != protocol.TreasureHouseResultSuccess {
				slog.Warn("设置免费次数优先失败，跳过抽奖以免误消耗元宝",
					"result", flagCode,
					"meaning", protocol.TreasureHouseResultName(flagCode))
				return nil
			}

			// 3. 自选面板 (Mod 594, Act 2): 只有 open_flag=1 的候选才能被服务端接受
			panelResp := call(protocol.BuildTreasureHouseInfoPacket(prevAct),
				protocol.ActionIDTreasureHouseSelectPanel, 8*time.Second)
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

			// 非交互模式默认按商品稀有度自动选择；显式模式下沿用配置值。
			var itemLookup func(int32) (*dictionary.Item, error)
			if cfg.DictRepo != nil {
				itemLookup = func(itemID int32) (*dictionary.Item, error) {
					return cfg.DictRepo.GetItem(int(itemID))
				}
			}
			selectedAwards := resolveTreasureAwards(cfg.Awards, mainInfo.WishList, offered)
			// 非交互模式选奖策略:
			//   auto     -> 采集账号养成进度并动态计算需求分 (每次刷新重新评估)
			//   explicit -> 使用配置奖励
			//   rarity   -> 稀有度/价值序 + 功能开放过滤 (默认)
			// 交互模式保留配置值作为回车默认。
			if !cfg.Interactive && cfg.Selector == nil {
				// 先取角色已开放功能集合，过滤掉所属系统未开放的奖励。
				var isFuncOpen func(int32) bool
				if openSet, fErr := session.RefreshPlayerFunctionsIfStale(ctx); fErr != nil {
					slog.Warn("获取功能开放列表失败，选奖时不做功能过滤",
						"role_id", session.RoleID(), "err", fErr)
				} else {
					isFuncOpen = func(functionID int32) bool { return openSet[functionID] }
				}

				switch cfg.SelectionMode {
				case TreasureHouseSelectionExplicit:
					// 保持 resolveTreasureAwards 的结果。
				case TreasureHouseSelectionAuto:
					snap := CollectNeedSnapshot(ctx, session, offered)
					if snap.hasProgressData() {
						needs := EvaluateTreasureNeeds(snap, offered)
						selectedAwards = SelectTreasureAwardsByNeed(offered, itemLookup, isFuncOpen, needs)
					} else {
						slog.Warn("账号养成进度采集全部失败，回退稀有度选奖", "role_id", session.RoleID())
						selectedAwards = SelectTreasureAwardsByRarity(offered, itemLookup, isFuncOpen, cfg.PreferAwards)
					}
				default:
					selectedAwards = SelectTreasureAwardsByRarity(offered, itemLookup, isFuncOpen, cfg.PreferAwards)
				}
			}
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
				protocol.ActionIDTreasureHouseSetAward, 8*time.Second)
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

			// 5. 消耗免费钥匙抽奖 (Mod 594, Act 6): 每次必须解析 result_code，失败立即熔断
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
					protocol.ActionIDTreasureHouseLottery, 8*time.Second)
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
				protocol.ActionIDTreasureHouseMainPanel, 8*time.Second); refresh != nil {
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

// treasureItemTypePriority 方案 A 的档内价值序:
// 自选包/礼包(42100) > 稀有养成材料(31000) > 货币(28000) > 宝石/随机包(10011) > 令牌/玉牌(10006)。
func treasureItemTypePriority(typeID int) int {
	switch typeID {
	case 42100:
		return 5
	case 31000:
		return 4
	case 28000:
		return 3
	case 10011:
		return 2
	case 10006:
		return 1
	default:
		return 0
	}
}

func treasureItemQuality(item *dictionary.Item) int {
	if item == nil {
		return 0
	}
	return item.Quality
}

func treasureItemTypeID(item *dictionary.Item) int {
	if item == nil {
		return 0
	}
	return item.TypeID
}

// betterTreasureAward 判断候选 a 是否优于 b。
//
// 排序键 (逐级比较):
//  1. 道具品质 (稀有度) 降序
//  2. 方案 A 类型价值序 (自选包 > 稀有材料 > 货币 > 宝石包 > 令牌) 降序
//  3. 发放数量降序
//  4. 官方展示 sort 升序
//  5. 奖励条目 id 升序 (保证结果稳定)
func betterTreasureAward(a protocol.TreasureAward, ia *dictionary.Item, b protocol.TreasureAward, ib *dictionary.Item) bool {
	if qa, qb := treasureItemQuality(ia), treasureItemQuality(ib); qa != qb {
		return qa > qb
	}
	if pa, pb := treasureItemTypePriority(treasureItemTypeID(ia)), treasureItemTypePriority(treasureItemTypeID(ib)); pa != pb {
		return pa > pb
	}
	if a.ItemNum != b.ItemNum {
		return a.ItemNum > b.ItemNum
	}
	if a.Sort != b.Sort {
		return a.Sort < b.Sort
	}
	return a.ID < b.ID
}

// treasureAwardCandidate 候选奖励及其道具元数据。
type treasureAwardCandidate struct {
	award protocol.TreasureAward
	item  *dictionary.Item
}

// pickBestTreasureAward 从候选中按方案 A 规则选出最优项。
func pickBestTreasureAward(candidates []treasureAwardCandidate) (treasureAwardCandidate, bool) {
	var best treasureAwardCandidate
	found := false
	for _, c := range candidates {
		if !found || betterTreasureAward(c.award, c.item, best.award, best.item) {
			best = c
			found = true
		}
	}
	return best, found
}

// SelectTreasureAwardsByNeed 依据账号养成需求分选择三档奖励。
//
// 需求分由 EvaluateAwardNeed 结合实机面板 (法阵/龙珠/血脉/藏经阁/丹魂/圣器/
// 兵法/混沌虚空/幻兽) 与道具持有量计算；同分时回退稀有度/价值序。
func SelectTreasureAwardsByNeed(
	offered *protocol.TreasureHousePool,
	lookup func(int32) (*dictionary.Item, error),
	isFuncOpen func(int32) bool,
	needs map[int32]AwardNeed,
) []protocol.TreasureAwardSelect {
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
		var best *treasureAwardCandidate
		var bestNeed AwardNeed
		var skipped []string
		for _, id := range t.options {
			award, ok := protocol.TreasureAwardByID(id)
			if !ok {
				continue
			}
			if isFuncOpen != nil && award.FuncID != 0 && !isFuncOpen(award.FuncID) {
				skipped = append(skipped, fmt.Sprintf("%d(func=%d)", award.ID, award.FuncID))
				continue
			}
			var item *dictionary.Item
			if lookup != nil {
				if it, err := lookup(award.ItemID); err == nil {
					item = it
				}
			}
			cand := treasureAwardCandidate{award: award, item: item}
			need := needs[id]
			if best == nil || need.Score > bestNeed.Score ||
				(need.Score == bestNeed.Score && betterTreasureAward(cand.award, cand.item, best.award, best.item)) {
				candCopy := cand
				best = &candCopy
				bestNeed = need
			}
		}

		if best == nil {
			if len(t.options) > 0 {
				resolved = append(resolved, protocol.TreasureAwardSelect{TierID: t.tier, ItemID: t.options[0]})
				slog.Warn("天帝宝库该档无可用的动态选奖候选，退回首个开放候选",
					"tier", t.tier, "award_id", t.options[0])
			}
			continue
		}

		itemName := ""
		if best.item != nil {
			itemName = best.item.Name
		}
		slog.Info("天帝宝库按账号需求选定大奖",
			"tier", t.tier,
			"tier_quality", protocol.TreasureAwardQualityName(best.award.Quality),
			"award_id", best.award.ID,
			"item_id", best.award.ItemID,
			"item_name", itemName,
			"item_num", best.award.ItemNum,
			"need_score", bestNeed.Score,
			"reason", bestNeed.Reason,
			"skipped_function_closed", skipped,
			"candidates", t.options,
		)
		resolved = append(resolved, protocol.TreasureAwardSelect{TierID: t.tier, ItemID: best.award.ID})
	}
	return resolved
}

// SelectTreasureAwardsByRarity 按商品稀有度与方案 A 价值序为三档各选一个开放候选。
//
// 服务端 select_panel 只下发档位与候选奖励 id，奖励 id 对应的道具、数量与品质
// 必须查官方 TreasureAward 表；道具元数据 (品质/类型/名称) 取自静态字典。
//
// isFuncOpen 用于过滤奖励所属功能未开放的候选 (FuncID==0 视为通用道具，始终保留)；
// 传 nil 表示不做功能过滤。若某档全部候选都被过滤，则按稀有度兜底，保证 set_award 能完成。
// pickPreferredTreasureAward 按优先清单为指定档位选择奖励。
// preferred 中的奖励条目必须属于该档、在当前开放候选中且所属功能已开放。
func pickPreferredTreasureAward(tier int32, options, preferred []int32, isFuncOpen func(int32) bool) (protocol.TreasureAward, bool) {
	inTier := make(map[int32]bool, len(options))
	for _, id := range options {
		inTier[id] = true
	}
	for _, id := range preferred {
		if !inTier[id] {
			continue
		}
		award, ok := protocol.TreasureAwardByID(id)
		if !ok || award.Quality != tier {
			continue
		}
		if isFuncOpen != nil && award.FuncID != 0 && !isFuncOpen(award.FuncID) {
			continue
		}
		return award, true
	}
	return protocol.TreasureAward{}, false
}

func SelectTreasureAwardsByRarity(
	offered *protocol.TreasureHousePool,
	lookup func(int32) (*dictionary.Item, error),
	isFuncOpen func(int32) bool,
	preferred []int32,
) []protocol.TreasureAwardSelect {
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
		// 1. 优先清单: 账号当前最缺且仍在候选池中的奖励直接命中。
		if preferredAward, ok := pickPreferredTreasureAward(t.tier, t.options, preferred, isFuncOpen); ok {
			var preferredItem *dictionary.Item
			if lookup != nil {
				if it, err := lookup(preferredAward.ItemID); err == nil {
					preferredItem = it
				}
			}
			itemName := ""
			if preferredItem != nil {
				itemName = preferredItem.Name
			}
			slog.Info("天帝宝库按优先清单选定大奖",
				"tier", t.tier,
				"tier_quality", protocol.TreasureAwardQualityName(preferredAward.Quality),
				"award_id", preferredAward.ID,
				"item_id", preferredAward.ItemID,
				"item_name", itemName,
				"item_num", preferredAward.ItemNum,
				"func_id", preferredAward.FuncID,
				"preferred", preferred,
			)
			resolved = append(resolved, protocol.TreasureAwardSelect{TierID: t.tier, ItemID: preferredAward.ID})
			continue
		}

		// 2. 常规: 按功能开放过滤 + 稀有度/价值序选择。
		var available, fallback []treasureAwardCandidate
		var skipped []string

		for _, id := range t.options {
			award, ok := protocol.TreasureAwardByID(id)
			if !ok {
				slog.Warn("天帝宝库候选奖励未收录于官方表，跳过",
					"tier", t.tier, "award_id", id)
				continue
			}
			var item *dictionary.Item
			if lookup != nil {
				if it, err := lookup(award.ItemID); err == nil {
					item = it
				}
			}
			cand := treasureAwardCandidate{award: award, item: item}
			fallback = append(fallback, cand)
			if isFuncOpen != nil && award.FuncID != 0 && !isFuncOpen(award.FuncID) {
				skipped = append(skipped, fmt.Sprintf("%d(func=%d)", award.ID, award.FuncID))
				continue
			}
			available = append(available, cand)
		}

		pool := available
		if len(pool) == 0 && len(fallback) > 0 {
			slog.Warn("天帝宝库该档候选对应功能均未开放，按稀有度兜底",
				"tier", t.tier, "skipped", skipped)
			pool = fallback
		} else if len(skipped) > 0 {
			slog.Info("天帝宝库跳过所属功能未开放的候选",
				"tier", t.tier, "skipped", skipped)
		}

		best, found := pickBestTreasureAward(pool)
		if !found {
			// 官方表未收录任何候选时，退回服务端首个开放候选，保持流程可用。
			if len(t.options) > 0 {
				resolved = append(resolved, protocol.TreasureAwardSelect{TierID: t.tier, ItemID: t.options[0]})
				slog.Warn("天帝宝库该档无官方奖励元数据，退回首个开放候选",
					"tier", t.tier, "award_id", t.options[0])
			}
			continue
		}

		itemName := ""
		if best.item != nil {
			itemName = best.item.Name
		}
		slog.Info("天帝宝库按稀有度选定大奖",
			"tier", t.tier,
			"tier_quality", protocol.TreasureAwardQualityName(best.award.Quality),
			"award_id", best.award.ID,
			"item_id", best.award.ItemID,
			"item_name", itemName,
			"item_num", best.award.ItemNum,
			"item_quality", treasureItemQuality(best.item),
			"type_priority", treasureItemTypePriority(treasureItemTypeID(best.item)),
			"func_id", best.award.FuncID,
			"skipped_function_closed", skipped,
			"candidates", t.options,
		)
		resolved = append(resolved, protocol.TreasureAwardSelect{TierID: t.tier, ItemID: best.award.ID})
	}
	return resolved
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
	pCtx, pCancel := context.WithTimeout(ctx, 8*time.Second)
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
	sCtx, sCancel := context.WithTimeout(ctx, 8*time.Second)
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
	kCtx, kCancel := context.WithTimeout(ctx, 8*time.Second)
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
	qCtx, qCancel := context.WithTimeout(ctx, 8*time.Second)
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
	fCtx, fCancel := context.WithTimeout(ctx, 8*time.Second)
	fResp, fErr := session.Call(fCtx, flagPkt, protocol.ActionIDFindImmortalIsFiveBlessingsOpen)
	fCancel()
	if fErr != nil || fResp == nil {
		slog.Debug("查询五福临门开启状态未收到回包", "role_id", session.RoleID(), "err", fErr)
		return
	}
	// 线上实测：Act 14 返回 12 (官方常量 NO)，但活动实际已开启且 start_bless
	// 可正常获得活跃度（面板 bless_number=10）。因此该标志不可作为门禁，
	// 仅记录日志，是否可祈福以面板返回的 bless_number 为准。
	open, err := protocol.ParseFiveBlessingsOpenFlagResult(fResp.Payload)
	if err != nil {
		slog.Warn("解析五福临门开启状态回包失败", "role_id", session.RoleID(), "err", err)
		return
	}
	if !open {
		slog.Info("五福临门开启标志返回 NO，改以面板次数为准继续", "role_id", session.RoleID())
	}
	prevAct = protocol.ActionIDFindImmortalIsFiveBlessingsOpen

	// 2. 查询祈福面板 (Mod 68, Act 10)
	infoPkt := protocol.BuildFindImmortalOpenFiveBlessingsPacket(prevAct)
	iCtx, iCancel := context.WithTimeout(ctx, 8*time.Second)
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
		sCtx, sCancel := context.WithTimeout(ctx, 8*time.Second)
		sResp, sErr := session.Call(sCtx, startPkt, protocol.ActionIDFindImmortalStartBless)
		sCancel()
		prevAct = protocol.ActionIDFindImmortalStartBless
		if sErr != nil || sResp == nil {
			slog.Info("五福临门开始祈福请求未收到回包，服务端暂不接受，跳过本轮", "role_id", session.RoleID(), "err", sErr)
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
	eCtx, eCancel := context.WithTimeout(ctx, 8*time.Second)
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
	const callTimeout = 8 * time.Second
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
				// 活动未开放/已结束时服务端会静默丢弃信息查询；按暂不可用跳过。
				slog.Info("问灵活动未开放或状态查询被服务端丢弃，跳过",
					"role_id", session.RoleID(), "err", err)
				return nil
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
	const callTimeout = 8 * time.Second
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
	const callTimeout = 8 * time.Second
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
			//    线上实测：面板同时下发"单次免费"与"多次元宝"两种入口，后者
			//    cur_number>0 且 cost_ingot=300；若不加门禁直接探索会消耗元宝。
			//    因此这里只探索 cost_ingot=0 且 cost_coin=0 的纯免费入口。
			for _, area := range info.Areas {
				if area.CostIngot > 0 || area.CostCoin > 0 {
					slog.Info("禁地之书跳过计费探索入口",
						"role_id", session.RoleID(),
						"type", area.ForbiddenType,
						"explore_type", area.ExploreType,
						"cost_ingot", area.CostIngot,
						"cost_coin", area.CostCoin,
						"cur_number", area.CurNumber,
					)
					continue
				}
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
