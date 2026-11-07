// Package daily 提供签到、邮件、在线奖励、绑定奖励与日常维护类玩法。
//
// 本文件实现兵法「购买圣水」(Mod_Tactics_Base, Module 299)：
// 每日活跃度任务 54「兵法」= 每次购买圣水 +5，每日上限 25 次。
//
// 只购买湛蓝圣水 (铜钱, 约 200 万/滴)；黄金圣水为元宝档，永不调用。
package daily

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// DailyTaskTactics 兵法每日任务编号。
const DailyTaskTactics int32 = 54

// tacticsBuyWaterCoinBuffer 购买圣水的铜钱安全下限 (单滴约 200 万)。
const tacticsBuyWaterCoinBuffer int64 = 3_000_000

// NewTacticsWaterRoutine 构造兵法购买湛蓝圣水任务。
func NewTacticsWaterRoutine() scheduler.ActivityRoutine {
	const callTimeout = 6 * time.Second
	const maxBuys = 25
	return scheduler.NewBaseRoutine(
		"tactics_water",
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
					slog.Warn("兵法协议无回包", "role_id", session.RoleID(),
						"action", act, "err", err)
					return nil
				}
				return resp
			}

			actResp := call(protocol.BuildGameAssistantInfoPacket(), protocol.ActionIDGameAssistantInfo)
			if actResp == nil {
				slog.Warn("查询每日活跃度失败，跳过兵法圣水", "role_id", session.RoleID())
				return nil
			}
			panel, err := protocol.ParseGameAssistantInfo(actResp.Payload)
			if err != nil {
				return err
			}
			remain := panel.RemainingTimesFor(DailyTaskTactics)
			if remain <= 0 {
				slog.Info("兵法活跃度已完成，跳过", "role_id", session.RoleID())
				return nil
			}
			if remain > maxBuys {
				remain = maxBuys
			}

			bought := 0
			for bought < remain {
				if ctx.Err() != nil {
					break
				}
				if session.GetPlayerState().Coins < tacticsBuyWaterCoinBuffer {
					slog.Info("铜钱不足，停止购买兵法圣水",
						"role_id", session.RoleID(), "coins", session.GetPlayerState().Coins)
					break
				}
				resp := call(protocol.BuildTacticsBuyItemPacket(protocol.TacticsWaterXiao),
					protocol.ActionIDTacticsBuyItem)
				if resp == nil {
					break
				}
				code, err := protocol.ParseTacticsResultCode(resp.Payload)
				if err != nil {
					break
				}
				switch code {
				case protocol.TacticsResultSucceed:
					bought++
				case protocol.TacticsResultBuyLimit:
					slog.Info("兵法圣水购买次数已达上限", "role_id", session.RoleID(), "bought", bought)
					remain = bought
				case protocol.TacticsResultNoEnoughCoin:
					slog.Info("铜钱不足，停止购买兵法圣水", "role_id", session.RoleID())
					return nil
				default:
					slog.Info("购买兵法圣水未成功",
						"role_id", session.RoleID(),
						"result", code,
						"result_text", protocol.TacticsResultText(code))
					return nil
				}
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(120 * time.Millisecond):
				}
			}
			if bought > 0 {
				slog.Info("兵法圣水购买完成", "role_id", session.RoleID(), "bought", bought)
			}
			return nil
		},
	)
}
