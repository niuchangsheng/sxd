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
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// maxEraRebirthAttrItems 单轮最多使用的时间道具数量。
const maxEraRebirthAttrItems = 20

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
				cCtx, cCancel := context.WithTimeout(ctx, callTimeout)
				defer cCancel()
				resp, err := session.Call(cCtx, pkt, act)
				if err != nil {
					slog.Info("修真境界协议无回包", "role_id", session.RoleID(),
						"action", act, "err", err)
					return nil
				}
				return resp
			}

			// 刷新赛季与属性面板，保证 851/4 返回当前赛季的道具列表。
			_ = call(protocol.BuildEraRebirthAttrSeasonPacket(), protocol.ActionIDEraRebirthAttrSeason)
			_ = call(protocol.BuildEraRebirthAttrPanelPacket(), protocol.ActionIDEraRebirthAttrPanel)

			pkgResp := call(protocol.BuildEraRebirthAttrResourcePackagePacket(),
				protocol.ActionIDEraRebirthAttrResourcePackage)
			if pkgResp == nil {
				return scheduler.NewCooldown(30*time.Minute, "修真境界礼包无回包")
			}
			items, autoLevel, err := protocol.ParseEraRebirthAttrTimeItems(pkgResp.Payload)
			if err != nil {
				slog.Warn("解析修真境界礼包失败，本轮跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			slog.Info("修真境界礼包", "role_id", session.RoleID(),
				"items", len(items), "auto_level", autoLevel)
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
					return scheduler.NewCooldown(30*time.Minute, "修真境界道具使用无回包")
				}
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