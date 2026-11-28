// 本文件实现「回忆之境」(Mod_MemoryZone_Base, Module 526 / 0x020E) 自动化日常玩法。
//
// 抓包基准: huiyizhijing.pcapng (2026-11-28, 主服 TCP 8200)：
//
//	0/69  check_function_open(216) -> 119 开放 / 120 未开放
//	526/1 main_panel []            -> 各分区进度 (Type 3 为三个魔神专区: 486/487/488)
//	526/2 back_panel [zoneType]    -> 伙伴碎片列表 + 今日回溯次数/免费次数/元宝消耗
//	526/4 backtrack  [roleID]      -> 对指定未拥有伙伴执行一次免费回忆
//
// 业务规则：
//   - 每天仅使用免费次数 (today_back_times < daily_free_times && cost_ingot == 0)，严禁消耗元宝；
//   - 按分区 Type 降序优先进入魔神专区 (Type=3)，跳过已拥有伙伴 (have_scrap >= need_scrap)，
//     默认选择第一个未拥有的伙伴执行回忆；
//   - 每日免费次数用尽后返回 scheduler.ErrAttemptsExhausted 熔断至次日重置。
package daily

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

const (
	memoryZoneRoutineID = "memory_zone"
	memoryZoneTimeout   = 8 * time.Second
)

func formatMemoryZoneAwards(awards []protocol.MemoryZoneAward) string {
	if len(awards) == 0 {
		return ""
	}
	parts := make([]string, 0, len(awards))
	for _, a := range awards {
		parts = append(parts, fmt.Sprintf("%d:%d", a.ItemID, a.ItemNum))
	}
	return strings.Join(parts, ",")
}

// selectMemoryZoneCategories 返回按 Type 降序排列的未集齐分区列表（对齐客户端 sortOn("type", DESCENDING)，
// 三个魔神专区 Type=3 排在最前）。
func selectMemoryZoneCategories(cats []protocol.MemoryZoneCategory) []protocol.MemoryZoneCategory {
	pending := make([]protocol.MemoryZoneCategory, 0, len(cats))
	for _, c := range cats {
		if c.MaxNum <= 0 || c.HaveNum < c.MaxNum {
			pending = append(pending, c)
		}
	}
	sort.SliceStable(pending, func(i, j int) bool {
		return pending[i].Type > pending[j].Type
	})
	return pending
}

// selectFirstUnownedPartner 从伙伴列表中选择第一个未拥有的伙伴；全部已拥有返回 false。
func selectFirstUnownedPartner(partners []protocol.MemoryZonePartner) (protocol.MemoryZonePartner, bool) {
	for _, p := range partners {
		if !p.Owned() {
			return p, true
		}
	}
	return protocol.MemoryZonePartner{}, false
}

// NewMemoryZoneRoutine 构造「回忆之境」每日免费回忆任务。
func NewMemoryZoneRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		memoryZoneRoutineID,
		"daily",
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

			caller := kit.NewCaller(ctx, session, memoryZoneTimeout)

			// 1. 拉取主面板，获取各分区收集进度。
			mainResp, err := caller.CallResync(
				protocol.BuildMemoryZoneMainPanelPacket(),
				protocol.ActionIDMemoryZoneMainPanel,
			)
			if err != nil {
				if abort := caller.AbortErr(); abort != nil {
					return abort
				}
				if session.State() != client.StateActive {
					return client.ErrNotConnected
				}
				return scheduler.HandleSilentDropCtx(ctx, session, memoryZoneRoutineID, 3,
					30*time.Minute, "回忆之境主面板无回包")
			}

			mainPanel, err := protocol.ParseMemoryZoneMainPanel(mainResp.Payload)
			if err != nil {
				slog.Warn("回忆之境主面板解析失败", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(10*time.Minute, "回忆之境主面板解析失败")
			}
			if mainPanel.Result != protocol.MemoryZoneResultSuccess {
				return handleMemoryZoneResultError(ctx, session, protocol.ActionIDMemoryZoneMainPanel, mainPanel.Result, 0)
			}

			categories := selectMemoryZoneCategories(mainPanel.Categories)
			if len(categories) == 0 {
				slog.Info("回忆之境所有分区伙伴均已拥有，跳过本轮", "role_id", session.RoleID())
				return scheduler.ErrConditionNotMet
			}

			// 2. 按分区 Type 降序（优先 Type=3 三个魔神）查询详情，找到第一个未拥有伙伴。
			for _, cat := range categories {
				if jitter != nil {
					_ = jitter.Wait(ctx)
				}
				backResp, err := caller.CallResync(
					protocol.BuildMemoryZoneBackPanelPacket(cat.Type),
					protocol.ActionIDMemoryZoneBackPanel,
				)
				if err != nil {
					if abort := caller.AbortErr(); abort != nil {
						return abort
					}
					if session.State() != client.StateActive {
						return client.ErrNotConnected
					}
					return scheduler.HandleSilentDropCtx(ctx, session, memoryZoneRoutineID, 3,
						30*time.Minute, "回忆之境分区面板无回包")
				}

				backPanel, err := protocol.ParseMemoryZoneBackPanel(backResp.Payload)
				if err != nil {
					slog.Warn("回忆之境分区面板解析失败", "role_id", session.RoleID(),
						"zone_type", cat.Type, "err", err)
					return scheduler.NewCooldown(10*time.Minute, "回忆之境分区面板解析失败")
				}
				if backPanel.Result != protocol.MemoryZoneResultSuccess {
					return handleMemoryZoneResultError(ctx, session, protocol.ActionIDMemoryZoneBackPanel, backPanel.Result, 0)
				}

				// 严格 0 元宝约束：免费次数已用完或需消耗元宝时，直接熔断至次日重置。
				if !backPanel.HasFreeBacktrack() {
					slog.Info("回忆之境今日免费次数已用尽，熔断至次日重置",
						"role_id", session.RoleID(),
						"zone_type", cat.Type,
						"today_back_times", backPanel.TodayBackTimes,
						"daily_free_times", backPanel.DailyFreeTimes,
						"cost_ingot", backPanel.CostIngot)
					return scheduler.ErrAttemptsExhausted
				}

				target, ok := selectFirstUnownedPartner(backPanel.Partners)
				if !ok {
					slog.Info("回忆之境当前分区伙伴均已拥有，检查下一分区",
						"role_id", session.RoleID(), "zone_type", cat.Type)
					continue
				}

				// 3. 对选中的第一个未拥有伙伴执行一次免费回忆。
				if jitter != nil {
					_ = jitter.Wait(ctx)
				}
				btResp, err := caller.CallResync(
					protocol.BuildMemoryZoneBacktrackPacket(target.RoleID),
					protocol.ActionIDMemoryZoneBacktrack,
				)
				if err != nil {
					if abort := caller.AbortErr(); abort != nil {
						return abort
					}
					if session.State() != client.StateActive {
						return client.ErrNotConnected
					}
					return scheduler.HandleSilentDropCtx(ctx, session, memoryZoneRoutineID, 3,
						30*time.Minute, "回忆之境回溯请求无回包")
				}

				bt, err := protocol.ParseMemoryZoneBacktrack(btResp.Payload)
				if err != nil {
					slog.Warn("回忆之境回溯回包解析失败", "role_id", session.RoleID(),
						"partner_role_id", target.RoleID, "err", err)
					return scheduler.NewCooldown(10*time.Minute, "回忆之境回溯回包解析失败")
				}
				if bt.Result != protocol.MemoryZoneResultSuccess {
					return handleMemoryZoneResultError(ctx, session, protocol.ActionIDMemoryZoneBacktrack, bt.Result, target.RoleID)
				}

				// 业务成功，清零静默丢包计数并校验副作用是否生效。
				scheduler.ResetSilentDropsCtx(ctx, session, memoryZoneRoutineID)

				newHaveScrap := target.HaveScrap
				newNeedScrap := target.NeedScrap
				for _, p := range bt.Partners {
					if p.RoleID == target.RoleID {
						newHaveScrap = p.HaveScrap
						newNeedScrap = p.NeedScrap
						break
					}
				}
				progressed := bt.TodayBackTimes > backPanel.TodayBackTimes ||
					newHaveScrap > target.HaveScrap ||
					len(bt.Awards) > 0
				if !progressed {
					slog.Warn("回忆之境回溯返回成功但次数/碎片/奖励均未变化，按异常冷却",
						"role_id", session.RoleID(),
						"zone_type", cat.Type,
						"partner_role_id", target.RoleID,
						"today_back_times", bt.TodayBackTimes,
						"have_scrap", newHaveScrap)
					return scheduler.NewCooldown(30*time.Minute, "回忆之境回溯无实际生效")
				}

				slog.Info("回忆之境回溯成功",
					"role_id", session.RoleID(),
					"zone_type", cat.Type,
					"partner_role_id", target.RoleID,
					"scrap", fmt.Sprintf("%d->%d/%d", target.HaveScrap, newHaveScrap, newNeedScrap),
					"awards", formatMemoryZoneAwards(bt.Awards),
					"today_back_times", bt.TodayBackTimes,
					"next_cost_ingot", bt.CostIngot)

				// 每日免费次数用尽（默认每日 1 次，执行后 next_cost_ingot > 0）立即熔断至次日重置。
				if bt.TodayBackTimes >= backPanel.DailyFreeTimes || bt.CostIngot > 0 {
					return scheduler.ErrAttemptsExhausted
				}
				return nil
			}

			slog.Info("回忆之境暂无可回忆的未拥有伙伴，跳过本轮", "role_id", session.RoleID())
			return scheduler.ErrConditionNotMet
		},
		scheduler.WithFunctionID(protocol.FunctionIDMemoryZone),
	)
}

func handleMemoryZoneResultError(
	ctx context.Context,
	session *client.RoleSession,
	actionID uint32,
	result uint8,
	partnerRoleID int32,
) error {
	switch result {
	case protocol.MemoryZoneResultMaxTimes:
		scheduler.ResetSilentDropsCtx(ctx, session, memoryZoneRoutineID)
		slog.Info("回忆之境今日回溯次数已满，熔断至次日重置",
			"routine", memoryZoneRoutineID,
			"role_id", session.RoleID(),
			"action", fmt.Sprintf("0x%08X", actionID),
			"result_code", result)
		return scheduler.ErrAttemptsExhausted
	case protocol.MemoryZoneResultNoIngot:
		scheduler.ResetSilentDropsCtx(ctx, session, memoryZoneRoutineID)
		slog.Info("回忆之境需要元宝，停止执行并等待次日免费次数重置",
			"routine", memoryZoneRoutineID,
			"role_id", session.RoleID(),
			"action", fmt.Sprintf("0x%08X", actionID),
			"result_code", result)
		return scheduler.ErrAttemptsExhausted
	default:
		slog.Warn("回忆之境业务失败",
			"routine", memoryZoneRoutineID,
			"role_id", session.RoleID(),
			"action", fmt.Sprintf("0x%08X", actionID),
			"partner_role_id", partnerRoleID,
			"result_code", result,
			"result_text", protocol.MemoryZoneResultText(result))
		return scheduler.NewCooldown(time.Hour, fmt.Sprintf("回忆之境返回失败码 %d", result))
	}
}
