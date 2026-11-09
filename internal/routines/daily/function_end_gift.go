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

// claimFunctionEndGift 领取单个终结礼包：每次先 random_award(127/3) 摇奖，再
// get_game_function_end_gift(127/2) 结算；is_have_next 为 true 且未达 bei 次数时继续。
//
// 线上实测：未先摇奖直接调用 127/2 会返回 result=SUCCESS + 全 0 奖励 +
// is_have_next=true，造成 20 次空转。摇奖本身免费，不消耗元宝。
func claimFunctionEndGift(ctx context.Context, session *client.RoleSession, gift protocol.FunctionEndGift, cfg OnlineGiftConfig) error {
	maxClaims := cfg.MaxClaimsPerGift
	if maxClaims <= 0 {
		maxClaims = 20
	}
	if gift.Bei > 0 && int(gift.Bei) < maxClaims {
		maxClaims = int(gift.Bei)
	}

	// 仅作诊断：列表中的 ingot 是随机奖励按钮价格，与免费领取按钮无关。
	if gift.IngotCost > 0 {
		slog.Debug("功能终结礼包随机奖励按钮价格 (免费领取不受影响)",
			"role_id", session.RoleID(),
			"gift_id", gift.GiftID,
			"random_ingot_price", gift.IngotCost,
		)
	}

	for attempt := 1; attempt <= maxClaims; attempt++ {
		// 1. 先摇奖 (127/3)，服务端才会为本次领取准备奖励结果。
		rCtx, rCancel := context.WithTimeout(ctx, 5*time.Second)
		rollResp, rollErr := session.Call(rCtx, protocol.BuildFunctionEndRandomAwardPacket(gift.GiftID), protocol.ActionIDFunctionEndRandomAward)
		rCancel()
		if rollErr != nil || rollResp == nil {
			return fmt.Errorf("random award gift %d: %w", gift.GiftID, rollErr)
		}
		preview, pErr := protocol.ParseFunctionEndRandomAwardResult(rollResp.Payload)
		if pErr != nil {
			return fmt.Errorf("random award gift %d: %w", gift.GiftID, pErr)
		}
		slog.Debug("功能终结礼包摇奖完成",
			"role_id", session.RoleID(),
			"gift_id", gift.GiftID,
			"attempt", attempt,
			"state_point", preview.StatePoint,
			"nimbus", preview.Nimbus,
			"juling", preview.JuLing,
		)

		// 2. 再领取 (127/2)。
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
			slog.Info("功能终结礼包免费领取成功",
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
			// 线上实测：已领完的礼包会持续返回 result=SUCCESS + 全零奖励 + is_have_next=1，
			// 按 is_have_next 无脑重试会连发满分请求且毫无收益。
			// 一旦本次领取未产生任何奖励，判定该礼包已无更多可领取内容，立即停止。
			if !functionEndClaimGranted(claim) {
				slog.Info("功能终结礼包本次未产生新奖励，停止该礼包",
					"role_id", session.RoleID(),
					"gift_id", gift.GiftID,
					"attempt", attempt,
				)
				return nil
			}
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
			// 免费领取不应出现元宝不足；若服务端仍返回该码，立刻停止，绝不重试付费路径。
			slog.Warn("功能终结礼包免费领取返回元宝不足，停止该礼包",
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

// functionEndClaimGranted 判断一次成功领取是否实际发放了奖励。
//
// 服务端对已无剩余内容的礼包仍可能返回 SUCCESS，但奖励字段全部为 0；
// 此时 is_have_next 不可作为继续领取的依据。
func functionEndClaimGranted(claim *protocol.FunctionEndClaimResult) bool {
	if claim == nil {
		return false
	}
	return claim.StatePoint != 0 ||
		claim.Skill != 0 ||
		claim.XianLing != 0 ||
		claim.Fame != 0 ||
		claim.Nimbus != 0 ||
		claim.CoinBuff != 0 ||
		claim.ExpBuff != 0 ||
		claim.JuLing != 0
}

func waitFunctionEndClaim(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(functionEndClaimInterval):
		return nil
	}
}
