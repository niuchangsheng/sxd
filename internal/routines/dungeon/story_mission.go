// Package dungeon 提供副本推图、主线任务循环、精英扫荡、六道轮回、伏魔塔等关卡挑战自动化。
//
// 本文件实现剧情副本推图、章节拉取与扫荡战斗封装。
package dungeon

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

// storyMissionCallTimeout 剧情副本 enter/fight 单请求超时。
// 变量形式便于单测缩短；生产保持 8 秒。
var storyMissionCallTimeout = 8 * time.Second

// advanceQuestMission 推进指定任务的关联剧情关卡（包含自动清理同章节前置未通关小关与秒扫荡闭环）。
func advanceQuestMission(
	ctx context.Context,
	session *client.RoleSession,
	dictRepo dictionary.Repository,
	q *dictionary.Quest,
	initialCount int32,
	maxCount int32,
	ensureStamina func() bool,
	totalCostStamina *int,
	totalGainExp *int64,
	totalGainCoins *int64,
	clearedMissions map[int]bool,
	failedMissions map[int]bool,
) bool {
	targetMission, err := dictRepo.GetMission(q.MissionID)
	if err != nil || targetMission == nil {
		return false
	}
	// 精英副本任务 (Quests.Type=4) 与主线/支线共用 Module 4 首通状态机，
	// 但精英关卡不在 get_sections 章节列表内，也不参与 Module 25 普通关卡扫荡。
	isEliteQuest := q.Type == 4
	if q.Type != 1 && q.Type != 2 && !isEliteQuest {
		slog.Info("任务关卡属于非主线/支线/精英任务，剧情推图跳过", "quest_id", q.ID, "title", q.Title, "type", q.Type)
		return false
	}
	if isEliteQuest {
		if targetMission.Type != 1 {
			slog.Info("精英任务关联关卡并非精英副本，剧情推图跳过", "quest_id", q.ID, "title", q.Title, "mission_id", targetMission.ID, "type", targetMission.Type)
			return false
		}
	} else if targetMission.Type != 0 {
		slog.Info("任务关卡属于非普通主线副本，剧情推图跳过", "quest_id", q.ID, "title", q.Title, "mission_id", targetMission.ID, "type", targetMission.Type)
		return false
	}

	playerLevel := session.GetPlayerState().Level
	if missionLevelLocked(targetMission.MissionLock, playerLevel) {
		slog.Warn("任务关卡等级未达标，暂不推进剧情副本",
			"role_id", session.RoleID(),
			"quest_id", q.ID,
			"title", q.Title,
			"mission_id", targetMission.ID,
			"name", targetMission.Name,
			"mission_lock", targetMission.MissionLock,
			"required_level", missionRequiredLevel(targetMission.MissionLock),
			"player_level", playerLevel,
		)
		return false
	}

	const maxStageSteps = 10
	lastCount := initialCount
	targetMax := maxCount
	noCountGainRuns := 0
	clearedPrereq := false

	for step := 0; step < maxStageSteps; step++ {
		if ctx.Err() != nil || session.State() == client.StateClosed {
			return clearedPrereq || lastCount > initialCount
		}
		if session.GetBaseStamina() < 5 && !ensureStamina() {
			return clearedPrereq || lastCount > initialCount
		}

		// 计算本批次需要扫荡的次数 (单批最多 5 次以保持平稳)。
		// 任务关卡只扣基础体力，额外体力池不参与，因此按基础体力估算批次。
		batchCount := uint16(1)
		if targetMax > lastCount {
			needed := targetMax - lastCount
			avail := int32(session.GetBaseStamina() / 5)
			if avail < needed && ensureStamina != nil {
				if ensureStamina() {
					avail = int32(session.GetBaseStamina() / 5)
				}
			}
			if needed > 0 && avail > 0 {
				cnt := min(needed, avail)
				if cnt > 5 {
					cnt = 5
				}
				if cnt > 0 {
					batchCount = uint16(cnt)
				}
			}
		}

		// 每关挑战前复位城镇场景：上一个剧情副本结束后服务端仍保留副本/战场上下文，
		// 直接请求 get_sections 会被静默丢弃（2026-11-14 线上：任务 560 通关后，
		// 任务 561/564 的 get_sections 连续无回包）。
		// 注意：首步且刚完成 3/9 任务列表同步时角色已在城镇上下文，严禁以 3/9 作锚点
		// 立即发 8 字节 enter_town（2026-11-22 s1 实机连续复现：0x00010000 携带
		// town=22 + prev=0x00030009 后 112~165ms 被网关直接 FIN 断链）。
		skipInitialTownReset := step == 0 && session.LastAckActionID() == protocol.ActionIDQuestList
		if !skipInitialTownReset {
			if _, ok := enterQuestTownScene(ctx, session); ok {
				select {
				case <-ctx.Done():
					return clearedPrereq || lastCount > initialCount
				case <-time.After(200 * time.Millisecond):
				}
			}
		}

		// 0. 激活章节关卡列表上下文 (Mod 4, Act 0: get_sections)，对齐官方前置调用链
		mapID := int32(targetMission.MapID)
		if mapID <= 0 {
			mapID = int32(targetMission.SectionID)
		}
		targetKnownUncleared := false
		// targetInSections 记录目标关卡是否出现在 get_sections 章节列表中。
		// 未出现说明该关卡尚未解锁 (或章节列表拉取失败)，此时 Module 25 扫荡会被网关静默丢弃，
		// 必须直接走剧情副本，避免每次空等 5 秒。
		targetInSections := false
		if mapID > 0 {
			secResp, secErr := fetchMissionSections(ctx, session, mapID)
			if secErr != nil {
				if errSessionTerminated(ctx, secErr) {
					return clearedPrereq || lastCount > initialCount
				}
				if isEliteQuest {
					// 精英副本本就不在 get_sections 列表中，继续走 Module 4 首通状态机。
					slog.Debug("精英副本跳过章节列表查询", "role_id", session.RoleID(), "map_id", mapID, "err", secErr)
				} else {
					// 非精英关卡：get_sections 失败说明章节/地图上下文不可用。
					// 若任务仍是「已通关需重复击杀」的进度型任务，直接尝试一次
					// 剧情副本重复挑战，避免整轮任务被放弃。
					slog.Warn("激活章节关卡列表上下文失败，跳过该关卡", "role_id", session.RoleID(), "map_id", mapID, "err", secErr)
					if maxCount > 0 && initialCount < maxCount {
						slog.Info("章节上下文不可用，直接尝试重复挑战剧情副本",
							"role_id", session.RoleID(),
							"mission_id", targetMission.ID,
							"progress", fmt.Sprintf("%d/%d", initialCount, maxCount))
						if challengeStoryMission(ctx, session, targetMission, ensureStamina,
							totalCostStamina, totalGainExp, totalGainCoins, clearedMissions) {
							return true
						}
					}
					return clearedPrereq || lastCount > initialCount
				}
			} else if secResp != nil {
				if sections, pErr := protocol.ParseMissionGetSectionsResponse(secResp.Payload); pErr == nil {
					clearedCount, otherCount := 0, 0
					targetStatus := int8(-1)
					targetFound := false
					for _, sec := range sections {
						if sec.Status == 1 {
							clearedMissions[int(sec.MissionID)] = true
							clearedCount++
						} else {
							otherCount++
						}
						if int(sec.MissionID) == targetMission.ID {
							targetStatus = sec.Status
							targetFound = true
							targetInSections = true
							targetKnownUncleared = sec.Status == 0
						}
					}
					if step == 0 {
						slog.Info("章节关卡状态同步",
							"role_id", session.RoleID(),
							"map_id", mapID,
							"total", len(sections),
							"status_1", clearedCount,
							"status_other", otherCount,
							"target_mission_id", targetMission.ID,
							"target_found", targetFound,
							"target_status", targetStatus,
						)
					}
				}
			}
		}

		// 1. 优先尝试 Module 25 秒扫荡推进关卡。
		// 服务端 get_sections 标明该关卡尚未通关 (status == 0) 时，Module 25 扫荡必然无响应，
		// 直接走剧情副本首通挑战，避免每次空等 5 秒超时。
		practiced := false
		var pErr error
		if isEliteQuest {
			// 精英副本不在 get_sections 章节列表内，Module 25 普通扫荡对其不生效，
			// 直接走 Module 4 首通状态机 (enter -> fight -> rank -> box -> pickup)。
			slog.Info("精英副本任务直接进入首通挑战",
				"role_id", session.RoleID(),
				"mission_id", targetMission.ID,
				"name", targetMission.Name,
			)
		} else if !targetKnownUncleared && targetInSections {
			// 已通关关卡同样优先扫荡：官方抓包 (/tmp/story_repeat.pcapng)
			// 对已通关的 647 连续执行 9 次 25/0→25/1→25/3 扫荡，任务重复击杀
			// 计数由扫荡推进。历史上 25/1 被丢弃是因为缺少 25/0 前置导致尾链
			// 不匹配（2026-11-27 已修复），不再需要绕行剧情副本。
			practiced, _, pErr = practiceMission(ctx, session, int32(targetMission.ID), batchCount, ensureStamina, totalCostStamina, totalGainExp, totalGainCoins)
			if pErr != nil {
				if errors.Is(pErr, scheduler.ErrStaminaDepleted) || errors.Is(pErr, scheduler.ErrBagFull) {
					return clearedPrereq || lastCount > initialCount
				}
			}
		} else {
			reason := "关卡尚未首通"
			if !targetInSections {
				reason = "关卡不在已开放章节列表中 (未解锁或列表拉取失败)"
			} else if clearedMissions[targetMission.ID] {
				reason = "关卡已通关且章节状态未确认，回退剧情副本"
			}
			slog.Info("跳过 Module 25 扫荡直接进入剧情副本",
				"role_id", session.RoleID(),
				"mission_id", targetMission.ID,
				"name", targetMission.Name,
				"reason", reason,
			)
		}

		if !practiced {
			// 关卡已通关但任务进度未完成 (如 1/30 的重复击杀任务)：
			// 25/1 已被服务端丢弃，改为重复挑战剧情副本推进进度。
			if clearedMissions[targetMission.ID] {
				if maxCount > 0 && initialCount < maxCount {
					slog.Info("关卡已通关但任务进度未完成，重复挑战剧情副本",
						"role_id", session.RoleID(),
						"mission_id", targetMission.ID,
						"progress", fmt.Sprintf("%d/%d", initialCount, maxCount),
					)
					if challengeStoryMission(ctx, session, targetMission, ensureStamina,
						totalCostStamina, totalGainExp, totalGainCoins, clearedMissions) {
						return true
					}
				}
				return clearedPrereq || lastCount > initialCount
			}

			// 若 Module 25 扫荡受限（如首通尚未解锁），降级检查前置关卡并走剧情副本手动挑战。
			// 精英关卡与普通章节关卡相互独立，不做同章节前置扫描，直接挑战目标关卡。
			var prereqMission *dictionary.Mission
			if !isEliteQuest && targetMission.MapID > 0 {
				secResp, secErr := fetchMissionSections(ctx, session, int32(targetMission.MapID))
				if errSessionTerminated(ctx, secErr) {
					return clearedPrereq || lastCount > initialCount
				}

				if secErr == nil && secResp != nil {
					if sections, pErr := protocol.ParseMissionGetSectionsResponse(secResp.Payload); pErr == nil {
						for _, sec := range sections {
							if sec.Status == 1 {
								clearedMissions[int(sec.MissionID)] = true
							}
							if sec.Status == 0 && int(sec.MissionID) != targetMission.ID && !failedMissions[int(sec.MissionID)] {
								if sm, mErr := dictRepo.GetMission(int(sec.MissionID)); mErr == nil && sm != nil {
									if sm.MissionLock <= targetMission.MissionLock {
										if prereqMission == nil || sm.MissionLock < prereqMission.MissionLock {
											prereqMission = sm
										}
									}
								}
							}
						}
					}
				}
			}

			if prereqMission != nil {
				slog.Info("发现前置未通关剧情关卡，先行推进挑战",
					"role_id", session.RoleID(),
					"quest_id", q.ID,
					"prereq_mission_id", prereqMission.ID,
					"prereq_mission_name", prereqMission.Name,
					"target_mission_id", targetMission.ID,
				)
				if !challengeStoryMission(ctx, session, prereqMission, ensureStamina, totalCostStamina, totalGainExp, totalGainCoins, clearedMissions) {
					failedMissions[prereqMission.ID] = true
					return clearedPrereq || lastCount > initialCount
				}
				clearedPrereq = true
				continue
			}

			// 正式挑战主线目标关卡
			if !challengeStoryMission(ctx, session, targetMission, ensureStamina, totalCostStamina, totalGainExp, totalGainCoins, clearedMissions) {
				return clearedPrereq || lastCount > initialCount
			}
		}

		// 2. 挑战/扫荡后，同步任务状态检查是否已达成可交付 (State == 2) 或目标计数增长
		refreshed, sErr := session.SyncQuests(ctx)
		if sErr == nil {
			questState := int32(-1)
			var matchedItem protocol.PlayerQuestItem
			for _, it := range refreshed {
				if int(it.QuestID) == q.ID {
					questState = it.State
					matchedItem = it
					break
				}
			}
			if questState == 2 {
				ok, _ := submitCompleteQuest(ctx, session, int32(q.ID), resolveTownNPCID(dictRepo, questEndNPCID(q)))
				if ok {
					session.RemoveQuest(uint32(q.ID))
					session.AddRewards(q.AwardExp, q.AwardCoins)
					*totalGainExp += q.AwardExp
					*totalGainCoins += q.AwardCoins
					slog.Info("任务达成并成功交付结算", "role_id", session.RoleID(), "quest_id", q.ID, "title", q.Title)
				}
				return true
			}
			if questState == -1 {
				// 任务已不在进行列表中（已自动完成）
				return true
			}
			if matchedItem.MaxCount > 0 {
				targetMax = matchedItem.MaxCount
			}
			if matchedItem.CurrentCount > lastCount {
				slog.Info("任务目标进度更新",
					"role_id", session.RoleID(),
					"quest_id", q.ID,
					"title", q.Title,
					"progress", fmt.Sprintf("%d/%d", matchedItem.CurrentCount, matchedItem.MaxCount),
				)
				lastCount = matchedItem.CurrentCount
				noCountGainRuns = 0
				if matchedItem.MaxCount > 0 && matchedItem.CurrentCount >= matchedItem.MaxCount {
					continue
				}
				continue
			}
			noCountGainRuns++
			// 若杀怪任务挑战后计数未增长，或掉落物任务连续 2 次未掉落，退出本轮避免空转
			if matchedItem.NeedType == 0 || noCountGainRuns >= 2 {
				return lastCount > initialCount
			}
		} else {
			return true
		}
	}

	return clearedPrereq || lastCount > initialCount
}

// practiceMission 使用 Module 25 (Mod_MissionPractice_Base) 执行关卡秒扫荡。
func practiceMission(
	ctx context.Context,
	session *client.RoleSession,
	missionID int32,
	count uint16,
	ensureStamina func() bool,
	totalCostStamina *int,
	totalGainExp *int64,
	totalGainCoins *int64,
) (bool, int, error) {
	if session == nil {
		return false, 0, client.ErrNotConnected
	}
	if session.GetBaseStamina() < 5 && !ensureStamina() {
		return false, 0, scheduler.ErrStaminaDepleted
	}
	if count <= 0 {
		count = 1
	}
	maxAvail := uint16(session.GetBaseStamina() / 5)
	if count > maxAvail {
		count = maxAvail
	}
	if count == 0 {
		return false, 0, scheduler.ErrStaminaDepleted
	}

	// 0. 先查询扫荡状态 (25/0)。官方抓包 (/tmp/story_repeat.pcapng)：
	//    25/1 的尾链必须是 25/0 (0x00190000)；本会话从未发过 25/0 时，
	//    服务端会因尾链不匹配而静默丢弃 25/1（历史上“25/1 会被丢弃”的根因）。
	statusPkt, statusBuildErr := protocol.BuildPracticeGetStatusPacket(missionID, questPrevAct(session))
	if statusBuildErr != nil {
		return false, 0, statusBuildErr
	}
	statusCtx, statusCancel := context.WithTimeout(ctx, 8*time.Second)
	statusResp, statusErr := session.Call(statusCtx, statusPkt, protocol.ActionIDPracticeGetStatus)
	statusCancel()
	if statusErr != nil || statusResp == nil {
		slog.Warn("Module 25 扫荡状态查询无回包，跳过本次扫荡",
			"role_id", session.RoleID(), "mission_id", missionID, "err", statusErr)
		return false, 0, fmt.Errorf("扫荡状态查询无回包: %w", statusErr)
	}

	// 1. 发起扫荡 (ActionIDPracticeStart: 0x00190001)
	m25Req := protocol.StartPracticeRequest{
		MissionID: missionID,
		Count:     int16(count),
		AutoSale:  1,
	}
	m25Pkt, _ := protocol.BuildStartPracticePacket(m25Req)

	callCtx, callCancel := context.WithTimeout(ctx, 8*time.Second)
	respPkt, callErr := session.Call(callCtx, m25Pkt, protocol.ActionIDPracticeStart)
	callCancel()

	if callErr != nil || respPkt == nil {
		slog.Warn("Module 25 扫荡请求未收到响应", "role_id", session.RoleID(), "mission_id", missionID, "err", callErr)
		return false, 0, callErr
	}

	m25Res, pErr := protocol.ParseStartPracticeResult(respPkt.Payload)
	if pErr != nil || m25Res == nil {
		slog.Warn("Module 25 扫荡回包解析失败", "role_id", session.RoleID(), "mission_id", missionID, "err", pErr)
		return false, 0, pErr
	}

	slog.Info("Module 25 扫荡回包", "role_id", session.RoleID(), "mission_id", missionID, "result", m25Res.Result, "count", m25Res.Count)

	switch m25Res.Result {
	case protocol.PracticeResultSuccess:
		// 快速完成扫荡 (ActionIDPracticeQuickly: 0x00190003)。必须解析回包结果：
		// 之前这里丢弃响应后无条件扣体力/加奖励，加速请求被静默丢弃时会凭空
		// 记账，导致本地体力与收益和服务端不一致。
		quickPkt, qBuildErr := protocol.BuildQuicklyPacket(protocol.QuicklyRequest{Count: 0})
		if qBuildErr != nil {
			return false, 0, qBuildErr
		}
		qCtx, qCancel := context.WithTimeout(ctx, 8*time.Second)
		quickResp, quickErr := session.Call(qCtx, quickPkt, protocol.ActionIDPracticeQuickly)
		qCancel()
		if quickErr != nil || quickResp == nil {
			slog.Warn("Module 25 扫荡加速无回包，不扣体力也不加奖励",
				"role_id", session.RoleID(), "mission_id", missionID, "err", quickErr)
			if quickErr != nil {
				return false, 0, fmt.Errorf("扫荡加速无回包: %w", quickErr)
			}
			return false, 0, errors.New("扫荡加速无回包")
		}
		quickRes, qParseErr := protocol.ParseQuicklyResult(quickResp.Payload)
		if qParseErr != nil {
			slog.Warn("Module 25 扫荡加速回包解析失败，不记账",
				"role_id", session.RoleID(), "mission_id", missionID, "err", qParseErr)
			return false, 0, qParseErr
		}
		if quickRes.Result != protocol.PracticeResultSuccess {
			slog.Info("Module 25 扫荡加速未成功，不记账",
				"role_id", session.RoleID(), "mission_id", missionID, "result", quickRes.Result)
			return false, 0, nil
		}

		actualRounds := int(count)
		if quickRes.Count > 0 && int(quickRes.Count) <= actualRounds {
			actualRounds = int(quickRes.Count)
		} else if m25Res.Count > 0 && int(m25Res.Count) <= actualRounds {
			actualRounds = int(m25Res.Count)
		}
		cost := actualRounds * 5
		_, _ = session.ConsumeStamina(cost)
		gainExp := int64(actualRounds) * 2500
		gainCoins := int64(actualRounds) * 12000
		session.AddRewards(gainExp, gainCoins)
		if totalCostStamina != nil {
			*totalCostStamina += cost
		}
		if totalGainExp != nil {
			*totalGainExp += gainExp
		}
		if totalGainCoins != nil {
			*totalGainCoins += gainCoins
		}
		return true, actualRounds, nil

	case protocol.PracticeResultNotEnoughPower:
		// 同上：仅在本地体力已接近耗尽时才清零，避免服务端单次拒绝污染本地状态。
		if session.GetStamina() <= 5 {
			session.SetStamina(0)
			session.SetExtraStamina(0)
		} else {
			slog.Warn("扫荡返回体力不足但本地体力充足，保留本地体力",
				"role_id", session.RoleID(), "local_stamina", session.GetStamina())
		}
		return false, 0, scheduler.ErrStaminaDepleted

	case protocol.PracticeResultBagFull:
		return false, 0, scheduler.ErrBagFull

	default:
		return false, 0, fmt.Errorf("practice rejected with code %d", m25Res.Result)
	}
}

func resetTownScene(session *client.RoleSession) {
	// 在神仙道官方客户端架构中，通关剧情副本或任务流转退出后直接留在当前主界面，
	// 严禁在会话运行中途重放登录期握手包 (0x0048/0x0063/0x00A50000/0x0002)，
	// 否则会重置网关状态机，导致后续六道轮回等战斗协议被服务端静默丢弃。
}

// missionRequiredLevel 计算剧情关卡 MissionLock 对应的角色等级门槛。
// Pieb.db 约定 MissionLock = 等级 * 1000 + 章节内关卡序号 (如 20080 表示 20 级章节第 8 关)，
// 因此 20080 在角色等级 20 时即可挑战，21010 需要等级 21。
func missionRequiredLevel(missionLock int) int {
	if missionLock <= 0 {
		return 0
	}
	return missionLock / 1000
}

// missionLevelLocked 判断角色等级是否尚未达到剧情关卡解锁门槛。
// playerLevel <= 0 表示等级未知，此时不做客户端拦截，交由服务端裁决。
func missionLevelLocked(missionLock, playerLevel int) bool {
	if playerLevel <= 0 {
		return false
	}
	return missionRequiredLevel(missionLock) > playerLevel
}

// challengeStoryMission 使用 Module 4 (Mod_Mission_Base) 完整五步状态机通关指定剧情关卡：
// enter_mission (0x00040001) -> fight_monster (0x00040002) -> rank_mission (0x00040003) -> open_box (0x00040004) -> pickup_award (0x00040005)。
// fetchMissionSections 拉取章节关卡列表，带一次快速重试。
//
// 2026-11-14 线上：同一会话中 get_sections(21) 偶发超时（随后重放同载荷即可成功），
// 属于网关瞬时丢包而非参数错误。超时缩短为 3 秒并重试一次，避免整轮任务被跳过。
// resyncDungeonChain 用 0/2 全量角色资产重建 PrevAct 链。
//
// 用户回包被丢弃后服务端锚点会停在旧动作，后续 enter_town/get_sections 全部超时；
// 依次尝试候选锚点，直到 0/2 被服务端处理（LastAck 变为 0/2）。
func resyncDungeonChain(ctx context.Context, session *client.RoleSession) bool {
	candidates := []uint32{
		session.LastAckActionID(),
		session.LastActionID(),
		protocol.ActionIDPlayerGetInfo,
		protocol.ActionIDPlayerHeartbeat,
		0x00A50000, // Mod 165 扩展模块初始化
		0x00000063, // Mod 0 Act 99 场景进阶
		0x00000048, // Mod 0 Act 72 城镇初始化
	}
	seen := make(map[uint32]bool, len(candidates))
	for _, cand := range candidates {
		if cand == 0 || seen[cand] {
			continue
		}
		seen[cand] = true
		cCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		resp, err := session.Call(cCtx,
			protocol.BuildPlayerGetInfoPacket(int32(cand)), protocol.ActionIDPlayerGetInfo)
		cancel()
		if err == nil && resp != nil && session.LastAckActionID() == protocol.ActionIDPlayerGetInfo {
			slog.Info("任务推图 PrevAct 链重建成功", "role_id", session.RoleID(),
				"anchor", fmt.Sprintf("0x%08X", cand))
			return true
		}
	}
	return false
}

// missionSectionsCallTimeout 单次 4/0 章节列表请求的应答等待时间。
//
// 4/0 的应答延迟多在 5~6 秒之间（2026-11-17 s1 实测：5 秒超时后约 0.6 秒
// 迟到回包，把 lastRx 更新为 0x00040000，紧随其后的 4/1 进入才成功），
// 因此放宽到 8 秒以捕获迟到应答。变量形式便于单测缩短。
var missionSectionsCallTimeout = 8 * time.Second

// questSectionsDownTTL 同一地图 4/0 连续失败后的静默期。
//
// 同一轮调度里多个任务常共用同一地图；4/0 被丢弃时逐个任务重试会浪费
// 数十秒并污染 PrevAct 链（2026-11-17 s1 实测：同轮两个任务各探测两轮）。
// 失败后 10 分钟内对该地图直接返回错误，交给直接挑战兜底，保证周期时长可控。
var questSectionsDownTTL = 10 * time.Minute

type sectionsDownEntry struct {
	mu sync.Mutex
	m  map[int32]time.Time
}

// questSectionsDown 按会话记录 4/0 连续失败的地图与截止时间。
var questSectionsDown sync.Map // *client.RoleSession -> *sectionsDownEntry

func sectionsDownEntryFor(session *client.RoleSession) *sectionsDownEntry {
	if v, ok := questSectionsDown.Load(session); ok {
		return v.(*sectionsDownEntry)
	}
	actual, _ := questSectionsDown.LoadOrStore(session, &sectionsDownEntry{m: make(map[int32]time.Time)})
	return actual.(*sectionsDownEntry)
}

// questSectionsRecentlyDown 返回该地图是否仍处于 4/0 静默期。
func questSectionsRecentlyDown(session *client.RoleSession, mapID int32) (time.Time, bool) {
	e := sectionsDownEntryFor(session)
	e.mu.Lock()
	defer e.mu.Unlock()
	until, ok := e.m[mapID]
	if !ok {
		return time.Time{}, false
	}
	if time.Now().After(until) {
		delete(e.m, mapID)
		return time.Time{}, false
	}
	return until, true
}

// waitForMod4Response 等待最近收到的包来自 Mod 4（4/0 章节列表回包）。
//
// 服务端对 get_sections 的应答常在超时后 0.5~2 秒才到达；进入剧情副本时
// 会话层会用 lastRx 覆盖 PrevAct，若该值不是 Mod 4 则 4/1 被静默丢弃。
// 返回 true 表示已在超时内观察到 Mod 4 回包。
func waitForMod4Response(ctx context.Context, session *client.RoleSession, timeout time.Duration) bool {
	if session == nil || timeout <= 0 {
		return false
	}
	if session.LastRxActionID()>>16 == 4 {
		return true
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(100 * time.Millisecond):
		}
		if session.LastRxActionID()>>16 == 4 {
			return true
		}
	}
	return false
}

func markQuestSectionsDown(session *client.RoleSession, mapID int32, ttl time.Duration) {
	e := sectionsDownEntryFor(session)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.m[mapID] = time.Now().Add(ttl)
}

func clearQuestSectionsDown(session *client.RoleSession, mapID int32) {
	e := sectionsDownEntryFor(session)
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.m, mapID)
}

func fetchMissionSections(ctx context.Context, session *client.RoleSession, mapID int32) (*protocol.Packet, error) {
	if until, down := questSectionsRecentlyDown(session, mapID); down {
		return nil, fmt.Errorf("dungeon: sections for map %d recently dropped (until %s)",
			mapID, until.Format(time.RFC3339))
	}
	// 3/9 任务列表回包后立刻发 4/0 会被网关丢弃（2026-11-14 线上：间隔 0ms 丢包，
	// 间隔 600ms 稳定回包），因此首发前统一留出 500ms 间隔。
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(500 * time.Millisecond):
	}
	var lastErr error
	recoverAnchor := uint32(0)
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			// 2026-11-15 线上：4/0 超时后直接重放仍会丢包。探针实测以「失败
			// 的 4/0」作为显式 PrevAct 的 enter_town 能恢复 Mod 4 上下文；
			// 若仍失败，再用 0/2 重建链后重试同一锚点。
			if townID, ok := enterQuestTownSceneWithAnchor(ctx, session, recoverAnchor); ok {
				slog.Info("章节列表重试前再次复位城镇场景",
					"role_id", session.RoleID(), "map_id", townID, "target_map", mapID,
					"anchor", fmt.Sprintf("0x%08X", recoverAnchor))
			} else if resyncDungeonChain(ctx, session) {
				slog.Info("章节列表重试前已重建 PrevAct 链", "role_id", session.RoleID())
				if townID, ok := enterQuestTownSceneWithAnchor(ctx, session, recoverAnchor); ok {
					slog.Info("章节列表重试前再次复位城镇场景",
						"role_id", session.RoleID(), "map_id", townID, "target_map", mapID,
						"anchor", fmt.Sprintf("0x%08X", recoverAnchor))
				}
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
		}
		// 4/0 的应答延迟在 5~6 秒之间（2026-11-17 s1 实测：5 秒超时后约 0.6 秒
		// 迟到回包，把 lastRx 更新为 0x00040000，紧随其后的 4/1 进入才成功）。
		// 5 秒会漏掉这部分迟到应答；放宽到 8 秒可直接拿到正常回包，避免
		// 走重同步/直接挑战兜底。
		secCtx, secCancel := context.WithTimeout(ctx, missionSectionsCallTimeout)
		resp, err := session.Call(secCtx,
			protocol.BuildMissionGetSectionsPacket(mapID, questPrevAct(session)),
			protocol.ActionIDMissionGetSections)
		secCancel()
		if err == nil && resp != nil {
			clearQuestSectionsDown(session, mapID)
			return resp, nil
		}
		lastErr = err
		if lastErr == nil {
			lastErr = errors.New("dungeon: empty mission sections response")
		}
		slog.Info("章节列表请求无回包", "role_id", session.RoleID(),
			"map_id", mapID, "attempt", attempt+1,
			"last_action", fmt.Sprintf("0x%08X", session.LastActionID()),
			"last_ack", fmt.Sprintf("0x%08X", session.LastAckActionID()),
			"err", lastErr)
		if attempt == 0 {
			// 保留失败 4/0 的动作号；后续 0/2 重同步会覆盖 LastAction。
			recoverAnchor = session.LastActionID()
		}
		if errSessionTerminated(ctx, err) {
			return nil, lastErr
		}
	}
	markQuestSectionsDown(session, mapID, questSectionsDownTTL)
	questContextBad.Store(session, true)
	return nil, lastErr
}

func challengeStoryMission(
	ctx context.Context,
	session *client.RoleSession,
	m *dictionary.Mission,
	ensureStamina func() bool,
	totalCostStamina *int,
	totalGainExp *int64,
	totalGainCoins *int64,
	clearedMissions map[int]bool,
) bool {
	if m == nil {
		return false
	}
	if ctx.Err() != nil || session.State() == client.StateClosed {
		return false
	}

	playerLevel := session.GetPlayerState().Level
	requiredLevel := missionRequiredLevel(m.MissionLock)
	if missionLevelLocked(m.MissionLock, playerLevel) {
		slog.Warn("角色等级未达到剧情关卡解锁门槛，跳过副本进入",
			"role_id", session.RoleID(),
			"mission_id", m.ID,
			"name", m.Name,
			"mission_lock", m.MissionLock,
			"required_level", requiredLevel,
			"player_level", playerLevel,
		)
		return false
	}

	var monsterTeams []int32
	for _, part := range strings.Split(m.Monsters, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if v, err := strconv.Atoi(part); err == nil && v > 0 {
			monsterTeams = append(monsterTeams, int32(v))
		}
	}
	if len(monsterTeams) == 0 {
		slog.Warn("关卡缺失怪物编队配置，无法发起挑战", "mission_id", m.ID, "name", m.Name)
		return false
	}

	if session.GetBaseStamina() < 5 && !ensureStamina() {
		return false
	}

	// 0. 激活章节关卡列表上下文 (Mod 4, Act 0: get_sections)，对齐官方前置调用链
	// 精英关卡不在 get_sections 章节列表中，跳过该前置请求，直接进入。
	if m.Type != 1 && session.LastAckActionID() != protocol.ActionIDMissionGetSections {
		mapID := int32(m.MapID)
		if mapID <= 0 {
			mapID = int32(m.SectionID)
		}
		_, _ = fetchMissionSections(ctx, session, mapID)
		if ctx.Err() != nil || session.State() == client.StateClosed {
			return false
		}
	}

	// 服务端常在 4/0 超时后 0.5~2 秒才回包。若最近收到的包不是 Mod 4 响应，
	// 短暂等待迟到回包：会话层发包时用 lastRx 覆盖 PrevAct，进入剧情副本要求
	// 该值为 Mod 4（2026-11-17 实测 lastRx=0x00040000 稳定成功，0x00000002 必失败）。
	if m.Type != 1 {
		waitForMod4Response(ctx, session, 2*time.Second)
	}

	// 官方抓包 (/tmp/story_repeat.pcapng)：enter 之前先发 4/25 场景准备（仅 4 字节
	// PrevAct），enter 的尾链随之对齐为 4/25。缺失该步时服务端仍可能放行 enter，
	// 但后续战斗阶段会拒绝，因此这里作为固定前置。
	plotPrepCtx, plotPrepCancel := context.WithTimeout(ctx, storyMissionCallTimeout)
	plotPrepResp, plotPrepErr := session.Call(plotPrepCtx,
		protocol.BuildMissionPlotPrepPacket(), protocol.ActionIDMissionPlotPrep)
	plotPrepCancel()
	if plotPrepErr != nil || plotPrepResp == nil {
		if errSessionTerminated(ctx, plotPrepErr) {
			return false
		}
		slog.Info("进入副本前场景准备 4/25 无回包，继续尝试进入",
			"role_id", session.RoleID(), "mission_id", m.ID, "err", plotPrepErr)
	}

	// 1. 进入副本 (Mod 4, Act 1: enter_mission, 前置对齐 ActionIDMissionGetSections)
	prevAct := questPrevAct(session)
	enterPkt := protocol.BuildMissionPlotEnterPacket(int32(m.ID), prevAct)
	slog.Info("准备进入剧情副本",
		"role_id", session.RoleID(),
		"mission_id", m.ID,
		"name", m.Name,
		"mission_lock", m.MissionLock,
		"required_level", requiredLevel,
		"player_level", playerLevel,
		"stamina", session.GetBaseStamina(),
		"stamina_total", session.GetStamina(),
		"prev_act", fmt.Sprintf("0x%08X", prevAct),
		"payload_hex", hex.EncodeToString(enterPkt.Payload),
	)
	enterCtx, enterCancel := context.WithTimeout(ctx, storyMissionCallTimeout)
	enterResp, enterErr := session.Call(enterCtx, enterPkt, protocol.ActionIDMissionPlotEnter)
	enterCancel()

	if enterErr != nil || enterResp == nil || len(enterResp.Payload) == 0 {
		if errSessionTerminated(ctx, enterErr) {
			return false
		}
		if enterErr != nil {
			// 网络层超时说明服务端已丢弃本会话的 Mod 4 上下文：标记后由
			// quest_cycle 统一重连一次再继续，避免逐关卡盲打。
			// 2026-11-26 实测：单轮 8~9 个关卡各空等 8 秒（107 次/19.5h）。
			questContextBad.Store(session, true)
		}
		slog.Warn("进入剧情副本未收到有效响应",
			"role_id", session.RoleID(),
			"mission_id", m.ID,
			"name", m.Name,
			"mission_lock", m.MissionLock,
			"required_level", requiredLevel,
			"player_level", playerLevel,
			"prev_act", fmt.Sprintf("0x%08X", prevAct),
			"payload_hex", hex.EncodeToString(enterPkt.Payload),
			"err", enterErr,
		)
		resetTownScene(session)
		return false
	}

	enterStatus := enterResp.Payload[0]
	if enterStatus == 20 { // AWARD_BOX_NO_OPEN: 上次通关宝箱尚未开启或拾取，先行清理再重进
		// 补开上次通关宝箱并拾取奖励：奖励类请求必须解析回包，避免静默丢弃
		// 导致「关卡显示已通关但没有资源进账」。
		boxCtx, boxCancel := context.WithTimeout(ctx, 8*time.Second)
		boxResp, boxErr := session.Call(boxCtx, protocol.BuildMissionOpenBoxPacket(protocol.ActionIDMissionRank), protocol.ActionIDMissionOpenBox)
		boxCancel()
		if boxErr != nil || boxResp == nil || len(boxResp.Payload) == 0 {
			slog.Warn("补开通关宝箱无回包", "role_id", session.RoleID(), "mission_id", m.ID, "err", boxErr)
		} else if boxResp.Payload[0] != 0 {
			slog.Info("补开通关宝箱结果码非 0", "role_id", session.RoleID(),
				"mission_id", m.ID, "result", boxResp.Payload[0])
		}
		pickCtx, pickCancel := context.WithTimeout(ctx, 8*time.Second)
		pickResp, pickErr := session.Call(pickCtx, protocol.BuildMissionPickupAwardPacket(protocol.ActionIDMissionOpenBox), protocol.ActionIDMissionPickupAward)
		pickCancel()
		if pickErr != nil || pickResp == nil || len(pickResp.Payload) == 0 {
			slog.Warn("拾取通关宝箱奖励无回包", "role_id", session.RoleID(), "mission_id", m.ID, "err", pickErr)
		} else if pickResp.Payload[0] != 0 {
			slog.Info("拾取通关宝箱奖励结果码非 0", "role_id", session.RoleID(),
				"mission_id", m.ID, "result", pickResp.Payload[0])
		}

		enterPkt = protocol.BuildMissionPlotEnterPacket(int32(m.ID), questPrevAct(session))
		enterCtxR, enterCancelR := context.WithTimeout(ctx, 8*time.Second)
		enterResp, enterErr = session.Call(enterCtxR, enterPkt, protocol.ActionIDMissionPlotEnter)
		enterCancelR()
		if enterErr != nil || enterResp == nil || len(enterResp.Payload) == 0 {
			return false
		}
		enterStatus = enterResp.Payload[0]
	}

	if enterStatus == protocol.MissionResultNotEnoughPower || enterStatus == 18 { // LESS_POWER == 18
		if session.GetBaseStamina() > 5 {
			// 本地体力充足却收到 LESS_POWER：服务端状态与本地快照不一致，
			// 清零本地体力会掩盖真实值并拖垮后续周期；记录后放弃本关。
			slog.Warn("进入剧情副本返回体力不足但本地体力充足，跳过本关",
				"role_id", session.RoleID(), "mission_id", m.ID,
				"base_stamina", session.GetBaseStamina(),
				"total_stamina", session.GetStamina())
			return false
		}
		session.SetStamina(0)
		session.SetExtraStamina(0)
		if !ensureStamina() {
			return false
		}
		enterPkt = protocol.BuildMissionPlotEnterPacket(int32(m.ID), questPrevAct(session))
		enterCtx2, enterCancel2 := context.WithTimeout(ctx, 8*time.Second)
		enterResp, enterErr = session.Call(enterCtx2, enterPkt, protocol.ActionIDMissionPlotEnter)
		enterCancel2()
		if enterErr != nil || enterResp == nil || len(enterResp.Payload) == 0 {
			return false
		}
		enterStatus = enterResp.Payload[0]
	}

	if enterStatus != protocol.MissionResultSuccess {
		slog.Warn("进入剧情副本未成功", "role_id", session.RoleID(), "mission_id", m.ID, "name", m.Name, "status", fmt.Sprintf("0x%02X", enterStatus))
		return false
	}

	// 依据服务端 enter_mission 实时回包动态获取当前关卡待挑战怪物波次列表
	// 官方回包规范: [UByte(status), Int32(mid), Int32(stam), Byte(b), Int32(i1), Int32(i2), Uint16(mCnt), ...[Int32(mt)]]
	// 实测 2026 网关首通时 mCnt 恒为 0，怪物编队需回退 Pieb.db Missions.monsters 静态配置，
	// 不能据此判定关卡已通关（是否通关以 get_sections status 为准）。
	var activeMonsterTeams []int32
	if len(enterResp.Payload) >= 20 {
		mCnt := binary.BigEndian.Uint16(enterResp.Payload[18:20])
		if mCnt == 0 {
			slog.Info("enter_mission 未下发怪物编队，回退关卡字典配置",
				"role_id", session.RoleID(),
				"mission_id", m.ID,
				"name", m.Name,
				"configured_waves", len(monsterTeams),
			)
		} else {
			r := protocol.NewReader(enterResp.Payload[20:])
			for i := uint16(0); i < mCnt && r.Remaining() >= 4; i++ {
				mt, _ := r.ReadInt32()
				activeMonsterTeams = append(activeMonsterTeams, mt)
			}
		}
	}
	if len(activeMonsterTeams) > 0 {
		monsterTeams = activeMonsterTeams
	}

	// 官方抓包：enter 成功后发送 4/24 战斗场景准备（8 字节业务参数，无 PrevAct 尾链）。
	// 必须走 SendVerbatim，避免 Module 4 的自动尾链覆盖业务参数 0x02F80001。
	if err := session.SendVerbatim(protocol.BuildMissionFightPrepPacket(int32(m.ID))); err != nil {
		slog.Warn("剧情副本战斗场景准备 4/24 发送失败",
			"role_id", session.RoleID(), "mission_id", m.ID, "err", err)
	}
	select {
	case <-ctx.Done():
		return false
	case <-time.After(100 * time.Millisecond):
	}

	// 2. 逐波次挑战副本内怪物编队 (Mod 4, Act 2: fight_monster)
	anyWaveSuccess := false
	for idx, mtID := range monsterTeams {
		if idx > 0 {
			// 服务端连续战斗存在冷却保护 (对齐客户端 CommonConstant.DelayTimeToWar)
			select {
			case <-ctx.Done():
				resetTownScene(session)
				return anyWaveSuccess
			case <-time.After(1000 * time.Millisecond):
			}
		}

		// 官方抓包：每场战斗前 60ms 查询任务战斗冷却 (0/41, warType=13)，
		// fight_monster 的 PrevAct 必须等于该请求动作 0x00000029；缺失时
		// 服务端静默丢弃战斗（2026-11-27 实机 107 次/19.5h 的直接原因）。
		if err := session.Send(protocol.BuildPlayerGetWarCdTimePacket(protocol.WarCdTypeMission, 0)); err != nil {
			if errSessionTerminated(ctx, err) {
				return anyWaveSuccess
			}
			slog.Warn("剧情副本战斗冷却查询发送失败",
				"role_id", session.RoleID(), "mission_id", m.ID, "monster_team_id", mtID, "err", err)
		}
		select {
		case <-ctx.Done():
			resetTownScene(session)
			return anyWaveSuccess
		case <-time.After(60 * time.Millisecond):
		}

		fightPkt := protocol.BuildMissionFightMonsterPacket(mtID, 0, 0, questPrevAct(session))
		fightCtx, fightCancel := context.WithTimeout(ctx, storyMissionCallTimeout)
		fightResp, fightErr := session.Call(fightCtx, fightPkt, protocol.ActionIDMissionFightMonster)
		fightCancel()

		if fightErr != nil || fightResp == nil || len(fightResp.Payload) == 0 {
			if errSessionTerminated(ctx, fightErr) {
				return anyWaveSuccess
			}
			if anyWaveSuccess {
				slog.Info("剧情副本已击败当前可用波次怪物，返回城镇同步进度", "role_id", session.RoleID(), "mission_id", m.ID, "monster_team_id", mtID)
				resetTownScene(session)
				return true
			}
			if fightErr != nil {
				// 同上：fight_monster 无回包意味着 Mod 4 上下文已不可用。
				questContextBad.Store(session, true)
			}
			slog.Warn("剧情副本挑战怪物未收到响应", "role_id", session.RoleID(), "mission_id", m.ID, "monster_team_id", mtID, "err", fightErr)
			resetTownScene(session)
			return false
		}

		// 4/2 回包是整场战斗日志（官方抓包 14KB，整包 zlib），首/末字节不是简单结果码；
		// 收到回包即说明服务端已执行战斗。胜负与奖励以结算 4/3 与任务进度同步为准。
		headLen := len(fightResp.Payload)
		if headLen > 8 {
			headLen = 8
		}
		tail := fightResp.Payload
		if len(tail) > 4 {
			tail = tail[len(tail)-4:]
		}
		slog.Info("剧情副本战斗回包",
			"role_id", session.RoleID(),
			"mission_id", m.ID,
			"monster_team_id", mtID,
			"wave", fmt.Sprintf("%d/%d", idx+1, len(monsterTeams)),
			"payload_len", len(fightResp.Payload),
			"head_hex", hex.EncodeToString(fightResp.Payload[:headLen]),
			"tail_hex", hex.EncodeToString(tail),
		)
		anyWaveSuccess = true
	}

	// 3. 结算星级评价 (Mod 4, Act 3: rank_mission)
	// 回包解析铁律: 结算三步必须持有回包并校验非空。任一步超时都说明服务端仍停留在副本态，
	// 此时继续发起后续 Mod 4/25 请求会被网关静默丢弃，必须立即中止并交由外层重试。
	rankCtx, rankCancel := context.WithTimeout(ctx, 8*time.Second)
	rankResp, rankErr := session.Call(rankCtx, protocol.BuildMissionRankPacket(protocol.ActionIDMissionFightMonster), protocol.ActionIDMissionRank)
	rankCancel()
	if rankErr != nil || rankResp == nil {
		if errSessionTerminated(ctx, rankErr) {
			return false
		}
		slog.Warn("副本星级结算未收到有效响应", "role_id", session.RoleID(), "mission_id", m.ID, "name", m.Name, "err", rankErr)
		resetTownScene(session)
		return false
	}

	// 4. 开启通关宝箱 (Mod 4, Act 4: open_box)
	boxCtx, boxCancel := context.WithTimeout(ctx, 8*time.Second)
	boxResp, boxErr := session.Call(boxCtx, protocol.BuildMissionOpenBoxPacket(protocol.ActionIDMissionRank), protocol.ActionIDMissionOpenBox)
	boxCancel()
	if boxErr != nil || boxResp == nil || len(boxResp.Payload) == 0 {
		if errSessionTerminated(ctx, boxErr) {
			return false
		}
		slog.Warn("副本通关宝箱开启未收到有效响应", "role_id", session.RoleID(), "mission_id", m.ID, "name", m.Name, "err", boxErr)
		resetTownScene(session)
		return false
	}
	// 回包解析铁律：open_box 首字节为结果码，0=成功。非成功时服务端不会掉落奖励，
	// 若继续拾取并本地记账就会出现「任务显示通关但资源没变化」。
	if boxResp.Payload[0] != 0 {
		slog.Warn("副本通关宝箱开启失败，放弃本关记账",
			"role_id", session.RoleID(), "mission_id", m.ID, "name", m.Name,
			"result", boxResp.Payload[0])
		resetTownScene(session)
		return false
	}

	// 5. 拾取宝箱奖励 (Mod 4, Act 5: pickup_award)
	pickCtx, pickCancel := context.WithTimeout(ctx, 8*time.Second)
	pickResp, pickErr := session.Call(pickCtx, protocol.BuildMissionPickupAwardPacket(protocol.ActionIDMissionOpenBox), protocol.ActionIDMissionPickupAward)
	pickCancel()
	if pickErr != nil || pickResp == nil || len(pickResp.Payload) == 0 {
		if errSessionTerminated(ctx, pickErr) {
			return false
		}
		slog.Warn("副本结算奖励拾取未收到有效响应", "role_id", session.RoleID(), "mission_id", m.ID, "name", m.Name, "err", pickErr)
		resetTownScene(session)
		return false
	}
	// 回包解析铁律：pickup_award 首字节为结果码。
	//   0  = 成功领取；
	//   23 = 重复挑战已通关副本（任务需重复击杀）时服务端返回「奖励已领取/无奖励」，
	//        关卡本身已结算，应按通关记账但不重复累加本地收益。
	rewardGranted := false
	switch pickResp.Payload[0] {
	case 0:
		rewardGranted = true
	case protocol.MissionResultAwardAlreadyPicked:
		slog.Info("副本重复挑战无新增奖励，按已通关记账",
			"role_id", session.RoleID(), "mission_id", m.ID, "name", m.Name,
			"result", pickResp.Payload[0])
	default:
		slog.Warn("副本结算奖励拾取失败，放弃本关记账",
			"role_id", session.RoleID(), "mission_id", m.ID, "name", m.Name,
			"result", pickResp.Payload[0])
		resetTownScene(session)
		return false
	}

	// 通关完成，返回城镇场景
	resetTownScene(session)

	// 预留战斗冷却间隔，避免下一关卡首波战斗触发服务端 CD
	kit.Sleep(ctx, 1200*time.Millisecond)

	remStamina, _ := session.ConsumeStamina(5)
	*totalCostStamina += 5
	if rewardGranted {
		session.AddRewards(2500, 12000)
		*totalGainExp += 2500
		*totalGainCoins += 12000
	}
	clearedMissions[m.ID] = true

	slog.Info("任务关卡推进挑战成功",
		"role_id", session.RoleID(),
		"mission_id", m.ID,
		"mission_name", m.Name,
		"waves", len(monsterTeams),
		"reward_granted", rewardGranted,
		"remaining_stamina", remStamina,
	)
	return true
}
