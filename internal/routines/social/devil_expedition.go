// 本文件实现魔界远征「单人远征」自动化 (Mod_DevilExpedition_Base, Module 429)。
//
// 抓包基准: /tmp/mojieyuanzheng.pcapng（2026-11-14，qq s0）。
//
// 链路:
//
//	activity_status(429/1) → open_match_panel(429/2)
//	→ 对 双生/魔神/隐仙 三类伙伴分别:
//	   start_expedition_single(429/63) → enter_town(429/13)
//	   → get_mission_data_m5/6/7(429/57|68|74) → move_to(429/17)
//	   → fight_monster_m5/6/7(429/58|69|75) → quit(429/29)
//
// 规则:
//   - 本周材料达到上限时不再触发对应伙伴挑战:
//     阴阳石 week_stone/week_stone_max 决定双生，隐仙石 week_stone2/week_stone2_max
//     决定隐仙，魔灵 soul.week_soul/week_max_soul 全部达到上限时跳过魔神；
//   - start_expedition_single 返回 NOT_ENOUGH_ITEM(10) 表示屠魔令不足，跳过该类型；
//   - 不购买宝箱（拒绝主动弹出的购买提示），不消耗元宝。
package social

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// devilMissionTownFallback 当服务端未推送 notify_enter_mission 时的已知城镇映射。
var devilMissionTownFallback = map[uint8]int32{
	protocol.DevilMissionYinYang: 144,
}

// devilFightPos 各类型怪物队伍的点击坐标（客户端 MonsterPos + 20px，按队伍 id-1 索引）。
var devilFightPos = map[uint8][][2]uint16{
	protocol.DevilMissionYinYang:    {{436, 413}, {802, 413}},
	protocol.DevilMissionMonsterGod: {{380, 261}, {710, 271}, {1040, 248}},
	protocol.DevilMissionHermit:     {{446, 408}, {816, 410}},
}

// devilTeamPos 返回指定队伍的点击坐标；越界时回退到首个位置。
func devilTeamPos(missionType uint8, teamID int32) [2]uint16 {
	positions := devilFightPos[missionType]
	if len(positions) == 0 {
		return [2]uint16{}
	}
	idx := int(teamID) - 1
	if idx < 0 || idx >= len(positions) {
		idx = 0
	}
	return positions[idx]
}

// devilEntryMissionTown 保存服务端下发的关卡/城镇。
type devilEntryMissionTown struct {
	missionID int32
	townID    int32
}

// devilEntryState 按会话缓存关卡通知处理器，避免每次 Routine 执行重复注册。
type devilEntryState struct {
	mu    sync.Mutex
	entry devilEntryMissionTown
}

var devilEntryStates sync.Map // *client.RoleSession -> *devilEntryState

// devilStateFor 返回会话对应的关卡状态；首次创建时注册 429/12 推送处理器。
func devilStateFor(session *client.RoleSession) *devilEntryState {
	if v, ok := devilEntryStates.Load(session); ok {
		return v.(*devilEntryState)
	}
	st := &devilEntryState{}
	actual, loaded := devilEntryStates.LoadOrStore(session, st)
	if loaded {
		return actual.(*devilEntryState)
	}
	session.RegisterHandler(protocol.ActionIDDevilNotifyEnterMission, func(pkt *protocol.Packet) {
		if len(pkt.Payload) < 12 {
			return
		}
		entry := devilEntryMissionTown{
			missionID: int32(uint32(pkt.Payload[0])<<24 | uint32(pkt.Payload[1])<<16 |
				uint32(pkt.Payload[2])<<8 | uint32(pkt.Payload[3])),
			townID: int32(uint32(pkt.Payload[4])<<24 | uint32(pkt.Payload[5])<<16 |
				uint32(pkt.Payload[6])<<8 | uint32(pkt.Payload[7])),
		}
		st.mu.Lock()
		st.entry = entry
		st.mu.Unlock()
		slog.Info("魔界远征进入关卡通知", "role_id", session.RoleID(),
			"mission_id", entry.missionID, "town_id", entry.townID)
	})
	return st
}

// NewDevilExpeditionRoutine 构造魔界远征单人对战任务。
func NewDevilExpeditionRoutine() scheduler.ActivityRoutine {
	const callTimeout = 10 * time.Second
	const fightTimeout = 25 * time.Second
	const maxFights = 20

	return scheduler.NewBaseRoutine(
		"devil_expedition",
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

			if open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDDevilExpedition); fErr != nil {
				slog.Info("魔界远征功能开放状态查询失败，本轮跳过",
					"role_id", session.RoleID(), "err", fErr)
				return nil
			} else if !open {
				slog.Info("魔界远征功能未开放，本轮跳过",
					"role_id", session.RoleID(), "function_id", protocol.FunctionIDDevilExpedition)
				return nil
			}

			call := func(pkt *protocol.Packet, act uint32, timeout time.Duration) *protocol.Packet {
				cCtx, cCancel := context.WithTimeout(ctx, timeout)
				defer cCancel()
				resp, err := session.Call(cCtx, pkt, act)
				if err != nil {
					slog.Info("魔界远征协议无回包", "role_id", session.RoleID(),
						"action", act, "err", err)
					return nil
				}
				return resp
			}

			statusResp := call(protocol.BuildDevilActivityStatusPacket(),
				protocol.ActionIDDevilActivityStatus, callTimeout)
			if statusResp == nil {
				return scheduler.NewCooldown(30*time.Minute, "魔界远征状态无回包")
			}
			status, err := protocol.ParseDevilActivityStatus(statusResp.Payload)
			if err != nil {
				return scheduler.NewCooldown(30*time.Minute, "魔界远征状态解析失败")
			}
			if status != protocol.DevilActivityOpen {
				wake := nextDevilOpenAt(time.Now())
				slog.Info("魔界远征未开放，等待下次开放", "role_id", session.RoleID(),
					"status", status, "wake_at", wake)
				return scheduler.NewCooldown(time.Until(wake), "魔界远征未开放")
			}

			panelResp := call(protocol.BuildDevilMatchPanelPacket(),
				protocol.ActionIDDevilMatchPanel, callTimeout)
			if panelResp == nil {
				return scheduler.NewCooldown(30*time.Minute, "魔界远征面板无回包")
			}
			panel, err := protocol.ParseDevilMatchPanel(panelResp.Payload)
			if err != nil {
				slog.Warn("解析魔界远征面板失败，本轮跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			slog.Info("魔界远征面板", "role_id", session.RoleID(),
				"tumoling", panel.TumoLingNum,
				"week_stone", panel.WeekStone, "week_stone_max", panel.WeekStoneMax,
				"week_stone2", panel.WeekStone2, "week_stone2_max", panel.WeekStone2Max,
				"souls", len(panel.Souls),
			)

			// 服务端在 start_expedition_single 后推送 notify_enter_mission。
			entryState := devilStateFor(session)

			types := []uint8{
				protocol.DevilMissionYinYang,
				protocol.DevilMissionMonsterGod,
				protocol.DevilMissionHermit,
			}
			for _, missionType := range types {
				if ctx.Err() != nil {
					return nil
				}
				name := protocol.DevilMissionName(missionType)
				if capped, detail := devilMaterialCapped(panel, missionType); capped {
					slog.Info("魔界远征本周材料已达上限，跳过该伙伴挑战",
						"role_id", session.RoleID(), "mission", name, "detail", detail)
					continue
				}

				entryState.mu.Lock()
				entryState.entry = devilEntryMissionTown{}
				entryState.mu.Unlock()

				startResp := call(protocol.BuildDevilStartSinglePacket(missionType),
					protocol.ActionIDDevilStartExpeditionSingle, callTimeout)
				if startResp == nil {
					slog.Info("魔界远征开始无回包，结束本轮", "role_id", session.RoleID(), "mission", name)
					return scheduler.NewCooldown(10*time.Minute, "魔界远征开始无回包")
				}
				code, pErr := protocol.ParseDevilResult(startResp.Payload)
				if pErr != nil {
					return scheduler.NewCooldown(10*time.Minute, "魔界远征开始回包解析失败")
				}
				switch code {
				case protocol.DevilResultSuccess:
					// 继续
				case protocol.DevilResultNotEnoughItem:
					slog.Info("魔界远征屠魔令不足，跳过该伙伴挑战",
						"role_id", session.RoleID(), "mission", name)
					continue
				case protocol.DevilActivityClose:
					slog.Info("魔界远征活动已关闭，结束本轮", "role_id", session.RoleID())
					return nil
				default:
					slog.Info("魔界远征开始未成功", "role_id", session.RoleID(),
						"mission", name, "result", code, "result_text", protocol.DevilResultText(code))
					continue
				}

				// 等待 notify_enter_mission 下发城镇。
				waitDeadline := time.Now().Add(2 * time.Second)
				for time.Now().Before(waitDeadline) {
					entryState.mu.Lock()
					town := entryState.entry.townID
					entryState.mu.Unlock()
					if town != 0 {
						break
					}
					select {
					case <-ctx.Done():
						return nil
					case <-time.After(100 * time.Millisecond):
					}
				}
				entryState.mu.Lock()
				town := entryState.entry.townID
				entryState.mu.Unlock()
				if town == 0 {
					town = devilMissionTownFallback[missionType]
				}
				if town == 0 {
					slog.Info("魔界远征未获得关卡城镇，跳过该伙伴挑战",
						"role_id", session.RoleID(), "mission", name)
					_ = call(protocol.BuildDevilQuitPacket(), protocol.ActionIDDevilQuit, callTimeout)
					continue
				}

				// 与抓包一致的进入序列。
				_ = call(protocol.BuildDevilCloseMatchPanelPacket(), protocol.ActionIDDevilCloseMatchPanel, callTimeout)
				_ = call(protocol.BuildDevilGetSkipWarInfoPacket(), protocol.ActionIDDevilGetSkipWarInfo, callTimeout)
				enterResp := call(protocol.BuildDevilEnterTownPacket(town), protocol.ActionIDDevilEnterTown, callTimeout)
				if enterResp == nil {
					return scheduler.NewCooldown(10*time.Minute, "魔界远征进入城镇无回包")
				}
				if enterCode, e := protocol.ParseDevilResult(enterResp.Payload); e != nil || enterCode != protocol.DevilResultSuccess {
					slog.Info("魔界远征进入城镇未成功", "role_id", session.RoleID(),
						"mission", name, "town", town, "result", enterCode)
					_ = call(protocol.BuildDevilQuitPacket(), protocol.ActionIDDevilQuit, callTimeout)
					continue
				}
				_ = call(protocol.BuildDevilGetPlayersPacket(), protocol.ActionIDDevilGetPlayers, callTimeout)

				dataResp := call(protocol.BuildDevilGetMissionDataPacket(missionType),
					protocol.DevilMissionDataAction(missionType), callTimeout)
				if dataResp == nil {
					_ = call(protocol.BuildDevilQuitPacket(), protocol.ActionIDDevilQuit, callTimeout)
					return scheduler.NewCooldown(10*time.Minute, "魔界远征关卡数据无回包")
				}
				data, dErr := protocol.ParseDevilMissionData(missionType, dataResp.Payload)
				if dErr != nil {
					slog.Warn("解析魔界远征关卡数据失败", "role_id", session.RoleID(),
						"mission", name, "err", dErr)
					_ = call(protocol.BuildDevilQuitPacket(), protocol.ActionIDDevilQuit, callTimeout)
					continue
				}
				if data.Result != protocol.DevilResultSuccess {
					slog.Info("魔界远征关卡数据未成功", "role_id", session.RoleID(),
						"mission", name, "result", data.Result)
					_ = call(protocol.BuildDevilQuitPacket(), protocol.ActionIDDevilQuit, callTimeout)
					continue
				}
				if devilRefinesCapped(data.Refines) {
					slog.Info("魔界远征本周材料已达上限，放弃本次挑战",
						"role_id", session.RoleID(), "mission", name, "refines", data.Refines)
					_ = call(protocol.BuildDevilQuitPacket(), protocol.ActionIDDevilQuit, callTimeout)
					continue
				}
				_ = call(protocol.BuildDevilRejectBuyBoxPacket(), protocol.ActionIDDevilRejectBuyBox, callTimeout)

				teams := devilSortedTeams(data.TeamIDs)
				if len(teams) == 0 {
					slog.Info("魔界远征没有可挑战的怪物队伍", "role_id", session.RoleID(), "mission", name)
					_ = call(protocol.BuildDevilQuitPacket(), protocol.ActionIDDevilQuit, callTimeout)
					continue
				}
				// 按队伍逐个尝试：当前队伍战斗失败后切换到下一个队伍，全部失败才结束。
				teamIdx := 0
				fights := 0
				for fights < maxFights && ctx.Err() == nil && teamIdx < len(teams) {
					teamID := teams[teamIdx]
					pos := devilTeamPos(missionType, teamID)
					// 429/17 无回包（官方 response 为空），只发送不等待。
					if sendErr := session.Send(protocol.BuildDevilMoveToPacket(pos[0], pos[1], pos[0], pos[1])); sendErr != nil {
						slog.Info("魔界远征移动发送失败", "role_id", session.RoleID(), "err", sendErr)
						break
					}
					fightResp := call(protocol.BuildDevilFightMonsterPacket(missionType, teamID),
						protocol.DevilFightAction(missionType), fightTimeout)
					if fightResp == nil {
						slog.Info("魔界远征战斗无回包，结束该伙伴挑战",
							"role_id", session.RoleID(), "mission", name)
						break
					}
					fightCode, fErr := protocol.ParseDevilResult(fightResp.Payload)
					if fErr != nil {
						break
					}
					if fightCode != protocol.DevilResultSuccess {
						slog.Info("魔界远征队伍战斗结束", "role_id", session.RoleID(),
							"mission", name, "team_id", teamID, "fights", fights,
							"result", fightCode, "result_text", protocol.DevilResultText(fightCode))
						teamIdx++
						continue
					}
					fights++
					slog.Info("魔界远征战斗成功", "role_id", session.RoleID(),
						"mission", name, "fights", fights, "team_id", teamID)

					// 每场战斗后复查本周材料上限。
					refresh := call(protocol.BuildDevilGetMissionDataPacket(missionType),
						protocol.DevilMissionDataAction(missionType), callTimeout)
					if refresh == nil {
						break
					}
					refreshed, rErr := protocol.ParseDevilMissionData(missionType, refresh.Payload)
					if rErr != nil {
						break
					}
					if devilRefinesCapped(refreshed.Refines) {
						slog.Info("魔界远征本周材料已达上限，停止挑战",
							"role_id", session.RoleID(), "mission", name, "fights", fights)
						break
					}
				}
				_ = call(protocol.BuildDevilGetDeployInfoPacket(), protocol.ActionIDDevilGetDeployInfo, callTimeout)
				_ = call(protocol.BuildDevilQuitPacket(), protocol.ActionIDDevilQuit, callTimeout)
				slog.Info("魔界远征该伙伴挑战完成", "role_id", session.RoleID(),
					"mission", name, "fights", fights)
			}
			return nil
		},
	)
}

// devilMaterialCapped 判断面板显示的本周材料是否达到上限。
func devilMaterialCapped(panel *protocol.DevilMatchPanel, missionType uint8) (bool, string) {
	if panel == nil {
		return false, ""
	}
	switch missionType {
	case protocol.DevilMissionYinYang:
		if panel.WeekStoneMax > 0 && panel.WeekStone >= panel.WeekStoneMax {
			return true, fmt.Sprintf("阴阳石 %d/%d", panel.WeekStone, panel.WeekStoneMax)
		}
	case protocol.DevilMissionHermit:
		if panel.WeekStone2Max > 0 && panel.WeekStone2 >= panel.WeekStone2Max {
			return true, fmt.Sprintf("隐仙石 %d/%d", panel.WeekStone2, panel.WeekStone2Max)
		}
	case protocol.DevilMissionMonsterGod:
		if len(panel.Souls) == 0 {
			return false, ""
		}
		minWeek := panel.Souls[0].WeekSoul
		for _, s := range panel.Souls {
			if s.WeekSoul < s.WeekMaxSoul {
				return false, ""
			}
			if s.WeekSoul < minWeek {
				minWeek = s.WeekSoul
			}
		}
		return true, fmt.Sprintf("魔灵 %d/%d", minWeek, panel.Souls[0].WeekMaxSoul)
	}
	return false, ""
}

// devilRefinesCapped 判断关卡内炼化进度是否全部达到本周上限。
func devilRefinesCapped(refines []protocol.DevilRefine) bool {
	if len(refines) == 0 {
		return false
	}
	for _, r := range refines {
		if r.WeekMax <= 0 || r.Week < r.WeekMax {
			return false
		}
	}
	return true
}

// devilSortedTeams 返回升序排列的怪物队伍编号（客户端按 id 降序展示，先打最小 id）。
func devilSortedTeams(ids []int32) []int32 {
	out := append([]int32(nil), ids...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// nextDevilOpenAt 返回下一次魔界远征开放时刻（每周五 00:05，活动持续五六日）。
func nextDevilOpenAt(now time.Time) time.Time {
	open := time.Date(now.Year(), now.Month(), now.Day(), 0, 5, 0, 0, now.Location())
	for open.Weekday() != time.Friday {
		open = open.AddDate(0, 0, 1)
	}
	if !now.Before(open) {
		open = open.AddDate(0, 0, 7)
	}
	return open
}
