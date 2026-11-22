// Package farming 提供药园、取经护送/劫镖、仙界矿山、粗铁淬炼池与仙葫芦等资源养成自动化。
package farming

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// NewGourdRealmRoutine 构造壶中界炼丹与灵气合成任务。
// NewGourdRealmRoutine 构造仙葫芦收益领取任务 (Module 220, Mod_FairyGourd_Base)。
//
// 先读取活动时间窗，再逐日领取已累积的铜钱/经验收益；
// 活动未开始/已结束或无可领日期时直接跳过。全程不消耗元宝。
func NewGourdRealmRoutine(dictRepo dictionary.Repository) scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"gourd_realm",
		"farming",
		scheduler.ScheduleLoop,
		scheduler.PriorityNormal,
		30*time.Minute,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			_ = dictRepo // 保留构造签名，当前领取流程不依赖静态字典

			// 1. 活动时间窗检测
			dCtx, dCancel := context.WithTimeout(ctx, callTimeout)
			dateResp, err := session.Call(dCtx, protocol.BuildFairyGourdDatePacket(), protocol.ActionIDFairyGourdDate)
			dCancel()
			if err != nil {
				slog.Info("仙葫芦功能不可用，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(30*time.Minute, "仙葫芦活动时间无回包")
			}
			date, err := protocol.ParseFairyGourdDateResult(dateResp.Payload)
			if err != nil {
				return fmt.Errorf("解析仙葫芦活动时间失败: %w", err)
			}
			now := time.Now().Unix()
			if date.StartTime > 0 && now < int64(date.StartTime) {
				slog.Info("仙葫芦活动尚未开始，等待开启", "role_id", session.RoleID(), "start_time", date.StartTime)
				return scheduler.NewCooldownUntil(
					time.Unix(int64(date.StartTime), 0).Add(2*time.Second), "仙葫芦活动尚未开始")
			}
			if date.EndTime > 0 && now > int64(date.EndTime) {
				slog.Info("仙葫芦活动已结束，跳过", "role_id", session.RoleID(), "end_time", date.EndTime)
				return scheduler.ErrAttemptsExhausted
			}

			// 2. 收益列表
			lCtx, lCancel := context.WithTimeout(ctx, callTimeout)
			listResp, err := session.Call(lCtx, protocol.BuildFairyGourdListPacket(), protocol.ActionIDFairyGourdList)
			lCancel()
			if err != nil {
				return fmt.Errorf("查询仙葫芦收益列表失败: %w", err)
			}
			entries, err := protocol.ParseFairyGourdListResult(listResp.Payload)
			if err != nil {
				return fmt.Errorf("解析仙葫芦收益列表失败: %w", err)
			}
			if len(entries) == 0 {
				slog.Info("仙葫芦暂无可领取收益", "role_id", session.RoleID())
				return nil
			}

			// 3. 逐日领取
			claimed := 0
			for _, entry := range entries {
				cCtx, cCancel := context.WithTimeout(ctx, callTimeout)
				resp, cErr := session.Call(cCtx, protocol.BuildFairyGourdClaimPacket(entry.Date), protocol.ActionIDFairyGourdClaim)
				cCancel()
				if cErr != nil {
					slog.Info("仙葫芦领取无应答", "role_id", session.RoleID(), "date", entry.Date, "err", cErr)
					break
				}
				result, pErr := protocol.ParseFairyGourdClaimResult(resp.Payload)
				if pErr != nil {
					slog.Warn("解析仙葫芦领取回包失败", "role_id", session.RoleID(), "err", pErr)
					break
				}
				switch result {
				case protocol.FairyGourdResultSuccess:
					claimed++
					slog.Info("仙葫芦收益领取成功",
						"role_id", session.RoleID(),
						"date", entry.Date,
						"coin", entry.Coin,
						"exp", entry.Exp,
					)
				case protocol.FairyGourdResultAlreadyGet:
					continue
				case protocol.FairyGourdResultNoGrid:
					return scheduler.ErrBagFull
				case protocol.FairyGourdResultTimeError:
					slog.Info("仙葫芦领取时间不符，结束本轮", "role_id", session.RoleID(), "date", entry.Date)
					return nil
				default:
					slog.Warn("仙葫芦领取失败", "role_id", session.RoleID(), "date", entry.Date, "result", result)
					return nil
				}
			}
			slog.Info("仙葫芦收益领取完成", "role_id", session.RoleID(), "claimed", claimed)
			return nil
		},
	)
}

// NewRuneRefineRoutine 构造符文提炼与八卦炼丹任务。
// NewRuneRefineRoutine 构造符文批量炼化任务 (Module 16, Mod_Rune_Base)。
//
// 先查询批量炼化信息；字段语义依赖客户端资源 SWF，当前只做数据检测，
// 语义对齐前不执行 batch_rune_use，避免误消耗符文/元宝。
// runeRefineBlocked 标记本会话内符文批量炼化不可用。
// s1（95 级）16/3 稳定超时，而 s0（300 级）正常返回；按会话缓存避免每
// 45 分钟固定丢包。
var runeRefineBlocked sync.Map // *client.RoleSession -> bool

func NewRuneRefineRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"rune_refine",
		"farming",
		scheduler.ScheduleLoop,
		scheduler.PriorityLow,
		45*time.Minute,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			if _, blocked := runeRefineBlocked.Load(session); blocked {
				slog.Debug("符文批量炼化本会话不可用，跳过", "role_id", session.RoleID())
				return nil
			}
			cCtx, cCancel := context.WithTimeout(ctx, callTimeout)
			resp, err := session.Call(cCtx, protocol.BuildRuneBatchListPacket(), protocol.ActionIDRuneBatchList)
			cCancel()
			if err != nil {
				runeRefineBlocked.Store(session, true)
				slog.Info("符文炼化功能不可用，本会话跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			info, err := protocol.ParseRuneBatchListResult(resp.Payload)
			if err != nil {
				return fmt.Errorf("解析符文批量信息失败: %w", err)
			}
			slog.Info("符文批量炼化信息（语义待资源 SWF 对齐，暂不执行）",
				"role_id", session.RoleID(),
				"field1", info.Field1,
				"field2", info.Field2,
				"field3", info.Field3,
				"field4", info.Field4,
			)
			return nil
		},
	)
}
