// Package dungeon 提供副本关卡扫荡、关卡智能寻址引擎与双模配置策略。
//
// @author Ateng
// @since 2026-10-22
package dungeon

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
)

// StaminaItemDef 体力恢复道具元数据契约。
type StaminaItemDef struct {
	ItemID int32  // 物品 ID
	Name   string // 物品友好名称
	Points int    // 单次补充体力数值
}

// StaminaItemPriorities 严格定义神仙道体力道具消耗优先级：包子 > 粽子 > 茶叶蛋。
var StaminaItemPriorities = []StaminaItemDef{
	{
		ItemID: protocol.ItemIDSteamedBun,
		Name:   "包子",
		Points: 5,
	},
	{
		ItemID: protocol.ItemIDRiceDumpling,
		Name:   "粽子",
		Points: 10,
	},
	{
		ItemID: protocol.ItemIDTeaEgg,
		Name:   "茶叶蛋",
		Points: 50,
	},
}

// TryReplenishStamina 检查角色背包并严格按照 [包子 > 粽子 > 茶叶蛋] 优先级自动使用道具补充体力。
// 返回:
//   - ok: 是否成功使用了体力道具并完成了体力补充
//   - itemName: 使用的道具名称 (如 "包子")
//   - addedPoints: 增加的体力数值
//   - err: 网络或协议级别错误 (无道具不视为错误)
func TryReplenishStamina(ctx context.Context, session *client.RoleSession) (bool, string, int, error) {
	if session == nil {
		return false, "", 0, client.ErrNotConnected
	}

	// 1. 发送 Mod 2, Act 41 请求拉取主背包物品列表
	packCtx, packCancel := context.WithTimeout(ctx, 3*time.Second)
	defer packCancel()

	packPkt, err := session.Call(packCtx, protocol.BuildGetPackItemListPacket(), protocol.ActionIDItemGetPackList)
	if err != nil {
		return false, "", 0, fmt.Errorf("dungeon/stamina: 拉取背包物品列表失败: %w", err)
	}
	if packPkt == nil || len(packPkt.Payload) == 0 {
		return false, "", 0, fmt.Errorf("dungeon/stamina: 服务端返回空背包响应")
	}

	packResp, err := protocol.ParsePackItemListResponse(packPkt.Payload)
	if err != nil {
		return false, "", 0, fmt.Errorf("dungeon/stamina: 解析背包列表失败: %w", err)
	}

	if len(packResp.PackItems) == 0 {
		slog.Debug("背包为空，未发现任何体力道具", "role_id", session.RoleID())
		return false, "", 0, nil
	}

	// 2. 严格按 [包子 > 粽子 > 茶叶蛋] 优先级检索背包格位
	for _, def := range StaminaItemPriorities {
		var matchedGridID int32 = -1
		for _, it := range packResp.PackItems {
			if it.ItemID == def.ItemID && it.GridID > 0 {
				matchedGridID = it.GridID
				break // 命中当前最高优先级的首个格位
			}
		}

		if matchedGridID <= 0 {
			continue // 当前优先级道具不存在，降级检索下一优先级
		}

		// 3. 构造并发送 Mod 2, Act 53 使用指定格位物品协议
		usePkt, err := protocol.BuildUseGridItemPacket(int16(matchedGridID))
		if err != nil {
			return false, "", 0, fmt.Errorf("dungeon/stamina: 构造使用物品封包失败: %w", err)
		}

		useCtx, useCancel := context.WithTimeout(ctx, 3*time.Second)
		useResp, useErr := session.Call(useCtx, usePkt, protocol.ActionIDItemUseGrid)
		useCancel()

		if useErr != nil {
			slog.Warn("使用体力道具未收到有效响应",
				"role_id", session.RoleID(),
				"item", def.Name,
				"grid_id", matchedGridID,
				"err", useErr,
			)
			continue
		}

		ok, resultCode, pErr := protocol.ParseUseGridItemResponse(useResp.Payload)
		if pErr != nil || !ok {
			slog.Warn("服务端返回使用体力道具失败",
				"role_id", session.RoleID(),
				"item", def.Name,
				"grid_id", matchedGridID,
				"result_code", resultCode,
				"err", pErr,
			)
			continue
		}

		// 4. 使用成功，更新角色体力池
		newStamina := session.AddStamina(def.Points)
		slog.Info("自动补充体力成功",
			"role_id", session.RoleID(),
			"item", def.Name,
			"added_power", def.Points,
			"grid_id", matchedGridID,
			"current_stamina", newStamina,
		)

		return true, def.Name, def.Points, nil
	}

	slog.Info("背包中未找到可用的体力道具 (已检索包子/粽子/茶叶蛋)", "role_id", session.RoleID())
	return false, "", 0, nil
}
