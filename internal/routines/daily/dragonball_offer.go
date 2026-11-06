// Package daily 提供签到、邮件、在线奖励、绑定奖励与日常维护类玩法。
//
// 本文件实现神龙普通上供，用于补齐每日活跃度 (每次 +5，上限 20 次)。
package daily

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// maxDragonballOffers 单轮最多普通上供次数 (游戏每日上限 20)。
const maxDragonballOffers = 20

// NewDragonballOfferRoutine 构造神龙普通上供任务。
//
// 只调用 Mod_Dragonball_Base.sacrifice(NORMAL, NOT_AUTO)：
//   - 普通上供消耗铜钱，不消耗元宝；
//   - 每次提供 +5 每日活跃度，是达到 1200 活跃度的关键免费来源；
//   - 高级上供 (HIGH_LEVEL) 消耗元宝，自动化永不调用。
func NewDragonballOfferRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"dragonball_offer",
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
			slog.Info("正在执行日常任务: 神龙普通上供", "role_id", session.RoleID())

			// collectTmp 把祭坛临时格中的龙珠收取到背包，腾出空间继续上供。
			collectTmp := func() (bool, error) {
				cCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				infoResp, err := session.Call(cCtx, protocol.BuildDragonballTmpInfoPacket(), protocol.ActionIDDragonballTmpInfo)
				cancel()
				if err != nil || infoResp == nil {
					return false, err
				}
				balls, err := protocol.ParseDragonballTmpInfoResult(infoResp.Payload)
				if err != nil || len(balls) == 0 {
					return false, err
				}
				ids := make([]int32, 0, len(balls))
				for _, b := range balls {
					ids = append(ids, b.PlayerDBID)
				}
				cCtx2, cancel2 := context.WithTimeout(ctx, 6*time.Second)
				resp, err := session.Call(cCtx2, protocol.BuildDragonballCollectPacket(ids), protocol.ActionIDDragonballCollect)
				cancel2()
				if err != nil || resp == nil {
					return false, err
				}
				result, collected, perr := protocol.ParseDragonballCollectResult(resp.Payload)
				if perr != nil {
					return false, perr
				}
				slog.Info("神龙祭坛临时格收取",
					"role_id", session.RoleID(), "result", result, "requested", len(ids), "collected", len(collected))
				return result == protocol.DragonballResultSuccess || result == protocol.DragonballResultPackageFull, nil
			}

			offers := 0
			collections := 0
			for i := 0; i < maxDragonballOffers; i++ {
				if err := ctx.Err(); err != nil {
					return nil
				}
				cCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
				resp, err := session.Call(cCtx,
					protocol.BuildDragonballSacrificePacket(protocol.DragonballSacrificeNormal, protocol.DragonballSacrificeNotAuto),
					protocol.ActionIDDragonballSacrifice)
				cancel()
				if err != nil || resp == nil {
					slog.Warn("神龙普通上供无回包", "role_id", session.RoleID(), "err", err)
					return nil
				}
				res, pErr := protocol.ParseDragonballSacrificeResult(resp.Payload)
				if pErr != nil {
					slog.Warn("解析神龙普通上供回包失败", "role_id", session.RoleID(), "err", pErr)
					return nil
				}
				switch res.Result {
				case protocol.DragonballResultSuccess:
					offers++
					slog.Info("神龙普通上供成功",
						"role_id", session.RoleID(),
						"times", offers,
						"remain_times", res.RemainTimes,
						"score", res.Score,
						"alerts", len(res.Alerts),
					)
					if res.RemainTimes == 0 {
						slog.Info("神龙普通上供今日次数已用尽", "role_id", session.RoleID())
						return nil
					}
				case protocol.DragonballResultTimesUseUp:
					slog.Info("神龙普通上供今日次数已用尽", "role_id", session.RoleID())
					return nil
				case protocol.DragonballResultNoEnoughCoin:
					slog.Info("铜钱不足，停止神龙普通上供", "role_id", session.RoleID())
					return nil
				case protocol.DragonballResultPackageFull:
					slog.Info("龙珠背包已满，停止神龙普通上供", "role_id", session.RoleID())
					return scheduler.ErrBagFull
				case protocol.DragonballResultTmpGridFull:
					if collections >= 3 {
						slog.Info("神龙祭坛临时格多次收取后仍满，停止本轮", "role_id", session.RoleID())
						return scheduler.ErrBagFull
					}
					collections++
					ok, cErr := collectTmp()
					if cErr != nil || !ok {
						slog.Info("神龙祭坛临时格收取失败，停止上供", "role_id", session.RoleID(), "err", cErr)
						return scheduler.ErrBagFull
					}
					// 收取后立即重试当前上供，不消耗循环次数。
					i--
					continue
				case protocol.DragonballResultFunctionNoOpen:
					slog.Info("神龙上供功能未开放", "role_id", session.RoleID())
					return nil
				case protocol.DragonballResultNotEnoughLevel:
					slog.Info("神龙上供等级不足", "role_id", session.RoleID())
					return nil
				case protocol.DragonballResultNoEnoughIngot:
					// 普通上供不应出现元宝不足；若出现立即停止，绝不切换高级上供。
					slog.Warn("神龙普通上供返回元宝不足，停止", "role_id", session.RoleID())
					return nil
				default:
					slog.Info("神龙普通上供未成功", "role_id", session.RoleID(), "result", res.Result)
					return nil
				}
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(150 * time.Millisecond):
				}
			}
			slog.Info("神龙普通上供巡检完成", "role_id", session.RoleID(), "offers", offers)
			return nil
		},
	)
}
