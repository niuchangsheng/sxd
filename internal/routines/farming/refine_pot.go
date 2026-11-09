// 本文件实现灵霄宝殿炼宝炉 (Mod_RefineFurnace_Base, Module 586)。
//
// 流程：open_panel(1) 读取元气材料 → get_award(6) 收取上一次炼制产物 →
// 对每种数量 >=500 的元气按最大批数 (每批 50) 执行 select_type(2) + start_refine(4)。
// 每轮只启动一炉，剩余材料由下一轮继续；材料 <500 直接跳过。不涉及元宝。
package farming

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

const (
	// minRefinePotMaterialCount 低于该值的元气跳过。
	minRefinePotMaterialCount int32 = 500
	// refineFurnaceBatchSize 每批消耗的元气数量（实测 1 批 = 50 个）。
	refineFurnaceBatchSize int32 = 50
	// 元气道具 → select_type 参数映射见 refineFurnaceTypeForMaterial。
)

// refineFurnaceTypeForMaterial 返回元气材料对应的 select_type 参数。
func refineFurnaceTypeForMaterial(itemID int32) (int32, bool) {
	switch itemID {
	case 7923:
		return 1, true
	case 7924:
		return 3, true
	case 7925:
		return 2, true
	default:
		return 0, false
	}
}

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

			panelResp := call(protocol.BuildRefineFurnacePanelPacket(), protocol.ActionIDRefineFurnacePanel)
			if panelResp == nil {
				slog.Info("炼宝炉面板无回包，稍后重试", "role_id", session.RoleID())
				return scheduler.NewCooldown(30*time.Minute, "炼宝炉面板无回包")
			}
			panel, err := protocol.ParseRefineFurnacePanel(panelResp.Payload)
			if err != nil {
				slog.Warn("炼宝炉面板解析失败，稍后重试", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(30*time.Minute, "炼宝炉面板解析失败")
			}
			if panel.Result != protocol.RefineFurnaceSuccess {
				slog.Info("炼宝炉面板暂不可用，稍后重试", "role_id", session.RoleID(), "result", panel.Result)
				return scheduler.NewCooldown(time.Hour, "炼宝炉面板不可用")
			}
			slog.Info("炼宝炉面板", "role_id", session.RoleID(),
				"level", panel.Level, "exp", panel.Exp, "selected", panel.Selected,
				"materials", panel.Materials, "products", panel.Products)

			// 1. 收取上一次炼制产物。
			if awardResp := call(protocol.BuildRefineFurnaceAwardPacket(), protocol.ActionIDRefineFurnaceAward); awardResp != nil {
				if code, aErr := protocol.ParseFirstByteResult(awardResp.Payload, "炼宝炉领奖"); aErr == nil {
					switch code {
					case protocol.RefineFurnaceSuccess:
						slog.Info("炼宝炉产物领取成功", "role_id", session.RoleID())
					case protocol.RefineFurnaceNotGetAward, protocol.RefineFurnaceNotComplete,
						protocol.RefineFurnaceFailed:
						slog.Info("炼宝炉暂无可领取产物", "role_id", session.RoleID(), "result", code)
					case protocol.RefineFurnacePackFull:
						return scheduler.ErrBagFull
					default:
						slog.Info("炼宝炉领奖未成功", "role_id", session.RoleID(), "result", code)
					}
				}
			}

			// 2. 选择一种数量 >=500 的元气，按最大批数开炉。
			for _, m := range panel.Materials {
				if ctx.Err() != nil {
					return nil
				}
				if m.Count < minRefinePotMaterialCount {
					slog.Info("炼宝炉材料不足 500，跳过", "role_id", session.RoleID(),
						"item_id", m.ItemID, "count", m.Count)
					continue
				}
				typeID, ok := refineFurnaceTypeForMaterial(m.ItemID)
				if !ok {
					slog.Info("炼宝炉材料类型未知，跳过", "role_id", session.RoleID(),
						"item_id", m.ItemID, "type_id", typeID)
					continue
				}
				selResp := call(protocol.BuildRefineFurnaceTypePacket(typeID), protocol.ActionIDRefineFurnaceType)
				if selResp == nil {
					// 该类型可能未解锁/请求被丢弃，继续尝试下一种材料。
					continue
				}
				selCode, selErr := protocol.ParseFirstByteResult(selResp.Payload, "炼宝炉选择元气")
				if selErr != nil {
					continue
				}
				if selCode != protocol.RefineFurnaceSuccess {
					slog.Info("炼宝炉选择元气未成功", "role_id", session.RoleID(),
						"item_id", m.ItemID, "type_id", typeID, "result", selCode)
					continue
				}

				batches := m.Count / refineFurnaceBatchSize
				if batches < 1 {
					continue
				}
				// 每批实测消耗 20,000,000 铜钱；铜钱不足时按半量回退，避免死循环。
				for batches >= 1 {
					startResp := call(protocol.BuildRefineFurnaceStartPacket(batches), protocol.ActionIDRefineFurnaceStart)
					if startResp == nil {
						slog.Info("炼宝炉开炉无回包，稍后重试", "role_id", session.RoleID())
						return scheduler.NewCooldown(30*time.Minute, "炼宝炉开炉无回包")
					}
					startCode, sErr := protocol.ParseFirstByteResult(startResp.Payload, "炼宝炉炼制")
					if sErr != nil {
						slog.Warn("解析炼宝炉炼制回包失败，稍后重试", "role_id", session.RoleID(), "err", sErr)
						return scheduler.NewCooldown(30*time.Minute, "炼宝炉炼制回包解析失败")
					}
					switch startCode {
					case protocol.RefineFurnaceSuccess:
						slog.Info("炼宝炉炼制已开始", "role_id", session.RoleID(),
							"item_id", m.ItemID, "count", m.Count, "batches", batches)
						return nil
					case protocol.RefineFurnaceLessCoin:
						slog.Info("炼宝炉铜钱不足，减半批数重试", "role_id", session.RoleID(),
							"item_id", m.ItemID, "batches", batches)
						batches /= 2
						continue
					case protocol.RefineFurnaceLessAir:
						slog.Info("炼宝炉元气不足，减半批数重试", "role_id", session.RoleID(),
							"item_id", m.ItemID, "batches", batches)
						batches /= 2
						continue
					case protocol.RefineFurnaceNotGetAward, protocol.RefineFurnaceNotComplete:
						slog.Info("炼宝炉仍有未收取产物，结束本轮", "role_id", session.RoleID(),
							"result", startCode)
						return nil
					case protocol.RefineFurnacePackFull:
						return scheduler.ErrBagFull
					default:
						slog.Info("炼宝炉炼制未成功", "role_id", session.RoleID(),
							"item_id", m.ItemID, "result", startCode)
						break
					}
					break
				}
			}
			slog.Info("炼宝炉本轮无可炼制材料", "role_id", session.RoleID())
			return nil
		},
	)
}
