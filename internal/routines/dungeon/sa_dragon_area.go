// Package dungeon 提供副本推图、扫荡与爬塔等高强度 PvE 玩法自动化。
//
// 本文件实现圣域「无尽龙域 / 龙域战场」(Mod_SaDragonArea_Base, Module 362) 自动扫荡与逐层挑战。
//
// 协议依据官方 Main.swf (Mod_SaDragonArea_Base / SaDragonAreaData / SaDragonAreaView):
//   - 全部动作走圣域独立连接 (DataBase.SOCKETSAINTAREA):
//     1. 362/0 get_dragon_area_info 读取当前层数 seq、可免费一键扫荡上限 quick_seq 与 is_dragon_clear；
//     2. 若 seq <= quick_seq，先调用 362/4 quick_fight 免费一键扫荡；
//     3. 随后循环调用 362/2 fight 逐层挑战，并在每次 SUCCESS 后复查 362/0 确认 seq 是否推进；
//     4. 同一层连续战败达到阈值、收到 COMPLETE_ALL 或 is_dragon_clear != 0 时停止。
//
// 全程不消耗元宝。
package dungeon

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
	saDragonAreaMaxFightsPerRun = 50
	saDragonAreaMaxCDRetries    = 5
	saDragonAreaMaxLossRetries  = 3
	saDragonAreaCDWait          = 2 * time.Second
	saDragonAreaCallTimeout     = 10 * time.Second
)

// openDragonAreaSaintSession 建立圣域独立连接；抽成变量便于单测注入。
var openDragonAreaSaintSession = func(ctx context.Context, s *client.RoleSession) (*client.RoleSession, error) {
	return s.OpenSaintAreaSession(ctx)
}

// NewSaDragonAreaRoutine 构造圣域「无尽龙域 / 龙域战场」自动挑战与免费扫荡任务。
func NewSaDragonAreaRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"sa_dragon_area",
		"dungeon",
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
			slog.Info("正在执行圣域副本任务: 无尽龙域（龙域战场）", "role_id", session.RoleID())

			// 1. 前置检查圣域功能是否开放（未开放区服直接跳过，不发起无效建连）。
			open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDSaintArea)
			if fErr != nil {
				if errors.Is(fErr, client.ErrNotConnected) || errors.Is(fErr, client.ErrSessionClosed) {
					return fErr
				}
				slog.Info("无尽龙域圣域开放状态查询失败，跳过", "role_id", session.RoleID(), "err", fErr)
				return nil
			}
			if !open {
				slog.Info("圣域功能未开放，跳过无尽龙域", "role_id", session.RoleID())
				return scheduler.ErrConditionNotMet
			}

			// 2. 建立圣域独立会话 (Mod_SaDragonArea_Base 走 SOCKETSAINTAREA)。
			sa, saErr := openDragonAreaSaintSession(ctx, session)
			if saErr != nil {
				if errors.Is(saErr, client.ErrSaintAreaCredentialMissing) {
					slog.Info("当前角色/区服圣域凭据为空（未开放圣域），跳过无尽龙域",
						"role_id", session.RoleID())
					return scheduler.ErrConditionNotMet
				}
				if errors.Is(saErr, client.ErrNotConnected) || errors.Is(saErr, client.ErrSessionClosed) {
					return saErr
				}
				slog.Warn("建立无尽龙域圣域会话失败，稍后重试", "role_id", session.RoleID(), "err", saErr)
				return scheduler.NewCooldown(30*time.Minute, "无尽龙域圣域会话建立失败")
			}
			defer sa.Close()

			fetchInfo := func() (*protocol.SaDragonAreaInfo, error) {
				resp, err := kit.Call(ctx, sa, protocol.BuildSaDragonAreaInfoPacket(),
					protocol.ActionIDSaDragonAreaInfo, saDragonAreaCallTimeout)
				if err != nil {
					return nil, err
				}
				if resp == nil {
					return nil, errors.New("无尽龙域面板无回包")
				}
				return protocol.ParseSaDragonAreaInfo(resp.Payload)
			}

			info, err := fetchInfo()
			if err != nil {
				if abort, ok := kit.IsAbortErr(err); ok {
					return abort
				}
				if errors.Is(err, context.DeadlineExceeded) {
					return scheduler.HandleSilentDropCtx(ctx, session, "sa_dragon_area", 3,
						30*time.Minute, "无尽龙域面板无回包")
				}
				slog.Warn("查询无尽龙域面板失败", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(30*time.Minute, "无尽龙域面板查询失败")
			}
			scheduler.ResetSilentDropsCtx(ctx, session, "sa_dragon_area")

			slog.Info("无尽龙域面板",
				"role_id", session.RoleID(),
				"seq", info.Seq,
				"quick_seq", info.QuickSeq,
				"last_week_seq", info.LastWeekSeq,
				"is_dragon_clear", info.IsDragonClear,
			)

			if info.IsDragonClear != 0 {
				slog.Info("无尽龙域本期已全部通关", "role_id", session.RoleID(), "seq", info.Seq)
				return scheduler.ErrAttemptsExhausted
			}

			// 3. 若存在可免费一键扫荡的层数 (seq <= quick_seq)，优先执行 362/4 quick_fight。
			if info.QuickSeq > 0 && info.Seq > 0 && info.Seq <= info.QuickSeq {
				qResp, qErr := kit.Call(ctx, sa, protocol.BuildSaDragonAreaQuickFightPacket(),
					protocol.ActionIDSaDragonAreaQuickFight, saDragonAreaCallTimeout)
				if qErr != nil {
					if abort, ok := kit.IsAbortErr(qErr); ok {
						return abort
					}
					slog.Warn("无尽龙域免费一键扫荡失败，继续尝试逐层挑战",
						"role_id", session.RoleID(), "err", qErr)
				} else if qResp != nil {
					entries, pErr := protocol.ParseSaDragonAreaQuickFightResult(qResp.Payload)
					if pErr != nil {
						slog.Warn("解析无尽龙域一键扫荡回包失败", "role_id", session.RoleID(), "err", pErr)
					} else {
						slog.Info("无尽龙域免费一键扫荡完成",
							"role_id", session.RoleID(),
							"swept_floors", len(entries),
							"from_seq", info.Seq,
							"quick_seq", info.QuickSeq,
						)
					}
					// 扫荡后刷新面板获取最新层数与通关状态。
					if refreshed, rErr := fetchInfo(); rErr == nil {
						info = refreshed
						if info.IsDragonClear != 0 {
							slog.Info("无尽龙域一键扫荡后已全部通关", "role_id", session.RoleID())
							return scheduler.ErrAttemptsExhausted
						}
					}
				}
			}

			// 4. 逐层挑战 (362/2 fight)，直至全部通关、连续战败达到阈值或冷却/关闭。
			challenged := 0
			cleared := 0
			lossRetries := 0
			cdRetries := 0

			for challenged < saDragonAreaMaxFightsPerRun {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if info.IsDragonClear != 0 {
					slog.Info("无尽龙域已全部通关",
						"role_id", session.RoleID(), "cleared", cleared, "seq", info.Seq)
					return scheduler.ErrAttemptsExhausted
				}

				prevSeq := info.Seq
				fResp, fErr := kit.Call(ctx, sa, protocol.BuildSaDragonAreaFightPacket(),
					protocol.ActionIDSaDragonAreaFight, saDragonAreaCallTimeout)
				if fErr != nil {
					if abort, ok := kit.IsAbortErr(fErr); ok {
						return abort
					}
					if errors.Is(fErr, context.DeadlineExceeded) {
						return scheduler.HandleSilentDropCtx(ctx, session, "sa_dragon_area", 3,
							30*time.Minute, "无尽龙域挑战无回包")
					}
					slog.Warn("无尽龙域挑战请求异常", "role_id", session.RoleID(), "seq", prevSeq, "err", fErr)
					return scheduler.NewCooldown(30*time.Minute, "无尽龙域挑战异常")
				}
				scheduler.ResetSilentDropsCtx(ctx, session, "sa_dragon_area")

				code, pErr := protocol.ParseSaDragonAreaFightResult(fResp.Payload)
				if pErr != nil {
					slog.Warn("解析无尽龙域挑战结果码失败", "role_id", session.RoleID(), "err", pErr)
					return scheduler.NewCooldown(30*time.Minute, "无尽龙域挑战回包异常")
				}

				switch code {
				case protocol.SaDragonAreaResultSuccess:
					challenged++
					cdRetries = 0
					nextInfo, rErr := fetchInfo()
					if rErr != nil {
						if abort, ok := kit.IsAbortErr(rErr); ok {
							return abort
						}
						slog.Warn("无尽龙域战后刷新面板失败", "role_id", session.RoleID(), "err", rErr)
						return nil
					}
					if nextInfo.IsDragonClear != 0 || nextInfo.Seq > prevSeq {
						cleared++
						lossRetries = 0
						slog.Info("无尽龙域关卡挑战通关",
							"role_id", session.RoleID(),
							"prev_seq", prevSeq,
							"next_seq", nextInfo.Seq,
							"is_dragon_clear", nextInfo.IsDragonClear,
							"cleared", cleared,
						)
						info = nextInfo
						if info.IsDragonClear != 0 {
							return scheduler.ErrAttemptsExhausted
						}
					} else {
						lossRetries++
						slog.Info("无尽龙域当前关卡挑战未通关",
							"role_id", session.RoleID(),
							"seq", prevSeq,
							"loss_retry", lossRetries,
							"max_loss_retries", saDragonAreaMaxLossRetries,
						)
						info = nextInfo
						if lossRetries >= saDragonAreaMaxLossRetries {
							return scheduler.NewCooldown(4*time.Hour,
								fmt.Sprintf("无尽龙域第 %d 关连续战败 %d 次", prevSeq, lossRetries))
						}
					}
					if !kit.Sleep(ctx, 300*time.Millisecond) {
						return ctx.Err()
					}

				case protocol.SaDragonAreaResultCompleteAll:
					slog.Info("无尽龙域服务端返回全部通关", "role_id", session.RoleID(), "cleared", cleared)
					return scheduler.ErrAttemptsExhausted

				case protocol.SaDragonAreaResultCD:
					cdRetries++
					if cdRetries > saDragonAreaMaxCDRetries {
						slog.Info("无尽龙域连续处于战斗冷却，稍后重试", "role_id", session.RoleID())
						return scheduler.NewCooldown(5*time.Minute, "无尽龙域战斗冷却中")
					}
					slog.Info("无尽龙域战斗冷却中，短暂等待后重试",
						"role_id", session.RoleID(), "seq", prevSeq, "retry", cdRetries)
					if !kit.Sleep(ctx, saDragonAreaCDWait) {
						return ctx.Err()
					}

				case protocol.SaDragonAreaResultFunctionNoOpen, protocol.SaDragonAreaResultSaintAreaClose:
					slog.Info("无尽龙域未开放或圣域已关闭",
						"role_id", session.RoleID(),
						"code", code,
						"meaning", protocol.SaDragonAreaResultText(code),
					)
					return scheduler.ErrConditionNotMet

				default:
					slog.Warn("无尽龙域挑战返回非成功结果码",
						"role_id", session.RoleID(),
						"seq", prevSeq,
						"code", code,
						"meaning", protocol.SaDragonAreaResultText(code),
					)
					return scheduler.NewCooldown(30*time.Minute,
						fmt.Sprintf("无尽龙域返回 %s", protocol.SaDragonAreaResultText(code)))
				}
			}

			slog.Info("无尽龙域本轮巡检完成",
				"role_id", session.RoleID(),
				"challenged", challenged,
				"cleared", cleared,
				"cur_seq", info.Seq,
			)
			return nil
		},
	)
}
