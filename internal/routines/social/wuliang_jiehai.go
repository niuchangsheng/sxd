// 本文件实现无量劫海（归墟）自动挑战 (Mod_EraInfiniteMission_Base, Module 853)。
//
// 抓包基准: /tmp/wuliangjiehai.pcapng（2026-11-15，qq s0）：
//
//	855/1(无关) → 853/0 赛季 → 853/1 面板
//	→ 853/2 [chapter_id]      （选择章节，官方客户端不等回包）
//	→ 854/39 布阵类型序列      （返回 [[type, deploy_id]]）
//	→ 854/1 [deploy_id]        （选择布阵，无回包等待）
//	→ 853/3 [now_level, 1]     （查看怪物队伍，无回包等待）
//	→ 853/4 [now_level]        （真正挑战，唯一需要等待的回包）
//
// 官方抓包中 853/2、854/39 后的 854/1、853/3 都不会回包；早期实现把 853/2
// 当作带状态的章节面板并等待回包，导致 8 个章节全部"无回包"空转、从未发起
// 853/4。这里改为按抓包顺序 fire-and-forget 前置包，只等待 853/4。
//
// 章节与关卡对应（静态表：1-10 / 11-30 / 31-60 / 61-300）：
// 第 2 章起服务端会自动结算多支队伍，整体 is_win 为 0 即本次战斗失败，
// 需要重试；连续失败 maxWuliangConsecutiveLosses 次后进入冷却。
// 不消耗元宝。
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

const (
	// maxWuliangFights 单轮最多挑战关卡数。
	maxWuliangFights = 20
	// maxWuliangConsecutiveLosses 同一关连续失败多少次后退出本轮。
	maxWuliangConsecutiveLosses = 3
	// wuliangMaxLevel 无量劫海最高关卡（第 4 章 61-300）。
	wuliangMaxLevel int32 = 300
)

// wuliangChapterForLevel 返回关卡所属章节 id（1..4）。
func wuliangChapterForLevel(level int32) int32 {
	switch {
	case level <= 10:
		return 1
	case level <= 30:
		return 2
	case level <= 60:
		return 3
	default:
		return 4
	}
}

// NewWuliangJieHaiRoutine 构造无量劫海挑战任务。
func NewWuliangJieHaiRoutine() scheduler.ActivityRoutine {
	const callTimeout = 10 * time.Second
	// 多队伍战斗（第 2 章起 2 队、后续更多）需要服务端逐队结算，回包可能超过
	// 20 秒；实测 20 秒会在战斗仍在计算时误判为静默丢弃。给足 45 秒。
	const fightTimeout = 45 * time.Second
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
				resp, err := session.CallWithPrevActResync(ctx, timeout, pkt, act)
				if err != nil {
					slog.Info("无量劫海协议无回包", "role_id", session.RoleID(),
						"action", act, "err", err,
						"recent_rx", fmt.Sprintf("%X", session.RecentRxActions()))
					return nil
				}
				return resp
			}
			// send 发送无需等待回包的前置动作（853/2、854/1、853/3）。
			send := func(pkt *protocol.Packet, name string) {
				if err := session.Send(pkt); err != nil {
					slog.Info("无量劫海前置动作发送失败", "role_id", session.RoleID(),
						"name", name, "err", err)
				}
			}

			_ = call(protocol.BuildEraInfiniteSeasonPacket(),
				protocol.ActionIDEraInfiniteSeason, callTimeout)
			panelResp := call(protocol.BuildEraInfinitePanelPacket(),
				protocol.ActionIDEraInfinitePanel, callTimeout)
			if panelResp == nil {
				return scheduler.HandleSilentDrop(session, "wuliang_jiehai", 3,
					30*time.Minute, "无量劫海面板无回包")
			}
			panel, pErr := protocol.ParseEraInfinitePanel(panelResp.Payload)
			if pErr != nil {
				return scheduler.NewCooldown(10*time.Minute, "无量劫海面板回包解析失败")
			}
			slog.Info("无量劫海面板", "role_id", session.RoleID(),
				"era_level", panel.EraLevel, "mission_level", panel.MissionLevel,
				"rank", panel.Rank, "rank_perc", panel.RankPerc,
				"fight_end_ts", panel.FightEndTS, "era_zone", panel.EraZone)
			// 面板成功不代表挑战可用，不能在这里 ResetSilentDrops。

			target := panel.MissionLevel + 1
			if target <= 0 {
				target = 1
			}
			if target > wuliangMaxLevel {
				slog.Info("无量劫海已达最高关卡", "role_id", session.RoleID(),
					"mission_level", panel.MissionLevel)
				return nil
			}

			// 布阵信息每次执行只需准备一次：854/39 取 deploy_id，854/1 选中。
			deployReady := false
			var deployID int32
			lastChapter := int32(0)
			fights, losses := 0, 0
			for fights < maxWuliangFights && ctx.Err() == nil {
				if target > wuliangMaxLevel {
					slog.Info("无量劫海已达最高关卡", "role_id", session.RoleID(),
						"fights", fights)
					return nil
				}
				chapter := wuliangChapterForLevel(target)
				if chapter != lastChapter {
					lastChapter = chapter
					// 官方客户端会先取章节面板，并用其中的 now_level 作为挑战关卡。
					// 面板若被静默丢弃，退回 mission_level+1 推断。
					chapterResp := call(protocol.BuildEraInfiniteChapterPacket(chapter),
						protocol.ActionIDEraInfiniteChapter, callTimeout)
					if chapterResp == nil {
						slog.Info("无量劫海章节面板无回包，使用 mission_level+1 推断关卡",
							"role_id", session.RoleID(), "chapter_id", chapter, "now_level", target)
					} else if ch, cErr := protocol.ParseEraInfiniteChapter(chapterResp.Payload); cErr != nil {
						slog.Info("解析无量劫海章节面板失败，使用 mission_level+1 推断关卡",
							"role_id", session.RoleID(), "chapter_id", chapter, "err", cErr)
					} else if ch.Result == protocol.EraInfiniteEnded {
						slog.Info("无量劫海当前赛季已结束", "role_id", session.RoleID(),
							"chapter_id", chapter, "fights", fights)
						return nil
					} else if ch.Result == protocol.EraInfiniteIsLock {
						slog.Info("无量劫海章节未解锁，稍后重试", "role_id", session.RoleID(),
							"chapter_id", chapter)
						return scheduler.NewCooldown(30*time.Minute, "无量劫海章节未解锁")
					} else if ch.Result == 0 && ch.NowLevel > 0 {
						if ch.NowLevel != target {
							slog.Info("无量劫海章节关卡以面板为准", "role_id", session.RoleID(),
								"chapter_id", chapter, "panel_now_level", ch.NowLevel,
								"assumed", target)
						}
						target = ch.NowLevel
					}
				}
				if !deployReady {
					deployReady = true
					if resp := call(protocol.BuildEraInfiniteDeployTypeSeqPacket(),
						protocol.ActionIDEraInfiniteDeployTypeSeq, callTimeout); resp != nil {
						ids, dErr := protocol.ParseEraInfiniteDeployTypeSeq(resp.Payload)
						if dErr != nil {
							slog.Info("解析无量劫海布阵序列失败，使用服务端默认布阵",
								"role_id", session.RoleID(), "err", dErr)
						} else if len(ids) > 0 {
							deployID = ids[0]
						}
						slog.Info("无量劫海布阵序列", "role_id", session.RoleID(),
							"deploy_ids", ids, "selected", deployID)
					}
					if deployID != 0 {
						send(protocol.BuildEraInfiniteSuperDeployListPacket(deployID), "854/1 deploy")
					}
				}
				send(protocol.BuildEraInfiniteMonsterTeamPacket(target, 1), "853/3 team")

				slog.Info("无量劫海发起挑战", "role_id", session.RoleID(),
					"chapter_id", chapter, "now_level", target,
					"deploy_id", deployID, "losses", losses)
				fightResp := call(protocol.BuildEraInfiniteFightPacket(target),
					protocol.ActionIDEraInfiniteFight, fightTimeout)
				if fightResp == nil {
					// 前置动作正常但挑战被静默丢弃：按连续丢包退避，
					// 计数只在挑战真正回包时清零。
					return scheduler.HandleSilentDrop(session, "wuliang_jiehai", 3,
						10*time.Minute, "无量劫海挑战无回包")
				}
				scheduler.ResetSilentDrops(session, "wuliang_jiehai")
				res, fErr := protocol.ParseEraInfiniteFightResult(fightResp.Payload)
				if fErr != nil {
					return scheduler.NewCooldown(10*time.Minute, "无量劫海挑战回包解析失败")
				}
				if res.Result != 0 {
					switch res.Result {
					case protocol.EraInfiniteEnded:
						slog.Info("无量劫海当前赛季已结束", "role_id", session.RoleID(),
							"mission_level", target-1, "fights", fights)
						return nil
					case protocol.EraInfiniteFuncNotOpen:
						slog.Info("无量劫海功能未开放，稍后重试", "role_id", session.RoleID())
						return scheduler.NewCooldown(2*time.Hour, "无量劫海功能未开放")
					default:
						slog.Info("无量劫海挑战未成功，本轮退出", "role_id", session.RoleID(),
							"chapter_id", chapter, "now_level", target, "fights", fights,
							"result", res.Result,
							"result_text", protocol.EraInfiniteResultText(res.Result))
						return scheduler.NewCooldown(10*time.Minute, "无量劫海挑战结果异常")
					}
				}
				if !res.IsWin {
					losses++
					slog.Info("无量劫海挑战失败（多队伍需全部成功）", "role_id", session.RoleID(),
						"chapter_id", chapter, "now_level", target,
						"losses", losses, "fights", fights)
					if losses >= maxWuliangConsecutiveLosses {
						return scheduler.NewCooldown(30*time.Minute,
							fmt.Sprintf("无量劫海连续挑战失败 %d 次", losses))
					}
					continue
				}
				losses = 0
				fights++
				target++
				slog.Info("无量劫海挑战成功", "role_id", session.RoleID(),
					"chapter_id", chapter, "now_level", target-1, "fights", fights)
			}
			slog.Info("无量劫海本轮完成", "role_id", session.RoleID(), "fights", fights,
				"next_level", target)
			return nil
		},
	)
}
