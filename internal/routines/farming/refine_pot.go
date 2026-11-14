// 本文件实现灵霄宝殿炼宝炉 (Mod_RefineFurnace_Base, Module 586)。
//
// 流程：open_panel(1) 读取元气材料 → get_award(6) 收取上一次炼制产物 →
// select_air(3, 参数为元气道具 ID) 选中材料 → start_refine(4, 裸 4 字节批数)。
// 每轮最多 2 次开炉尝试，剩余材料由下一轮继续；材料 <500 直接跳过。不涉及元宝。
//
// 2026-11-14 线上探针结论：
//   - select_air 的参数是道具 ID (7923/7924/7925)，成功后面板 selected 同步更新；
//   - start_refine 必须为裸 4 字节，追加 PrevAct 的 8 字节载荷会被静默丢弃；
//   - 连续开炉超过一定次数后服务端开始静默丢弃，因此限制每轮尝试次数。
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
	// refineFurnacePacing 服务端对 586 连续请求敏感：面板后立刻发包会被静默丢弃，
	// 2026-11-14 线上探针实测间隔 500ms 可稳定回包。
	refineFurnacePacing = 500 * time.Millisecond
	// refineFurnaceCoinPerBatch 每批消耗的铜钱数量（2026-11-09 实测 50 元气 = 20,000,000 铜钱）。
	refineFurnaceCoinPerBatch int64 = 20_000_000
)

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

			// 面板后立刻发包会被静默丢弃，先节流再继续。
			time.Sleep(refineFurnacePacing)

			// 2. 铜钱不足时不开炉：每批 20,000,000 铜钱。服务端对连续开炉有限流，
			// 反复试探会被静默丢弃，因此直接按余额计算可负担批数。
			coins := session.GetPlayerState().Coins
			maxByCoin := coins / refineFurnaceCoinPerBatch
			if maxByCoin < 1 {
				slog.Info("炼宝炉铜钱不足，跳过开炉", "role_id", session.RoleID(),
					"coins", coins, "coin_per_batch", refineFurnaceCoinPerBatch)
				return scheduler.NewCooldown(30*time.Minute, "炼宝炉铜钱不足")
			}

			// 3. 选择一种数量 >=500 的元气，按可负担批数开炉。
			// 官方流程先 select_type 再 select_air；实测 select_air(道具 ID)
			// 会直接更新面板 selected 字段，因此这里按道具 ID 选择即可。
			const maxStartAttempts = 2
			starts := 0
			for _, m := range panel.Materials {
				if ctx.Err() != nil {
					return nil
				}
				if starts >= maxStartAttempts {
					break
				}
				if m.Count < minRefinePotMaterialCount {
					slog.Info("炼宝炉材料不足 500，跳过", "role_id", session.RoleID(),
						"item_id", m.ItemID, "count", m.Count)
					continue
				}
				batches := m.Count / refineFurnaceBatchSize
				if int64(batches) > maxByCoin {
					batches = int32(maxByCoin)
				}
				if batches < 1 {
					continue
				}

				selResp := call(protocol.BuildRefineFurnaceAirPacket(m.ItemID), protocol.ActionIDRefineFurnaceAir)
				if selResp == nil {
					// 该材料可能未解锁/请求被丢弃，继续尝试下一种材料。
					time.Sleep(refineFurnacePacing)
					continue
				}
				selCode, selErr := protocol.ParseFirstByteResult(selResp.Payload, "炼宝炉选择元气")
				if selErr != nil {
					continue
				}
				if selCode != protocol.RefineFurnaceSuccess {
					slog.Info("炼宝炉选择元气未成功", "role_id", session.RoleID(),
						"item_id", m.ItemID, "result", selCode)
					time.Sleep(refineFurnacePacing)
					continue
				}

				time.Sleep(refineFurnacePacing)
				starts++
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
					// 铜钱是全局资源，换材料也不会更便宜；连续重试会触发限流丢包，直接冷却。
					slog.Info("炼宝炉铜钱不足，稍后重试", "role_id", session.RoleID(),
						"item_id", m.ItemID, "batches", batches)
					return scheduler.NewCooldown(30*time.Minute, "炼宝炉铜钱不足")
				case protocol.RefineFurnaceLessAir:
					slog.Info("炼宝炉元气不足，尝试下一种材料", "role_id", session.RoleID(),
						"item_id", m.ItemID, "batches", batches)
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
				}
				time.Sleep(refineFurnacePacing)
			}
			slog.Info("炼宝炉本轮无可炼制材料", "role_id", session.RoleID())
			return nil
		},
	)
}
