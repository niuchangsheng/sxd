// Package minigame 提供吉星高照、仙旅奇缘答题、刮刮卡、法力星图等益智趣味活动。
//
// @author Ateng
// @since 2026-11-07
package minigame

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// NewStarPictureRoutine 构造法力星图免费占星任务 (Mod_StarPicture_Base, Module 485)。
//
// 每日活跃度任务 81「法力星图占星」每次 +10。
//
// 线上实测 (2026-11-07)：`has_divination == 0` 时的首次占星免费；
// 同一日继续使用 `free_divination_count` 抽第 2、3 次时服务端仍会扣 50 元宝
// （面板 free 计数与实际扣费不同步）。因此本例程**每天最多占星 1 次**，
// 且仅在 `has_divination == 0 && free_divination_count > 0` 时执行，
// 任何情况下都不会进入元宝档。
func NewStarPictureRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"star_picture",
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

			infoPkt, err := protocol.BuildStarPictureInfoPacket(session.LastActionID())
			if err != nil {
				return fmt.Errorf("构造法力星图面板封包失败: %w", err)
			}
			infoCtx, infoCancel := context.WithTimeout(ctx, 8*time.Second)
			infoResp, err := session.Call(infoCtx, infoPkt, protocol.ActionIDStarPictureInfo)
			infoCancel()
			if err != nil {
				return fmt.Errorf("查询法力星图面板失败: %w", err)
			}
			info, err := protocol.ParseStarPictureInfoResult(infoResp.Payload)
			if err != nil {
				return fmt.Errorf("解析法力星图面板失败: %w", err)
			}
			slog.Info("法力星图面板",
				"role_id", session.RoleID(),
				"has_divination", info.HasDivination,
				"free_divination", info.FreeDivination,
				"ingot_divination", info.IngotDivination,
				"chip_count", info.ChipCount,
			)
			// 安全闸门: 只有当天尚未占星且存在免费次数时才占星一次。
			// has_divination > 0 时继续占星会被服务端按 50 元宝扣费 (实测)。
			if info.HasDivination > 0 {
				slog.Info("法力星图今日已占星，跳过以避免元宝扣费",
					"role_id", session.RoleID(), "has_divination", info.HasDivination)
				return nil
			}
			if info.FreeDivination <= 0 {
				slog.Info("法力星图无免费占星次数，本轮跳过", "role_id", session.RoleID())
				return nil
			}

			pkt, err := protocol.BuildStarPictureDivinationPacket(session.LastActionID())
			if err != nil {
				return fmt.Errorf("构造法力星图占星封包失败: %w", err)
			}
			c, cancel := context.WithTimeout(ctx, 8*time.Second)
			resp, err := session.Call(c, pkt, protocol.ActionIDStarPictureDivination)
			cancel()
			if err != nil {
				return fmt.Errorf("法力星图占星请求失败: %w", err)
			}
			code, err := protocol.ParseStarPictureDivinationResult(resp.Payload)
			if err != nil {
				return fmt.Errorf("解析法力星图占星结果失败: %w", err)
			}
			if code != protocol.StarPictureResultSuccess {
				slog.Warn("法力星图占星被服务端拒绝",
					"role_id", session.RoleID(), "result", code)
				return nil
			}
			slog.Info("法力星图免费占星完成", "role_id", session.RoleID(), "draw_count", 1)
			return nil
		},
	)
}
