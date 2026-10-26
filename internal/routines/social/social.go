// Package social 提供仙盟、圣盟、住宅与好友互动的帮派社交自动化玩法。
//
// @author Ateng
// @since 2026-10-08
package social

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// NewJieYuanBoxRoutine 构造结缘宝箱批量开启任务 (Feature 2: 结缘宝箱，除了传说，其他都开)。
func NewJieYuanBoxRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"jieyuan_box",
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
			slog.Info("正在执行结缘任务: 结缘宝箱自动开启 (严格排除传说宝箱)", "role_id", session.RoleID())

			// 1. 查询背包持有的结缘宝箱列表 (Mod 232, Act 30)
			queryPkt := protocol.BuildJieYuanBoxListPacket(protocol.ActionIDTownEnter)
			callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			resp, err := session.Call(callCtx, queryPkt, protocol.ActionIDJieYuanBoxList)
			cancel()

			if err != nil || resp == nil {
				slog.Warn("查询结缘宝箱列表超时或失败", "err", err)
				return nil
			}

			boxes, err := protocol.ParseJieYuanBoxListResult(resp.Payload)
			if err != nil {
				slog.Warn("解析结缘宝箱列表失败", "err", err)
				return nil
			}

			// 2. 严格遵循规则: 除了传说 (3942)，其他都开
			openable := protocol.FilterOpenableJieYuanBoxes(boxes)
			if len(openable) == 0 {
				slog.Info("无待开启的非传说结缘宝箱", "total_kinds", len(boxes))
				return nil
			}

			var totalOpened int
			prevAct := protocol.ActionIDJieYuanBoxList

			for _, box := range openable {
				remaining := box.Count
				maxBatchesPerItem := 50 // 单个道具最大批次上限，防止无限循环
				batchCount := 0

				for remaining > 0 && batchCount < maxBatchesPerItem {
					batchCount++
					openCount := int32(4)
					if remaining < openCount {
						openCount = remaining
					}

					// 2.1 开箱前置握手 (Mod 528, Act 1)
					prepPkt := protocol.BuildJieYuanBoxOpenContextPacket(prevAct)
					pCtx, pCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
					_, _ = session.Call(pCtx, prepPkt, protocol.ActionIDJieYuanBoxOpenContext)
					pCancel()

					// 2.2 发送批量开启指令 (Mod 232, Act 34: 抓包基准每批次开4个)
					openPkt := protocol.BuildJieYuanBoxOpenPacket(box.ItemID, openCount, protocol.ActionIDJieYuanBoxOpenContext)
					oCtx, oCancel := context.WithTimeout(ctx, 2*time.Second)
					openResp, oErr := session.Call(oCtx, openPkt, protocol.ActionIDJieYuanBoxOpen)
					oCancel()

					prevAct = protocol.ActionIDJieYuanBoxOpen
					if oErr == nil && openResp != nil {
						totalOpened += int(openCount)
						remaining -= openCount
					} else {
						break
					}

					time.Sleep(50 * time.Millisecond)
				}

				slog.Info("结缘宝箱批次处理完成",
					"item_id", box.ItemID,
					"opened", box.Count-remaining,
					"legendary_skipped", true,
				)
			}

			slog.Info("结缘宝箱批量开启流程完成", "role_id", session.RoleID(), "total_opened", totalOpened)
			return nil
		},
	)
}

// NewGuildActivitiesRoutine 构造仙盟神兽与捐献任务。
func NewGuildActivitiesRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"guild_activities",
		"social",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行社交任务: 仙盟捐献与神兽挑战")
			return nil
		},
	)
}

// NewSacredAllianceRoutine 构造圣盟祭祀与幻魔塔任务。
func NewSacredAllianceRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"sacred_alliance",
		"social",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行社交任务: 圣盟祭祀与幻魔塔挑战")
			return nil
		},
	)
}

// NewHomesteadRoutine 构造住宅家具祝福与夫妻宝箱任务。
func NewHomesteadRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"homestead",
		"social",
		scheduler.ScheduleCron,
		scheduler.PriorityLow,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行社交任务: 住宅家具祝福与夫妻宝箱领取")
			return nil
		},
	)
}

// NewFriendshipRoutine 构造好友结义与鲜花赠送任务。
func NewFriendshipRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"friendship",
		"social",
		scheduler.ScheduleCron,
		scheduler.PriorityLow,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行社交任务: 好友结义与日常送花互动")
			return nil
		},
	)
}

// NewFameBlessingRoutine 构造仙界膜拜大神与声望祝福任务。
func NewFameBlessingRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"fame_blessing",
		"social",
		scheduler.ScheduleCron,
		scheduler.PriorityNormal,
		0,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			slog.Info("正在执行社交任务: 膜拜大神尊像与声望领用")
			return nil
		},
	)
}

