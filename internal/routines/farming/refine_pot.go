// 本文件实现灵霄宝殿炼宝炉 (Mod_RefinePot_Base, Module 654)。
//
// 规则：对每种炼制材料，数量 >= min (默认 500) 时选择最大数量炼制；
// 低于阈值直接跳过。仅消耗材料，不涉及元宝。
package farming

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// minRefinePotMaterialCount 低于该值的材料跳过。
const minRefinePotMaterialCount int32 = 500

// NewRefinePotRoutine 构造炼宝炉炼制任务。
func NewRefinePotRoutine() scheduler.ActivityRoutine {
	const callTimeout = 10 * time.Second
	return scheduler.NewBaseRoutine(
		"refine_pot",
		"farming",
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
					slog.Info("炼宝炉协议无回包", "role_id", session.RoleID(), "action", act, "err", err)
					return nil
				}
				return resp
			}

			listResp := call(protocol.BuildRefinePotFromItemsPacket(), protocol.ActionIDRefinePotFromItems)
			if listResp == nil {
				return nil
			}
			result, materials, err := protocol.ParseRefinePotMaterials(listResp.Payload)
			if err != nil {
				slog.Warn("解析炼宝炉材料列表失败", "role_id", session.RoleID(), "err", err)
				return nil
			}
			if result != protocol.RefinePotResultSuccess {
				slog.Info("炼宝炉材料列表返回失败", "role_id", session.RoleID(), "result", result)
				return nil
			}
			slog.Info("炼宝炉材料列表", "role_id", session.RoleID(), "materials", materials)

			refined := 0
			for _, m := range materials {
				if ctx.Err() != nil {
					return nil
				}
				if m.Count < minRefinePotMaterialCount {
					slog.Info("炼宝炉材料不足 500，跳过",
						"role_id", session.RoleID(), "item_id", m.ItemID, "count", m.Count)
					continue
				}
				selResp := call(protocol.BuildRefinePotSelectPacket(m.ItemID, m.Count), protocol.ActionIDRefinePotSelect)
				if selResp == nil {
					return nil
				}
				selCode, selErr := protocol.ParseRefinePotResult(selResp.Payload)
				if selErr != nil {
					slog.Warn("解析炼宝炉选择材料回包失败", "role_id", session.RoleID(), "err", selErr)
					return nil
				}
				if selCode != protocol.RefinePotResultSuccess {
					slog.Info("炼宝炉选择材料未成功",
						"role_id", session.RoleID(), "item_id", m.ItemID,
						"count", m.Count, "result", selCode)
					continue
				}

				refResp := call(protocol.BuildRefinePotRefinePacket(), protocol.ActionIDRefinePotRefine)
				if refResp == nil {
					return nil
				}
				refCode, refErr := protocol.ParseRefinePotResult(refResp.Payload)
				if refErr != nil {
					slog.Warn("解析炼宝炉炼制回包失败", "role_id", session.RoleID(), "err", refErr)
					return nil
				}
				switch refCode {
				case protocol.RefinePotResultSuccess:
					refined++
					slog.Info("炼宝炉炼制成功",
						"role_id", session.RoleID(), "item_id", m.ItemID, "count", m.Count)
				case protocol.RefinePotResultNoFromItem, protocol.RefinePotResultNoRefineItem:
					slog.Info("炼宝炉无可炼制材料/配方，结束本轮",
						"role_id", session.RoleID(), "result", refCode)
					return nil
				case protocol.RefinePotResultPackFull:
					return scheduler.ErrBagFull
				default:
					slog.Info("炼宝炉炼制未成功",
						"role_id", session.RoleID(), "item_id", m.ItemID, "result", refCode)
				}
			}

			// 领取炼制奖励（若有）。
			if awardResp := call(protocol.BuildRefinePotAwardPacket(), protocol.ActionIDRefinePotAward); awardResp != nil {
				if code, aErr := protocol.ParseRefinePotResult(awardResp.Payload); aErr == nil {
					slog.Info("炼宝炉奖励领取结果", "role_id", session.RoleID(), "result", code)
				}
			}
			slog.Info("炼宝炉巡检完成", "role_id", session.RoleID(), "refined", refined)
			return nil
		},
	)
}
