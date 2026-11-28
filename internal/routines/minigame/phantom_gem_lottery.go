// Package minigame 提供益智趣味与日常养成活动自动化。
//
// 本文件实现「山海幻兽·多宝山普通寻宝」(Mod_PhantomGemLottery_Base, Module 694 / 0x02B6) 自动化：
//   - 四座宝山：沧溟 (1)、玄晖 (2)、翠青 (3)、灵玉 (4)，默认选择灵玉；
//   - 仅执行普通寻宝 (find_type=0，消耗道源)，永不调用高级寻宝 (find_type=1，消耗元宝/寻宝司南)；
//   - 先通过 FunctionIDEudemons (256) 检测功能开放状态，再拉取 694/0 panel_info 读取剩余普通寻宝次数
//     （含宗门 buff 额外普通次数 remain_extra_normal_times）；
//   - 每次 694/1 find 严格解析首字节 result_code 分流，成功后复查 694/0 校验剩余次数是否扣减。
package minigame

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

const (
	phantomGemLotteryRoutineName = "phantom_gem_lottery"
	phantomGemLotteryCallTimeout = 6 * time.Second
	maxPhantomGemLotteryFinds    = 10
)

// PhantomGemLotteryConfig 多宝山普通寻宝配置。
type PhantomGemLotteryConfig struct {
	// Mountain 目标宝山：沧溟 / 玄晖 / 翠青 / 灵玉（也支持拼音或 1~4），留空默认 "灵玉"。
	Mountain string
}

// DefaultPhantomGemLotteryConfig 返回默认配置（默认灵玉宝山）。
func DefaultPhantomGemLotteryConfig() PhantomGemLotteryConfig {
	return PhantomGemLotteryConfig{
		Mountain: "灵玉",
	}
}

// NewPhantomGemLotteryRoutine 构造山海幻兽·多宝山普通寻宝任务。
func NewPhantomGemLotteryRoutine(cfgs ...PhantomGemLotteryConfig) scheduler.ActivityRoutine {
	cfg := DefaultPhantomGemLotteryConfig()
	if len(cfgs) > 0 && cfgs[0].Mountain != "" {
		cfg.Mountain = cfgs[0].Mountain
	}

	return scheduler.NewBaseRoutine(
		phantomGemLotteryRoutineName,
		"minigame",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				if err := jitter.Wait(ctx); err != nil {
					return err
				}
			}

			mountainID, ok := protocol.ParsePhantomGemMountain(cfg.Mountain)
			if !ok {
				slog.Warn("多宝山普通寻宝配置的目标宝山无效，回退默认灵玉",
					"role_id", session.RoleID(),
					"configured", cfg.Mountain,
				)
				mountainID = protocol.PhantomGemMountainLingYu
			}
			mountainName := protocol.PhantomGemMountainName(mountainID)

			slog.Info("正在执行益智任务: 多宝山普通寻宝",
				"role_id", session.RoleID(),
				"mountain", mountainName,
				"mountain_id", mountainID,
			)

			// 1. 原生探测山海幻兽功能是否开放 (FunctionIDEudemons = 256)。
			open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDEudemons)
			if fErr != nil {
				if abortErr, isAbort := kit.IsAbortErr(fErr); isAbort {
					return abortErr
				}
				slog.Info("多宝山普通寻宝功能开放状态查询失败，跳过",
					"role_id", session.RoleID(),
					"err", fErr,
				)
				return nil
			}
			if !open {
				slog.Info("山海幻兽功能未开放，跳过多宝山普通寻宝",
					"role_id", session.RoleID(),
					"function_id", protocol.FunctionIDEudemons,
				)
				return scheduler.ErrConditionNotMet
			}

			caller := kit.NewCaller(ctx, session, phantomGemLotteryCallTimeout)

			fetchPanel := func() (*protocol.PhantomGemLotteryPanel, error) {
				resp, err := caller.Call(protocol.BuildPhantomGemLotteryPanelPacket(), protocol.ActionIDPhantomGemLotteryPanel)
				if err != nil {
					return nil, err
				}
				if resp == nil {
					return nil, fmt.Errorf("多宝山面板无回包")
				}
				return protocol.ParsePhantomGemLotteryPanelResult(resp.Payload)
			}

			// 2. 拉取 694/0 面板，检查目标宝山与剩余普通寻宝次数。
			panel, err := fetchPanel()
			if err != nil {
				if abortErr, isAbort := kit.IsAbortErr(err); isAbort {
					return abortErr
				}
				if errors.Is(err, context.DeadlineExceeded) {
					return scheduler.HandleSilentDropCtx(ctx, session, phantomGemLotteryRoutineName, 3, 6*time.Hour,
						fmt.Sprintf("多宝山面板 694/0 超时: %v", err))
				}
				slog.Warn("多宝山面板获取/解析失败，跳过本轮",
					"role_id", session.RoleID(),
					"err", err,
				)
				return nil
			}

			mountain, found := panel.MountainByID(mountainID)
			if !found {
				slog.Warn("多宝山面板未包含目标宝山，跳过本轮",
					"role_id", session.RoleID(),
					"mountain", mountainName,
					"mountain_id", mountainID,
					"panel_mountains", len(panel.Mountains),
				)
				return scheduler.ErrConditionNotMet
			}

			remain := mountain.TotalRemainNormalTimes()
			slog.Info("多宝山面板状态",
				"role_id", session.RoleID(),
				"mountain", mountainName,
				"mountain_id", mountainID,
				"remain_normal", mountain.RemainNormalTimes,
				"remain_extra_normal", mountain.RemainExtraNormalTimes,
				"requite_value", panel.RequiteValue,
			)
			if remain <= 0 {
				slog.Info("多宝山今日普通寻宝次数已用尽",
					"role_id", session.RoleID(),
					"mountain", mountainName,
				)
				return scheduler.ErrAttemptsExhausted
			}

			// 3. 逐次发起普通寻宝 (find_type = 0，消耗道源)，严禁调用高级寻宝。
			finds := 0
			for remain > 0 && finds < maxPhantomGemLotteryFinds {
				if err := ctx.Err(); err != nil {
					return err
				}

				resp, fErr := caller.Call(
					protocol.BuildPhantomGemLotteryFindPacket(mountainID, protocol.PhantomGemFindTypeNormal),
					protocol.ActionIDPhantomGemLotteryFind,
				)
				if fErr != nil || resp == nil {
					if abortErr, isAbort := kit.IsAbortErr(fErr); isAbort {
						return abortErr
					}
					if errors.Is(fErr, context.DeadlineExceeded) || resp == nil {
						return scheduler.HandleSilentDropCtx(ctx, session, phantomGemLotteryRoutineName, 3, 6*time.Hour,
							fmt.Sprintf("多宝山普通寻宝 694/1 超时: %v", fErr))
					}
					slog.Warn("多宝山普通寻宝请求失败",
						"role_id", session.RoleID(),
						"mountain", mountainName,
						"err", fErr,
					)
					return nil
				}

				res, pErr := protocol.ParsePhantomGemLotteryFindResult(resp.Payload)
				if pErr != nil {
					slog.Warn("解析多宝山普通寻宝回包失败",
						"role_id", session.RoleID(),
						"mountain", mountainName,
						"err", pErr,
					)
					return nil
				}

				switch res.Result {
				case protocol.PhantomGemLotteryResultSuccess:
					scheduler.ResetSilentDropsCtx(ctx, session, phantomGemLotteryRoutineName)
					finds++
					slog.Info("多宝山普通寻宝成功",
						"role_id", session.RoleID(),
						"mountain", mountainName,
						"mountain_id", mountainID,
						"finds", finds,
						"awards", res.Awards,
						"lucky_items", res.LuckyItems,
					)

					// 对齐官方客户端：寻宝成功后刷新 694/0 面板，校验剩余次数确实扣减（副作用校验）。
					prevRemain := remain
					nextPanel, npErr := fetchPanel()
					if npErr != nil {
						if abortErr, isAbort := kit.IsAbortErr(npErr); isAbort {
							return abortErr
						}
						slog.Warn("多宝山普通寻宝后刷新面板失败，结束本轮",
							"role_id", session.RoleID(),
							"mountain", mountainName,
							"err", npErr,
						)
						return nil
					}
					nextMountain, nextFound := nextPanel.MountainByID(mountainID)
					if !nextFound {
						return nil
					}
					newRemain := nextMountain.TotalRemainNormalTimes()
					if newRemain >= prevRemain {
						slog.Warn("多宝山普通寻宝返回成功但剩余次数未减少，停止继续发包",
							"role_id", session.RoleID(),
							"mountain", mountainName,
							"prev_remain", prevRemain,
							"new_remain", newRemain,
						)
						return nil
					}
					remain = newRemain

				case protocol.PhantomGemLotteryResultNotEnoughTimes:
					slog.Info("多宝山普通寻宝次数不足",
						"role_id", session.RoleID(),
						"mountain", mountainName,
						"result_code", res.Result,
					)
					return scheduler.ErrAttemptsExhausted

				case protocol.PhantomGemLotteryResultNotEnoughDaoyuan:
					slog.Info("多宝山普通寻宝道缘不足，停止本轮",
						"role_id", session.RoleID(),
						"mountain", mountainName,
						"result_code", res.Result,
					)
					return scheduler.ErrConditionNotMet

				case protocol.PhantomGemLotteryResultEudemonsFunNotOpened:
					slog.Info("山海幻兽功能未开启，停止多宝山普通寻宝",
						"role_id", session.RoleID(),
						"mountain", mountainName,
						"result_code", res.Result,
					)
					return scheduler.ErrConditionNotMet

				case protocol.PhantomGemLotteryResultItemPackFull:
					slog.Warn("多宝山普通寻宝失败: 背包已满",
						"role_id", session.RoleID(),
						"mountain", mountainName,
						"result_code", res.Result,
					)
					return scheduler.ErrBagFull

				case protocol.PhantomGemLotteryResultNotEnoughIngot:
					slog.Warn("多宝山普通寻宝返回元宝不足（异常分支），立即停止",
						"role_id", session.RoleID(),
						"mountain", mountainName,
						"action_id", fmt.Sprintf("0x%08X", protocol.ActionIDPhantomGemLotteryFind),
						"result_code", res.Result,
					)
					return scheduler.ErrConditionNotMet

				default:
					slog.Warn("多宝山普通寻宝返回失败/未知结果码，终止本轮",
						"routine", phantomGemLotteryRoutineName,
						"role_id", session.RoleID(),
						"mountain", mountainName,
						"action_id", fmt.Sprintf("0x%08X", protocol.ActionIDPhantomGemLotteryFind),
						"result_code", res.Result,
						"result_name", protocol.PhantomGemLotteryResultName(res.Result),
					)
					return nil
				}

				if remain > 0 {
					if !kit.Sleep(ctx, 300*time.Millisecond) {
						return ctx.Err()
					}
				}
			}

			slog.Info("多宝山普通寻宝完成",
				"role_id", session.RoleID(),
				"mountain", mountainName,
				"finds", finds,
				"remain", remain,
			)
			if remain <= 0 {
				return scheduler.ErrAttemptsExhausted
			}
			return nil
		},
	)
}
