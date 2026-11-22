// Package dungeon 提供副本推图、主线任务循环、精英扫荡、六道轮回、伏魔塔等关卡挑战自动化。
//
// 本文件实现伏魔塔 (Mod_DemonTower_Base) 自动爬塔挑战。
package dungeon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// 锁妖塔单轮挑战上限。官方客户端最多展示 40 层，超过后不再追加下一层。
const demonTowerMaxChallengesPerRun = 40

// demonTowerMaxLossRetries 同一层战斗失败后的最大重试次数。
const demonTowerMaxLossRetries = 3

// demonTowerCallTimeout 锁妖塔单次请求超时。
const demonTowerCallTimeout = 8 * time.Second

// NewDemonTowerRoutine 构造锁妖塔挑战任务 (Feature 9: 锁妖塔)。
//
// 协议对齐 com.protocols.Mod_RepressDemonTower_Base (Module 705):
//
//	panel_info    Act 0 0x02C10000 request=[]
//	challenge     Act 1 0x02C10001 request=[Int layer_id]
//	get_base_info Act 8 0x02C10008 request=[] -> [Byte isSeasonTime]
//
// 每轮先查询面板，按 layer_list 中最高层 +1 作为下一层目标，
// 依据 challenge 回包结果码决定继续、领取次数耗尽退出或熔断。
func NewDemonTowerRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"demon_tower",
		"dungeon",
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
			slog.Info("正在执行副本任务: 锁妖塔挑战与试炼推图", "role_id", session.RoleID())

			// 0. 先通过官方功能开放列表判定门槛，未开放时不再发送会被静默丢弃的业务请求。
			open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDRepressDemonTower)
			if fErr != nil {
				slog.Info("锁妖塔功能开放状态查询失败，跳过", "role_id", session.RoleID(), "err", fErr)
				return nil
			}
			if !open {
				slog.Info("锁妖塔功能未开放，跳过", "role_id", session.RoleID(), "function_id", protocol.FunctionIDRepressDemonTower)
				return nil
			}

			// 1. 查询赛季开放状态，未开赛时面板请求无意义。
			baseCtx, baseCancel := context.WithTimeout(ctx, demonTowerCallTimeout)
			baseResp, baseErr := session.Call(baseCtx, protocol.BuildDemonTowerBaseInfoPacket(), protocol.ActionIDDemonTowerBaseInfo)
			baseCancel()
			if baseErr != nil || baseResp == nil {
				slog.Info("锁妖塔赛季状态查询失败，跳过", "role_id", session.RoleID(), "err", baseErr)
				return nil
			}
			inSeason, parseErr := protocol.ParseDemonTowerBaseInfoResult(baseResp.Payload)
			if parseErr != nil {
				slog.Info("锁妖塔赛季状态解析失败，跳过", "role_id", session.RoleID(), "err", parseErr)
				return nil
			}
			if !inSeason {
				slog.Info("锁妖塔当前不在赛季时间，跳过", "role_id", session.RoleID())
				return nil
			}

			// 2. 循环挑战：每轮以最新面板数据计算下一层目标，成功则继续。
			challenged := 0
			losses := 0
			for round := 0; round < demonTowerMaxChallengesPerRun; round++ {
				if ctx.Err() != nil {
					return nil
				}

				panel, err := fetchDemonTowerPanel(ctx, session)
				if err != nil {
					slog.Info("锁妖塔面板查询失败，结束本轮", "role_id", session.RoleID(), "err", err)
					return nil
				}
				switch panel.Result {
				case protocol.DemonTowerResultSuccess:
					// 继续执行
				case protocol.DemonTowerResultFunctionNoOpen:
					slog.Info("锁妖塔功能未开放，跳过", "role_id", session.RoleID(), "result", panel.Result)
					return nil
				default:
					slog.Info("锁妖塔面板返回失败结果码，结束本轮",
						"role_id", session.RoleID(), "result", panel.Result, "result_text", demonTowerResultText(panel.Result))
					return nil
				}

				if panel.NowTicketNum <= 0 {
					slog.Info("锁妖塔挑战次数已耗尽", "role_id", session.RoleID(), "challenged", challenged)
					return scheduler.ErrAttemptsExhausted
				}

				// 当前阶层数已满但升阶回合条件未满足时，challenge 必然返回
				// RACE_STEP_ERROR(6)，直接收尾并说明原因。
				// 线上实测 2026-11-11：finish_layer=40、total_round=521 > 434。
				if layerNeed, roundNeed, ok := panel.StepUpRequirement(); ok &&
					layerNeed > 0 && roundNeed > 0 &&
					panel.NowFinishLayer >= layerNeed && panel.NowTotalRound > roundNeed {
					slog.Info("锁妖塔当前阶已通关但升阶回合条件未满足，跳过挑战",
						"role_id", session.RoleID(),
						"now_step", panel.NowStep,
						"finish_layer", panel.NowFinishLayer,
						"layer_need", layerNeed,
						"total_round", panel.NowTotalRound,
						"round_need", roundNeed)
					return nil
				}

				target := panel.NextLayerID()
				// 跨阶次时 layer_list 可能清空且 now_finish_layer 归零，
				// 此时用排行榜中的 self_layer 兜底。线上实测 self_layer 即当前
				// 可挑战层，不需 +1（+1 会返回 LAYER_ERROR）。
				if len(panel.LayerList) == 0 && panel.NowFinishLayer <= 0 {
					if selfLayer, rankErr := fetchDemonTowerSelfLayer(ctx, session); rankErr == nil && selfLayer > 0 {
						target = selfLayer
					}
				}

				result, err := challengeDemonTowerLayer(ctx, session, target)
				if err != nil {
					slog.Info("锁妖塔挑战请求失败，结束本轮",
						"role_id", session.RoleID(), "layer", target, "err", err)
					return nil
				}

				if result != protocol.DemonTowerResultSuccess {
					slog.Info("锁妖塔挑战未成功，结束本轮",
						"role_id", session.RoleID(), "layer", target,
						"result", result, "result_text", demonTowerResultText(result),
						"challenged", challenged)
					switch result {
					case protocol.DemonTowerResultFailed:
						losses++
						if losses > demonTowerMaxLossRetries {
							slog.Info("锁妖塔连续挑战失败，达到重试上限，结束本轮",
								"role_id", session.RoleID(), "layer", target,
								"retries", losses, "challenged", challenged)
							return nil
						}
						slog.Info("锁妖塔挑战失败，重试同一层",
							"role_id", session.RoleID(), "layer", target,
							"retry", losses, "max_retries", demonTowerMaxLossRetries,
							"challenged", challenged)
						select {
						case <-ctx.Done():
							return nil
						case <-time.After(time.Second):
						}
						continue
					case protocol.DemonTowerResultNotEnoughTicket:
						return scheduler.ErrAttemptsExhausted
					case protocol.DemonTowerResultItemPackFull:
						return scheduler.ErrBagFull
					case protocol.DemonTowerResultRaceStepError:
						slog.Info("锁妖塔赛季阶段错误（可能已满阶或需升阶）",
							"role_id", session.RoleID(), "layer", target,
							"now_step", panel.NowStep, "finish_layer", panel.NowFinishLayer,
							"total_round", panel.NowTotalRound)
						return nil
					case protocol.DemonTowerResultNotEnoughIngot, protocol.DemonTowerResultBuyNumError:
						// 本实现从不购买次数；出现元宝/购买类错误说明服务端状态异常，记录后熔断。
						return scheduler.ErrConditionNotMet
					default:
						// LAYER_ERROR 表示已到当前阶层的层数上限，属正常收尾；其余失败码本轮退出。
						return nil
					}
				}

				challenged++
				losses = 0
				slog.Info("锁妖塔挑战成功",
					"role_id", session.RoleID(), "layer", target,
					"tickets_before", panel.NowTicketNum, "challenged", challenged)

				if jitter != nil {
					_ = jitter.Wait(ctx)
				}
			}

			slog.Info("锁妖塔单轮挑战达到上限，等待下次调度",
				"role_id", session.RoleID(), "challenged", challenged)
			return nil
		},
	)
}

// fetchDemonTowerPanel 查询并解析锁妖塔主面板。
func fetchDemonTowerPanel(ctx context.Context, session *client.RoleSession) (*protocol.DemonTowerPanel, error) {
	callCtx, cancel := context.WithTimeout(ctx, demonTowerCallTimeout)
	defer cancel()
	resp, err := session.Call(callCtx, protocol.BuildDemonTowerPanelPacket(), protocol.ActionIDDemonTowerPanel)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("锁妖塔面板回包为空")
	}
	return protocol.ParseDemonTowerPanelResult(resp.Payload)
}

// fetchDemonTowerSelfLayer 查询锁妖塔排行榜中的赛季累计通关层数。
func fetchDemonTowerSelfLayer(ctx context.Context, session *client.RoleSession) (int32, error) {
	callCtx, cancel := context.WithTimeout(ctx, demonTowerCallTimeout)
	defer cancel()
	resp, err := session.Call(callCtx, protocol.BuildDemonTowerRankPacket(), protocol.ActionIDDemonTowerRank)
	if err != nil {
		return 0, err
	}
	if resp == nil {
		return 0, errors.New("锁妖塔排行榜回包为空")
	}
	result, selfLayer, err := protocol.ParseDemonTowerRankSelfLayer(resp.Payload)
	if err != nil {
		return 0, err
	}
	if result != protocol.DemonTowerResultSuccess {
		return 0, fmt.Errorf("锁妖塔排行榜结果码 %d", result)
	}
	return selfLayer, nil
}

// challengeDemonTowerLayer 挑战指定层并返回业务结果码。
func challengeDemonTowerLayer(ctx context.Context, session *client.RoleSession, layerID int32) (uint8, error) {
	callCtx, cancel := context.WithTimeout(ctx, demonTowerCallTimeout)
	defer cancel()
	resp, err := session.Call(callCtx, protocol.BuildDemonTowerChallengePacket(layerID), protocol.ActionIDDemonTowerChallenge)
	if err != nil {
		return 0, err
	}
	if resp == nil {
		return 0, errors.New("锁妖塔挑战回包为空")
	}
	return protocol.ParseDemonTowerChallengeResult(resp.Payload)
}

// demonTowerResultText 返回锁妖塔结果码的中文语义文本。
func demonTowerResultText(result uint8) string {
	switch result {
	case protocol.DemonTowerResultSuccess:
		return "成功"
	case protocol.DemonTowerResultFailed:
		return "失败"
	case protocol.DemonTowerResultFunctionNoOpen:
		return "功能未开放"
	case protocol.DemonTowerResultNotEnoughTicket:
		return "挑战次数不足"
	case protocol.DemonTowerResultLayerError:
		return "层数错误"
	case protocol.DemonTowerResultItemPackFull:
		return "背包已满"
	case protocol.DemonTowerResultRaceStepError:
		return "赛季阶段错误"
	case protocol.DemonTowerResultNotEnoughIngot:
		return "元宝不足"
	case protocol.DemonTowerResultBuyNumError:
		return "购买次数错误"
	default:
		return "未知结果码"
	}
}
