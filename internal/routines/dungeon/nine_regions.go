// Package dungeon 提供副本推图、爬塔、练功房与九空无界等挑战类玩法。
//
// 本文件实现「九空无界」(Mod_NineRegions_Base, Module 58) 自动挑战：
//
//	open_panel -> 检查 CD -> start_challenge -> 创建/复用队伍 ->
//	选择助战角色 -> fight_region -> 重新读取面板确认是否推进
//
// 九空无界是真实战斗玩法，胜负取决于队伍战力；例程只保证按官方链路发起挑战，
// 不消耗元宝，也不会购买次数。
package dungeon

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

const (
	// maxNineRegionsAttempts 单轮最多战斗次数 (含失败重试)，防止服务端异常时死循环。
	maxNineRegionsAttempts = 20
	// nineRegionsCallTimeout 普通协议请求超时。
	nineRegionsCallTimeout = 8 * time.Second
	// nineRegionsFightTimeout 挑战包含完整战报下发，超时放宽。
	nineRegionsFightTimeout = 25 * time.Second
	// nineRegionsMaxLevel 每个「界」的最大关卡数 (NineRegionsType.MaxLevel)。
	nineRegionsMaxLevel = 10
	// nineRegionsLossRetries 同一关卡战斗失败后的最大重试次数。
	nineRegionsLossRetries = 3
)

// NewNineRegionsRoutine 构造九空无界自动挑战任务。
//
// 官方客户端 (NineRegions2View/NineRegionsLevel2View) 流程:
//  1. open_panel 读取 cur_jie/cur_level；cur_level=0 时实际挑战坐标是 (jie,1)；
//  2. check_is_can_challenge / start_challenge 校验关卡；
//  3. 无队伍时 create_region_team 创建队伍；
//  4. get_helper_role_list / choose_helper_role 选择助战角色；
//  5. fight_region 发起挑战；
//  6. 挑战成功后重新读取面板，cur_level 推进则继续下一关。
//
// 自动化只调用普通挑战；隐藏关卡入口存在时额外尝试一次 challenge_hidden_level。
// 永不购买挑战次数，不消耗元宝。
func NewNineRegionsRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"nine_regions",
		"dungeon",
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
			if session.State() != client.StateActive {
				return nil
			}
			slog.Info("正在执行副本任务: 九空无界挑战", "role_id", session.RoleID())

			call := func(pkt *protocol.Packet, actionID uint32, timeout time.Duration) *protocol.Packet {
				cCtx, cancel := context.WithTimeout(ctx, timeout)
				defer cancel()
				resp, err := session.Call(cCtx, pkt, actionID)
				if err != nil {
					slog.Info("九空无界协议无回包，跳过本轮",
						"role_id", session.RoleID(), "action", actionID, "err", err)
					return nil
				}
				return resp
			}
			fetchPanel := func() *protocol.NineRegionsPanel {
				resp := call(protocol.BuildNineRegionsOpenPacket(), protocol.ActionIDNineRegionsOpen, nineRegionsCallTimeout)
				if resp == nil {
					return nil
				}
				panel, err := protocol.ParseNineRegionsPanelResult(resp.Payload)
				if err != nil {
					slog.Warn("解析九空无界面板失败", "role_id", session.RoleID(), "err", err)
					return nil
				}
				return panel
			}
			sleep := func(d time.Duration) bool {
				select {
				case <-ctx.Done():
					return false
				case <-time.After(d):
					return true
				}
			}

			panel := fetchPanel()
			if panel == nil {
				slog.Info("九空无界功能不可用或未开放，跳过", "role_id", session.RoleID())
				return nil
			}
			slog.Info("九空无界面板", "role_id", session.RoleID(),
				"is_first", panel.IsFirst, "cur_jie", panel.CurJie,
				"cur_level", panel.CurLevel, "hidden_level", panel.HiddenLevel)

			// 1. 挑战冷却。
			if cdResp := call(protocol.BuildNineRegionsCDPacket(), protocol.ActionIDNineRegionsCD, nineRegionsCallTimeout); cdResp != nil {
				if cd, err := protocol.ParseNineRegionsCDResult(cdResp.Payload); err == nil {
					slog.Info("九空无界挑战冷却", "role_id", session.RoleID(), "cd_seconds", cd)
					if cd > 0 {
						return nil
					}
				}
			}

			// ensureTeam 保证当前关卡有可用队伍。
			ensureTeam := func(jie, level int32) bool {
				resp := call(protocol.BuildNineRegionsTeamInfoPacket(), protocol.ActionIDNineRegionsTeamInfo, nineRegionsCallTimeout)
				if resp != nil {
					if team, err := protocol.ParseNineRegionsTeamInfoResult(resp.Payload); err == nil {
						slog.Info("九空无界队伍信息", "role_id", session.RoleID(),
							"leader_id", team.LeaderID, "members", len(team.Members))
						if team.LeaderID > 0 && len(team.Members) > 0 {
							return true
						}
					}
				}
				createResp := call(protocol.BuildNineRegionsCreateTeamPacket(jie, level),
					protocol.ActionIDNineRegionsCreateTeam, nineRegionsCallTimeout)
				if createResp == nil {
					return false
				}
				created, err := protocol.ParseNineRegionsCreateTeamResult(createResp.Payload)
				if err != nil {
					slog.Warn("解析九空无界创建队伍回包失败", "role_id", session.RoleID(), "err", err)
					return false
				}
				if created.Result != protocol.NineRegionsResultSuccess {
					slog.Info("九空无界创建队伍未成功", "role_id", session.RoleID(),
						"result", created.Result, "result_text", protocol.NineRegionsResultText(created.Result))
					return false
				}
				slog.Info("九空无界创建队伍成功", "role_id", session.RoleID(), "team_id", created.TeamID)
				return true
			}

			// chooseHelpers 尽力选择高等级助战角色；失败不阻断挑战。
			chooseHelpers := func() {
				resp := call(protocol.BuildNineRegionsHelperListPacket(),
					protocol.ActionIDNineRegionsHelperList, nineRegionsCallTimeout)
				if resp == nil {
					return
				}
				roles, err := protocol.ParseNineRegionsHelperListResult(resp.Payload)
				if err != nil || len(roles) == 0 {
					return
				}
				// 未上阵角色优先，其次等级从高到低。
				sort.SliceStable(roles, func(i, j int) bool {
					leftReady := roles[i].State == protocol.NineRegionsResultNoDeploy
					rightReady := roles[j].State == protocol.NineRegionsResultNoDeploy
					if leftReady != rightReady {
						return leftReady
					}
					return roles[i].Level > roles[j].Level
				})
				slots := []uint8{0, 1}
				chosen := 0
				for _, role := range roles {
					if chosen >= len(slots) {
						break
					}
					chooseResp := call(protocol.BuildNineRegionsChooseHelperPacket(role.RoleID, slots[chosen]),
						protocol.ActionIDNineRegionsChooseHelper, nineRegionsCallTimeout)
					if chooseResp == nil {
						continue
					}
					code, pErr := protocol.ParseNineRegionsSimpleResult(chooseResp.Payload)
					if pErr != nil {
						continue
					}
					if code == protocol.NineRegionsResultSuccess {
						chosen++
						slog.Info("九空无界选择助战成功", "role_id", session.RoleID(),
							"helper_role_id", role.RoleID, "helper_name", role.Name,
							"helper_level", role.Level, "slot", slots[chosen-1])
					} else {
						slog.Info("九空无界选择助战未成功", "role_id", session.RoleID(),
							"helper_role_id", role.RoleID, "result", code,
							"result_text", protocol.NineRegionsResultText(code))
					}
				}
			}

			battles := 0
			losses := 0
			for i := 0; i < maxNineRegionsAttempts; i++ {
				if ctx.Err() != nil {
					return nil
				}
				jie := panel.CurJie
				// 面板 cur_level 是当前界已通关的关卡数 (0..10)，实际挑战关卡为
				// cur_level+1；已通关满 10 关则推进到下一界的第 1 关。
				level := panel.CurLevel + 1
				if level > nineRegionsMaxLevel {
					jie++
					level = 1
				}

				// 2. 开始挑战校验。
				startResp := call(protocol.BuildNineRegionsStartChallengePacket(jie, level),
					protocol.ActionIDNineRegionsStartChallenge, nineRegionsCallTimeout)
				if startResp == nil {
					break
				}
				start, err := protocol.ParseNineRegionsStartResult(startResp.Payload)
				if err != nil {
					slog.Warn("解析九空无界开始挑战回包失败", "role_id", session.RoleID(), "err", err)
					break
				}
				if start.Result != protocol.NineRegionsResultChallengeSuccess {
					slog.Info("九空无界当前关卡不可挑战", "role_id", session.RoleID(),
						"jie", jie, "level", level, "result", start.Result,
						"result_text", protocol.NineRegionsResultText(start.Result))
					break
				}

				// 3. 队伍与助战。
				if !ensureTeam(jie, level) {
					break
				}
				chooseHelpers()
				if !sleep(300 * time.Millisecond) {
					return nil
				}

				// 4. 发起挑战。
				fightResp := call(protocol.BuildNineRegionsFightPacket(jie, level),
					protocol.ActionIDNineRegionsFight, nineRegionsFightTimeout)
				if fightResp == nil {
					break
				}
				fight, err := protocol.ParseNineRegionsFightResult(fightResp.Payload)
				if err != nil {
					slog.Warn("解析九空无界挑战回包失败", "role_id", session.RoleID(), "err", err)
					break
				}
				if fight.Result != protocol.NineRegionsResultSuccess {
					slog.Info("九空无界挑战未成功", "role_id", session.RoleID(),
						"jie", jie, "level", level, "result", fight.Result,
						"result_text", protocol.NineRegionsResultText(fight.Result))
					break
				}
				battles++
				slog.Info("九空无界战斗已完成", "role_id", session.RoleID(),
					"jie", jie, "level", level, "war_count", fight.WarCount)

				// 5. 等待战斗结算：重新读取面板，确认是否推进到下一关。
				// 战斗回包 result=SUCCESS 只表示战斗已执行；胜负由服务端面板推进体现。
				var next *protocol.NineRegionsPanel
				for attempt := 0; attempt < 3; attempt++ {
					if next = fetchPanel(); next != nil {
						if next.CurJie > panel.CurJie ||
							(next.CurJie == panel.CurJie && next.CurLevel > panel.CurLevel) {
							break
						}
					}
					if !sleep(500 * time.Millisecond) {
						return nil
					}
				}
				if next == nil {
					slog.Info("九空无界战斗结果读取失败，结束本轮", "role_id", session.RoleID())
					break
				}
				if next.CurJie == panel.CurJie && next.CurLevel == panel.CurLevel {
					losses++
					if losses > nineRegionsLossRetries {
						slog.Info("九空无界连续战斗失败，达到重试上限，结束本轮",
							"role_id", session.RoleID(), "retries", losses,
							"jie", jie, "level", level)
						break
					}
					slog.Info("九空无界战斗失败，重试同一关卡",
						"role_id", session.RoleID(), "retry", losses,
						"max_retries", nineRegionsLossRetries, "jie", jie, "level", level)
					if !sleep(800 * time.Millisecond) {
						return nil
					}
					continue
				}
				losses = 0
				slog.Info("九空无界关卡推进", "role_id", session.RoleID(),
					"from_jie", panel.CurJie, "from_level", panel.CurLevel,
					"to_jie", next.CurJie, "to_level", next.CurLevel)
				panel = next
				if !sleep(500 * time.Millisecond) {
					return nil
				}
			}

			// 6. 隐藏关卡入口存在时尝试一次 (免费，不购买次数)。
			if panel.HiddenLevel > 0 {
				hiddenResp := call(protocol.BuildNineRegionsHiddenPacket(panel.HiddenLevel),
					protocol.ActionIDNineRegionsHidden, nineRegionsFightTimeout)
				if hiddenResp != nil && len(hiddenResp.Payload) > 0 {
					code, err := protocol.ParseNineRegionsSimpleResult(hiddenResp.Payload)
					if err == nil {
						if code == protocol.NineRegionsResultCantChallengeHidden {
							slog.Info("九空无界隐藏关卡暂不可挑战", "role_id", session.RoleID(),
								"hidden_level", panel.HiddenLevel)
						} else {
							slog.Info("九空无界隐藏关卡挑战完成", "role_id", session.RoleID(),
								"hidden_level", panel.HiddenLevel, "result", code)
						}
					}
				}
			}

			slog.Info("九空无界巡检完成", "role_id", session.RoleID(), "battles", battles)
			return nil
		},
	)
}
