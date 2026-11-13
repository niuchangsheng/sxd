// 本文件实现精品杂货 VIP5/VIP8 商店购买 (Mod_StDaoyuanShop_Base, Module 132)。
//
// 目标：VIP5 购买神甲晶石(7298)、天魂自选包(7089)；VIP8 购买天魂自选包(7089)。
// 价格 >= 500 视为元宝档，一律跳过；仅购买低价（道缘/铜钱）档。
package daily

import (
	"context"
	"errors"
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
// errVipShopNoResponse 标记 132 协议在会话上被服务端静默丢弃。
// Module 132 (Mod_StDaoyuanShop) 是圣域专属商店：主服连接对所有 132 动作
// 均不回包，必须优先改走圣域独立会话 (Mod 293)，避免每轮固定产生一次丢包。
var errVipShopNoResponse = errors.New("精品杂货协议无回包")

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
			call := func(s *client.RoleSession, pkt *protocol.Packet, act uint32) *protocol.Packet {
				cCtx, cancel := context.WithTimeout(ctx, callTimeout)
				defer cancel()
				resp, err := s.Call(cCtx, pkt, act)
				if err != nil {
					slog.Info("道缘商店协议无回包", "role_id", s.RoleID(), "action", act, "err", err)
					return nil
				}
				return resp
			}

			run := func(s *client.RoleSession, vipLevel uint8, targets []int32) error {
				listResp := call(s, protocol.BuildDaoyuanVipShopListPacket(vipLevel), protocol.ActionIDDaoyuanVipShopList)
				if listResp == nil {
					slog.Info("精品杂货商店列表无回包", "role_id", s.RoleID(), "vip", vipLevel)
					return errVipShopNoResponse
				}
				items, err := protocol.ParseDaoyuanVipShopList(listResp.Payload)
				if err != nil {
					slog.Warn("解析精品杂货商店列表失败", "role_id", s.RoleID(), "vip", vipLevel, "err", err)
					return nil
				}
				slog.Info("精品杂货 VIP 商店列表", "role_id", s.RoleID(), "vip", vipLevel, "items", items)

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
							"role_id", s.RoleID(), "vip", vipLevel, "item_id", item.ItemID, "price", item.Price)
						continue
					}
					if item.Price >= vipShopIngotPriceThreshold {
						slog.Info("精品杂货商品疑似元宝定价，跳过",
							"role_id", s.RoleID(), "vip", vipLevel,
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
					resp := call(s, protocol.BuildDaoyuanShopBuyPacket(item.Index, count), protocol.ActionIDDaoyuanShopBuy)
					if resp == nil {
						return errVipShopNoResponse
					}
					code, pErr := protocol.ParseDaoyuanShopBuyResult(resp.Payload)
					if pErr != nil {
						slog.Warn("解析精品杂货购买回包失败", "role_id", s.RoleID(), "err", pErr)
						return nil
					}
					switch code {
					case protocol.DaoyuanShopResultSuccess:
						slog.Info("精品杂货购买成功",
							"role_id", s.RoleID(), "vip", vipLevel,
							"item_id", item.ItemID, "count", count, "price", item.Price)
					case protocol.DaoyuanShopResultPackFull:
						slog.Warn("精品杂货购买背包已满", "role_id", s.RoleID())
						return scheduler.ErrBagFull
					case protocol.DaoyuanShopResultNoDaoYuan, protocol.DaoyuanShopResultNoIngot,
						protocol.DaoyuanShopResultVipTwo, protocol.DaoyuanShopResultVipFive,
						protocol.DaoyuanShopResultVipEight, protocol.DaoyuanShopResultItemNotExist:
						slog.Info("精品杂货购买未成功", "role_id", s.RoleID(),
							"item_id", item.ItemID, "result", code,
							"result_text", protocol.DaoyuanShopResultText(code))
					default:
						slog.Info("精品杂货购买未成功", "role_id", s.RoleID(),
							"item_id", item.ItemID, "result", code)
					}
				}
				return nil
			}

			runAll := func(s *client.RoleSession) error {
				if err := run(s, protocol.DaoyuanVipShopTabFive, []int32{itemShenJiaJingShi, itemTianHunPack}); err != nil {
					return err
				}
				return run(s, protocol.DaoyuanVipShopTabEight, []int32{itemTianHunPack})
			}

			// Module 132 是圣域专属协议；圣域功能未开放时 (如低等级角色)
			// 主服同样静默丢弃，直接跳过，避免每轮空等 8 秒。
			if open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDSaintArea); fErr != nil {
				slog.Info("圣域功能开放状态查询失败，跳过精品杂货",
					"role_id", session.RoleID(), "err", fErr)
				return nil
			} else if !open {
				slog.Info("圣域功能未开放，跳过精品杂货 (132 仅圣域提供)",
					"role_id", session.RoleID(),
					"function_id", protocol.FunctionIDSaintArea)
				return nil
			}

			// Module 132 是圣域专属协议，优先在圣域会话执行；圣域不可用时
			// 才回退主服（主服大概率静默丢包）。
			// 注意：凭据申请可用短超时；圣域会话本身必须绑定长生命周期 ctx，
			// 若把短超时 ctx 传给 OpenSaintAreaSession，超时后会话会被立即关闭。
			saCtx, saCancel := context.WithTimeout(ctx, 5*time.Second)
			cred, credErr := session.FetchSaintAreaCredentials(saCtx)
			saCancel()
			if credErr == nil {
				sa, openErr := session.OpenSaintAreaSessionWithInfo(ctx, cred)
				if openErr == nil {
					saErr := runAll(sa)
					sa.Close()
					if !errors.Is(saErr, errVipShopNoResponse) {
						return saErr
					}
					slog.Info("圣域精品杂货商店无回包，改走主服会话", "role_id", session.RoleID())
				}
			}

			err := runAll(session)
			if errors.Is(err, errVipShopNoResponse) {
				// 圣域不可用且主服也丢包：保留冷却，等待圣域会话恢复。
				return scheduler.NewCooldown(30*time.Minute, "精品杂货商店无回包")
			}
			return err
		},
	)
}
