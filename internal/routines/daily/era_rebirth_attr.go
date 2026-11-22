// 本文件实现修真境界礼包自动使用 (Mod_EraRebirthLandAttr_Base, Module 851)。
//
// 抓包基准: /tmp/henduo.pcapng（2026-11-14，qq s0）：
//
//	851/0 当前赛季 → 851/1 属性面板 → 851/4 资源礼包
//	→ 851/5 逐个使用时间道具 [item_id, item_num]
//
// 结果码：0 成功、1 功能未开放、2 已开始、3 失败、4 道具不足、5 任务次数上限。
// 不消耗元宝；道具不足按 4 跳过，次数上限按 5 结束本轮。
package daily

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// maxEraRebirthAttrItems 单轮最多使用的时间道具数量。
const maxEraRebirthAttrItems = 20

// eraRebirthItemDetail 将时间道具列表格式化为 "item_id:num" 便于排查
// 「面板正常但 851/5 被静默丢弃」时道具 id/数量是否解析正确。
func eraRebirthItemDetail(items []protocol.EraRebirthTimeItem) string {
	var b strings.Builder
	for i, it := range items {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "%d:%d", it.ItemID, it.Num)
	}
	return b.String()
}

// NewEraRebirthAttrGiftRoutine 构造修真境界礼包使用任务。
func NewEraRebirthAttrGiftRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"era_rebirth_attr_gift",
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
				resp, err := session.CallWithPrevActResync(ctx, callTimeout, pkt, act)
				if err != nil {
					slog.Info("修真境界协议无回包", "role_id", session.RoleID(),
						"action", act, "err", err)
					return nil
				}
				return resp
			}

			// 刷新赛季与属性面板，保证 851/4 返回当前赛季的道具列表。
			_ = call(protocol.BuildEraRebirthAttrSeasonPacket(), protocol.ActionIDEraRebirthAttrSeason)
			// 官方客户端只有在 851/1 的 is_auto_use_time_item 开启时才自动
			// 使用时间道具；关闭时服务端会静默丢弃 851/5，不能盲目发送。
			autoUse := int32(1) // 解析失败时沿用原流程，避免误阻断
			if panelResp := call(protocol.BuildEraRebirthAttrPanelPacket(),
				protocol.ActionIDEraRebirthAttrPanel); panelResp != nil {
				if flag, pErr := protocol.ParseEraRebirthAttrPanelAutoUse(panelResp.Payload); pErr == nil {
					autoUse = flag
					slog.Info("修真境界属性面板", "role_id", session.RoleID(),
						"is_auto_use_time_item", flag)
				} else {
					slog.Warn("解析修真境界属性面板自动使用开关失败，沿用原流程",
						"role_id", session.RoleID(), "err", pErr)
				}
			}
			if autoUse != 1 {
				slog.Info("修真境界未开启自动使用时间道具，跳过 851/5",
					"role_id", session.RoleID(), "is_auto_use_time_item", autoUse)
				return scheduler.NewCooldown(2*time.Hour, "修真境界未开启自动使用时间道具")
			}

			pkgResp := call(protocol.BuildEraRebirthAttrResourcePackagePacket(),
				protocol.ActionIDEraRebirthAttrResourcePackage)
			if pkgResp == nil {
				return scheduler.NewCooldown(time.Hour, "修真境界礼包无回包")
			}
			items, autoLevel, err := protocol.ParseEraRebirthAttrTimeItems(pkgResp.Payload)
			if err != nil {
				slog.Warn("解析修真境界礼包失败，本轮跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			slog.Info("修真境界礼包", "role_id", session.RoleID(),
				"items", len(items), "auto_level", autoLevel, "detail", eraRebirthItemDetail(items))
			if len(items) == 0 {
				slog.Info("修真境界暂无可使用的时间道具", "role_id", session.RoleID())
				return nil
			}

			used := 0
			for _, it := range items {
				if used >= maxEraRebirthAttrItems || ctx.Err() != nil {
					break
				}
				if it.ItemID <= 0 || it.Num <= 0 {
					continue
				}
				resp := call(protocol.BuildEraRebirthAttrUseTimeItemPacket(it.ItemID, it.Num),
					protocol.ActionIDEraRebirthAttrUseTimeItem)
				if resp == nil {
					// 道具不可用/当日状态结束后服务端会静默丢弃 851/5；
					// 连续 3 次挂起到次日重置，避免每 30 分钟空等。
					return scheduler.HandleSilentDrop(session, "era_rebirth_attr_gift", 3,
						30*time.Minute, "修真境界道具使用无回包")
				}
				scheduler.ResetSilentDrops(session, "era_rebirth_attr_gift")
				code, pErr := protocol.ParseEraRebirthAttrUseResult(resp.Payload)
				if pErr != nil {
					return scheduler.NewCooldown(10*time.Minute, "修真境界道具回包解析失败")
				}
				switch code {
				case protocol.EraRebirthAttrSuccess:
					used++
					slog.Info("修真境界道具使用成功", "role_id", session.RoleID(),
						"item_id", it.ItemID, "num", it.Num)
				case protocol.EraRebirthAttrNotEnoughItem:
					slog.Info("修真境界道具不足，跳过", "role_id", session.RoleID(),
						"item_id", it.ItemID, "num", it.Num)
				case protocol.EraRebirthAttrMissionLimit:
					slog.Info("修真境界任务次数已达上限，结束本轮", "role_id", session.RoleID(),
						"item_id", it.ItemID, "result", code)
					return nil
				case protocol.EraRebirthAttrFuncNotOpen:
					slog.Info("修真境界功能未开放，结束本轮", "role_id", session.RoleID())
					return scheduler.NewCooldown(2*time.Hour, "修真境界功能未开放")
				default:
					slog.Info("修真境界道具使用未成功", "role_id", session.RoleID(),
						"item_id", it.ItemID, "num", it.Num,
						"result", code, "result_text", protocol.EraRebirthAttrResultText(code))
					return nil
				}
			}
			slog.Info("修真境界礼包处理完成", "role_id", session.RoleID(), "used", used)
			return nil
		},
	)
}
