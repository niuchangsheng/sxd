// Package daily 提供签到、邮件、在线奖励、绑定奖励与日常维护类玩法。
//
// 本文件实现「免费猎命」与「购买命宫碎片」两项每日活跃度来源：
//   - 任务 4 免费猎命：在猎命师列表中逐个尝试指定，成功后 +5 活跃度；
//   - 任务 56 购买命宫碎片：用铜钱兑换 1 次命宫碎片，成功后 +5 活跃度。
//
// 两个动作都不消耗元宝；猎命指定只消耗免费次数，命宫碎片兑换只消耗铜钱。
package daily

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// 每日活跃度任务编号 (对齐官方 AssistantTypeData.objFunc["1"])。
const (
	// DailyTaskFreeFate 免费猎命
	DailyTaskFreeFate int32 = 4
	// DailyTaskBuyMingGongScrap 每日购买命宫碎片
	DailyTaskBuyMingGongScrap int32 = 56
)

// NewFateHuntRoutine 构造免费猎命与命宫碎片兑换任务。
//
// 先读取每日活跃度面板确认对应任务尚未完成，再执行对应动作，
// 避免在任务已满时白白消耗免费次数或铜钱。
func NewFateHuntRoutine() scheduler.ActivityRoutine {
	const callTimeout = 6 * time.Second
	return scheduler.NewBaseRoutine(
		"fate_hunt",
		"daily",
		scheduler.ScheduleCron,
		// 活跃度补分任务本身调用很少，提升到高优先级可确保每轮调度尽早执行，
		// 避免在长任务队列中被挤到末尾。
		scheduler.PriorityHigh,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}

			actCtx, actCancel := context.WithTimeout(ctx, callTimeout)
			actResp, err := session.Call(actCtx, protocol.BuildGameAssistantInfoPacket(), protocol.ActionIDGameAssistantInfo)
			actCancel()
			if err != nil || actResp == nil {
				slog.Info("查询每日活跃度失败，跳过猎命任务", "role_id", session.RoleID(), "err", err)
				return nil
			}
			panel, err := protocol.ParseGameAssistantInfo(actResp.Payload)
			if err != nil {
				return err
			}

			if panel.RemainingTimesFor(DailyTaskFreeFate) > 0 {
				runFreeFateAppoint(ctx, session, callTimeout)
			} else {
				slog.Info("免费猎命活跃度已完成，跳过", "role_id", session.RoleID())
			}

			if panel.RemainingTimesFor(DailyTaskBuyMingGongScrap) > 0 {
				runMingGongScrapExchange(ctx, session, callTimeout)
			} else {
				slog.Info("命宫碎片购买活跃度已完成，跳过", "role_id", session.RoleID())
			}
			return nil
		},
	)
}

// runFreeFateAppoint 查询猎命师列表并逐个尝试免费指定。
//
// 线上只有当前已解锁的猎命师可被指定，其余编号返回 INVALID_FATE_NPC_ID(12)；
// 因此按列表顺序尝试，直到出现 SUCCESS 或遇到次数/资源类失败为止。
func runFreeFateAppoint(ctx context.Context, session *client.RoleSession, callTimeout time.Duration) {
	cCtx, cancel := context.WithTimeout(ctx, callTimeout)
	resp, err := session.Call(cCtx, protocol.BuildFateGetNPCPacket(), protocol.ActionIDFateGetNPC)
	cancel()
	if err != nil || resp == nil {
		slog.Info("查询猎命师列表失败，跳过免费猎命", "role_id", session.RoleID(), "err", err)
		return
	}
	panel, err := protocol.ParseFateHunterPanel(resp.Payload)
	if err != nil {
		slog.Warn("解析猎命师列表失败，跳过免费猎命", "role_id", session.RoleID(), "err", err)
		return
	}
	if len(panel.Hunters) == 0 {
		slog.Info("猎命师列表为空，跳过免费猎命", "role_id", session.RoleID())
		return
	}

	// 已激活的猎命师优先，其次按列表顺序。
	ordered := make([]uint8, 0, len(panel.Hunters))
	for _, hunter := range panel.Hunters {
		if hunter.Status == 1 {
			ordered = append(ordered, hunter.NpcID)
		}
	}
	for _, hunter := range panel.Hunters {
		if hunter.Status != 1 {
			ordered = append(ordered, hunter.NpcID)
		}
	}

	slog.Info("正在执行日常任务: 免费猎命",
		"role_id", session.RoleID(),
		"free_appoint_times", panel.FreeAppointTimes,
		"hunters", len(panel.Hunters))

	for _, npcID := range ordered {
		if ctx.Err() != nil {
			return
		}
		aCtx, cancel := context.WithTimeout(ctx, callTimeout)
		apptResp, aErr := session.Call(aCtx, protocol.BuildFateAppointNPCPacket(npcID), protocol.ActionIDFateAppointNPC)
		cancel()
		if aErr != nil || apptResp == nil {
			slog.Info("指定猎命师无回包", "role_id", session.RoleID(), "npc_id", npcID, "err", aErr)
			return
		}
		result, pErr := protocol.ParseFateAppointResult(apptResp.Payload)
		if pErr != nil {
			slog.Warn("解析猎命结果失败", "role_id", session.RoleID(), "npc_id", npcID, "err", pErr)
			return
		}
		switch result.Result {
		case protocol.FateHuntResultSuccess:
			slog.Info("免费猎命成功",
				"role_id", session.RoleID(),
				"npc_id", result.NpcID,
				"fate_id", result.FateID)
			return
		case protocol.FateHuntResultInvalidFateNPCID:
			slog.Info("猎命师未解锁，尝试下一位", "role_id", session.RoleID(), "npc_id", npcID)
			continue
		case protocol.FateHuntResultNotEnoughCoin:
			slog.Info("铜钱不足，停止免费猎命", "role_id", session.RoleID(), "npc_id", npcID)
			return
		default:
			slog.Info("免费猎命未成功，停止",
				"role_id", session.RoleID(), "npc_id", npcID, "result", result.Result)
			return
		}
	}
	slog.Info("猎命师列表中无可指定目标", "role_id", session.RoleID())
}

// runMingGongScrapExchange 用铜钱兑换 1 次命宫碎片。
func runMingGongScrapExchange(ctx context.Context, session *client.RoleSession, callTimeout time.Duration) {
	iCtx, cancel := context.WithTimeout(ctx, callTimeout)
	infoResp, err := session.Call(iCtx, protocol.BuildFateMingGongInfoPacket(), protocol.ActionIDFateMingGongInfo)
	cancel()
	if err != nil || infoResp == nil {
		slog.Warn("查询命宫信息失败，跳过命宫碎片兑换", "role_id", session.RoleID(), "err", err)
		return
	}
	info, err := protocol.ParseFateMingGongInfo(infoResp.Payload)
	if err != nil {
		slog.Warn("解析命宫信息失败，跳过命宫碎片兑换", "role_id", session.RoleID(), "err", err)
		return
	}
	if info.ExchangeTimes <= 0 {
		slog.Info("命宫碎片兑换次数已用尽", "role_id", session.RoleID(), "scrap", info.Scrap)
		return
	}

	slog.Info("正在执行日常任务: 购买命宫碎片",
		"role_id", session.RoleID(),
		"ming_gong", info.MingGong,
		"scrap", info.Scrap,
		"exchange_times", info.ExchangeTimes)

	eCtx, cancel := context.WithTimeout(ctx, callTimeout)
	exResp, err := session.Call(eCtx, protocol.BuildFateExchangeMingGongScrapPacket(1), protocol.ActionIDFateExchangeMingGongScrap)
	cancel()
	if err != nil || exResp == nil {
		slog.Info("购买命宫碎片无回包", "role_id", session.RoleID(), "err", err)
		return
	}
	result, err := protocol.ParseFateMingGongExchangeResult(exResp.Payload)
	if err != nil {
		slog.Warn("解析命宫碎片购买回包失败", "role_id", session.RoleID(), "err", err)
		return
	}
	switch result.Result {
	case protocol.FateHuntResultSuccess:
		slog.Info("购买命宫碎片成功", "role_id", session.RoleID(), "scrap", result.Scrap)
	case protocol.FateHuntResultNotEnoughCoin:
		slog.Info("铜钱不足，未能购买命宫碎片", "role_id", session.RoleID(), "scrap", result.Scrap)
	case protocol.FateHuntResultLimitNum:
		slog.Info("命宫碎片兑换次数已达上限", "role_id", session.RoleID(), "scrap", result.Scrap)
	default:
		slog.Info("购买命宫碎片未成功", "role_id", session.RoleID(), "result", result.Result)
	}
}
