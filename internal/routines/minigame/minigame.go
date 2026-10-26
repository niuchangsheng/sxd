// Package minigame 提供吉星高照、仙履奇缘答题、刮刮卡、钓鱼等益智趣味活动。
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

