// Package social 提供帮派、结缘、家园等社交类自动化玩法。
//
// 本文件实现家园每日奖励领取 (Mod_MarryHome_Base, Module 210)，
// 对应每日活跃度任务 58「每日家园奖励」(每次 +5)。
//
// 该玩法完全免费：只调用 get_cake (210/19)，服务端在非活动时间返回
// NON_ACTIVITY_TIME(18)，不会产生任何元宝消耗。
package social

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// DailyTaskMarryHome 每日家园奖励任务编号。
const DailyTaskMarryHome int32 = 58

// NewMarryHomeRoutine 构造家园每日奖励领取任务。
func NewMarryHomeRoutine() scheduler.ActivityRoutine {
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"marry_home",
		"social",
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

			// 家园功能未开放时服务端会静默丢弃 Module 210 请求，先用 Action 6 缓存判定。
			open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDMarryHome)
			if fErr != nil {
				slog.Info("家园功能开放状态查询失败，跳过", "role_id", session.RoleID(), "err", fErr)
				return nil
			}
			if !open {
				slog.Info("家园功能未开放，跳过", "role_id", session.RoleID(), "function_id", protocol.FunctionIDMarryHome)
				return nil
			}

			// 1. 领取自家小蛋糕奖励 (免费)。
			//
			// 注意: 不要在此之前调用活跃度面板 0x0000002A。线上实测该面板会污染
			// 会话上下文，紧随其后的 Module 210 请求会被静默丢弃 (不回包直至超时)。
			// 直接领取即可，服务端会用结果码 (已领取 12 / 非活动时间 18 / 上限 20)
			// 表达当日状态，无需客户端预判。
			playerID := int32(session.PlayerID())
			pkt := protocol.BuildMarryHomeGetCakePacket(playerID, session.LastActionID())
			cCtx, cancel := context.WithTimeout(ctx, callTimeout)
			resp, err := session.Call(cCtx, pkt, protocol.ActionIDMarryHomeGetCake)
			cancel()
			if err != nil {
				// 会话上下文不匹配时服务端会静默丢弃 Module 210 请求（见上方注释），
				// 无回包属于正常拒绝路径，跳过本轮即可。
				slog.Info("家园奖励请求无回包，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			res, err := protocol.ParseMarryHomeCakeResult(resp.Payload)
			if err != nil {
				return fmt.Errorf("解析家园奖励回包失败: %w", err)
			}
			switch res.Result {
			case protocol.MarryHomeResultSuccess:
				slog.Info("家园奖励领取成功",
					"role_id", session.RoleID(),
					"player_id", playerID,
					"awards", formatCakeAwards(res.Awards))
			case protocol.MarryHomeResultNonActivityTime:
				slog.Info("家园奖励当前不在活动时间，本轮跳过", "role_id", session.RoleID())
			case protocol.MarryHomeResultAlreadyGet, protocol.MarryHomeResultTodayLimit:
				slog.Info("家园奖励今日已领取", "role_id", session.RoleID(), "result", res.Result)
			case protocol.MarryHomeResultNonMarryStatus, protocol.MarryHomeResultFunctionNoOpen:
				slog.Info("家园功能不可用（未结婚或未开放），跳过",
					"role_id", session.RoleID(), "result", res.Result)
			default:
				slog.Warn("家园奖励领取失败", "role_id", session.RoleID(), "result", res.Result)
			}
			return nil
		},
	)
}

// formatCakeAwards 将家园奖励格式化为日志文本。
func formatCakeAwards(awards []protocol.MarryHomeCakeAward) string {
	if len(awards) == 0 {
		return "无"
	}
	out := ""
	for i, award := range awards {
		if i > 0 {
			out += ","
		}
		out += fmt.Sprintf("%d×%d", award.ItemID, award.ItemNum)
	}
	return out
}
