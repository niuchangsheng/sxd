// Package minigame 提供吉星高照、仙旅奇缘答题、喜从天降、刮刮卡、钓鱼等益智趣味活动。
//
// 本文件实现「寻找异兽」(Mod_SpaceFind_Base, Module 123) 自动化。
package minigame

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// maxSpaceFindRounds 单轮调度的最大免费寻找次数，防止服务端异常时死循环。
const maxSpaceFindRounds = 40

// NewFindBeastRoutine 构造每日寻找异兽任务。
//
// 官方流程 (com.views.xian_yuan.SpaceFindView)：
//  1. open_space_find 读取剩余免费寻找次数；
//  2. do_space_find(use_type=0) 执行一次普通寻找；
//  3. get_space_find 领取本次寻找到的道具。
//
// 自动化只使用免费次数；绝不使用 use_type=1 的元宝寻找，也不调用
// call_golden_chaos_monster (150 元宝召唤金色异兽)。
func NewFindBeastRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"find_beast",
		"minigame",
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
			slog.Info("正在执行益智任务: 寻找异兽", "role_id", session.RoleID())

			call := func(pkt *protocol.Packet, act uint32, timeout time.Duration) (*protocol.Packet, error) {
				cCtx, cancel := context.WithTimeout(ctx, timeout)
				defer cancel()
				return session.Call(cCtx, pkt, act)
			}

			resp, err := call(protocol.BuildSpaceFindOpenPacket(), protocol.ActionIDSpaceFindOpen, 5*time.Second)
			if err != nil || resp == nil {
				slog.Info("寻找异兽功能不可用或未开放，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			info, err := protocol.ParseSpaceFindOpenResult(resp.Payload)
			if err != nil {
				slog.Info("解析寻找异兽面板失败，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			slog.Info("寻找异兽面板",
				"role_id", session.RoleID(),
				"free_count", info.Count,
				"pending_item_id", info.ItemID,
				"golden", info.Golden,
				"integral", info.Integral,
				"remain_catch", info.RemainCatchCount,
			)

			// 若上一次已寻找到道具但尚未领取，先领取。
			if info.ItemID != 0 {
				if getResp, gErr := call(protocol.BuildSpaceFindGetPacket(), protocol.ActionIDSpaceFindGet, 5*time.Second); gErr == nil && getResp != nil {
					if got, pErr := protocol.ParseSpaceFindGetResult(getResp.Payload); pErr == nil {
						slog.Info("寻找异兽补领上一次结果", "role_id", session.RoleID(), "result", got.Result, "goods", got.Goods)
					}
				}
			}

			if info.Count <= 0 {
				slog.Info("寻找异兽今日免费次数已用尽", "role_id", session.RoleID())
				return nil
			}

			rounds := 0
			for rounds < int(info.Count) && rounds < maxSpaceFindRounds {
				rounds++
				if err := ctx.Err(); err != nil {
					return nil
				}

				doResp, dErr := call(protocol.BuildSpaceFindDoPacket(protocol.SpaceFindUseNormal), protocol.ActionIDSpaceFindDo, 5*time.Second)
				if dErr != nil || doResp == nil {
					slog.Info("寻找异兽请求无回包", "role_id", session.RoleID(), "err", dErr)
					return nil
				}
				doRes, pErr := protocol.ParseSpaceFindDoResult(doResp.Payload)
				if pErr != nil {
					slog.Warn("解析寻找异兽回包失败", "role_id", session.RoleID(), "err", pErr)
					return nil
				}
				switch doRes.Result {
				case protocol.SpaceFindResultSuccess:
					slog.Info("寻找异兽成功",
						"role_id", session.RoleID(),
						"item_id", doRes.ItemID,
						"golden", doRes.Golden,
						"integral", doRes.Integral,
					)
				case protocol.SpaceFindResultPackFull:
					return scheduler.ErrBagFull
				case protocol.SpaceFindResultNoCount, protocol.SpaceFindResultNoEnoughTime:
					slog.Info("寻找异兽次数已用尽", "role_id", session.RoleID(), "result", doRes.Result)
					return nil
				case protocol.SpaceFindResultHaveMonster:
					slog.Info("寻找异兽：已有异兽等待处理，跳过本轮", "role_id", session.RoleID())
					return nil
				case protocol.SpaceFindResultAlreadyFind:
					slog.Info("寻找异兽：已有未领取结果，先领取", "role_id", session.RoleID())
				default:
					slog.Info("寻找异兽未成功", "role_id", session.RoleID(), "result", doRes.Result)
					return nil
				}

				// 领取本次结果；成功后再继续下一次寻找。
				getResp, gErr := call(protocol.BuildSpaceFindGetPacket(), protocol.ActionIDSpaceFindGet, 5*time.Second)
				if gErr != nil || getResp == nil {
					slog.Info("领取寻找异兽结果无回包", "role_id", session.RoleID(), "err", gErr)
					return nil
				}
				got, pErr := protocol.ParseSpaceFindGetResult(getResp.Payload)
				if pErr != nil {
					slog.Warn("解析寻找异兽领取回包失败", "role_id", session.RoleID(), "err", pErr)
					return nil
				}
				switch got.Result {
				case protocol.SpaceFindResultSuccess:
					slog.Info("寻找异兽奖励领取成功", "role_id", session.RoleID(), "goods", got.Goods)
					info.Count = got.Count
				case protocol.SpaceFindResultPackFull:
					return scheduler.ErrBagFull
				case protocol.SpaceFindResultNotHaveFind:
					slog.Info("寻找异兽暂无可领取结果", "role_id", session.RoleID())
				default:
					slog.Info("寻找异兽奖励领取未成功", "role_id", session.RoleID(), "result", got.Result)
					return nil
				}
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(200 * time.Millisecond):
				}
			}
			slog.Info("寻找异兽巡检完成", "role_id", session.RoleID(), "rounds", rounds)
			return nil
		},
	)
}
