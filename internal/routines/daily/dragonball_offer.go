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
			//
			// 服务端对批次做整体校验：只要混入一条失效/过期记录，整批都会返回
			// INVALID_DRAGONBALL(2)。因此这里逐颗发送，跳过失效记录，只要至少
			// 收取成功一颗就视为腾出了空间。
			collectTmp := func() (bool, error) {
				cCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
				infoResp, err := session.Call(cCtx, protocol.BuildDragonballTmpInfoPacket(), protocol.ActionIDDragonballTmpInfo)
				cancel()
				if err != nil || infoResp == nil {
					return false, err
				}
				balls, err := protocol.ParseDragonballTmpInfoResult(infoResp.Payload)
				if err != nil || len(balls) == 0 {
					return false, err
				}
				collected, invalid := 0, 0
				invalidIDs := make([]int32, 0)
				for _, b := range balls {
					if err := ctx.Err(); err != nil {
						return collected > 0, nil
					}
					cCtx2, cancel2 := context.WithTimeout(ctx, 6*time.Second)
					resp, cErr := session.Call(cCtx2, protocol.BuildDragonballCollectPacket([]int32{b.PlayerDBID}), protocol.ActionIDDragonballCollect)
					cancel2()
					if cErr != nil || resp == nil {
						continue
					}
					result, ids, perr := protocol.ParseDragonballCollectResult(resp.Payload)
					if perr != nil {
						continue
					}
					switch result {
					case protocol.DragonballResultSuccess, protocol.DragonballResultPackageFull:
						collected++
						slog.Info("神龙祭坛临时格收取成功",
							"role_id", session.RoleID(), "player_d_b_id", b.PlayerDBID, "collected", ids)
					case protocol.DragonballResultInvalidDragonball:
						invalid++
						invalidIDs = append(invalidIDs, b.PlayerDBID)
						slog.Info("神龙祭坛临时格发现失效龙珠，稍后出售",
							"role_id", session.RoleID(), "player_d_b_id", b.PlayerDBID)
					}
				}
				// 失效龙珠会长期占住祭坛格子，必须出售才能腾出空间。
				sold := 0
				if len(invalidIDs) > 0 {
					sCtx, sCancel := context.WithTimeout(ctx, 8*time.Second)
					sellResp, sErr := session.Call(sCtx,
						protocol.BuildDragonballSellPacket(invalidIDs),
						protocol.ActionIDDragonballSell)
					sCancel()
					if sErr != nil || sellResp == nil {
						slog.Warn("出售失效龙珠无回包",
							"role_id", session.RoleID(), "count", len(invalidIDs), "err", sErr)
					} else if code, pErr := protocol.ParseDragonballSellResult(sellResp.Payload); pErr != nil {
						slog.Warn("解析出售失效龙珠回包失败",
							"role_id", session.RoleID(), "err", pErr)
					} else if code == protocol.DragonballResultSuccess {
						sold = len(invalidIDs)
						slog.Info("神龙祭坛失效龙珠出售成功",
							"role_id", session.RoleID(), "count", sold)
					} else {
						slog.Info("出售失效龙珠未成功",
							"role_id", session.RoleID(), "count", len(invalidIDs), "result", code)
					}
				}
				slog.Info("神龙祭坛临时格收取完成",
					"role_id", session.RoleID(), "requested", len(balls),
					"collected", collected, "invalid", invalid, "sold", sold)
				return collected > 0 || sold > 0, nil
			}

			// mergeQualities 按品质一键合成龙珠。
			// 普通龙珠只合成紫色及以下 (1/2/3)；火龙珠 (quality=6) 单独合并。
			mergeQualities := func(qualities []int32, label string) {
				mCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				resp, err := session.Call(mCtx,
					protocol.BuildDragonballOneKeyMergePacket(qualities),
					protocol.ActionIDDragonballOneKeyMerge)
				cancel()
				if err != nil || resp == nil {
					slog.Warn("神龙龙珠一键合成无回包",
						"role_id", session.RoleID(), "label", label, "qualities", qualities, "err", err)
					return
				}
				result, ids, pErr := protocol.ParseDragonballOneKeyMergeResult(resp.Payload)
				if pErr != nil {
					slog.Warn("解析神龙龙珠一键合成回包失败",
						"role_id", session.RoleID(), "label", label, "err", pErr)
					return
				}
				slog.Info("神龙龙珠一键合成完成",
					"role_id", session.RoleID(),
					"label", label,
					"qualities", qualities,
					"result", result,
					"merged", len(ids),
				)
			}
			mergeAll := func() {
				mergeQualities(protocol.DragonballNormalMergeQualities(), "紫及以下")
				mergeQualities([]int32{protocol.DragonballQualityFire}, "火龙珠")
			}

			// 0. 先拾取祭坛临时格中的龙珠，并做一次合成，再进入上供循环。
			_, _ = collectTmp()
			mergeAll()

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
						mergeAll()
						return nil
					}
				case protocol.DragonballResultTimesUseUp:
					slog.Info("神龙普通上供今日次数已用尽", "role_id", session.RoleID())
					mergeAll()
					return nil
				case protocol.DragonballResultNoEnoughCoin:
					slog.Info("铜钱不足，停止神龙普通上供", "role_id", session.RoleID())
					return nil
				case protocol.DragonballResultPackageFull:
					slog.Info("龙珠背包已满，停止神龙普通上供", "role_id", session.RoleID())
					return scheduler.ErrBagFull
				case protocol.DragonballResultTmpGridFull:
					// 每次收取会把祭坛龙珠移入背包并出售失效龙珠，循环内最多
					// 与上供次数同量处理，避免过早中止。
					if collections >= maxDragonballOffers {
						slog.Info("神龙祭坛临时格多次收取后仍满，停止本轮", "role_id", session.RoleID())
						return scheduler.ErrBagFull
					}
					collections++
					ok, cErr := collectTmp()
					if cErr != nil || !ok {
						slog.Info("神龙祭坛临时格收取失败，停止上供", "role_id", session.RoleID(), "err", cErr)
						return scheduler.ErrBagFull
					}
					// 收取后立即合成并重试当前上供，不消耗循环次数。
					mergeAll()
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
			mergeAll()
			slog.Info("神龙普通上供巡检完成", "role_id", session.RoleID(), "offers", offers)
			return nil
		},
	)
}
