package daily

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// OnlineGiftConfig 在线礼包与功能终结礼包自动化策略。
//
// 功能终结礼包 (Mod_FunctionEnd, Module 127) 是吉星高照、喜从天降
// (龙鱼仙令/境界点)、灵脉、拜关公、聚灵等玩法达到等级上限终结后，
// 转为每日领取的统一补偿礼包。它与各玩法的活跃协议互不干扰。
type OnlineGiftConfig struct {
	// ClaimFunctionEndGifts 是否领取功能终结礼包 (默认 true)
	ClaimFunctionEndGifts bool
	// MaxClaimsPerGift 单个终结礼包连续领取次数上限 (默认 20，防止服务端异常导致死循环)
	MaxClaimsPerGift int
}

// DefaultOnlineGiftConfig 返回默认策略: 领取全部免费终结礼包。
func DefaultOnlineGiftConfig() OnlineGiftConfig {
	return OnlineGiftConfig{
		ClaimFunctionEndGifts: true,
		MaxClaimsPerGift:      20,
	}
}

// functionEndClaimInterval 连续领取同一礼包之间的拟人延迟。
const functionEndClaimInterval = 200 * time.Millisecond

// functionEndRollIsFree 判断本次摇奖结果是否免元宝。
//
// 线上实测 (s0-niuchangsheng, 2026-11-05): random_award 回包的 ingot 字段是
// 本次奖励的领取价格，领取时会按该值扣除元宝。只有 ingot == 0 才是真正免费。
func functionEndRollIsFree(preview *protocol.FunctionEndRandomAward) bool {
	return preview != nil && preview.Ingot <= 0
}

// functionEndGiftHasPendingRoll 判断礼包列表记录是否已经携带摇奖结果。
//
// 线上实测: 已摇奖记录的奖励字段会被服务端回填，此时 ingot 字段是本次奖励的
// 领取价格。对已摇奖记录再次 random_award 会按该价格扣元宝，因此绝不能重摇。
func functionEndGiftHasPendingRoll(gift protocol.FunctionEndGift) bool {
	return gift.StatePoint != 0 || gift.Skill != 0 || gift.XianLing != 0 ||
		gift.Fame != 0 || gift.Nimbus != 0 || gift.CoinBuff != 0 ||
		gift.ExpBuff != 0 || gift.JuLing != 0
}

// claimFunctionEndGifts 查询并领取当前所有可领取的功能终结礼包。
//
// 协议依据原版客户端 com.views.GetAwardView:
//   - Mod_FunctionEnd_Base.game_function_end_gift (Mod 127, Act 1) 拉取礼包列表;
//   - Mod_FunctionEnd_Base.get_game_function_end_gift (Mod 127, Act 2) 逐个领取;
//   - 回包 is_have_next 为 true 时客户端保留同一礼包继续领取。
func claimFunctionEndGifts(ctx context.Context, session *client.RoleSession, cfg OnlineGiftConfig) error {
	qCtx, qCancel := context.WithTimeout(ctx, 5*time.Second)
	resp, err := session.Call(qCtx, protocol.BuildFunctionEndGiftListPacket(), protocol.ActionIDGameFunctionEndGift)
	qCancel()
	if err != nil {
		return fmt.Errorf("function end gift list: %w", err)
	}
	if resp == nil {
		return errors.New("function end gift list: empty response")
	}

	gifts, err := protocol.ParseFunctionEndGiftListResult(resp.Payload)
	if err != nil {
		return fmt.Errorf("function end gift list: %w", err)
	}
	if len(gifts) == 0 {
		slog.Debug("暂无待领取功能终结礼包", "role_id", session.RoleID())
		return nil
	}

	// 背包已满时服务端会返回 ITEM_PACK_FULL；提前熔断避免无意义请求。
	if state := session.GetPlayerState(); state.BagCapacity <= 0 {
		slog.Warn("背包容量不足，暂不领取功能终结礼包", "role_id", session.RoleID())
		return scheduler.ErrBagFull
	}

	slog.Info("发现待领取功能终结礼包",
		"role_id", session.RoleID(),
		"count", len(gifts),
	)

	for _, gift := range gifts {
		if err := claimFunctionEndGift(ctx, session, gift, cfg); err != nil {
			if errors.Is(err, scheduler.ErrBagFull) {
				return err
			}
			slog.Warn("领取功能终结礼包失败",
				"role_id", session.RoleID(),
				"gift_id", gift.GiftID,
				"err", err,
			)
		}
	}
	return nil
}

// claimFunctionEndGift 领取单个终结礼包；is_have_next 为 true 时按客户端语义继续领取。
func claimFunctionEndGift(ctx context.Context, session *client.RoleSession, gift protocol.FunctionEndGift, cfg OnlineGiftConfig) error {
	maxClaims := cfg.MaxClaimsPerGift
	if maxClaims <= 0 {
		maxClaims = 20
	}

	// pending 表示列表记录已经携带摇奖结果，此时不能再调用 random_award，
	// 否则服务端会按该结果的价格再次扣除元宝。
	pending := functionEndGiftHasPendingRoll(gift)
	for attempt := 1; attempt <= maxClaims; attempt++ {
		if pending {
			// 已摇奖: 只按列表中的价格判断，价格 > 0 直接放弃，绝不重摇。
			if gift.IngotCost > 0 {
				slog.Info("跳过需要消耗元宝的功能终结礼包",
					"role_id", session.RoleID(),
					"gift_id", gift.GiftID,
					"ingot_cost", gift.IngotCost,
					"attempt", attempt,
					"already_rolled", true,
				)
				return nil
			}
		} else {
			// 官方客户端 GetAwardView 在领取前必须先 random_award 摇出本份奖励；
			// 直接调用 get_game_function_end_gift 会返回全 0 奖励且 is_have_next 持续为真。
			preview, err := rollFunctionEndGift(ctx, session, gift.GiftID)
			if err != nil {
				return fmt.Errorf("roll gift %d: %w", gift.GiftID, err)
			}
			// 铁律: 自动化永不消耗元宝。摇奖回包的 ingot 是领取价格，>0 一律放弃。
			if !functionEndRollIsFree(preview) {
				slog.Info("跳过需要消耗元宝的功能终结礼包",
					"role_id", session.RoleID(),
					"gift_id", gift.GiftID,
					"ingot_cost", preview.Ingot,
					"attempt", attempt,
				)
				return nil
			}
			slog.Debug("功能终结礼包摇奖完成",
				"role_id", session.RoleID(),
				"gift_id", gift.GiftID,
				"attempt", attempt,
				"state_point", preview.StatePoint,
				"skill", preview.Skill,
				"xian_ling", preview.XianLing,
				"fame", preview.Fame,
				"nimbus", preview.Nimbus,
				"exp_buff", preview.ExpBuff,
				"ingot", preview.Ingot,
				"juling", preview.JuLing,
			)
		}

		cCtx, cCancel := context.WithTimeout(ctx, 5*time.Second)
		resp, err := session.Call(cCtx, protocol.BuildGetFunctionEndGiftPacket(gift.GiftID), protocol.ActionIDGetGameFunctionEndGift)
		cCancel()
		if err != nil {
			return fmt.Errorf("claim gift %d: %w", gift.GiftID, err)
		}
		if resp == nil {
			return fmt.Errorf("claim gift %d: empty response", gift.GiftID)
		}

		claim, err := protocol.ParseGetGameFunctionEndGiftResult(resp.Payload)
		if err != nil {
			return fmt.Errorf("claim gift %d: %w", gift.GiftID, err)
		}

		switch claim.Result {
		case protocol.FunctionEndSuccess:
			slog.Info("功能终结礼包领取成功",
				"role_id", session.RoleID(),
				"gift_id", gift.GiftID,
				"attempt", attempt,
				"state_point", claim.StatePoint,
				"skill", claim.Skill,
				"xian_ling", claim.XianLing,
				"fame", claim.Fame,
				"nimbus", claim.Nimbus,
				"coin_buff", claim.CoinBuff,
				"exp_buff", claim.ExpBuff,
				"juling", claim.JuLing,
				"is_have_next", claim.IsHaveNext,
			)
			if !claim.IsHaveNext {
				return nil
			}
			// 本次奖励已领取，摇奖结果已消费，下一份需要重新摇奖。
			pending = false
			if attempt == maxClaims {
				break
			}
			if err := waitFunctionEndClaim(ctx); err != nil {
				return err
			}
		case protocol.FunctionEndItemPackFull:
			slog.Warn("功能终结礼包领取失败: 背包已满",
				"role_id", session.RoleID(),
				"gift_id", gift.GiftID,
				"result", claim.Result,
			)
			return scheduler.ErrBagFull
		case protocol.FunctionEndNoIngot:
			slog.Warn("功能终结礼包领取失败: 元宝不足",
				"role_id", session.RoleID(),
				"gift_id", gift.GiftID,
				"result", claim.Result,
			)
			return nil
		case protocol.FunctionEndNoVIPLevel:
			slog.Warn("功能终结礼包领取失败: VIP 等级不足",
				"role_id", session.RoleID(),
				"gift_id", gift.GiftID,
				"result", claim.Result,
			)
			return nil
		default:
			slog.Warn("功能终结礼包领取返回未知结果码",
				"role_id", session.RoleID(),
				"gift_id", gift.GiftID,
				"result", claim.Result,
				"result_text", protocol.FunctionEndClaimResultText(claim.Result),
			)
			return nil
		}
	}

	slog.Warn("功能终结礼包连续领取达到上限，停止本轮",
		"role_id", session.RoleID(),
		"gift_id", gift.GiftID,
		"max_claims", maxClaims,
	)
	return nil
}

// rollFunctionEndGift 调用 Mod_FunctionEnd_Base.random_award 为指定礼包摇出本份奖励。
func rollFunctionEndGift(ctx context.Context, session *client.RoleSession, giftID int16) (*protocol.FunctionEndRandomAward, error) {
	rCtx, rCancel := context.WithTimeout(ctx, 5*time.Second)
	resp, err := session.Call(rCtx, protocol.BuildFunctionEndRandomAwardPacket(giftID), protocol.ActionIDFunctionEndRandomAward)
	rCancel()
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("empty response")
	}
	return protocol.ParseFunctionEndRandomAwardResult(resp.Payload)
}

func waitFunctionEndClaim(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(functionEndClaimInterval):
		return nil
	}
}
