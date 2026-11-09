// Package dungeon 提供副本推图、扫荡与爬塔等高强度 PvE 玩法自动化。
//
// 本文件实现通天塔自动挑战 (Mod_TongTianTower_Base, Module 648)。
package dungeon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// 通天塔每日挑战次数由服务端面板给出（ChallengeTimes 为剩余次数）；
// 挑战成功后服务端可能立即返回 CD_TIME，需短暂等待后重试同层，CD 拒绝不消耗挑战次数。
// 本地仅保留一个远高于正常每日次数的安全上限，防止服务端次数异常导致死循环。
const (
	tongTianTowerMaxChallengesPerRun = 200
	tongTianTowerMaxCDRetries        = 5
	// tongTianTowerMaxLossRetries 同一层战斗失败后的最大重试次数。
	tongTianTowerMaxLossRetries = 3
	tongTianTowerCDBaseWait     = 2 * time.Second
	tongTianTowerCDMaxWait      = 10 * time.Second
)

// NewTongTianTowerRoutine 构造通天塔自动挑战任务。
//
// 协议依据官方 com.protocols.Mod_TongTianTower_Base 与 com.datas.TongTianTowerData:
//  1. panel_info 读取当前重天/层数、剩余挑战次数与突破状态；
//  2. 到达当前重天顶层且 is_break_up=1 时调用 break_up；
//  3. challenge 逐层挑战，战报 win_player_id 等于自身 PlayerID 即通关；
//  4. 失败语义按结果码分流：背包满/体力不足熔断，次数用尽正常结束，需要突破则先突破。
//
// 全程不消耗元宝；通天塔挑战仅消耗体力与每日挑战次数。
func NewTongTianTowerRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"tong_tian_tower",
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
			slog.Info("正在执行副本任务: 通天塔自动挑战", "role_id", session.RoleID())

			// 通天塔隶属山海九界玩法，先查功能开放列表；未开放时不发送业务请求。
			open, fErr := session.IsFunctionOpen(ctx, protocol.FunctionIDShanhaiWorld)
			if fErr != nil {
				slog.Info("通天塔功能开放状态查询失败，跳过", "role_id", session.RoleID(), "err", fErr)
				return nil
			}
			if !open {
				slog.Info("通天塔功能未开放，跳过", "role_id", session.RoleID(), "function_id", protocol.FunctionIDShanhaiWorld)
				return nil
			}

			fetchPanel := func() (*protocol.TongTianTowerPanel, error) {
				cCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				resp, err := session.Call(cCtx, protocol.BuildTongTianTowerPanelPacket(), protocol.ActionIDTongTianTowerPanel)
				if err != nil {
					return nil, err
				}
				if resp == nil {
					return nil, errors.New("通天塔面板无回包")
				}
				return protocol.ParseTongTianTowerPanelResult(resp.Payload)
			}

			panel, err := fetchPanel()
			if err != nil {
				slog.Info("通天塔功能不可用或未开放，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			slog.Info("通天塔面板",
				"role_id", session.RoleID(),
				"heaven", panel.Heaven,
				"floor", panel.Floor,
				"challenge_times", panel.ChallengeTimes,
				"is_break_up", panel.IsBreakUp,
				"completed_num", panel.CompletedNum,
				"sky_palace_level", panel.SkyPalaceLevel,
			)

			breakUp := func() (uint8, error) {
				cCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				resp, err := session.Call(cCtx, protocol.BuildTongTianTowerBreakUpPacket(), protocol.ActionIDTongTianTowerBreakUp)
				if err != nil {
					return 0, err
				}
				if resp == nil {
					return 0, errors.New("通天塔突破无回包")
				}
				return protocol.ParseTongTianTowerActionCode(resp.Payload)
			}

			// 到达当前重天顶层且满足突破条件时先突破。
			if panel.IsBreakUp == 1 {
				code, bErr := breakUp()
				if bErr != nil {
					slog.Warn("通天塔突破失败", "role_id", session.RoleID(), "err", bErr)
					return nil
				}
				switch code {
				case protocol.TongTianTowerResultSuccess:
					slog.Info("通天塔突破成功", "role_id", session.RoleID())
					if refreshed, rErr := fetchPanel(); rErr == nil {
						panel = refreshed
					}
				case protocol.TongTianTowerResultConditionLimit:
					slog.Info("通天塔突破条件未满足，跳过", "role_id", session.RoleID())
					return nil
				case protocol.TongTianTowerResultAlreadyBreakUp, protocol.TongTianTowerResultNotNeedBreakUp:
					slog.Info("通天塔无需突破或已突破", "role_id", session.RoleID(), "result", code)
				default:
					slog.Info("通天塔突破未成功", "role_id", session.RoleID(), "result", code)
					return nil
				}
			}

			if panel.ChallengeTimes <= 0 {
				slog.Info("通天塔今日挑战次数已用尽", "role_id", session.RoleID())
				return nil
			}

			challenged := 0
			cdRetries := 0
			losses := 0
			for panel.ChallengeTimes > 0 && challenged < tongTianTowerMaxChallengesPerRun {
				if err := ctx.Err(); err != nil {
					return nil
				}
				if panel.Heaven <= 0 || panel.Floor <= 0 {
					slog.Info("通天塔已到达最高层，结束挑战", "role_id", session.RoleID())
					return nil
				}
				monsterTeamID := protocol.TongTianTowerMonsterTeamID(panel.Heaven, panel.Floor)
				if monsterTeamID == 0 {
					slog.Warn("通天塔当前层缺少怪物队伍静态数据，跳过",
						"role_id", session.RoleID(), "heaven", panel.Heaven, "floor", panel.Floor)
					return nil
				}

				teams := []protocol.TongTianTowerTeam{{
					TeamType:      protocol.TongTianTowerMainTeam,
					MonsterTeamID: monsterTeamID,
				}}
				cCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
				resp, cErr := session.Call(cCtx, protocol.BuildTongTianTowerChallengePacket(teams), protocol.ActionIDTongTianTowerChallenge)
				cancel()
				if cErr != nil {
					return fmt.Errorf("通天塔挑战请求失败: %w", cErr)
				}
				if resp == nil {
					return errors.New("通天塔挑战无回包")
				}
				result, pErr := protocol.ParseTongTianTowerChallengeResult(resp.Payload)
				if pErr != nil {
					return fmt.Errorf("解析通天塔挑战回包失败: %w", pErr)
				}

				switch result.Result {
				case protocol.TongTianTowerResultSuccess:
					// 战报中的 win_player_id 并非角色 PlayerID (线上实测为内部战斗 ID)，
					// 因此以面板层数是否推进作为通关判据。
					refreshed, rErr := fetchPanel()
					if rErr != nil {
						slog.Warn("通天塔挑战后刷新面板失败，结束本轮", "role_id", session.RoleID(), "err", rErr)
						return nil
					}
					won := refreshed.Heaven > panel.Heaven ||
						(refreshed.Heaven == panel.Heaven && refreshed.Floor > panel.Floor)
					oldHeaven, oldFloor := panel.Heaven, panel.Floor
					panel = refreshed
					if !won {
						losses++
						if losses > tongTianTowerMaxLossRetries {
							slog.Info("通天塔连续挑战失败，达到重试上限，结束本轮",
								"role_id", session.RoleID(),
								"heaven", oldHeaven,
								"floor", oldFloor,
								"retries", losses,
								"win_player_id", result.WinPlayerID,
							)
							return nil
						}
						slog.Info("通天塔挑战失败，重试同一层",
							"role_id", session.RoleID(),
							"heaven", oldHeaven,
							"floor", oldFloor,
							"retry", losses,
							"max_retries", tongTianTowerMaxLossRetries,
							"win_player_id", result.WinPlayerID,
						)
						select {
						case <-ctx.Done():
							return nil
						case <-time.After(time.Second):
						}
						continue
					}
					challenged++
					cdRetries = 0
					losses = 0
					slog.Info("通天塔挑战通关",
						"role_id", session.RoleID(),
						"heaven", oldHeaven,
						"floor", oldFloor,
						"next_heaven", panel.Heaven,
						"next_floor", panel.Floor,
						"award_items", result.AwardItemIDs,
						"award_nums", result.AwardItemNums,
						"today_challenged", challenged,
					)
				case protocol.TongTianTowerResultTopFloor:
					slog.Info("通天塔已到达最高层", "role_id", session.RoleID())
					return nil
				case protocol.TongTianTowerResultBagFull:
					return scheduler.ErrBagFull
				case protocol.TongTianTowerResultNotEnoughPower:
					return scheduler.ErrStaminaDepleted
				case protocol.TongTianTowerResultChallengeTimesLimit:
					slog.Info("通天塔挑战次数已用尽", "role_id", session.RoleID())
					return nil
				case protocol.TongTianTowerResultCDTime:
					if cdRetries >= tongTianTowerMaxCDRetries {
						slog.Warn("通天塔挑战连续冷却，结束本轮",
							"role_id", session.RoleID(), "retries", cdRetries, "today_challenged", challenged)
						return nil
					}
					wait := tongTianTowerCDBaseWait << cdRetries
					if wait > tongTianTowerCDMaxWait {
						wait = tongTianTowerCDMaxWait
					}
					cdRetries++
					slog.Info("通天塔挑战冷却中，等待后重试同层",
						"role_id", session.RoleID(),
						"heaven", panel.Heaven,
						"floor", panel.Floor,
						"wait", wait,
						"retry", cdRetries,
						"today_challenged", challenged,
					)
					select {
					case <-ctx.Done():
						return nil
					case <-time.After(wait):
					}
					continue
				case protocol.TongTianTowerResultNeedBreakUp:
					code, bErr := breakUp()
					if bErr != nil {
						slog.Warn("通天塔需要突破但突破请求失败", "role_id", session.RoleID(), "err", bErr)
						return nil
					}
					if code != protocol.TongTianTowerResultSuccess {
						slog.Info("通天塔突破未成功，结束挑战", "role_id", session.RoleID(), "result", code)
						return nil
					}
					slog.Info("通天塔突破成功", "role_id", session.RoleID())
					if refreshed, rErr := fetchPanel(); rErr == nil {
						panel = refreshed
					}
				case protocol.TongTianTowerResultNoComplete:
					slog.Info("通天塔当前层尚未通关，结束挑战", "role_id", session.RoleID())
					return nil
				case protocol.TongTianTowerResultMonsterTeamError, protocol.TongTianTowerResultTeamTypeError:
					slog.Warn("通天塔队伍数据与当前层不匹配，跳过",
						"role_id", session.RoleID(), "heaven", panel.Heaven, "floor", panel.Floor,
						"monster_team_id", monsterTeamID, "result", result.Result)
					return nil
				case protocol.TongTianTowerResultMaterialNumLimit:
					slog.Info("通天塔材料掉落达到上限，结束挑战", "role_id", session.RoleID())
					return nil
				case protocol.TongTianTowerResultConditionLimit:
					slog.Info("通天塔条件不满足，结束挑战", "role_id", session.RoleID())
					return nil
				default:
					slog.Info("通天塔挑战未成功", "role_id", session.RoleID(), "result", result.Result)
					return nil
				}

				// 面板已在通关分支刷新；此处仅处理通关后出现的突破条件。
				if panel.IsBreakUp == 1 && panel.ChallengeTimes > 0 {
					code, bErr := breakUp()
					if bErr != nil {
						slog.Warn("通天塔自动突破请求失败", "role_id", session.RoleID(), "err", bErr)
						return nil
					}
					if code == protocol.TongTianTowerResultSuccess {
						slog.Info("通天塔自动突破成功", "role_id", session.RoleID())
						if again, aErr := fetchPanel(); aErr == nil {
							panel = again
						}
					}
				}
			}
			if challenged >= tongTianTowerMaxChallengesPerRun && panel.ChallengeTimes > 0 {
				slog.Warn("通天塔达到单轮安全上限，结束本轮",
					"role_id", session.RoleID(), "today_challenged", challenged,
					"remaining_times", panel.ChallengeTimes)
			}
			slog.Info("通天塔巡检完成",
				"role_id", session.RoleID(),
				"today_challenged", challenged,
				"remaining_times", panel.ChallengeTimes)
			return nil
		},
	)
}
