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
			slog.Info("正在执行益智任务: 帮派吉星高照摇骰子", "role_id", session.RoleID())

			// 1. 查询吉星高照战绩与剩余投掷次数 (Mod 10, Act 45)
			infoPkt := protocol.BuildFactionRollCakeInfoPacket(protocol.ActionIDTownEnter)
			infoCtx, infoCancel := context.WithTimeout(ctx, 3*time.Second)
			infoResp, infoErr := session.Call(infoCtx, infoPkt, protocol.ActionIDFactionRollCakeInfo)
			infoCancel()

			var remain int8
			needRemainFallback := true
			if infoErr == nil && infoResp != nil {
				info, err := protocol.ParseFactionRollCakeInfoResult(infoResp.Payload)
				if err != nil {
					slog.Warn("解析吉星高照战绩回包失败，尝试仅查询剩余次数", "role_id", session.RoleID(), "err", err)
				} else {
					needRemainFallback = false
					remain = info.RemainTimes
					slog.Info("吉星高照当前战绩",
						"role_id", session.RoleID(),
						"week_best_score", info.WeekBestScore,
						"today_best_score", info.TodayBestScore,
						"remain_times", info.RemainTimes,
						"faction_score", info.FactionScore,
						"member_count", len(info.Members),
					)
				}
			} else {
				slog.Warn("查询吉星高照战绩失败，尝试仅查询剩余次数", "role_id", session.RoleID(), "err", infoErr)
			}

			// 2. 战绩查询或解析失败时兜底查询剩余次数 (Mod 10, Act 44)
			if needRemainFallback {
				remainPkt := protocol.BuildFactionRollCakeRemainTimesPacket(protocol.ActionIDTownEnter)
				rCtx, rCancel := context.WithTimeout(ctx, 3*time.Second)
				remainResp, err := session.Call(rCtx, remainPkt, protocol.ActionIDFactionRollCakeRemainTimes)
				rCancel()
				if err != nil || remainResp == nil {
					slog.Warn("查询吉星高照剩余次数失败", "role_id", session.RoleID(), "err", err)
					return nil
				}
				parsed, err := protocol.ParseFactionRollCakeRemainTimesResult(remainResp.Payload)
				if err != nil {
					slog.Warn("解析吉星高照剩余次数回包失败", "role_id", session.RoleID(), "err", err)
					return nil
				}
				remain = parsed
			}

			if remain <= 0 {
				slog.Info("今日吉星高照投掷次数已用尽", "role_id", session.RoleID())
				return nil
			}

			// 3. 循环投掷直至次数用尽 (Mod 10, Act 46)
			prevAct := protocol.ActionIDFactionRollCakeInfo
			if needRemainFallback {
				prevAct = protocol.ActionIDFactionRollCakeRemainTimes
			}
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
func NewPartnerGuessRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"partner_guess",
		"minigame",
		scheduler.ScheduleCron,
		scheduler.PriorityLow,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行益智任务: 伙伴猜猜看自动竞猜")
			return nil
		},
	)
}

// NewFishingRoutine 构造自动钓鱼与命格整理任务。
func NewFishingRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"fishing",
		"minigame",
		scheduler.ScheduleCron,
		scheduler.PriorityLow,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行益智任务: 自动钓鱼与命格吸收融合")
			return nil
		},
	)
}

// NewScratchCardRoutine 构造刮刮卡福利任务。
func NewScratchCardRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"scratch_card",
		"minigame",
		scheduler.ScheduleCron,
		scheduler.PriorityLow,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行益智任务: 幸运刮刮卡福利抽取")
			return nil
		},
	)
}

// NewIceCaveRoutine 构造一键冰窟探险任务。
func NewIceCaveRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"ice_cave",
		"minigame",
		scheduler.ScheduleCron,
		scheduler.PriorityLow,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行益智任务: 一键冰窟最优通关")
			return nil
		},
	)
}

// NewImperialExamRoutine 构造金榜题名会试自动答题任务。
func NewImperialExamRoutine(qaEngine qa.Engine) scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"imperial_exam",
		"minigame",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行益智任务: 金榜题名会试科举智能答题")
			if qaEngine != nil {
				sampleQ := "李白字什么？"
				ans, conf, found := qaEngine.Match(sampleQ)
				if found {
					slog.Debug("金榜题名答题决策成功", "question", sampleQ, "answer", ans, "confidence", conf)
				}
			}
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

			if err != nil || resp == nil {
				slog.Warn("查询山河游历状态超时", "err", err)
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

			if err != nil || resp == nil {
				slog.Warn("查询灵道状态超时", "err", err)
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

			// 1. 查询幻兽面板 (Mod 701, Act 0)
			panelPkt := protocol.BuildHuanshouPanelPacket(protocol.ActionIDTownEnter)
			pCtx, pCancel := context.WithTimeout(ctx, 2*time.Second)
			_, _ = session.Call(pCtx, panelPkt, protocol.ActionIDHuanshouPanel)
			pCancel()

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
			drawPkt := protocol.BuildWuFaDrawCardPacket(fieldType, drawType, protocol.ActionIDWuFaFieldInfo)
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

			// 1. 查询宗师修行面板 (Mod 665, Act 1)
			panelPkt := protocol.BuildMasterPracticePanelPacket(protocol.ActionIDTownEnter)
			pCtx, pCancel := context.WithTimeout(ctx, 2*time.Second)
			resp, err := session.Call(pCtx, panelPkt, protocol.ActionIDMasterPracticePanel)
			pCancel()

			if err != nil || resp == nil {
				slog.Warn("查询宗师修行状态超时", "err", err)
				return nil
			}

			// 2. 选定心法 (Mod 665, Act 3)
			skillPkt := protocol.BuildMasterPracticeSelectSkillPacket(1, 1, protocol.ActionIDMasterPracticePanel)
			sCtx, sCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
			_, _ = session.Call(sCtx, skillPkt, protocol.ActionIDMasterPracticeSelectSkill)
			sCancel()

			// 3. 发起试炼挑战 (Mod 665, Act 2)
			chalPkt := protocol.BuildMasterPracticeChallengePacket(protocol.ActionIDMasterPracticeSelectSkill)
			cCtx, cCancel := context.WithTimeout(ctx, 2*time.Second)
			_, _ = session.Call(cCtx, chalPkt, protocol.ActionIDMasterPracticeChallenge)
			cCancel()

			// 3.1 极速通关试炼报告 (Mod 665, Act 4: 抓包实采 00 0f cc af 09 秒过试炼)
			finishPkt := protocol.BuildMasterPracticeFastFinishPacket(protocol.ActionIDMasterPracticeChallenge)
			fCtx, fCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
			_, _ = session.Call(fCtx, finishPkt, protocol.ActionIDMasterPracticeSettle)
			fCancel()

			// 4. 境界突破 (Mod 665, Act 5)
			breakPkt := protocol.BuildMasterPracticeBreakthroughPacket(1, 1, protocol.ActionIDMasterPracticeSettle)
			bCtx, bCancel := context.WithTimeout(ctx, 2*time.Second)
			bResp, bErr := session.Call(bCtx, breakPkt, protocol.ActionIDMasterPracticeBreakthrough)
			bCancel()

			if bErr == nil && bResp != nil {
				slog.Info("宗师境界突破成功", "role_id", session.RoleID())
			}
			return nil
		},
	)
}

// AwardSelectorFunc 允许注入自定义交互选择器（例如命令行交互、Web交互回调或自动化测试桩）。
type AwardSelectorFunc func(ctx context.Context, pool *protocol.TreasureHousePool, dict dictionary.Repository) ([]protocol.TreasureAwardSelect, error)

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
		scheduler.PriorityNormal,
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

			// 1. 查询宝库基本信息 (Mod 594, Act 2)
			infoPkt := protocol.BuildTreasureHouseInfoPacket(protocol.ActionIDTownEnter)
			iCtx, iCancel := context.WithTimeout(ctx, 2*time.Second)
			resp, err := session.Call(iCtx, infoPkt, protocol.ActionIDTreasureHouseInfo)
			iCancel()

			if err != nil || resp == nil {
				slog.Warn("查询天帝宝库状态超时", "err", err)
				return nil
			}

			// 2. 向服务端拉取刷新奖池并解析当前可选道具 (Mod 594, Act 9)
			poolPkt := protocol.BuildTreasureHousePoolPacket(protocol.ActionIDTreasureHouseInfo)
			pCtx, pCancel := context.WithTimeout(ctx, 2*time.Second)
			poolResp, poolErr := session.Call(pCtx, poolPkt, protocol.ActionIDTreasureHousePool)
			pCancel()

			selectedAwards := cfg.Awards
			if poolErr == nil && poolResp != nil {
				if pool, pErr := protocol.ParseTreasureHousePoolResult(poolResp.Payload); pErr == nil && pool != nil {
					slog.Info("天帝宝库备选奖池拉取成功",
						"role_id", session.RoleID(),
						"total_items", pool.TotalCount,
						"tier1_options", pool.Tier1Items,
						"tier2_options", pool.Tier2Items,
						"tier3_options", pool.Tier3Items,
					)

					// 交互选奖：若配置了 Selector 则优先调用，否则若开启 Interactive 则通过交互式终端输入
					if cfg.Selector != nil {
						if chosen, cErr := cfg.Selector(ctx, pool, cfg.DictRepo); cErr == nil && len(chosen) >= 3 {
							selectedAwards = chosen
						}
					} else if cfg.Interactive {
						selectedAwards = promptUserAwardSelection(ctx, cfg.In, cfg.Out, pool, cfg.DictRepo, cfg.Awards)
					}
				}
			}

			// 3. 设定自选大奖 (Mod 594, Act 3: 依据用户交互输入或配置设定三档奖品)
			selPkt := protocol.BuildTreasureHouseSelectPacket(selectedAwards, protocol.ActionIDTreasureHousePool)
			sCtx, sCancel := context.WithTimeout(ctx, 2*time.Second)
			_, _ = session.Call(sCtx, selPkt, protocol.ActionIDTreasureHouseSelect)
			sCancel()

			// 4. 确认锁定自选大奖 (Mod 594, Act 4)
			confPkt := protocol.BuildTreasureHouseConfirmPacket(1, protocol.ActionIDTreasureHouseSelect)
			cCtx, cCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
			_, _ = session.Call(cCtx, confPkt, protocol.ActionIDTreasureHouseConfirm)
			cCancel()

			// 5. 消耗宝库钥匙开启抽奖 (Mod 594, Act 6 与 Act 1 连续抽奖流)
			drawCount := 0
			prevAct := protocol.ActionIDTreasureHouseConfirm
			for i := 0; i < cfg.MaxDrawTimes; i++ {
				drawPkt := protocol.BuildTreasureHouseDrawPacket(prevAct)
				dCtx, dCancel := context.WithTimeout(ctx, 2*time.Second)
				dResp, dErr := session.Call(dCtx, drawPkt, protocol.ActionIDTreasureHouseDraw)
				dCancel()

				if dErr != nil || dResp == nil {
					break
				}
				drawCount++
				prevAct = protocol.ActionIDTreasureHouseDraw

				// 每次抽奖后刷新宝库状态 (Act 1)
				refPkt := protocol.BuildTreasureHouseRefreshPacket(prevAct)
				rCtx, rCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
				_, _ = session.Call(rCtx, refPkt, protocol.ActionIDTreasureHouseRefresh)
				rCancel()
				prevAct = protocol.ActionIDTreasureHouseRefresh

				time.Sleep(50 * time.Millisecond)
			}

			slog.Info("天帝宝库抽奖流程完成", "role_id", session.RoleID(), "draw_count", drawCount)
			return nil
		},
	)
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

	// 3. 串行召唤并直接领取龙鱼奖励，直到龙鱼令耗尽 (Mod 68, Act 2 + Act 4)
	initialTokens := tokens
	rounds := 0
	for tokens > 0 {
		if err := ctx.Err(); err != nil {
			slog.Info("喜从天降任务已取消，提前退出", "role_id", session.RoleID(), "err", err)
			return
		}
		if jitter != nil {
			_ = jitter.Wait(ctx)
		}

		beforeTokens := tokens
		startPkt := protocol.BuildFindImmortalStartPacket(prevAct)
		sCtx, sCancel := context.WithTimeout(ctx, 3*time.Second)
		sResp, sErr := session.Call(sCtx, startPkt, protocol.ActionIDFindImmortalStart)
		sCancel()
		if sErr != nil || sResp == nil {
			slog.Warn("喜从天降召唤龙鱼未收到回包", "role_id", session.RoleID(), "err", sErr)
			return
		}
		start, err := protocol.ParseFindImmortalStartResult(sResp.Payload)
		if err != nil {
			slog.Warn("解析喜从天降召唤龙鱼回包失败", "role_id", session.RoleID(), "err", err)
			return
		}
		prevAct = protocol.ActionIDFindImmortalStart
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
		if start.RemainNumber >= beforeTokens {
			slog.Warn("喜从天降龙鱼令未按预期递减，熔断退出",
				"role_id", session.RoleID(),
				"before_xun_xian_ling", beforeTokens,
				"after_xun_xian_ling", start.RemainNumber,
			)
			tokens = start.RemainNumber
			break
		}
		tokens = start.RemainNumber
		rounds++

		pickupFindImmortal(ctx, session, &prevAct)
	}

	slog.Info("喜从天降画龙鱼流程完成",
		"role_id", session.RoleID(),
		"initial_xun_xian_ling", initialTokens,
		"rounds", rounds,
		"remain_xun_xian_ling", tokens,
	)
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
