// 本文件实现背包礼包道具的直接打开 (Mod 2, Act 41/53)。
//
// 在线礼包/在线奖励领取后，奖励常以「xx礼包」道具形式进入背包；客户端会
// 在背包里直接点击使用。此处按道具名包含「礼包」且无元宝价值筛选后逐格使用。
package daily

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/protocol"
)

const (
	maxGiftPacksPerRun  = 20
	giftPackCallTimeout = 8 * time.Second
)

// giftPackRepo 由注册工厂注入的静态字典，用于识别「礼包」道具。
var giftPackRepo dictionary.Repository

// SetGiftPackRepo 注入静态字典；未注入时跳过背包礼包打开。
func SetGiftPackRepo(repo dictionary.Repository) { giftPackRepo = repo }

// OpenGiftPacks 直接打开背包中的礼包道具。
func OpenGiftPacks(ctx context.Context, session *client.RoleSession, repo dictionary.Repository) {
	if session == nil || repo == nil {
		return
	}
	qCtx, qCancel := context.WithTimeout(ctx, giftPackCallTimeout)
	resp, err := session.Call(qCtx, protocol.BuildGetPackItemListPacket(), protocol.ActionIDItemGetPackList)
	qCancel()
	if err != nil || resp == nil {
		return
	}
	pack, pErr := protocol.ParsePackItemListResponse(resp.Payload)
	if pErr != nil || pack == nil {
		return
	}

	opened := 0
	for _, it := range pack.PackItems {
		if opened >= maxGiftPacksPerRun || ctx.Err() != nil {
			break
		}
		item, dErr := repo.GetItem(int(it.ItemID))
		if dErr != nil || item == nil {
			continue
		}
		if !strings.Contains(item.Name, "礼包") || item.Ingot > 0 {
			continue
		}
		usePkt, bErr := protocol.BuildUseGridItemPacket(int16(it.GridID))
		if bErr != nil {
			continue
		}
		uCtx, uCancel := context.WithTimeout(ctx, giftPackCallTimeout)
		uResp, uErr := session.Call(uCtx, usePkt, protocol.ActionIDItemUseGrid)
		uCancel()
		if uErr != nil || uResp == nil {
			slog.Info("礼包打开无回包", "role_id", session.RoleID(), "item_id", it.ItemID, "grid_id", it.GridID)
			continue
		}
		ok, code, rErr := protocol.ParseUseGridItemResponse(uResp.Payload)
		if rErr != nil {
			continue
		}
		if ok {
			opened++
			slog.Info("背包礼包已直接打开", "role_id", session.RoleID(), "item_id", it.ItemID, "item_name", item.Name, "grid_id", it.GridID)
		} else {
			slog.Debug("背包礼包打开失败", "role_id", session.RoleID(), "item_id", it.ItemID, "result", code)
		}
	}
	if opened > 0 {
		slog.Info("背包礼包打开完成", "role_id", session.RoleID(), "opened", opened)
	}
}
