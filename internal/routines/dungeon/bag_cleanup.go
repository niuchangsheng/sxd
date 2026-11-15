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
	"sxd-pie-ng/internal/scheduler"
)

const (
	// bagCleanupFreeThreshold 剩余空格低于该值时触发清理。
	bagCleanupFreeThreshold = 5
	// bagCleanupTargetFree 清理后希望达到的空格数。
	bagCleanupTargetFree = 10
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
			if int(pack.PackEmptyGrids) > bagCleanupFreeThreshold {
				return nil
			}
			// 只卖到「目标空格数」即可（每卖一叠腾 1 格），避免一次清空全部制作卷。
			need := bagCleanupTargetFree - int(pack.PackEmptyGrids)
			if need > bagCleanupMaxSell {
				need = bagCleanupMaxSell
			}
			if need <= 0 {
				return nil
			}
			slog.Info("背包清理：空格不足，开始出售装备制作卷", "role_id", session.RoleID(),
				"empty_grids", pack.PackEmptyGrids, "items", len(pack.PackItems), "need", need)
			sold := 0
			for _, it := range pack.PackItems {
				if sold >= need || ctx.Err() != nil {
					break
				}
				if !isEquipmentCraftScroll(dictRepo, it.ItemID) {
					continue
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
