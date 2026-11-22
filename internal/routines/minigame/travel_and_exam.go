// Package minigame 提供吉星高照、仙旅奇缘答题、喜从天降、刮刮卡、钓鱼等益智趣味活动。
//
// 本文件实现仙旅奇缘答题、伙伴猜猜看与金榜题名科举答题自动化。
package minigame

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/qa"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

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
				slog.Info("仙旅奇缘事件冷却中，等待冷却结束", "role_id", session.RoleID(), "cd_timer", infoRes.CdTimer)
				return scheduler.NewCooldown(
					time.Duration(infoRes.CdTimer)*time.Second+time.Second,
					"仙旅奇缘事件冷却")
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
					slog.Info("仙旅奇缘事件进入冷却，等待冷却结束后再唤醒",
						"role_id", session.RoleID(),
						"cd_timer", infoRes.CdTimer,
					)
					// 按服务端 cd_timer 精确定点唤醒（+1s 余量），而不是等下一次 Cron。
					return scheduler.NewCooldown(
						time.Duration(infoRes.CdTimer)*time.Second+time.Second,
						"仙旅奇缘事件冷却")
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
				if abortErr, ok := kit.IsAbortErr(err); ok {
					return abortErr
				}
				// 次数查询被静默丢弃时不能 return nil：要等到下一个 cron 时段。
				slog.Info("伙伴猜猜看次数查询无回包，按连续丢包退避",
					"role_id", session.RoleID(), "err", err)
				return scheduler.HandleSilentDropCtx(ctx, session, "partner_guess", 3,
					30*time.Minute, "伙伴猜猜看次数查询无回包")
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
					if abortErr, ok := kit.IsAbortErr(sErr); ok {
						return abortErr
					}
					slog.Info("伙伴猜猜看开局无应答，按连续丢包退避",
						"role_id", session.RoleID(), "err", sErr)
					return scheduler.HandleSilentDropCtx(ctx, session, "partner_guess", 3,
						10*time.Minute, "伙伴猜猜看开局无应答")
				}
				rd, pErr := protocol.ParseGuessPartnerStartResult(startResp.Payload)
				if pErr != nil {
					slog.Warn("解析伙伴猜猜看开局回包失败", "role_id", session.RoleID(), "err", pErr)
					break
				}
				if rd.Result == protocol.GuessPartnerResultPackFull {
					return guessPartnerPackFullError(ctx, session, "start_guess", times)
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
					scRes, next, e := protocol.ParseGuessPartnerScrapeResult(scResp.Payload)
					if e != nil {
						break
					}
					switch scRes {
					case protocol.GuessPartnerResultSuccess:
						// 正常，继续按剩余次数刮图。
					case protocol.GuessPartnerResultPackFull:
						return guessPartnerPackFullError(ctx, session, "scrape", times)
					default:
						slog.Info("伙伴猜猜看刮图未成功，停止刮图", "role_id", session.RoleID(),
							"result", scRes)
						remain = 0
						continue
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
					if abortErr, ok := kit.IsAbortErr(gErr); ok {
						return abortErr
					}
					slog.Info("伙伴猜猜看作答无应答，按连续丢包退避",
						"role_id", session.RoleID(), "err", gErr)
					return scheduler.HandleSilentDropCtx(ctx, session, "partner_guess", 3,
						10*time.Minute, "伙伴猜猜看作答无应答")
				}
				gr, gpErr := protocol.ParseGuessPartnerGuessResult(gResp.Payload)
				if gpErr != nil {
					slog.Warn("解析伙伴猜猜看作答回包失败", "role_id", session.RoleID(), "err", gpErr)
					break
				}
				switch gr.Result {
				case protocol.GuessPartnerResultRight:
					right++
					scheduler.ResetSilentDropsCtx(ctx, session, "partner_guess")
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
					return guessPartnerPackFullError(ctx, session, "guess", times)
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

// logGuessPartnerPackFull 在伙伴猜猜看返回「背包已满」时输出背包实测快照，并返回真实空格数。
//
// 服务端 369 的 PACK_FULL 与 2/41 的真实空格常不一致（2026-11-20 实测 10 个空格仍返回
// PACK_FULL），该码在本服更像是「活动/前置条件不满足」的兜底码。调用方据此区分：
// 空格确实不足才按背包熔断（10 分钟），否则按语义冷却长时间退避，避免整日空转。
// 返回 -1 表示背包快照不可用（查询/解析失败），此时无法证伪，交由调用方保守处理。
func logGuessPartnerPackFull(ctx context.Context, session *client.RoleSession, stage string, remainTimes int32) int {
	pCtx, pCancel := context.WithTimeout(ctx, 8*time.Second)
	resp, err := session.Call(pCtx, protocol.BuildGetPackItemListPacket(), protocol.ActionIDItemGetPackList)
	pCancel()
	if err != nil || resp == nil {
		slog.Warn("伙伴猜猜看返回背包已满，但背包快照查询失败", "role_id", session.RoleID(),
			"stage", stage, "remain_times", remainTimes, "err", err)
		return -1
	}
	pack, pErr := protocol.ParsePackItemListResponse(resp.Payload)
	if pErr != nil || pack == nil {
		slog.Warn("伙伴猜猜看返回背包已满，但背包快照解析失败", "role_id", session.RoleID(),
			"stage", stage, "remain_times", remainTimes, "err", pErr)
		return -1
	}
	freeSlots := int(pack.PackCapacity) - len(pack.PackItems)
	slog.Warn("伙伴猜猜看返回背包已满", "role_id", session.RoleID(),
		"stage", stage, "remain_times", remainTimes,
		"capacity", pack.PackCapacity, "pack_items", len(pack.PackItems),
		"free_slots", freeSlots, "empty_grids", pack.PackEmptyGrids)
	return freeSlots
}

// guessPartnerPackFullError 把 369 的 PACK_FULL 映射为具体的调度语义。
func guessPartnerPackFullError(ctx context.Context, session *client.RoleSession, stage string, remainTimes int32) error {
	freeSlots := logGuessPartnerPackFull(ctx, session, stage, remainTimes)
	if freeSlots < 0 {
		// 无法证伪背包是否已满：按错误类型保守处理，10 分钟后复核。
		return scheduler.ErrBagFull
	}
	if freeSlots > 0 {
		// 背包仍有空格却返回 PACK_FULL：该码在本服并非严格的背包判据，
		// 不再每 10 分钟空转，改为 2 小时语义冷却。
		return scheduler.NewCooldown(2*time.Hour, "伙伴猜猜看返回背包已满但背包仍有空格")
	}
	return scheduler.ErrBagFull
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
				// 该模块在非活动期/未开放时会被服务端静默丢弃；本任务只做状态检测、
				// 不提交答案，10 分钟轮询只会持续丢包。改为 6 小时低频探测。
				slog.Info("金榜题名状态查询无回包，6 小时后再试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(6*time.Hour, "金榜题名状态查询无回包")
			}
			status, err := protocol.ParseImperialExamStatusResult(statusResp.Payload)
			if err != nil {
				return fmt.Errorf("解析金榜题名活动状态失败: %w", err)
			}
			if status.Status != protocol.ImperialExamActivityStart {
				now := time.Now().Unix()
				if int64(status.StartTime) > now {
					slog.Info("金榜题名活动未开始，等待开启",
						"role_id", session.RoleID(), "status", status.Status,
						"start_time", status.StartTime)
					return scheduler.NewCooldownUntil(
						time.Unix(int64(status.StartTime), 0).Add(2*time.Second),
						"金榜题名活动未开始")
				}
				if status.EndTime > 0 && now > int64(status.EndTime) {
					slog.Info("金榜题名活动时间已结束，今日不再检测",
						"role_id", session.RoleID(), "status", status.Status,
						"end_time", status.EndTime)
					return scheduler.ErrAttemptsExhausted
				}
				slog.Info("金榜题名当前非进行中阶段，稍后再查",
					"role_id", session.RoleID(), "status", status.Status,
					"start_time", status.StartTime, "end_time", status.EndTime)
				return scheduler.NewCooldown(30*time.Minute, "金榜题名非进行中")
			}

			answered, right, wrong := 0, 0, 0
			for round := 0; round < 100; round++ {
				qCtx, qCancel := context.WithTimeout(ctx, callTimeout)
				qResp, qErr := session.Call(qCtx, protocol.BuildImperialExamStartPacket(), protocol.ActionIDImperialExamStart)
				qCancel()
				if qErr != nil {
					if abortErr, ok := kit.IsAbortErr(qErr); ok {
						return abortErr
					}
					slog.Info("金榜题名开始答题无应答，按连续丢包退避",
						"role_id", session.RoleID(), "err", qErr)
					return scheduler.HandleSilentDropCtx(ctx, session, "imperial_exam", 3,
						10*time.Minute, "金榜题名开始答题无应答")
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
					if abortErr, ok := kit.IsAbortErr(sErr); ok {
						return abortErr
					}
					slog.Info("金榜题名提交答案无应答，按连续丢包退避",
						"role_id", session.RoleID(), "err", sErr)
					return scheduler.HandleSilentDropCtx(ctx, session, "imperial_exam", 3,
						10*time.Minute, "金榜题名提交答案无应答")
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
					scheduler.ResetSilentDropsCtx(ctx, session, "imperial_exam")
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
