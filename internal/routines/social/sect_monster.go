// Package social 提供帮派、结缘、家园、宗门灵兽等社交类自动化玩法。
//
// 本文件实现宗门灵兽免费喂养 (Mod_SectMonster_Base, Module 576)，
// 对应每日活跃度任务 77「每日宗门灵兽喂养」(每次 +20)。
//
// 线上实测要点：
//   - 576 模块必须走**仙盟城镇会话**，主服会话/普通仙盟会话不回包；
//   - 请求**不能带 PrevAct**（4 字节 [sect_id] / 8 字节 [monster_type, feed_type]）；
//   - feed_type=0 为免费喂养，type=1 为高级档（自动化永不使用）；
//   - 免费喂养无元宝/铜钱/体力消耗，成功时返回道具奖励。
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

// DailyTaskSectMonster 每日宗门灵兽喂养任务编号。
const DailyTaskSectMonster int32 = 77

// NewSectMonsterRoutine 构造宗门灵兽免费喂养任务。
func NewSectMonsterRoutine() scheduler.ActivityRoutine {
	const townCtxTimeout = 30 * time.Second
	const callTimeout = 8 * time.Second
	return scheduler.NewBaseRoutine(
		"sect_monster",
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

			// 1. 活跃度任务 77 已完成则跳过，节省仙盟会话开销。
			actCtx, actCancel := context.WithTimeout(ctx, callTimeout)
			actResp, err := session.Call(actCtx, protocol.BuildGameAssistantInfoPacket(), protocol.ActionIDGameAssistantInfo)
			actCancel()
			if err != nil {
				slog.Info("宗门灵兽: 查询每日活跃度失败，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			panel, err := protocol.ParseGameAssistantInfo(actResp.Payload)
			if err != nil {
				return fmt.Errorf("解析每日活跃度面板失败: %w", err)
			}
			if panel.RemainingTimesFor(DailyTaskSectMonster) <= 0 {
				slog.Info("宗门灵兽活跃度已完成，跳过", "role_id", session.RoleID())
				return nil
			}

			// 2. 仙盟功能开放检查。
			open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDFaction)
			if fErr != nil {
				slog.Info("仙盟功能开放状态查询失败，稍后重试宗门灵兽", "role_id", session.RoleID(), "err", fErr)
				return scheduler.NewCooldown(30*time.Minute, "宗门灵兽功能开放状态无回包")
			}
			if !open {
				slog.Info("仙盟功能未开放，跳过宗门灵兽", "role_id", session.RoleID())
				return nil
			}

			// 3. 建立仙盟城镇会话 (576 只在该会话上回包)。
			townCtx, townCancel := context.WithTimeout(ctx, townCtxTimeout)
			defer townCancel()
			town, err := session.OpenSectTownSession(townCtx)
			if err != nil {
				slog.Info("仙盟城镇会话不可用，跳过宗门灵兽", "role_id", session.RoleID(), "err", err)
				return nil
			}
			defer town.Close()

			cred, err := session.FetchSectTownCredentials(townCtx)
			if err != nil {
				slog.Warn("读取仙盟凭据失败，稍后重试宗门灵兽", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(30*time.Minute, "宗门灵兽仙盟凭据无回包")
			}
			sectID := cred.LocalSectID
			if sectID <= 0 {
				sectID = cred.SectID
			}

			// 4. 查询可喂养灵兽 (请求不带 PrevAct)。
			lCtx, lCancel := context.WithTimeout(townCtx, callTimeout)
			listResp, err := town.Call(lCtx, protocol.BuildSectMonsterListPacket(sectID), protocol.ActionIDSectMonsterList)
			lCancel()
			if err != nil {
				slog.Info("宗门灵兽列表无回包，稍后重试", "role_id", session.RoleID(), "sect_id", sectID, "err", err)
				return scheduler.NewCooldown(30*time.Minute, "宗门灵兽列表无回包")
			}
			list, err := protocol.ParseSectMonsterListResult(listResp.Payload)
			if err != nil {
				return fmt.Errorf("解析宗门灵兽列表失败: %w", err)
			}
			slog.Info("宗门灵兽列表",
				"role_id", session.RoleID(),
				"sect_id", sectID,
				"monster_count", len(list.Monsters),
				"free_feed_num", list.FreeFeedNum,
			)
			if len(list.Monsters) == 0 {
				slog.Info("宗门暂无灵兽可喂养，跳过", "role_id", session.RoleID())
				return nil
			}

			// 5. 免费喂养一只 (feed_type=0)。
			monsterType := list.Monsters[0].Type
			fCtx, fCancel := context.WithTimeout(townCtx, 10*time.Second)
			feedResp, err := town.Call(fCtx, protocol.BuildSectMonsterFeedPacket(monsterType, 0), protocol.ActionIDSectMonsterFeed)
			fCancel()
			if err != nil {
				slog.Info("宗门灵兽喂养无回包，跳过", "role_id", session.RoleID(), "monster_type", monsterType, "err", err)
				return nil
			}
			res, err := protocol.ParseSectMonsterFeedResult(feedResp.Payload)
			if err != nil {
				return fmt.Errorf("解析宗门灵兽喂养回包失败: %w", err)
			}
			switch res.Result {
			case protocol.SectMonsterResultSuccess:
				slog.Info("宗门灵兽喂养成功",
					"role_id", session.RoleID(),
					"monster_type", monsterType,
					"awards", formatSectMonsterAwards(res.Awards),
				)
			case protocol.SectMonsterResultFeedNumLimit:
				slog.Info("宗门灵兽免费喂养次数不足，跳过", "role_id", session.RoleID())
			case protocol.SectMonsterResultNotHaveMonsterType:
				slog.Info("宗门灵兽类型无效，跳过", "role_id", session.RoleID(), "monster_type", monsterType)
			default:
				slog.Warn("宗门灵兽喂养失败", "role_id", session.RoleID(),
					"monster_type", monsterType, "result", res.Result)
			}
			return nil
		},
	)
}

// formatSectMonsterAwards 将喂养奖励格式化为日志文本。
func formatSectMonsterAwards(awards []protocol.SectMonsterFeedAward) string {
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
