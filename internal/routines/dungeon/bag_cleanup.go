// 本文件实现「背包满时清理装备制作卷」的兜底任务。
//
// 规则（用户确认）：
//   - 只出售装备制作卷（TypeID=1002 且名称含「制作卷」、描述含「装备」）；
//   - 低空格位阈值时触发，每轮最多出售固定叠数，避免一次性清空玩家材料；
//   - 其它类型物品一律不动，绝不消耗元宝。
package dungeon

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

const (
	// bagCleanupFreeThreshold 剩余空格低于该值时触发清理。
	//
	// 2026-11-28：原阈值 5 时 s1 长期在 8~15 空格间徘徊，清理任务从未触发，
	// 装备制作卷沉积到 40/48 格；而伙伴猜猜看等服务端接口在 10 空格时已报
	// 「背包已满」。阈值提到 12，让清理先于服务端判定满仓发生。
	bagCleanupFreeThreshold = 12
	// bagCleanupTargetFree 清理后希望达到的空格数。
	bagCleanupTargetFree = 20
	// bagCleanupMaxSell 单轮最多出售的装备制作卷叠数（兜底上限）。
	bagCleanupMaxSell = 20
)

// isEquipmentCraftScroll 严格判定物品是否为装备制作卷。
func isEquipmentCraftScroll(dictRepo dictionary.Repository, itItemID int32) bool {
	if dictRepo == nil {
		return false
	}
	dItem, err := dictRepo.GetItem(int(itItemID))
	if err != nil || dItem == nil {
		return false
	}
	if dItem.TypeID != eliteSweepStackItemTypeID {
		return false
	}
	return strings.Contains(dItem.Name, "制作卷") && strings.Contains(dItem.Description, "装备")
}

// NewBagCleanupRoutine 构造背包清理任务（仅装备制作卷）。
func NewBagCleanupRoutine(dictRepo dictionary.Repository) scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"bag_cleanup",
		"dungeon",
		scheduler.ScheduleLoop,
		scheduler.PriorityLow,
		10*time.Minute,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if dictRepo == nil {
				return nil
			}
			pack, err := fetchPackItemList(ctx, session)
			if err != nil || pack == nil {
				slog.Warn("背包清理：拉取背包列表失败", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(10*time.Minute, "背包清理查询无回包")
			}
			// 线上 2/41 的 empty_grids 字段恒为 0（容量 60、已用 47 时仍报 0），
			// 不能作为满仓判据；用「容量 - 已用格位」计算真实空格。
			freeSlots := int(pack.PackCapacity) - len(pack.PackItems)
			if freeSlots > bagCleanupFreeThreshold {
				slog.Debug("背包清理：空格充足，跳过", "role_id", session.RoleID(),
					"free_slots", freeSlots, "threshold", bagCleanupFreeThreshold)
				return nil
			}
			// 只卖到「目标空格数」即可（每卖一叠腾 1 格），避免一次清空全部制作卷。
			need := bagCleanupTargetFree - freeSlots
			if need > bagCleanupMaxSell {
				need = bagCleanupMaxSell
			}
			if need <= 0 {
				return nil
			}
			var candidates []protocol.ItemRecord
			for _, it := range pack.PackItems {
				if isEquipmentCraftScroll(dictRepo, it.ItemID) {
					candidates = append(candidates, it)
				}
			}
			if len(candidates) == 0 {
				// 没有装备制作卷可卖，保持安静，避免每 10 分钟刷日志。
				return nil
			}
			slog.Info("背包清理：空格不足，开始出售装备制作卷", "role_id", session.RoleID(),
				"free_slots", freeSlots, "capacity", pack.PackCapacity,
				"empty_grids", pack.PackEmptyGrids, "items", len(pack.PackItems),
				"scrolls", len(candidates), "need", need)
			sold := 0
			for _, it := range candidates {
				if sold >= need || ctx.Err() != nil {
					break
				}
				if ok, _ := sellPackItem(ctx, session, it); ok {
					sold++
				}
			}
			if sold > 0 {
				slog.Info("背包清理完成：已出售装备制作卷", "role_id", session.RoleID(), "sold", sold)
			} else {
				slog.Info("背包清理：没有可出售的装备制作卷", "role_id", session.RoleID())
			}
			return nil
		},
	)
}

// sellEquipmentScrollsAfterEliteSweep 精英副本扫荡结束后的兜底出售：
// 卖出背包内所有装备制作卷 (TypeID=1002、名称含「制作卷」、描述含「装备」、
// 无元宝价值)。网络/解析失败只记录日志，不影响扫荡结果。
//
// 2026-11-28：此前仅 s0 执行，s1 背包沉积了 11 叠装备制作卷（赤霄/天罡/倚天），
// 加上推图与活动掉落一度逼近满仓（40/48）。丹药制作卷为 TypeID 1001
// （如 382/383 一品/二品武力丹制作卷），不在本函数判定范围内，因此对
// s1（配置保留 1-5 品丹药制作卷）同样安全。
func sellEquipmentScrollsAfterEliteSweep(ctx context.Context, session *client.RoleSession, dictRepo dictionary.Repository) {
	if session == nil || dictRepo == nil {
		return
	}
	// 扫荡收尾阶段会话可能刚好在重连：等状态机恢复再卖，避免整包兜底
	// 在恢复窗口里以 "session is not active" 静默失败（2026-11-28 s1 实测）。
	if !awaitSessionActive(ctx, session) {
		slog.Warn("精英扫荡后出售装备制作卷：会话未恢复，跳过本轮", "role_id", session.RoleID())
		return
	}
	pack, err := fetchPackItemList(ctx, session)
	if err != nil || pack == nil {
		slog.Warn("精英扫荡后出售装备制作卷：拉取背包失败",
			"role_id", session.RoleID(), "err", err)
		return
	}
	sold := 0
	for _, it := range pack.PackItems {
		if !isEquipmentCraftScroll(dictRepo, it.ItemID) {
			continue
		}
		// 元宝价值物品一律跳过，避免误卖玩家投入元宝获得的道具。
		if dItem, dErr := dictRepo.GetItem(int(it.ItemID)); dErr == nil && dItem != nil && dItem.Ingot > 0 {
			continue
		}
		if ok, _ := sellPackItem(ctx, session, it); ok {
			sold++
		}
	}
	if sold > 0 {
		slog.Info("精英副本扫荡结束，已出售装备制作卷",
			"role_id", session.RoleID(), "sold", sold)
	}
}
