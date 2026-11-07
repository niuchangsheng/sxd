// 本文件实现精品杂货 VIP5/VIP8 商店购买 (Mod_StDaoyuanShop_Base, Module 132)。
//
// 目标：VIP5 购买神甲晶石(7298)、天魂自选包(7089)；VIP8 购买天魂自选包(7089)。
// 价格 >= 500 视为元宝档，一律跳过；仅购买低价（道缘/铜钱）档。
package daily

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

const (
	itemShenJiaJingShi int32 = 7298 // 神甲晶石
	itemTianHunPack    int32 = 7089 // 天魂自选包
	maxVipShopBuys           = 5
	// vipShopIngotPriceThreshold 价格达到该值视为元宝档，跳过以遵守元宝红线。
	vipShopIngotPriceThreshold int16 = 500
)

// NewDaoyuanVipShopRoutine 构造精品杂货 VIP 商店购买任务。
func NewDaoyuanVipShopRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"daoyuan_vip_shop",
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
			call := func(pkt *protocol.Packet, act uint32) *protocol.Packet {
				cCtx, cancel := context.WithTimeout(ctx, callTimeout)
				defer cancel()
				resp, err := session.Call(cCtx, pkt, act)
				if err != nil {
					slog.Info("道缘商店协议无回包", "role_id", session.RoleID(), "action", act, "err", err)
					return nil
				}
				return resp
			}

			run := func(vipLevel uint8, targets []int32) {
				listResp := call(protocol.BuildDaoyuanVipShopListPacket(vipLevel), protocol.ActionIDDaoyuanVipShopList)
				if listResp == nil {
					slog.Info("精品杂货商店列表无回包", "role_id", session.RoleID(), "vip", vipLevel)
					return
				}
				items, err := protocol.ParseDaoyuanVipShopList(listResp.Payload)
				if err != nil {
					slog.Warn("解析精品杂货商店列表失败", "role_id", session.RoleID(), "vip", vipLevel, "err", err)
					return
				}
				slog.Info("精品杂货 VIP 商店列表", "role_id", session.RoleID(), "vip", vipLevel, "items", items)

				for _, item := range items {
					wanted := false
					for _, id := range targets {
						if item.ItemID == id {
							wanted = true
							break
						}
					}
					if !wanted || item.Limit <= 0 {
						continue
					}
					if item.Price <= 0 {
						slog.Info("精品杂货商品价格异常，跳过",
							"role_id", session.RoleID(), "vip", vipLevel, "item_id", item.ItemID, "price", item.Price)
						continue
					}
					if item.Price >= vipShopIngotPriceThreshold {
						slog.Info("精品杂货商品疑似元宝定价，跳过",
							"role_id", session.RoleID(), "vip", vipLevel,
							"item_id", item.ItemID, "price", item.Price)
						continue
					}
					count := int16(1)
					if item.Limit < count {
						count = item.Limit
					}
					if count > maxVipShopBuys {
						count = maxVipShopBuys
					}
					resp := call(protocol.BuildDaoyuanShopBuyPacket(item.Index, count), protocol.ActionIDDaoyuanShopBuy)
					if resp == nil {
						return
					}
					code, pErr := protocol.ParseDaoyuanShopBuyResult(resp.Payload)
					if pErr != nil {
						slog.Warn("解析精品杂货购买回包失败", "role_id", session.RoleID(), "err", pErr)
						return
					}
					switch code {
					case protocol.DaoyuanShopResultSuccess:
						slog.Info("精品杂货购买成功",
							"role_id", session.RoleID(), "vip", vipLevel,
							"item_id", item.ItemID, "count", count, "price", item.Price)
					case protocol.DaoyuanShopResultPackFull:
						slog.Warn("精品杂货购买背包已满", "role_id", session.RoleID())
						return
					case protocol.DaoyuanShopResultNoDaoYuan, protocol.DaoyuanShopResultNoIngot,
						protocol.DaoyuanShopResultVipTwo, protocol.DaoyuanShopResultVipFive,
						protocol.DaoyuanShopResultVipEight, protocol.DaoyuanShopResultItemNotExist:
						slog.Info("精品杂货购买未成功", "role_id", session.RoleID(),
							"item_id", item.ItemID, "result", code,
							"result_text", protocol.DaoyuanShopResultText(code))
					default:
						slog.Info("精品杂货购买未成功", "role_id", session.RoleID(),
							"item_id", item.ItemID, "result", code)
					}
				}
			}

			run(5, []int32{itemShenJiaJingShi, itemTianHunPack})
			run(8, []int32{itemTianHunPack})
			return nil
		},
	)
}
