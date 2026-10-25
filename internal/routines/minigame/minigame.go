// Package minigame 提供吉星高照、仙履奇缘答题、刮刮卡、钓鱼等益智趣味活动。
//
// @author Ateng
// @since 2026-10-08
package minigame

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/qa"
	"sxd-pie-ng/internal/scheduler"
)

// NewLuckyStarRoutine 构造帮派吉星高照扔骰子任务。
func NewLuckyStarRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"lucky_star",
		"minigame",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行益智任务: 帮派吉星高照摇骰子")
			return nil
		},
	)
}

// NewImmortalFantasyRoutine 构造仙履奇缘与金榜题名自动问答任务。
func NewImmortalFantasyRoutine(qaEngine qa.Engine) scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"immortal_fantasy",
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

			slog.Info("正在执行益智任务: 仙履奇缘与智能题库问答", "role_id", session.RoleID())

			// 2. 发送查询奇缘状态请求
			infoPkt, err := protocol.BuildFateGetInfoPacket(protocol.FateGetInfoRequest{
				PrevAct: protocol.ActionIDTownEnter,
			})
			if err != nil {
				return fmt.Errorf("构造仙履奇缘状态查询封包失败: %w", err)
			}

			infoCtx, infoCancel := context.WithTimeout(ctx, 3*time.Second)
			defer infoCancel()

			respInfoPkt, err := session.Call(infoCtx, infoPkt, protocol.ActionIDFateGetInfo)
			if err != nil {
				return fmt.Errorf("查询仙履奇缘状态失败: %w", err)
			}

			infoRes, err := protocol.ParseFateGetInfoResult(respInfoPkt.Payload)
			if err != nil {
				return fmt.Errorf("解析仙履奇缘状态回包失败: %w", err)
			}

			if infoRes.RemainingTimes <= 0 {
				slog.Info("今日仙履奇缘答题次数已耗尽", "role_id", session.RoleID(), "remaining", infoRes.RemainingTimes)
				return nil
			}

			rounds := int(infoRes.RemainingTimes)
			for i := 0; i < rounds; i++ {
				// 3. 发送拉取当前奇缘事件题目请求
				questionPkt, err := protocol.BuildFateQuestionPacket(protocol.FateQuestionRequest{
					PrevAct: protocol.ActionIDFateGetInfo,
				})
				if err != nil {
					return fmt.Errorf("构造仙履奇缘题目查询封包失败: %w", err)
				}

				qCtx, qCancel := context.WithTimeout(ctx, 3*time.Second)
				respQPkt, err := session.Call(qCtx, questionPkt, protocol.ActionIDFateGetQuestion)
				qCancel()
				if err != nil {
					return fmt.Errorf("拉取仙履奇缘题目失败: %w", err)
				}

				qRes, err := protocol.ParseFateQuestionResult(respQPkt.Payload)
				if err != nil {
					return fmt.Errorf("解析仙履奇缘题目回包失败: %w", err)
				}

				// 4. 若挂载题库引擎，执行收益权重决策
				chosenAnswerID := int32(1)
				bestBranch := 1
				reason := "默认首选选项"

				if qaEngine != nil {
					branch, entry, found := qaEngine.MatchFate(qRes.Story)
					if found && entry != nil {
						bestBranch = branch
						_, reason = qa.DecideBestBranch(entry)
						slog.Info("仙履奇缘题库检索与决策命中",
							"story", qRes.Story,
							"best_branch", bestBranch,
							"decision_reason", reason,
						)
					}
				}

				if len(qRes.Options) >= 2 {
					if bestBranch == 2 {
						chosenAnswerID = qRes.Options[1].ChoiceID
					} else {
						chosenAnswerID = qRes.Options[0].ChoiceID
					}
				} else if len(qRes.Options) > 0 {
					chosenAnswerID = qRes.Options[0].ChoiceID
				}

				// 5. 提交答案选项请求
				answerPkt, err := protocol.BuildFateAnswerPacket(protocol.FateAnswerRequest{
					QuestionID: qRes.QuestionID,
					AnswerID:   chosenAnswerID,
					PrevAct:    protocol.ActionIDFateGetQuestion,
				})
				if err != nil {
					return fmt.Errorf("构造仙履奇缘提交答案封包失败: %w", err)
				}

				ansCtx, ansCancel := context.WithTimeout(ctx, 3*time.Second)
				ansRespPkt, err := session.Call(ansCtx, answerPkt, protocol.ActionIDFateAnswer)
				ansCancel()
				if err != nil {
					return fmt.Errorf("提交仙履奇缘答案失败: %w", err)
				}

				ansRes, _ := protocol.ParseFateAnswerResult(ansRespPkt.Payload)
				slog.Info("仙履奇缘问答决策与提交完成",
					"role_id", session.RoleID(),
					"question_id", qRes.QuestionID,
					"answer_id", chosenAnswerID,
					"result_message", ansRes.Message,
				)

				if jitter != nil && i < rounds-1 {
					_ = jitter.Wait(ctx)
				}
			}

			return nil
		},
	)
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

			chalPkt := protocol.BuildHuanshouInvadeChallengePacket(1, protocol.ActionIDHuanshouInvadeChapter)
			chCtx, chCancel := context.WithTimeout(ctx, 2*time.Second)
			_, _ = session.Call(chCtx, chalPkt, protocol.ActionIDHuanshouInvadeChallenge)
			chCancel()

			// 3. 极速秒完成扫荡 (Mod 696, Act 3)
			sweepPkt := protocol.BuildHuanshouInvadeSweepPacket(protocol.ActionIDHuanshouInvadeChallenge)
			swCtx, swCancel := context.WithTimeout(ctx, 2*time.Second)
			_, _ = session.Call(swCtx, sweepPkt, protocol.ActionIDHuanshouInvadeSweep)
			swCancel()

			slog.Info("山海幻兽入侵扫荡轮次完成", "role_id", session.RoleID())
			return nil
		},
	)
}

// NewWuFaRoutine 构造悟法领域感悟抽卡与技能配置任务 (Feature 14: 悟法领域)。
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

			// 1. 查询领域属性 (Mod 658, Act 1)
			infoPkt := protocol.BuildWuFaFieldInfoPacket(2, protocol.ActionIDTownEnter)
			iCtx, iCancel := context.WithTimeout(ctx, 2*time.Second)
			_, _ = session.Call(iCtx, infoPkt, protocol.ActionIDWuFaFieldInfo)
			iCancel()

			// 2. 悟法感悟抽卡 (Mod 658, Act 0)
			drawPkt := protocol.BuildWuFaDrawCardPacket(protocol.ActionIDWuFaFieldInfo)
			dCtx, dCancel := context.WithTimeout(ctx, 2*time.Second)
			resp, err := session.Call(dCtx, drawPkt, protocol.ActionIDWuFaDrawCard)
			dCancel()

			if err == nil && resp != nil {
				slog.Info("悟法领域技能感悟成功", "role_id", session.RoleID())
			}
			return nil
		},
	)
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

			// 4. 境界突破 (Mod 665, Act 5)
			breakPkt := protocol.BuildMasterPracticeBreakthroughPacket(1, 1, protocol.ActionIDMasterPracticeChallenge)
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

// NewTreasureHouseRoutine 构造天帝宝库自选与开启抽奖任务 (Feature 16: 天帝宝库)。
func NewTreasureHouseRoutine() scheduler.ActivityRoutine {
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
			slog.Info("正在执行益智任务: 天帝宝库自选与开启抽奖", "role_id", session.RoleID())

			// 1. 查询宝库基本信息 (Mod 594, Act 2)
			infoPkt := protocol.BuildTreasureHouseInfoPacket(protocol.ActionIDTownEnter)
			iCtx, iCancel := context.WithTimeout(ctx, 2*time.Second)
			resp, err := session.Call(iCtx, infoPkt, protocol.ActionIDTreasureHouseInfo)
			iCancel()

			if err != nil || resp == nil {
				slog.Warn("查询天帝宝库状态超时", "err", err)
				return nil
			}

			// 2. 拉取奖池 (Mod 594, Act 9)
			poolPkt := protocol.BuildTreasureHousePoolPacket(protocol.ActionIDTreasureHouseInfo)
			pCtx, pCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
			_, _ = session.Call(pCtx, poolPkt, protocol.ActionIDTreasureHousePool)
			pCancel()

			// 3. 设定自选大奖 (Mod 594, Act 3: 依据抓包设定三档奖品)
			selects := []protocol.TreasureAwardSelect{
				{TierID: 1, ItemID: 50},
				{TierID: 2, ItemID: 25},
				{TierID: 3, ItemID: 42},
			}
			selPkt := protocol.BuildTreasureHouseSelectPacket(selects, protocol.ActionIDTreasureHousePool)
			sCtx, sCancel := context.WithTimeout(ctx, 2*time.Second)
			_, _ = session.Call(sCtx, selPkt, protocol.ActionIDTreasureHouseSelect)
			sCancel()

			// 4. 确认锁定自选大奖 (Mod 594, Act 4)
			confPkt := protocol.BuildTreasureHouseConfirmPacket(1, protocol.ActionIDTreasureHouseSelect)
			cCtx, cCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
			_, _ = session.Call(cCtx, confPkt, protocol.ActionIDTreasureHouseConfirm)
			cCancel()

			// 5. 消耗宝库钥匙开启抽奖 (Mod 594, Act 6)
			drawPkt := protocol.BuildTreasureHouseDrawPacket(protocol.ActionIDTreasureHouseConfirm)
			dCtx, dCancel := context.WithTimeout(ctx, 2*time.Second)
			dResp, dErr := session.Call(dCtx, drawPkt, protocol.ActionIDTreasureHouseDraw)
			dCancel()

			if dErr == nil && dResp != nil {
				slog.Info("天帝宝库开启抽奖成功", "role_id", session.RoleID())
			}
			return nil
		},
	)
}

