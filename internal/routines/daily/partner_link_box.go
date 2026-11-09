// Package daily 提供签到、邮件、在线奖励、绑定奖励与日常维护类玩法。
//
// 本文件实现天缘录缘魂宝箱购买 (Mod_PartnerLink_Base, Module 290)：
// 每日活跃度任务 55「每日开启魂缘宝箱」每次 +5，每日上限 9 次。
//
// 只购买道缘/铜钱结算的宝箱；元宝档 (money_type=10) 永不购买。
package daily

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// DailyTaskPartnerLinkBox 每日开启魂缘宝箱任务编号。
const DailyTaskPartnerLinkBox int32 = 55

// NewPartnerLinkBoxRoutine 构造缘魂宝箱购买任务。
func NewPartnerLinkBoxRoutine() scheduler.ActivityRoutine {
	const callTimeout = 6 * time.Second
	const maxBuys = 9
	return scheduler.NewBaseRoutine(
		"partner_link_box",
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
					// 重连/服务端静默丢弃均表现为无回包；缘魂宝箱为低优先级福利，直接跳过本轮。
					slog.Info("缘魂宝箱协议无回包，跳过本轮", "role_id", session.RoleID(),
						"action", act, "err", err)
					return nil
				}
				return resp
			}

			actResp := call(protocol.BuildGameAssistantInfoPacket(), protocol.ActionIDGameAssistantInfo)
			if actResp == nil {
				slog.Info("查询每日活跃度失败，稍后重试缘魂宝箱", "role_id", session.RoleID())
				return scheduler.NewCooldown(10*time.Minute, "缘魂宝箱活跃度查询无回包")
			}
			panel, err := protocol.ParseGameAssistantInfo(actResp.Payload)
			if err != nil {
				return err
			}
			remain := panel.RemainingTimesFor(DailyTaskPartnerLinkBox)
			if remain <= 0 {
				slog.Info("魂缘宝箱活跃度已完成，跳过", "role_id", session.RoleID())
				return nil
			}
			if remain > maxBuys {
				remain = maxBuys
			}

			bought := 0
			var boughtBoxType uint8
			var boughtMoneyType uint8
			// 每次购买前都重新拉取商店列表。服务端可能在首购后调整价格/货币
			// （药园种子已实测会从铜钱切换为元宝），绝不能复用旧列表的货币信息。
			for bought < remain {
				if ctx.Err() != nil {
					break
				}
				shopResp := call(protocol.BuildPartnerLinkShopPacket(), protocol.ActionIDPartnerLinkShop)
				if shopResp == nil {
					slog.Info("缘魂宝箱商店无回包，稍后重试", "role_id", session.RoleID())
					return scheduler.NewCooldown(30*time.Minute, "缘魂宝箱商店无回包")
				}
				boxes, pErr := protocol.ParsePartnerLinkShopResult(shopResp.Payload)
				if pErr != nil {
					slog.Warn("解析缘魂宝箱商店失败，稍后重试", "role_id", session.RoleID(), "err", pErr)
					return scheduler.NewCooldown(10*time.Minute, "缘魂宝箱商店解析失败")
				}
				// 优先铜钱结算，其次道缘；元宝档与售罄档一律跳过。
				var target *protocol.PartnerLinkBox
				for i := range boxes {
					b := &boxes[i]
					if b.MoneyType == protocol.PartnerLinkMoneyIngot || b.Times <= 0 {
						continue
					}
					if target == nil || b.MoneyType == protocol.PartnerLinkMoneyCoin {
						target = b
					}
				}
				if target == nil {
					if bought == 0 {
						slog.Info("没有道缘/铜钱结算的可购缘魂宝箱，跳过", "role_id", session.RoleID())
					} else {
						slog.Info("缘魂宝箱已切换为元宝档或售罄，停止购买",
							"role_id", session.RoleID(), "bought", bought)
					}
					break
				}
				boughtBoxType = target.BoxType
				boughtMoneyType = target.MoneyType
				resp := call(protocol.BuildPartnerLinkBuyPacket(target.BoxType), protocol.ActionIDPartnerLinkBuy)
				if resp == nil || len(resp.Payload) == 0 {
					slog.Info("购买缘魂宝箱无回包，稍后重试",
						"role_id", session.RoleID(), "bought", bought)
					return scheduler.NewCooldown(30*time.Minute, "缘魂宝箱购买无回包")
				}
				code := resp.Payload[0]
				switch code {
				case protocol.PartnerLinkResultSuccess:
					bought++
				case protocol.PartnerLinkResultNotBuyCount:
					slog.Info("缘魂宝箱可购次数不足", "role_id", session.RoleID(), "bought", bought)
					remain = bought
				case protocol.PartnerLinkResultNotEnoughDaoYuan:
					slog.Info("道缘不足，停止购买缘魂宝箱", "role_id", session.RoleID())
					return nil
				default:
					slog.Info("购买缘魂宝箱未成功",
						"role_id", session.RoleID(), "box_type", target.BoxType, "result", code)
					return nil
				}
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(120 * time.Millisecond):
				}
			}
			if bought > 0 {
				slog.Info("缘魂宝箱购买完成",
					"role_id", session.RoleID(), "box_type", boughtBoxType,
					"money_type", boughtMoneyType, "bought", bought)
			}
			return nil
		},
	)
}
