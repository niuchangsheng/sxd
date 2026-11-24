// Package pvp 提供竞技场、天梯、劫镖、世界 Boss 等对抗玩法自动化。
//
// 本文件实现界渊之战 (Mod_AbyssClash_Base, Module 818) 自动化。
package pvp

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/dungeon"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

// maxAbyssClashFights 单轮最多挑战次数 (用户确认每日共 5 次，面板为权威值)。
const maxAbyssClashFights = 5

// errAbyssClashNoResponse 标记界渊之战请求被服务端静默丢弃。
var errAbyssClashNoResponse = errors.New("界渊之战请求无回包")

// openAbyssClashSession 建立界渊之战使用的跨服会话 (抓包为 SOCKETSERVERTOWN)。
// 抽成变量以便单测注入。
var openAbyssClashSession = func(ctx context.Context, s *client.RoleSession) (*client.RoleSession, error) {
	return s.OpenCrossServerSession(ctx)
}

// NewAbyssClashRoutine 构造界渊之战挑战任务。
//
// 官方流程 (抓包 /tmp/jieyuanzhizhan.pcapng)：
//
//	818/0 is_func_open (主服) → 818/1|2 面板 (跨服) →
//	818/3 get_monster_team_info(step_monster_id) → 818/4 start_fight(step_monster_id)
//
// 第一阶段面板有 3 个怪物/Boss，每日共 5 次挑战；每次挑战后刷新面板读取
// 剩余次数。按 step_monster_id 轮询目标，挑战成功/失败都消耗 1 次，
// 次数不足/上限时结束。不消耗元宝、不购买次数。
func NewAbyssClashRoutine() scheduler.ActivityRoutine {
	const (
		infoTimeout  = 12 * time.Second
		fightTimeout = 45 * time.Second
	)
	return scheduler.NewBaseRoutine(
		"abyss_clash",
		"pvp",
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
			slog.Info("正在执行对抗任务: 界渊之战", "role_id", session.RoleID())

			// 主服查询功能开放状态与阶段。
			openResp, err := kit.CallResync(ctx, session,
				protocol.BuildAbyssClashFuncOpenPacket(), protocol.ActionIDAbyssClashFuncOpen, 8*time.Second)
			if err != nil || openResp == nil {
				if abort, ok := kit.IsAbortErr(err); ok {
					return abort
				}
				slog.Info("界渊之战开放状态无回包，按连续丢包退避",
					"role_id", session.RoleID(), "err", err)
				return scheduler.HandleSilentDropCtx(ctx, session, "abyss_clash", 3,
					30*time.Minute, "界渊之战开放状态无回包")
			}
			open, err := protocol.ParseAbyssClashFuncOpen(openResp.Payload)
			if err != nil {
				slog.Warn("解析界渊之战开放状态失败", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(10*time.Minute, "界渊之战开放状态解析失败")
			}
			if open.OpenFlag != 1 {
				slog.Info("界渊之战当前未开放", "role_id", session.RoleID(),
					"open_flag", open.OpenFlag, "step", open.Step)
				return scheduler.NewCooldown(time.Hour, "界渊之战未开放")
			}
			if open.Step != 1 && open.Step != 2 {
				slog.Info("界渊之战阶段不可挑战", "role_id", session.RoleID(), "step", open.Step)
				return scheduler.NewCooldown(time.Hour, "界渊之战阶段不可挑战")
			}

			// 面板/挑战走跨服会话；主服只用于开放状态与凭据。
			if err := session.EnsureFreshConnection(ctx, client.HealthySessionAge); err != nil {
				return err
			}
			cross, err := openAbyssClashSession(ctx, session)
			if err != nil || cross == nil {
				slog.Info("建立界渊之战跨服会话失败，稍后重试",
					"role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(10*time.Minute, "建立界渊之战跨服会话失败")
			}
			if cross != session {
				defer cross.Close()
			}

			handleNoResponse := func(where string, err error) error {
				if abort, ok := kit.IsAbortErr(err); ok {
					return abort
				}
				slog.Info("界渊之战协议无回包，按连续丢包退避",
					"role_id", session.RoleID(), "where", where, "err", err)
				return scheduler.HandleSilentDropCtx(ctx, session, "abyss_clash", 3,
					30*time.Minute, "界渊之战"+where+"无回包")
			}

			fetchPanel := func() (*protocol.AbyssClashPanel, error) {
				var (
					pkt  *protocol.Packet
					act  uint32
					step = open.Step
				)
				if step == 2 {
					pkt, act = protocol.BuildAbyssClashStep2PanelPacket(), protocol.ActionIDAbyssClashStep2Panel
				} else {
					pkt, act = protocol.BuildAbyssClashStep1PanelPacket(), protocol.ActionIDAbyssClashStep1Panel
				}
				resp, err := kit.CallResync(ctx, cross, pkt, act, infoTimeout)
				if err != nil {
					return nil, err
				}
				if resp == nil {
					return nil, errAbyssClashNoResponse
				}
				if step == 2 {
					return protocol.ParseAbyssClashStep2Panel(resp.Payload)
				}
				return protocol.ParseAbyssClashStep1Panel(resp.Payload)
			}

			panel, err := fetchPanel()
			if err != nil {
				if errors.Is(err, errAbyssClashNoResponse) || errors.Is(err, context.DeadlineExceeded) {
					return handleNoResponse("面板", err)
				}
				if abort, ok := kit.IsAbortErr(err); ok {
					return abort
				}
				slog.Warn("界渊之战面板解析失败", "role_id", session.RoleID(), "err", err)
				return scheduler.NewCooldown(10*time.Minute, "界渊之战面板解析失败")
			}
			scheduler.ResetSilentDropsCtx(ctx, session, "abyss_clash")
			if panel.Result != protocol.AbyssClashResultSuccess {
				return handleAbyssClashPanelResult(session, panel.Result)
			}
			logAbyssClashPanel(session, panel)
			if panel.FightNum <= 0 {
				slog.Info("界渊之战今日挑战次数已用尽", "role_id", session.RoleID())
				return nil
			}

			// 单次挑战消耗体力 (抓包 cost_power=425/40)。体力不足时服务端会
			// 静默丢弃 818/3、818/4；每次挑战前按 [包子 > 粽子 > 茶叶蛋] 补充。
			const maxAbyssStaminaItems = 12
			replenished := 0

			attempts := 0
			excluded := make(map[int32]bool)
			for attempts < maxAbyssClashFights && panel.FightNum > 0 && ctx.Err() == nil {
				target, ok := pickAbyssClashTarget(panel, attempts, excluded)
				if !ok {
					if panel.Step == 2 {
						// 第二阶段 Boss 未解锁时普通怪物不可挑战 (服务端静默丢弃)，
						// 30 分钟后复查 boss_can_fight_num。
						slog.Info("界渊之战 Boss 暂不可挑战，稍后再试",
							"role_id", session.RoleID(), "fight_num", panel.FightNum)
						return scheduler.NewCooldown(30*time.Minute, "界渊之战Boss暂不可挑战")
					}
					slog.Info("界渊之战暂无可挑战目标", "role_id", session.RoleID())
					return nil
				}

				for int32(session.GetPlayerState().Stamina) < panel.CostPower &&
					replenished < maxAbyssStaminaItems && ctx.Err() == nil {
					rOk, item, added, rErr := dungeon.TryReplenishStamina(ctx, session)
					if rErr != nil {
						slog.Warn("界渊之战补充体力失败", "role_id", session.RoleID(), "err", rErr)
						break
					}
					if !rOk {
						break
					}
					replenished++
					slog.Info("界渊之战补充体力", "role_id", session.RoleID(),
						"item", item, "added", added,
						"stamina", session.GetPlayerState().Stamina,
						"cost_power", panel.CostPower)
					if !kit.Sleep(ctx, 300*time.Millisecond) {
						return ctx.Err()
					}
				}
				if int32(session.GetPlayerState().Stamina) < panel.CostPower {
					slog.Info("界渊之战体力不足，稍后重试", "role_id", session.RoleID(),
						"stamina", session.GetPlayerState().Stamina, "cost_power", panel.CostPower)
					return scheduler.NewCooldown(30*time.Minute, "界渊之战体力不足")
				}

				// 官方客户端挑战前先打开怪物队伍信息面板；该请求仅用于展示，
				// 2026-11-25 实机在 step2 普通怪物上会被静默丢弃，不能因此
				// 放弃真正的挑战请求。
				infoResp, iErr := kit.CallResync(ctx, cross,
					protocol.BuildAbyssClashMonsterTeamPacket(target.StepMonsterID),
					protocol.ActionIDAbyssClashMonsterTeam, infoTimeout)
				if abort, ok := kit.IsAbortErr(iErr); ok {
					return abort
				}
				if iErr != nil || infoResp == nil {
					slog.Info("界渊之战怪物信息无回包，直接尝试挑战",
						"role_id", session.RoleID(), "step_monster_id", target.StepMonsterID, "err", iErr)
				} else if infoCode, pErr := protocol.ParseAbyssClashMonsterTeamResult(infoResp.Payload); pErr != nil {
					slog.Warn("界渊之战怪物信息解析失败，继续尝试挑战",
						"role_id", session.RoleID(), "step_monster_id", target.StepMonsterID, "err", pErr)
				} else if infoCode != protocol.AbyssClashResultSuccess {
					slog.Info("界渊之战怪物信息不可用，跳过该目标",
						"role_id", session.RoleID(), "step_monster_id", target.StepMonsterID,
						"result", infoCode, "result_text", protocol.AbyssClashResultText(infoCode))
					excluded[target.StepMonsterID] = true
					continue
				}

				fightResp, fErr := kit.CallResync(ctx, cross,
					protocol.BuildAbyssClashStartFightPacket(target.StepMonsterID),
					protocol.ActionIDAbyssClashStartFight, fightTimeout)
				if fErr != nil || fightResp == nil {
					return handleNoResponse("挑战", fErr)
				}
				scheduler.ResetSilentDropsCtx(ctx, session, "abyss_clash")
				result, pErr := protocol.ParseAbyssClashFightResult(fightResp.Payload)
				if pErr != nil {
					slog.Warn("界渊之战挑战回包解析失败", "role_id", session.RoleID(), "err", pErr)
					return scheduler.NewCooldown(10*time.Minute, "界渊之战挑战回包解析失败")
				}
				switch result.Result {
				case protocol.AbyssClashResultSuccess:
					attempts++
					slog.Info("界渊之战挑战完成", "role_id", session.RoleID(),
						"step_monster_id", target.StepMonsterID,
						"monster_team_id", target.MonsterTeamID,
						"win", result.WinFlag > 0, "total_damage", result.TotalDamage,
						"attempts", attempts)
				case protocol.AbyssClashResultNoFightNum, protocol.AbyssClashResultNumLimit:
					slog.Info("界渊之战挑战次数已用尽", "role_id", session.RoleID(),
						"result", result.Result,
						"result_text", protocol.AbyssClashResultText(result.Result),
						"attempts", attempts)
					return nil
				case protocol.AbyssClashResultFuncNotOpen:
					slog.Info("界渊之战已关闭", "role_id", session.RoleID())
					return scheduler.NewCooldown(time.Hour, "界渊之战已关闭")
				case protocol.AbyssClashResultWrongStep:
					slog.Info("界渊之战阶段已变化", "role_id", session.RoleID())
					return scheduler.NewCooldown(10*time.Minute, "界渊之战阶段已变化")
				case protocol.AbyssClashResultWrongID, protocol.AbyssClashResultBossCanNotFight:
					slog.Info("界渊之战目标不可挑战，跳过",
						"role_id", session.RoleID(), "step_monster_id", target.StepMonsterID,
						"result", result.Result,
						"result_text", protocol.AbyssClashResultText(result.Result))
					excluded[target.StepMonsterID] = true
					continue
				case protocol.AbyssClashResultFailed:
					// 战斗失败同样消耗一次挑战次数，刷新面板后继续。
					attempts++
					slog.Info("界渊之战挑战失败", "role_id", session.RoleID(),
						"step_monster_id", target.StepMonsterID, "attempts", attempts)
				case protocol.AbyssClashResultPackageFull:
					return scheduler.ErrBagFull
				default:
					slog.Info("界渊之战挑战未成功", "role_id", session.RoleID(),
						"result", result.Result,
						"result_text", protocol.AbyssClashResultText(result.Result))
					return nil
				}

				refreshed, rErr := fetchPanel()
				if rErr != nil {
					if errors.Is(rErr, errAbyssClashNoResponse) || errors.Is(rErr, context.DeadlineExceeded) {
						return handleNoResponse("刷新面板", rErr)
					}
					slog.Warn("刷新界渊之战面板失败", "role_id", session.RoleID(), "err", rErr)
					return nil
				}
				panel = refreshed
				if panel.Result != protocol.AbyssClashResultSuccess {
					return handleAbyssClashPanelResult(session, panel.Result)
				}
				slog.Info("界渊之战剩余次数", "role_id", session.RoleID(),
					"fight_num", panel.FightNum, "attempts", attempts)
				if !kit.Sleep(ctx, 500*time.Millisecond) {
					return ctx.Err()
				}
			}
			slog.Info("界渊之战本轮完成", "role_id", session.RoleID(),
				"attempts", attempts, "fight_num", panel.FightNum)
			return nil
		},
	)
}

// handleAbyssClashPanelResult 将面板结果码转换为调度错误。
func handleAbyssClashPanelResult(session *client.RoleSession, code uint8) error {
	slog.Info("界渊之战面板不可用", "role_id", session.RoleID(),
		"result", code, "result_text", protocol.AbyssClashResultText(code))
	switch code {
	case protocol.AbyssClashResultFuncNotOpen:
		return scheduler.NewCooldown(time.Hour, "界渊之战未开放")
	case protocol.AbyssClashResultWrongStep:
		return scheduler.NewCooldown(10*time.Minute, "界渊之战阶段不可挑战")
	case protocol.AbyssClashResultNoFightNum, protocol.AbyssClashResultNumLimit:
		return nil
	case protocol.AbyssClashResultPackageFull:
		return scheduler.ErrBagFull
	default:
		return scheduler.NewCooldown(10*time.Minute, "界渊之战面板失败")
	}
}

// logAbyssClashPanel 输出面板摘要与怪物列表。
func logAbyssClashPanel(session *client.RoleSession, panel *protocol.AbyssClashPanel) {
	slog.Info("界渊之战面板", "role_id", session.RoleID(),
		"step", panel.Step, "score", panel.Score, "fight_num", panel.FightNum,
		"cost_power", panel.CostPower, "monsters", len(panel.Monsters),
		"zone_id", panel.ZoneID, "total_score", panel.TotalScore)
	for _, m := range panel.Monsters {
		slog.Info("界渊之战目标", "role_id", session.RoleID(),
			"step_monster_id", m.StepMonsterID, "monster_team_id", m.MonsterTeamID,
			"is_boss", m.IsBoss, "boss_can_fight_num", m.BossCanFightNum,
			"win_num", m.WinNum, "last_health", m.LastHealth, "max_health", m.MaxHealth)
	}
}

// pickAbyssClashTarget 按 step_monster_id 升序轮询目标。
//
// 第一阶段三个怪物都有效。第二阶段：Boss 只有在 boss_can_fight_num>0 时
// 可挑战；普通怪物使用玩家剩余 fight_num 挑战 (官方 MonsterInfoView 中
// 非 Boss 的 fight_num 取 info.fight_num)，胜利可累积 Boss 挑战次数。
// excluded 记录服务端返回 WRONG_ID/BOSS_CAN_NOT_FIGHT 的目标。
func pickAbyssClashTarget(panel *protocol.AbyssClashPanel, attempts int, excluded map[int32]bool) (protocol.AbyssClashMonster, bool) {
	candidates := make([]protocol.AbyssClashMonster, 0, len(panel.Monsters))
	for _, m := range panel.Monsters {
		if m.StepMonsterID == 0 || excluded[m.StepMonsterID] {
			continue
		}
		if panel.Step == 2 && (m.IsBoss != 1 || m.BossCanFightNum <= 0) {
			// 第二阶段只有 Boss 可挑战；普通怪物请求会被服务端静默丢弃。
			continue
		}
		candidates = append(candidates, m)
	}
	if len(candidates) == 0 {
		return protocol.AbyssClashMonster{}, false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].StepMonsterID < candidates[j].StepMonsterID
	})
	return candidates[attempts%len(candidates)], true
}
