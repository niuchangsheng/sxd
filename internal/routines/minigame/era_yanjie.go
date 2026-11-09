// Package minigame 提供纪元湮劫等纪元系列玩法自动化。
//
// 本文件实现纪元湮劫每日战斗 (Mod_EraInfiniteMission_Base, Module 853)，
// 对应每日活跃度任务 71「每日纪元湮劫中完成战斗」(每次 +20)。
//
// 关卡战斗完全免费：线上实测胜利后元宝/铜钱/体力均无变化。
// 战斗按 mission_level+1 推进一关，每日只打一关即可满足活跃度。
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

// DailyTaskEraYanjie 每日纪元湮劫完成战斗任务编号。
const DailyTaskEraYanjie int32 = 71

// eraYanjieDeployType 默认阵容类型 (官方客户端 _curDeployType 初值)。
const eraYanjieDeployType int32 = 1

// NewEraYanjieRoutine 构造纪元湮劫每日战斗任务。
func NewEraYanjieRoutine() scheduler.ActivityRoutine {
	const callTimeout = 10 * time.Second
	return scheduler.NewBaseRoutine(
		"era_yanjie",
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

			call := func(pkt *protocol.Packet, act uint32, timeout time.Duration) *protocol.Packet {
				cCtx, cancel := context.WithTimeout(ctx, timeout)
				defer cancel()
				resp, err := session.Call(cCtx, pkt, act)
				if err != nil {
					return nil
				}
				return resp
			}

			// 1. 活跃度任务 71 已完成则跳过，避免重复推进关卡。
			actResp := call(protocol.BuildGameAssistantInfoPacket(), protocol.ActionIDGameAssistantInfo, callTimeout)
			if actResp == nil {
				slog.Info("纪元湮劫: 查询每日活跃度失败，跳过", "role_id", session.RoleID())
				return nil
			}
			panel, err := protocol.ParseGameAssistantInfo(actResp.Payload)
			if err != nil {
				return fmt.Errorf("解析每日活跃度面板失败: %w", err)
			}
			if panel.RemainingTimesFor(DailyTaskEraYanjie) <= 0 {
				slog.Info("纪元湮劫活跃度已完成，跳过", "role_id", session.RoleID())
				return nil
			}

			// 2. 主面板: 读取当前已通关关卡进度。
			mainResp := call(protocol.BuildEraYanjiePanelPacket(session.LastActionID()),
				protocol.ActionIDEraYanjiePanel, callTimeout)
			if mainResp == nil {
				// 主面板被静默丢弃通常意味着活动未开启/不在赛季，稍后重试。
				slog.Info("纪元湮劫: 主面板无回包，稍后重试", "role_id", session.RoleID())
				return scheduler.NewCooldown(30*time.Minute, "纪元湮劫主面板无回包")
			}
			main, err := protocol.ParseEraYanjiePanelResult(mainResp.Payload)
			if err != nil {
				return fmt.Errorf("解析纪元湮劫主面板失败: %w", err)
			}
			slog.Info("纪元湮劫主面板",
				"role_id", session.RoleID(),
				"era_level", main.EraLevel,
				"mission_level", main.MissionLevel,
				"era_zone", main.EraZone,
			)

			// 3. 依次尝试下一关/下下关/当前关。
			//
			// 注意: 不能先调用 get_monster_team_info (853/3) 预检。线上实测
			// 活跃度面板 0x0000002A 会让 853/2(章节)、853/3(敌方队伍) 被静默丢弃，
			// 但 853/1(主面板) 与 853/4(战斗) 不受影响。因此直接发起战斗，
			// 由结果码 WRONG_ID/LOCKED 判断关卡是否有效（无效不产生任何消耗）。
			candidates := []int32{main.MissionLevel + 1, main.MissionLevel + 2, main.MissionLevel}
			for _, level := range candidates {
				if level <= 0 {
					continue
				}
				fightResp := call(protocol.BuildEraYanjieFightPacket(level, eraYanjieDeployType, 1, session.LastActionID()),
					protocol.ActionIDEraYanjieFight, 20*time.Second)
				if fightResp == nil {
					slog.Info("纪元湮劫战斗无回包，稍后重试", "role_id", session.RoleID(), "level", level)
					return scheduler.NewCooldown(10*time.Minute, "纪元湮劫战斗无回包")
				}
				res, fErr := protocol.ParseEraYanjieFightResult(fightResp.Payload)
				if fErr != nil {
					return fmt.Errorf("解析纪元湮劫战斗回包失败: %w", fErr)
				}
				switch res.Result {
				case protocol.EraYanjieResultSuccess:
					slog.Info("纪元湮劫战斗完成",
						"role_id", session.RoleID(),
						"level", level,
						"is_win", res.IsWin,
						"battle_count", len(res.Battles),
					)
					return nil
				case protocol.EraYanjieResultAlreadyPass:
					slog.Info("纪元湮劫该关卡已通关", "role_id", session.RoleID(), "level", level)
					return nil
				case protocol.EraYanjieResultWrongID, protocol.EraYanjieResultLocked:
					slog.Debug("纪元湮劫关卡未解锁/编号无效，尝试下一候选", "level", level, "result", res.Result)
					continue
				default:
					slog.Warn("纪元湮劫战斗被服务端拒绝", "role_id", session.RoleID(),
						"level", level, "result", res.Result)
					return nil
				}
			}
			slog.Info("纪元湮劫未找到可战关卡，跳过", "role_id", session.RoleID())
			return nil
		},
	)
}
