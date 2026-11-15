// 本文件实现无量劫海（归墟）自动挑战 (Mod_EraInfiniteMission_Base, Module 853)。
//
// 抓包基准: /tmp/wuliangjiehai.pcapng（2026-11-15，qq s0）：
//
//	853/0 赛季 → 853/1 面板 → 853/2 [chapter] 章节面板（含 now_level）
//	→ 853/3 [now_level, 1] 怪物队伍 → 853/4 [now_level] 挑战
//
// 无量劫海分为归墟 1/2/3 与极境，每境 10 关；客户端按 now_level 逐关挑战。
// 失败/冷却/次数不足即停止或进入冷却唤醒；不消耗元宝。
package social

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// maxWuliangFights 单轮最多挑战关卡数。
const maxWuliangFights = 20

// NewWuliangJieHaiRoutine 构造无量劫海挑战任务。
func NewWuliangJieHaiRoutine() scheduler.ActivityRoutine {
	const callTimeout = 10 * time.Second
	const fightTimeout = 35 * time.Second
	return scheduler.NewBaseRoutine(
		"wuliang_jiehai",
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
			call := func(pkt *protocol.Packet, act uint32, timeout time.Duration) *protocol.Packet {
				cCtx, cCancel := context.WithTimeout(ctx, timeout)
				defer cCancel()
				resp, err := session.Call(cCtx, pkt, act)
				if err != nil {
					slog.Info("无量劫海协议无回包", "role_id", session.RoleID(),
						"action", act, "err", err)
					return nil
				}
				return resp
			}

			_ = call(protocol.BuildEraInfiniteSeasonPacket(), protocol.ActionIDEraInfiniteSeason, callTimeout)
			_ = call(protocol.BuildEraInfinitePanelPacket(), protocol.ActionIDEraInfinitePanel, callTimeout)

			fights := 0
			// 归墟 1/2/3 境，每境按 now_level 逐关挑战；当前境无可打关卡时进入下一境。
			for chapterID := int32(1); chapterID <= 3 && fights < maxWuliangFights; chapterID++ {
				lastLevel := int32(-1)
				for fights < maxWuliangFights && ctx.Err() == nil {
					chapterResp := call(protocol.BuildEraInfiniteChapterPacket(chapterID),
						protocol.ActionIDEraInfiniteChapter, callTimeout)
					if chapterResp == nil {
						return scheduler.NewCooldown(30*time.Minute, "无量劫海章节面板无回包")
					}
					chapter, cErr := protocol.ParseEraInfiniteChapter(chapterResp.Payload)
					if cErr != nil {
						slog.Warn("解析无量劫海章节面板失败，本轮跳过", "role_id", session.RoleID(), "err", cErr)
						return nil
					}
					if chapter.Result != 0 || chapter.NowLevel <= 0 {
						slog.Info("无量劫海该境暂无可挑战关卡", "role_id", session.RoleID(),
							"chapter_id", chapterID, "result", chapter.Result,
							"now_level", chapter.NowLevel)
						break
					}
					level := chapter.NowLevel
					slog.Info("无量劫海章节状态", "role_id", session.RoleID(),
						"chapter_id", chapterID, "now_level", level)
					if level == lastLevel {
						slog.Info("无量劫海关卡未推进，进入下一境", "role_id", session.RoleID(), "now_level", level)
						break
					}
					lastLevel = level

					// 怪物队伍信息用于确认关卡状态；被静默丢弃时不阻断挑战。
					_ = call(protocol.BuildEraInfiniteMonsterTeamPacket(level, 1),
						protocol.ActionIDEraInfiniteMonsterTeam, callTimeout)

					fightResp := call(protocol.BuildEraInfiniteFightPacket(level),
						protocol.ActionIDEraInfiniteFight, fightTimeout)
					if fightResp == nil {
						return scheduler.NewCooldown(10*time.Minute, "无量劫海挑战无回包")
					}
					res, fErr := protocol.ParseEraInfiniteFightResult(fightResp.Payload)
					if fErr != nil {
						return scheduler.NewCooldown(10*time.Minute, "无量劫海挑战回包解析失败")
					}
					if res.Result != 0 {
						slog.Info("无量劫海挑战结束", "role_id", session.RoleID(),
							"chapter_id", chapterID, "now_level", level, "fights", fights,
							"result", res.Result, "result_text", protocol.EraInfiniteResultText(res.Result))
						return nil
					}
					if !res.IsWin {
						slog.Info("无量劫海挑战失败，稍后重试", "role_id", session.RoleID(),
							"chapter_id", chapterID, "now_level", level, "fights", fights)
						return scheduler.NewCooldown(10*time.Minute, "无量劫海挑战失败")
					}
					fights++
					slog.Info("无量劫海挑战成功", "role_id", session.RoleID(),
						"chapter_id", chapterID, "now_level", level, "fights", fights)
				}
			}
			slog.Info("无量劫海本轮完成", "role_id", session.RoleID(), "fights", fights)
			return nil
		},
	)
}
