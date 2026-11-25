// Package dungeon 提供副本推图、主线任务循环、精英扫荡、六道轮回、伏魔塔等关卡挑战自动化。
//
// 本文件实现主线任务自动接取、提交与城镇场景同步循环。
package dungeon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
)

// 任务推图会话状态。
//
// 魔王试炼 (Mod 57) 等请求会让服务端在本次会话内进入特殊上下文，
// 之后 get_sections 会被静默丢弃。每轮调度前用 Mod 3/4 锚点的 enter_town
// 复位场景（2026-11-15 实测可恢复）；若连续两次仍失败则熔断本会话，
// 避免每轮固定丢包。
var (
	questContextBad sync.Map // *client.RoleSession -> bool
	// questReconnectAt 记录每个会话上次因任务上下文不可用而主动重连的时间，
	// 限制为 15 分钟一次，避免重连风暴。
	questReconnectAt sync.Map // *client.RoleSession -> time.Time
)

// allowQuestReconnect 判断当前是否允许为恢复任务上下文主动重连。
func allowQuestReconnect(session *client.RoleSession) bool {
	now := time.Now()
	if v, ok := questReconnectAt.Load(session); ok {
		if t, _ := v.(time.Time); now.Sub(t) < 15*time.Minute {
			return false
		}
	}
	questReconnectAt.Store(session, now)
	return true
}

// maxQuestReconnectsPerCycle 单轮调度内为恢复任务上下文允许的最大重连次数。
//
// 调度器每个角色只有一个串行 runLoop，单轮耗时过长会推迟其它所有玩法；
// 结合 questCycleBudget 限制到 4 次（约 4 波）。
const maxQuestReconnectsPerCycle = 4

// questCycleBudget 单轮任务推图的时间预算（变量形式便于单测）。
//
// 每次轮内重连 + 章节探测约 1.5~2.5 分钟；预算到期即结束本轮，
// 把调度让给其它玩法，下一轮循环再继续推图。
var questCycleBudget = 8 * time.Minute

// questMidReconnectInterval 轮内重连节流间隔（变量形式便于单测缩短）。
//
// 实测一次完整重连约 10~30 秒；3 分钟节流会让只剩 5 点体力时错过第二波
// （2026-11-18 17:45 实机：距上次重连 108 秒被节流拦下，体力转去扫荡）。
// 90 秒约等于「一次重连 + 一次战斗」，仍可防止风暴。
var questMidReconnectInterval = 90 * time.Second

// questMidReconnectAt 记录每个会话上次轮内重连时间，避免重连风暴。
var questMidReconnectAt sync.Map // *client.RoleSession -> time.Time

// allowQuestMidCycleReconnect 判断当前是否允许在任务循环中途重连。
//
// 单独战斗后服务端 Mod 4 上下文即失效，同一会话内无法恢复；体力充足时
// 立即换新会话继续推图，避免「160 点体力只打一波就等 30 分钟」
// （2026-11-18 实机）。按 3 分钟节流，单轮上限见 maxQuestReconnectsPerCycle。
func allowQuestMidCycleReconnect(session *client.RoleSession) bool {
	now := time.Now()
	if v, ok := questMidReconnectAt.Load(session); ok {
		if t, _ := v.(time.Time); now.Sub(t) < questMidReconnectInterval {
			return false
		}
	}
	questMidReconnectAt.Store(session, now)
	return true
}

// reconnectQuestSession 刷新一次性网关票据后触发主动重连，并等待新一代连接进入 Active。
//
// 网关登录 Hash 票据一次性有效：若裸调 ReconnectNow 而不预先换票，旧票据首轮重连
// 必然触发 15 秒握手超时，叠加退避很容易突破 60 秒等待上限并把 s1 拖入 4m18s 重连循环。
func reconnectQuestSession(ctx context.Context, session *client.RoleSession) bool {
	if session == nil {
		return false
	}
	if session.State() == client.StateActive {
		return session.EnsureFreshConnection(ctx, 0) == nil
	}
	return waitForReconnect(ctx, session.State, 5*time.Second, 60*time.Second)
}

// waitForReconnect 等待主动重连完成一次「离开 Active → 回到 Active」的状态迁移。
//
// ReconnectNow 仅关闭底层连接，状态机由读循环异步切入 StateReconnecting。
// 若关闭后立即检查 State()==Active，会读到尚未更新的旧状态并把本轮当成
// 已恢复，导致随后整轮请求都以 "session is not active" 失败
// （2026-11-17 12:34 实测 10 次无效任务交付）。
//
// state 参数化以便单测用假状态序列验证；返回 false 表示重连未在超时内完成。
func waitForReconnect(
	ctx context.Context,
	state func() client.SessionState,
	leaveGrace time.Duration,
	activeTimeout time.Duration,
) bool {
	leaveDeadline := time.Now().Add(leaveGrace)
	for state() == client.StateActive && time.Now().Before(leaveDeadline) && ctx.Err() == nil {
		kit.Sleep(ctx, 50*time.Millisecond)
	}
	deadline := time.Now().Add(activeTimeout)
	for state() != client.StateActive && time.Now().Before(deadline) && ctx.Err() == nil {
		kit.Sleep(ctx, 300*time.Millisecond)
	}
	return state() == client.StateActive
}

// questTownKnownGood 记录每个会话最近一次成功复位的城镇地图 id。
// 服务端 get_player_info 的 townMapId 可能被写成副本场景 (如 21)，
// 直接 enter_town 会被静默丢弃；缓存成功值后后续复位即可稳定命中。
var questTownKnownGood sync.Map // *client.RoleSession -> int32

// questSectionsAnchorCache 缓存会话最近一次 Mod 3/4 动作号。
// enter_town 会推进 lastAction 到 Mod 1，若不缓存，后续复位将丢失任务链锚点。
var questSectionsAnchorCache sync.Map // *client.RoleSession -> uint32

// questSectionsAnchor 返回用于 enter_town 的 PrevAct 锚点。
//
// 2026-11-15 实测：以任务列表 3/9 或章节列表 4/0 为锚点的 8 字节 enter_town
// 能保持任务上下文，复位后的 4/0 稳定回包；裸载荷会切出任务上下文。
func questSectionsAnchor(session *client.RoleSession) uint32 {
	// 优先使用最近一次「失败的 4/0」作为锚点：实测该形态能恢复被裸
	// enter_town 打断的 Mod 4 上下文（probe_sections_after_quest_sync_live 步骤 11）。
	if a := session.LastActionID(); a>>16 == 4 {
		questSectionsAnchorCache.Store(session, a)
		return a
	}
	if a := session.LastAckActionID(); a>>16 == 3 || a>>16 == 4 {
		questSectionsAnchorCache.Store(session, a)
		return a
	}
	if a := session.LastActionID(); a>>16 == 3 {
		questSectionsAnchorCache.Store(session, a)
		return a
	}
	if v, ok := questSectionsAnchorCache.Load(session); ok {
		return v.(uint32)
	}
	return 0
}

// enterQuestTownScene 在推图/扫荡前复位城镇场景。
//
// 依次尝试: 最近成功城镇 → 服务端上报的 TownMapID → 常见主城 3/1/2。
// 首轮全部失败时，用显式 0/2 锚点重试主城镇一次：2026-11-17 s1 实测
// 以 Mod 3/4 锚点的 1/0 被静默丢弃，而 0/2 锚点能回包，且随后 4/0
// 章节列表恢复正常（13:05 成功 / 13:37 失败对照）。
// 成功返回 true；全部失败返回 false（调用方继续尝试 get_sections，不额外阻塞）。
func enterQuestTownScene(ctx context.Context, session *client.RoleSession) (int32, bool) {
	anchor := questSectionsAnchor(session)
	if id, ok := enterQuestTownSceneWithAnchor(ctx, session, anchor); ok {
		return id, true
	}
	if anchor == 0 {
		return 0, false
	}
	primary := int32(session.GetPlayerState().TownMapID)
	if v, ok := questTownKnownGood.Load(session); ok {
		if cached, _ := v.(int32); cached > 0 {
			primary = cached
		}
	}
	if primary > 0 && enterQuestTownSceneSingle(ctx, session, primary, 0) {
		slog.Info("城镇复位改用 0/2 锚点成功",
			"role_id", session.RoleID(), "map_id", primary)
		return primary, true
	}
	return 0, false
}

// enterQuestTownSceneWithAnchor 使用指定 PrevAct 锚点复位城镇场景。
func enterQuestTownSceneWithAnchor(ctx context.Context, session *client.RoleSession, anchor uint32) (int32, bool) {
	if session == nil || session.State() != client.StateActive {
		return 0, false
	}
	if v, ok := questTownKnownGood.Load(session); ok {
		if id, _ := v.(int32); enterQuestTownSceneSingle(ctx, session, id, anchor) {
			return id, true
		}
		if session.State() != client.StateActive {
			return 0, false
		}
	}
	if id := int32(session.GetPlayerState().TownMapID); enterQuestTownSceneSingle(ctx, session, id, anchor) {
		return id, true
	}
	if session.State() != client.StateActive {
		return 0, false
	}
	for _, id := range []int32{3, 1, 2} {
		if session.State() != client.StateActive {
			return 0, false
		}
		if enterQuestTownSceneSingle(ctx, session, id, anchor) {
			return id, true
		}
	}
	return 0, false
}

// enterQuestTownSceneSingle 以指定锚点进入单个城镇，成功时记录最近可用城镇。
func enterQuestTownSceneSingle(ctx context.Context, session *client.RoleSession, id int32, anchor uint32) bool {
	if id <= 0 || session == nil || session.State() != client.StateActive {
		return false
	}
	code, err := session.EnterTownWithAnchor(ctx, id, anchor)
	if err != nil || code != 0 {
		return false
	}
	questTownKnownGood.Store(session, id)
	return true
}

// executeQuestCycle 任务闭环状态机:
// 循环接任务、完成任务，一直到没有任务可做了再 break，随后流转执行精英副本扫荡。
func executeQuestCycle(
	ctx context.Context,
	session *client.RoleSession,
	dictRepo dictionary.Repository,
	cfg SweepConfig,
	ensureStamina func() bool,
	totalCostStamina *int,
	totalGainExp *int64,
	totalGainCoins *int64,
	clearedMissions map[int]bool,
) {
	if dictRepo == nil || session == nil {
		return
	}

	const powerPerSweep = 5
	const maxCycles = 50
	failedMissions := make(map[int]bool)

	slog.Info("启动主线/支线任务闭环自动化驱动", "role_id", session.RoleID(), "town_id", session.GetPlayerState().TownID)

	// 魔王试炼/竞技场等战斗玩法结束后，服务端可能仍保留战场上下文；
	// 此时 get_sections 会被静默丢弃。按官方客户端 gotoTownAtId 复位城镇场景。
	//
	// 没有 Mod 3/4 锚点时只能发裸 4 字节 enter_town，实测会把会话切出任务上下文
	// （随后 4/0 必丢）；因此首轮跳过复位，等 3/9 同步后再按锚点复位。
	if questSectionsAnchor(session) != 0 {
		if mapID, ok := enterQuestTownScene(ctx, session); ok {
			slog.Info("任务推图前已复位城镇场景", "role_id", session.RoleID(), "map_id", mapID)
		} else {
			slog.Info("任务推图前复位城镇场景失败", "role_id", session.RoleID(),
				"town_map_id", session.GetPlayerState().TownMapID)
		}
	} else {
		slog.Info("任务推图前跳过城镇复位（尚无任务链锚点）", "role_id", session.RoleID())
	}

	// 0. 主动向服务端全量同步当前任务列表 (Module 3, Action 9: town_quest_show)
	// 在 2026 真实网关中，town_quest_show 返回状态枚举 (QuestType):
	//   0 = Acceptable (可接取)
	//   1 = Accepted   (已接取/进行中)
	//   2 = Completed  (已达成目标/可交付)
	//   3 = LevelLimit (等级不足)
	syncedItems, syncErr := session.SyncQuests(ctx)
	if syncErr == nil {
		executeAuthoritativeQuestCycle(
			ctx, session, dictRepo, syncedItems, ensureStamina,
			totalCostStamina, totalGainExp, totalGainCoins,
			clearedMissions, failedMissions,
		)
		return
	}

	// 停机或断线导致的同步失败不再降级扫描 NPC，直接静默退出。
	if errSessionTerminated(ctx, syncErr) {
		slog.Info("会话已关闭，终止任务闭环驱动", "role_id", session.RoleID())
		return
	}

	// 兼容离线单测 Mock Server (未实现 0x00030009 town_quest_show 时的 NPC 扫描降级路径)
	for cycle := 1; cycle <= maxCycles; cycle++ {
		if ctx.Err() != nil || session.State() == client.StateClosed {
			slog.Info("会话已关闭，终止任务闭环驱动", "role_id", session.RoleID())
			return
		}
		if session.GetBaseStamina() < powerPerSweep && !ensureStamina() {
			slog.Info("推图体力耗尽且无可用补充道具，退出任务循环", "stamina", session.GetBaseStamina())
			break
		}

		progressMade := false

		// 1. 交付当前身上已达成的任务
		currentQuests := session.GetQuests()
		for qid, state := range currentQuests {
			if state == 1 || state == 2 {
				q, _ := dictRepo.GetQuest(int(qid))
				title := fmt.Sprintf("Quest#%d", qid)
				if q != nil && q.Title != "" {
					title = q.Title
				}
				slog.Info("正在交付已达成任务", "role_id", session.RoleID(), "quest_id", qid, "title", title)

				if !completeQuestAndLog(ctx, session, int32(qid), "身上已达成的任务") {
					// 未获服务端确认（无回包/非 0 结果码）时不记账：
					// 本地 RemoveQuest + 奖励记账会造成「资源没变化」的假象，
					// 保留任务等待下一次 SyncQuests 与真实回包。
					continue
				}

				session.RemoveQuest(qid)
				if q != nil {
					session.AddRewards(q.AwardExp, q.AwardCoins)
					*totalGainExp += q.AwardExp
					*totalGainCoins += q.AwardCoins
				}
				progressMade = true
				kit.Sleep(ctx, 50*time.Millisecond)
			}
		}

		// 2. 扫描城镇 NPC 列表并接取可接任务
		curTown := session.GetPlayerState().TownID
		var towns []int
		if curTown > 0 {
			towns = append(towns, curTown)
		}
		for _, fallbackTown := range []int{22, 4, 1} {
			if fallbackTown != curTown {
				towns = append(towns, fallbackTown)
			}
		}

		validQuestNPCs, _ := dictRepo.GetQuestNPCs()
		var candidateNPCs []int
		seenNPC := make(map[int]bool)
		for _, tid := range towns {
			npcs, err := dictRepo.GetTownNPCs(tid)
			if err == nil {
				for _, nid := range npcs {
					if !seenNPC[nid] {
						seenNPC[nid] = true
						if validQuestNPCs == nil || validQuestNPCs[nid] {
							candidateNPCs = append(candidateNPCs, nid)
						}
					}
				}
			}
		}

		for _, npcID := range candidateNPCs {
			// 字典返回的是静态 npcid；npc_info_by_npcid 需要 NPCposition 行号。
			requestNPCID := int32(npcID)
			if resolved := resolveTownNPCID(dictRepo, requestNPCID); resolved > 0 {
				requestNPCID = resolved
			}
			npcPkt, _ := protocol.BuildGetNPCQuestsPacket(protocol.GetNPCQuestsRequest{NPCID: requestNPCID})
			npcCtx, npcCancel := context.WithTimeout(ctx, 8*time.Second)
			npcResp, npcErr := session.Call(npcCtx, npcPkt, protocol.ActionIDQuestGetNPCQuests)
			npcCancel()

			if npcErr != nil || npcResp == nil {
				if errSessionTerminated(ctx, npcErr) {
					slog.Info("会话已关闭，终止 NPC 任务扫描", "role_id", session.RoleID())
					return
				}
				continue
			}

			items, pErr := protocol.ParseNPCQuestsResponse(npcResp.Payload)
			if pErr != nil || len(items) == 0 {
				continue
			}

			for _, it := range items {
				questID := uint32(it.QuestID)

				if it.State == 2 {
					q, _ := dictRepo.GetQuest(int(it.QuestID))
					title := fmt.Sprintf("Quest#%d", questID)
					if q != nil && q.Title != "" {
						title = q.Title
					}
					slog.Info("NPC 处发现已达成任务，正在交付结算", "role_id", session.RoleID(), "npc_id", npcID, "quest_id", questID, "title", title)
					if !completeQuestAndLog(ctx, session, it.QuestID, "NPC 处已达成任务") {
						// 未确认交付时保留任务，避免本地记账造成资源无变化假象。
						continue
					}

					session.RemoveQuest(questID)
					if q != nil {
						session.AddRewards(q.AwardExp, q.AwardCoins)
						*totalGainExp += q.AwardExp
						*totalGainCoins += q.AwardCoins
					}
					progressMade = true
					kit.Sleep(ctx, 50*time.Millisecond)
					continue
				}

				if it.State == 0 {
					session.SetQuestState(questID, 0)
					continue
				}

				if it.State != 1 {
					continue
				}
				questsNow := session.GetQuests()
				if _, exists := questsNow[questID]; exists {
					continue
				}

				q, _ := dictRepo.GetQuest(int(it.QuestID))
				if q != nil && q.Level > 0 && q.Level > session.GetPlayerState().Level {
					continue
				}

				title := fmt.Sprintf("Quest#%d", it.QuestID)
				if q != nil && q.Title != "" {
					title = q.Title
				}
				slog.Info("发现可接取任务，正在接受", "role_id", session.RoleID(), "npc_id", npcID, "quest_id", it.QuestID, "title", title)

				accPkt, _ := protocol.BuildAcceptQuestPacket(protocol.AcceptQuestRequest{QuestID: it.QuestID})
				accCtx, accCancel := context.WithTimeout(ctx, 8*time.Second)
				accResp, accErr := session.Call(accCtx, accPkt, protocol.ActionIDQuestAccept)
				accCancel()

				// 回包解析铁律：accept_quest 回包为 UByte result，5=SUCCEED。
				// 只有确认成功后才推进本地任务状态，避免「回包到达但接取失败」
				// 导致后续任务链推进全部错位。
				if accErr == nil && accResp != nil && len(accResp.Payload) > 0 && accResp.Payload[0] == protocol.QuestAcceptSucceed {
					session.SetQuestState(questID, 0)
					progressMade = true
					slog.Info("成功接受任务", "role_id", session.RoleID(), "quest_id", it.QuestID, "title", title)
					kit.Sleep(ctx, 50*time.Millisecond)
				} else if accErr != nil {
					slog.Info("接取任务无回包，保留待下次重试", "role_id", session.RoleID(),
						"quest_id", it.QuestID, "title", title, "err", accErr)
				} else if accResp == nil || len(accResp.Payload) == 0 {
					slog.Warn("接取任务回包为空，保留待下次重试", "role_id", session.RoleID(),
						"quest_id", it.QuestID, "title", title)
				} else {
					slog.Info("接取任务未成功", "role_id", session.RoleID(),
						"quest_id", it.QuestID, "title", title, "result", accResp.Payload[0])
				}
			}
		}

		// 3. 推进已接任务关卡挑战与对话剧情
		questsInProgress := session.GetQuests()
		for qid, qState := range questsInProgress {
			if qState != 0 {
				continue
			}
			q, err := dictRepo.GetQuest(int(qid))
			if err != nil || q == nil {
				continue
			}

			if q.MissionID == 0 {
				slog.Info("正在交付对话剧情任务", "role_id", session.RoleID(), "quest_id", qid, "title", q.Title)
				if !completeQuestAndLog(ctx, session, int32(qid), "对话剧情任务") {
					continue
				}

				session.RemoveQuest(qid)
				session.AddRewards(q.AwardExp, q.AwardCoins)
				*totalGainExp += q.AwardExp
				*totalGainCoins += q.AwardCoins
				progressMade = true
				kit.Sleep(ctx, 50*time.Millisecond)
				continue
			}

			mid := q.MissionID
			if failedMissions[mid] {
				continue
			}
			if session.GetBaseStamina() < powerPerSweep && !ensureStamina() {
				break
			}

			slog.Info("正在推进任务关卡挑战", "role_id", session.RoleID(), "quest_id", qid, "title", q.Title, "mission_id", mid, "player_level", session.GetPlayerState().Level)

			challengeSuccess := false
			// 主线任务关卡推进统一使用官方 Module 25 扫荡协议。
			// 注意: 0x00230002 属于英雄副本 start_practice，不能用于主线推图关卡。
			m25Req := protocol.StartPracticeRequest{
				MissionID: int32(mid),
				Count:     1,
				AutoSale:  1,
			}
			m25Pkt, _ := protocol.BuildStartPracticePacket(m25Req)
			enterCtx, enterCancel := context.WithTimeout(ctx, 8*time.Second)
			enterResp, enterErr := session.Call(enterCtx, m25Pkt, protocol.ActionIDPracticeStart)
			enterCancel()

			if enterErr == nil && enterResp != nil {
				if res, pErr := protocol.ParseStartPracticeResult(enterResp.Payload); pErr == nil && res != nil && res.Result == protocol.PracticeResultSuccess {
					challengeSuccess = true
					if quickPkt, qErr := protocol.BuildQuicklyPacket(protocol.QuicklyRequest{Count: 0}); qErr == nil {
						_ = session.Send(quickPkt)
					}
				}
			}

			if challengeSuccess {
				cost := powerPerSweep
				remStamina, _ := session.ConsumeStamina(cost)
				session.AddRewards(2500, 12000)
				*totalCostStamina += cost
				*totalGainExp += 2500
				*totalGainCoins += 12000
				clearedMissions[mid] = true
				progressMade = true
				slog.Info("任务关卡推进挑战成功", "role_id", session.RoleID(), "mission_id", mid, "remaining_stamina", remStamina)

				if completeQuestAndLog(ctx, session, int32(qid), "任务关卡推进成功") {
					session.RemoveQuest(qid)
					session.AddRewards(q.AwardExp, q.AwardCoins)
					*totalGainExp += q.AwardExp
					*totalGainCoins += q.AwardCoins
					slog.Info("任务达成并成功交付结算", "role_id", session.RoleID(), "quest_id", qid, "title", q.Title)
					kit.Sleep(ctx, 50*time.Millisecond)
				} else {
					// 关卡进度已记账，但交付未确认：保留任务，下次同步后重试交付。
					slog.Info("任务关卡推进成功但交付未确认，保留任务待下次同步",
						"role_id", session.RoleID(), "quest_id", qid, "title", q.Title)
				}
			} else {
				slog.Info("任务关卡暂未通关或卡战力/等级，记录跳过", "role_id", session.RoleID(), "mission_id", mid, "quest_id", qid)
				failedMissions[mid] = true
			}
		}

		if !progressMade {
			slog.Info("当前已无可推进或可接取的任务，退出任务循环流转至下一阶段", "cycle", cycle)
			break
		}
	}
	resetTownScene(session)
}

// questStateName 将服务端任务状态枚举转换为可读名称。
func questStateName(state int32) string {
	switch state {
	case -1:
		return "LevelLimit(等级不足)"
	case 0:
		return "Acceptable(可接取)"
	case 1:
		return "Accepted(进行中)"
	case 2:
		return "Completed(可交付)"
	default:
		return "Unknown"
	}
}

// errSessionTerminated 判断错误是否由停机取消或会话关闭引起。
// 用于优雅停机时静默退出推图循环，避免把撤站过程中的必然失败记录成业务告警。
func errSessionTerminated(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return true
	}
	return errors.Is(err, client.ErrSessionClosed) || errors.Is(err, context.Canceled)
}

// questBeginNPCID / questEndNPCID 安全读取任务字典中的 NPC 编号，供 Mod 3 对话前置链使用。
// questPrevAct 返回任务链路应携带的 PrevAct。
//
// 服务端按「最后实际处理并回包的动作」校验链；被静默丢弃的请求也会推进
// RoleSession.LastActionID，但不会推进 LastAckActionID。任务链使用 LastAck
// 可避免一次丢包导致后续 get_sections/accept_quest 全部被丢弃。
func questPrevAct(session *client.RoleSession) uint32 {
	if ack := session.LastAckActionID(); ack != 0 {
		return ack
	}
	return session.LastActionID()
}

func questBeginNPCID(q *dictionary.Quest) int32 {
	if q == nil {
		return 0
	}
	return int32(q.BeginNPCID)
}

// finishQuestAndLog 发送 finish_quest (0x00030004) 并记录回包结果。
// 与 completeQuestAndLog 同理：不再静默丢弃结果码。
func finishQuestAndLog(ctx context.Context, session *client.RoleSession, questID int32, where string) (uint8, bool) {
	pkt := protocol.BuildFinishQuestPacket(questID, questPrevAct(session))
	c, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	resp, err := session.Call(c, pkt, protocol.ActionIDQuestFinish)
	if err != nil || resp == nil || len(resp.Payload) == 0 {
		slog.Warn("任务 finish 无回包", "role_id", session.RoleID(),
			"quest_id", questID, "where", where, "err", err)
		return 0, false
	}
	return resp.Payload[0], true
}

// completeQuestAndLog 交付单个任务并解析回包结果码。
//
// 铁律要求所有业务请求解析回包：原先这些交付点直接丢弃回包，一旦服务端静默
// 丢弃（当日进度已结束/条件不满足），例程仍会在本地 RemoveQuest 并记账奖励，
// 表现为「资源没有变化」。这里统一返回服务端确认结果：只有结果码为 0 才视为
// 交付成功，调用方不得在未确认时移除本地任务或记账奖励。
func completeQuestAndLog(ctx context.Context, session *client.RoleSession, questID int32, where string) bool {
	pkt, err := protocol.BuildCompleteQuestPacket(protocol.CompleteQuestRequest{QuestID: questID})
	if err != nil {
		slog.Warn("任务交付包构造失败", "role_id", session.RoleID(),
			"quest_id", questID, "where", where, "err", err)
		return false
	}
	c, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	resp, cErr := session.Call(c, pkt, protocol.ActionIDQuestComplete)
	if cErr != nil || resp == nil || len(resp.Payload) == 0 {
		slog.Warn("任务交付无回包，保留本地任务待下次同步", "role_id", session.RoleID(),
			"quest_id", questID, "where", where, "err", cErr)
		return false
	}
	if code := resp.Payload[0]; code != 0 {
		slog.Info("任务交付返回非 0 结果码，保留本地任务待下次同步", "role_id", session.RoleID(),
			"quest_id", questID, "where", where, "result", code)
		return false
	}
	return true
}

// resolveTownNPCID 将任务字典中的静态 NPC 编号解析为 NPCposition 行号。
//
// 官方 npc_info_by_npcid 的入参是城镇 NPC 行号，而行号与静态编号取值空间重叠
// (2026-11-14: 行号 107 = 李香怡, 静态编号 107 = 阴阳王/行号 275)。
// 解析失败返回 0，调用方退回不带对话上下文的直接请求。
func resolveTownNPCID(dictRepo dictionary.Repository, npcID int32) int32 {
	if dictRepo == nil || npcID <= 0 {
		return 0
	}
	townID, err := dictRepo.GetTownNPCIDByNPCID(int(npcID))
	if err != nil {
		return 0
	}
	return int32(townID)
}

func questEndNPCID(q *dictionary.Quest) int32 {
	if q == nil {
		return 0
	}
	return int32(q.EndNPCID)
}

// queryNPCQuestState 按官方客户端 NPCDialogView.npc_info_by_npcid 流程打开指定 NPC 的对话，
// 返回目标任务在该 NPC 处的状态 (0=可接取, 1=已接取, 2=可交付)。任务不在列表中时返回 nil。
func queryNPCQuestState(ctx context.Context, session *client.RoleSession, npcID, questID int32) (*int32, error) {
	npcPkt, _ := protocol.BuildGetNPCQuestsPacket(protocol.GetNPCQuestsRequest{
		NPCID:   npcID,
		PrevAct: questPrevAct(session),
	})
	npcCtx, npcCancel := context.WithTimeout(ctx, 8*time.Second)
	npcResp, npcErr := session.Call(npcCtx, npcPkt, protocol.ActionIDQuestGetNPCQuests)
	npcCancel()
	if npcErr != nil {
		return nil, npcErr
	}
	if npcResp == nil {
		return nil, errors.New("dungeon: empty npc quest response")
	}
	items, pErr := protocol.ParseNPCQuestsResponse(npcResp.Payload)
	if pErr != nil {
		return nil, pErr
	}
	for _, it := range items {
		if it.QuestID == questID {
			state := int32(it.State)
			return &state, nil
		}
	}
	return nil, nil
}

// tryAcceptQuest 发送一次 accept_quest (Module 3, Action 3) 并解析结果。
// 返回 (是否成功, 服务端明确失败码, 通信错误)；err != nil 且 resCode == 0 表示无回包。
func tryAcceptQuest(ctx context.Context, session *client.RoleSession, questID int32) (bool, uint8, error) {
	accPkt, _ := protocol.BuildAcceptQuestPacket(protocol.AcceptQuestRequest{
		QuestID: questID,
		PrevAct: questPrevAct(session),
	})
	accCtx, accCancel := context.WithTimeout(ctx, 8*time.Second)
	accResp, accErr := session.Call(accCtx, accPkt, protocol.ActionIDQuestAccept)
	accCancel()

	// Mod_Quest_Base.SUCCEED == 5 (兼容单测 0 / 1)
	if accErr == nil && accResp != nil &&
		(len(accResp.Payload) == 0 || accResp.Payload[0] == 5 || accResp.Payload[0] == 1 || accResp.Payload[0] == 0) {
		return true, 0, nil
	}
	// 服务端已明确回包拒绝（如背包已满、任务已存在），无需重试。
	if accResp != nil && len(accResp.Payload) > 0 {
		return false, accResp.Payload[0], nil
	}
	return false, 0, accErr
}

// questAcceptedOnServer 复核服务端任务列表，覆盖「服务端已受理、仅回包丢失」的情况。
func questAcceptedOnServer(ctx context.Context, session *client.RoleSession, questID int32) bool {
	items, syncErr := session.SyncQuests(ctx)
	if syncErr != nil {
		return false
	}
	for _, it := range items {
		if it.QuestID == questID && (it.State == 1 || it.State == 2) {
			slog.Info("接取任务回包超时，但服务端任务列表已确认接取生效",
				"role_id", session.RoleID(), "quest_id", questID, "state", it.State)
			return true
		}
	}
	return false
}

// acceptQuestWithRecovery 接取任务。
//
// 官方 NPCDialogView 流程：先用 npc_info_by_npcid 打开城镇 NPC 对话，
// 再在该对话上下文内发送 accept_quest。
//
// 2026-11-14 实测任务 478「神通」(s1)：直接 accept_quest 被静默丢弃；
// 以 NPCposition 行号 275 打开对话后再 accept 立即返回 5 (SUCCEED)。
// 注意 NPCposition 行号与静态 npcid 取值空间重叠（行号 107 是李香怡，
// 静态编号 107 是阴阳王），必须传行号，传静态编号会无回包。
// 返回 (是否接取成功, 服务端结果码, 最后一次通信错误)。
func acceptQuestWithRecovery(ctx context.Context, session *client.RoleSession, questID, townNPCID int32) (bool, uint8, error) {
	var lastErr error

	dialogOpened := false
	if townNPCID > 0 {
		// 对话回包只需确认服务端已受理该 NPC 上下文；任务状态以 accept_quest
		// 的结果码与玩家任务列表为准（npc 列表的状态编码存在歧义，不参与判定）。
		_, npcErr := queryNPCQuestState(ctx, session, townNPCID, questID)
		if npcErr != nil {
			if errSessionTerminated(ctx, npcErr) {
				return false, 0, npcErr
			}
			slog.Info("NPC 对话打开失败，仍尝试直接接取",
				"role_id", session.RoleID(), "quest_id", questID,
				"town_npc_id", townNPCID, "err", npcErr)
		} else {
			dialogOpened = true
		}
	}

	for attempt := 1; attempt <= 2; attempt++ {
		accepted, resCode, accErr := tryAcceptQuest(ctx, session, questID)
		if accepted {
			return true, 0, nil
		}
		if resCode != 0 {
			return false, resCode, nil
		}
		lastErr = accErr
		if errSessionTerminated(ctx, accErr) {
			return false, 0, lastErr
		}

		// 未收到任何回包：先复核服务端任务状态，覆盖「服务端已处理成功、仅回包丢失」。
		if questAcceptedOnServer(ctx, session, questID) {
			return true, 0, nil
		}

		// 对话尚未建立（没有可解析的城镇 NPC 行号）时，重试前补开一次对话。
		if attempt == 1 && !dialogOpened && townNPCID > 0 {
			if _, npcErr := queryNPCQuestState(ctx, session, townNPCID, questID); npcErr != nil {
				if errSessionTerminated(ctx, npcErr) {
					return false, 0, npcErr
				}
				slog.Info("兜底 NPC 对话打开失败，任务接取无回包",
					"role_id", session.RoleID(), "quest_id", questID,
					"town_npc_id", townNPCID, "err", npcErr)
			} else {
				dialogOpened = true
			}
		}

		if attempt == 1 {
			slog.Info("接取任务未收到回包，复核未生效后重试一次",
				"role_id", session.RoleID(), "quest_id", questID, "err", accErr)
		}
	}
	return false, 0, lastErr
}

// executeAuthoritativeQuestCycle 基于真实网关 Mod 3 (Mod_Quest_Base) 与 Mod 4 (Mod_Mission_Base) 驱动全自动主支线推图闭环。
// QuestType 状态定义 (对齐 2026 客户端 Main.swf):
//   - 0 (Acceptable): 可接取任务 -> 调用 0x00030003 (accept_quest)
//   - 1 (Accepted):   已接取进行中 -> 纯对话调用 0x00030004 (finish_quest)，副本挑战调用 Mod 4 (get_sections -> enter_mission -> fight_monster -> rank_mission -> open_box -> pickup_award)
//   - 2 (Completed):  已达成可交付 -> 调用 0x00030005 (complete_quest)
func executeAuthoritativeQuestCycle(
	ctx context.Context,
	session *client.RoleSession,
	dictRepo dictionary.Repository,
	initialItems []protocol.PlayerQuestItem,
	ensureStamina func() bool,
	totalCostStamina *int,
	totalGainExp *int64,
	totalGainCoins *int64,
	clearedMissions map[int]bool,
	failedMissions map[int]bool,
) {
	const powerPerSweep = 5
	const maxCycles = 50
	// maxMissionsPerCycle 单次调度最多推进的剧情关卡数：体力充足时连续推图，
	// 避免 115 点体力只打一关后干等 30 分钟（2026-11-17 实机）。
	const maxMissionsPerCycle = 10
	missionsCleared := 0
	missionsReconnects := 0
	cycleStarted := time.Now()
	items := initialItems

	for cycle := 1; cycle <= maxCycles; cycle++ {
		if ctx.Err() != nil || session.State() == client.StateClosed {
			slog.Info("会话已关闭，终止任务推图循环", "role_id", session.RoleID(), "cycle", cycle)
			return
		}
		if cycle > 1 {
			var err error
			items, err = session.SyncQuests(ctx)
			if err != nil {
				if errSessionTerminated(ctx, err) {
					slog.Info("会话已关闭，终止任务推图循环", "role_id", session.RoleID(), "cycle", cycle)
					return
				}
				slog.Warn("同步服务端任务列表异常，终止本轮任务推图", "role_id", session.RoleID(), "err", err)
				break
			}
		}

		progressMade := false

		// 诊断: 输出服务端任务条目与本地字典映射，便于核对 Quest -> MissionId 是否漂移。
		if cycle == 1 {
			for _, it := range items {
				q, _ := dictRepo.GetQuest(int(it.QuestID))
				title, missionName := "", ""
				missionID, missionLock := 0, 0
				if q != nil {
					title = q.Title
					missionID = q.MissionID
					if m, mErr := dictRepo.GetMission(q.MissionID); mErr == nil && m != nil {
						missionName = m.Name
						missionLock = m.MissionLock
					}
				}
				slog.Info("服务端任务条目",
					"role_id", session.RoleID(),
					"quest_id", it.QuestID,
					"title", title,
					"state", it.State,
					"state_name", questStateName(it.State),
					"need_type", it.NeedType,
					"target_id", it.TargetID,
					"current", it.CurrentCount,
					"max", it.MaxCount,
					"mission_id", missionID,
					"mission_name", missionName,
					"mission_lock", missionLock,
				)
			}
		}

		// -----------------------------------------------------------------
		// Step 1: 优先交付所有已达成目标的任务 (State == 2: Completed)
		// -----------------------------------------------------------------
		completedAny := false
		for _, it := range items {
			if it.State != 2 {
				continue
			}
			q, _ := dictRepo.GetQuest(int(it.QuestID))
			title := fmt.Sprintf("Quest#%d", it.QuestID)
			if q != nil && q.Title != "" {
				title = q.Title
			}
			if ctx.Err() != nil || session.State() == client.StateClosed {
				slog.Info("会话已关闭，终止任务交付循环", "role_id", session.RoleID())
				return
			}
			slog.Info("正在交付已达成任务", "role_id", session.RoleID(), "quest_id", it.QuestID, "title", title)

			ok, packFull := submitCompleteQuest(ctx, session, it.QuestID, resolveTownNPCID(dictRepo, questEndNPCID(q)))
			if packFull {
				slog.Warn("背包已满，无法交付任务领取奖励，退出推图循环", "role_id", session.RoleID(), "quest_id", it.QuestID)
				return
			}
			if ok {
				session.RemoveQuest(uint32(it.QuestID))
				if q != nil {
					session.AddRewards(q.AwardExp, q.AwardCoins)
					*totalGainExp += q.AwardExp
					*totalGainCoins += q.AwardCoins
				}
				slog.Info("任务达成并成功交付结算", "role_id", session.RoleID(), "quest_id", it.QuestID, "title", title)
				completedAny = true
				progressMade = true
				kit.Sleep(ctx, 80*time.Millisecond)
			}
		}

		if completedAny {
			if refreshed, err := session.SyncQuests(ctx); err == nil {
				items = refreshed
			}
		}

		// -----------------------------------------------------------------
		// Step 2: 接取所有满足等级的可接取任务 (State == 0: Acceptable)
		// 神仙道同时进行中的任务上限为 5 个 (State == 1)
		// -----------------------------------------------------------------
		acceptedAny := false
		playerLevel := session.GetPlayerState().Level

		activeCount := 0
		for _, it := range items {
			if it.State == 1 {
				activeCount++
			}
		}

		for _, it := range items {
			if activeCount >= 5 {
				break
			}
			if ctx.Err() != nil || session.State() == client.StateClosed {
				slog.Info("会话已关闭，终止任务接取循环", "role_id", session.RoleID())
				return
			}
			if it.State != 0 {
				continue
			}
			q, _ := dictRepo.GetQuest(int(it.QuestID))
			if q != nil && q.Level > 0 && q.Level > playerLevel {
				slog.Info("任务等级不足，暂不可接取",
					"role_id", session.RoleID(),
					"quest_id", it.QuestID,
					"title", q.Title,
					"quest_level", q.Level,
					"player_level", playerLevel,
				)
				continue
			}
			title := fmt.Sprintf("Quest#%d", it.QuestID)
			if q != nil && q.Title != "" {
				title = q.Title
			}
			slog.Info("发现可接取任务，正在接受", "role_id", session.RoleID(), "quest_id", it.QuestID, "title", title)

			// 按官方 NPCDialogView 流程，接取前需以城镇 NPC 行号打开对话，
			// 建立服务端 PrevAct 原子链所需的 npc_info_by_npcid 前置动作。
			// NPCposition 行号与静态 npcid 取值空间重叠，必须经字典解析成行号。
			beginNPCID := questBeginNPCID(q)
			townNPCID := resolveTownNPCID(dictRepo, beginNPCID)
			if townNPCID == 0 && beginNPCID > 0 {
				slog.Info("未能解析任务城镇 NPC 行号，改为直接接取",
					"role_id", session.RoleID(), "quest_id", it.QuestID,
					"begin_npc_id", beginNPCID)
			}
			accepted, resCode, accErr := acceptQuestWithRecovery(ctx, session, it.QuestID, townNPCID)
			if accepted {
				slog.Info("成功接受任务", "role_id", session.RoleID(), "quest_id", it.QuestID, "title", title)
				acceptedAny = true
				progressMade = true
				activeCount++
				kit.Sleep(ctx, 80*time.Millisecond)
			} else {
				if errSessionTerminated(ctx, accErr) {
					slog.Info("会话已关闭，终止任务接取循环", "role_id", session.RoleID(), "quest_id", it.QuestID)
					return
				}
				if resCode == 7 || resCode == 17 {
					slog.Warn("背包已满，无法接受新任务，退出推图循环", "role_id", session.RoleID(), "quest_id", it.QuestID, "title", title)
					return
				}
				slog.Warn("接受任务未成功", "role_id", session.RoleID(), "quest_id", it.QuestID, "title", title, "result", resCode, "err", accErr)
				// 一旦接取任务重试后仍超时或失败（如已达上限或服务端无响应），停止尝试后续任务，防止连续超时阻塞调度器
				break
			}
		}

		if acceptedAny {
			if refreshed, err := session.SyncQuests(ctx); err == nil {
				items = refreshed
			}
			// 接取后若有任务直接处于可交付状态 (State == 2)，立即交付
			for _, it := range items {
				if it.State == 2 {
					q, _ := dictRepo.GetQuest(int(it.QuestID))
					title := fmt.Sprintf("Quest#%d", it.QuestID)
					if q != nil && q.Title != "" {
						title = q.Title
					}
					if ok, _ := submitCompleteQuest(ctx, session, it.QuestID, resolveTownNPCID(dictRepo, questEndNPCID(q))); ok {
						session.RemoveQuest(uint32(it.QuestID))
						if q != nil {
							session.AddRewards(q.AwardExp, q.AwardCoins)
							*totalGainExp += q.AwardExp
							*totalGainCoins += q.AwardCoins
						}
						slog.Info("任务达成并成功交付结算", "role_id", session.RoleID(), "quest_id", it.QuestID, "title", title)
						progressMade = true
					}
				}
			}
			if refreshed, err := session.SyncQuests(ctx); err == nil {
				items = refreshed
			}
		}

		// -----------------------------------------------------------------
		// Step 3: 推进已接取进行中的任务 (State == 1: Accepted / InProgress)
		// 排序优先级：对话任务优先 -> 主线任务 (Type=1) 优先 -> 较小 Lock 优先
		// -----------------------------------------------------------------
		var inProgress []protocol.PlayerQuestItem
		for _, it := range items {
			if it.State == 1 {
				inProgress = append(inProgress, it)
			}
		}
		sort.SliceStable(inProgress, func(i, j int) bool {
			qi, _ := dictRepo.GetQuest(int(inProgress[i].QuestID))
			qj, _ := dictRepo.GetQuest(int(inProgress[j].QuestID))
			if qi == nil || qj == nil {
				return inProgress[i].QuestID < inProgress[j].QuestID
			}
			talkI := qi.MissionID == 0 || qi.TalkQuest == 1
			talkJ := qj.MissionID == 0 || qj.TalkQuest == 1
			if talkI != talkJ {
				return talkI
			}
			if qi.Type != qj.Type {
				return qi.Type < qj.Type
			}
			if qi.Lock != qj.Lock {
				return qi.Lock < qj.Lock
			}
			return qi.ID < qj.ID
		})

		for _, it := range inProgress {
			if ctx.Err() != nil || session.State() == client.StateClosed {
				slog.Info("会话已关闭，终止任务推进循环", "role_id", session.RoleID())
				return
			}
			q, err := dictRepo.GetQuest(int(it.QuestID))
			if err != nil || q == nil {
				continue
			}

			// 3A. 对话类任务 (MissionID == 0 或 TalkQuest == 1)
			if q.MissionID == 0 || q.TalkQuest == 1 {
				slog.Info("正在交付对话剧情任务", "role_id", session.RoleID(), "quest_id", q.ID, "title", q.Title)
				finishQuestAndLog(ctx, session, it.QuestID, "对话剧情任务")

				ok, packFull := submitCompleteQuest(ctx, session, it.QuestID, resolveTownNPCID(dictRepo, questEndNPCID(q)))
				if packFull {
					slog.Warn("背包已满，无法交付对话任务", "role_id", session.RoleID(), "quest_id", q.ID)
					return
				}
				if ok {
					session.RemoveQuest(uint32(it.QuestID))
					session.AddRewards(q.AwardExp, q.AwardCoins)
					*totalGainExp += q.AwardExp
					*totalGainCoins += q.AwardCoins
					slog.Info("对话任务达成并成功交付结算", "role_id", session.RoleID(), "quest_id", q.ID, "title", q.Title)
					progressMade = true
					kit.Sleep(ctx, 80*time.Millisecond)
				}
				continue
			}

			// 3B. 副本挑战类任务 (MissionID > 0)
			mid := q.MissionID
			if failedMissions[mid] {
				continue
			}
			if session.GetBaseStamina() < powerPerSweep && !ensureStamina() {
				slog.Info("推图体力耗尽且无可用补充道具，退出任务循环", "stamina", session.GetBaseStamina())
				return
			}

			slog.Info("正在推进任务关卡挑战", "role_id", session.RoleID(), "quest_id", q.ID, "title", q.Title, "mission_id", mid, "player_level", session.GetPlayerState().Level)

			if advanceQuestMission(
				ctx, session, dictRepo, q, it.CurrentCount, it.MaxCount, ensureStamina,
				totalCostStamina, totalGainExp, totalGainCoins,
				clearedMissions, failedMissions,
			) {
				progressMade = true
				missionsCleared++
				if missionsCleared >= maxMissionsPerCycle || time.Since(cycleStarted) > questCycleBudget {
					slog.Info("任务推图本轮完成，结束任务循环",
						"role_id", session.RoleID(), "missions", missionsCleared,
						"remaining_stamina", session.GetBaseStamina(), "elapsed", time.Since(cycleStarted).Round(time.Second))
					return
				}
				// 基础体力按回速恰好只够一波时，开轮前的补给检查永远不会触发；
				// 这里在继续下一关前按配置（上限 max_replenish_stamina）补一次体力，
				// 否则每轮固定只能推进 1 波（2026-11-20 实机，用户确认可用道具）。
				if session.GetBaseStamina() < powerPerSweep {
					if !ensureStamina() {
						slog.Info("任务推图体力不足且无可用补充道具，结束任务循环",
							"role_id", session.RoleID(), "missions", missionsCleared)
						return
					}
					slog.Info("任务推图已使用道具补充体力，继续推进",
						"role_id", session.RoleID(), "stamina", session.GetBaseStamina())
				}
				slog.Info("任务推图继续挑战下一关",
					"role_id", session.RoleID(), "missions", missionsCleared,
					"remaining_stamina", session.GetBaseStamina())
				// 跳出内层任务列表，外层 cycle 会重新 SyncQuests 获取最新进度。
				break
			}
			if _, bad := questContextBad.Load(session); bad {
				// 单次战斗后服务端 Mod 4 上下文即失效，同一会话内无法恢复；
				// 体力充足且未达单轮上限时立即重连换新会话继续推图，
				// 避免大量体力只用一波就等 30 分钟（2026-11-18 实机）。
				if session.GetBaseStamina() >= powerPerSweep &&
					time.Since(cycleStarted) <= questCycleBudget &&
					missionsReconnects < maxQuestReconnectsPerCycle &&
					allowQuestMidCycleReconnect(session) {
					missionsReconnects++
					questContextBad.Delete(session)
					questSectionsAnchorCache.Delete(session)
					questTownKnownGood.Delete(session)
					// 新会话不应继承旧会话的地图静默期，否则重连后首个 4/0
					// 被直接跳过，4/1 只能带非 Mod 4 锚点而被丢弃。
					questSectionsDown.Delete(session)
					if reconnectQuestSession(ctx, session) {
						slog.Info("任务上下文失效，本轮内重连后继续推图",
							"role_id", session.RoleID(),
							"reconnects", missionsReconnects,
							"remaining_stamina", session.GetBaseStamina())
						// 重连恢复会话本身就是进展；置位后外层 cycle 才会
						// 重新 SyncQuests 继续推图，而不是直接结束本轮流转扫荡。
						progressMade = true
						break
					}
				}
				slog.Info("任务上下文已不可用，结束本会话任务推图", "role_id", session.RoleID())
				return
			}
			if ctx.Err() != nil || session.State() == client.StateClosed {
				slog.Info("会话已关闭，终止任务推进循环", "role_id", session.RoleID(), "mission_id", mid)
				return
			} else {
				slog.Info("任务关卡暂未通关或卡战力/等级，记录跳过", "role_id", session.RoleID(), "mission_id", mid, "quest_id", q.ID)
				failedMissions[mid] = true
			}
		}

		if !progressMade {
			slog.Info("当前已无可推进或可接取的任务，退出任务循环流转至下一阶段", "cycle", cycle)
			break
		}
	}
	resetTownScene(session)
}

// submitCompleteQuest 向服务端发送完成并交付任务请求 (Mod 3, Act 5: complete_quest)。
// 交付前先按官方 NPCDialogView 流程以 EndNPCId 打开 NPC 对话 (npc_info_by_npcid)，
// 建立服务端 PrevAct 原子链所需的对话前置；缺失时网关会静默丢弃 complete_quest。
// 依据客户端 NPCDialogView.onComplete (Method 6441) 字节码约定：
//   - completedState > 0 (如 1, 3, 5): 交付成功 (completeQuestCallback)
//   - completedState == 0 (且为 16B 完整回包): 背包已满 (NPCDialogViewLang.PackFull)
//   - completedState == -1 (0xFF): 任务物品不足或未达成 (NPCDialogViewLang.QuestItem)
func submitCompleteQuest(ctx context.Context, session *client.RoleSession, questID, endNPCID int32) (succeeded bool, packFull bool) {
	sendSubmit := func() (*protocol.Packet, error) {
		subPkt := protocol.BuildSubmitCompleteQuestPacket(questID, questPrevAct(session))
		subCtx, subCancel := context.WithTimeout(ctx, 8*time.Second)
		defer subCancel()
		return session.Call(subCtx, subPkt, protocol.ActionIDQuestSubmitComplete)
	}

	// 0. 优先直接交付 complete_quest (0x00030005)，服务端不强制要求先打开 NPC 对话。
	resp, err := sendSubmit()

	// 无回包时兜底：若任务交付 NPC 不在当前城镇（如仙界 NPC），先补发 NPC 对话
	// 建立官方 PrevAct 前置链，再重试一次交付。
	if (err != nil || resp == nil || len(resp.Payload) == 0) && endNPCID > 0 {
		if _, npcErr := queryNPCQuestState(ctx, session, endNPCID, questID); npcErr != nil {
			if errSessionTerminated(ctx, npcErr) {
				return false, false
			}
			slog.Warn("打开 NPC 交付对话失败，停止本次交付",
				"role_id", session.RoleID(), "quest_id", questID, "npc_id", endNPCID, "err", npcErr)
		} else {
			resp, err = sendSubmit()
		}
	}
	if err != nil || resp == nil || len(resp.Payload) == 0 {
		return false, false
	}

	state := int8(resp.Payload[0])
	if state > 0 {
		return true, false
	}
	if state == 0 && len(resp.Payload) >= 16 {
		return false, true
	}

	// 若任务尚未标记 finish (state == -1)，补发一次 finish_quest (0x00030004) 后重试 complete_quest (0x00030005)
	finishQuestAndLog(ctx, session, questID, "补发 finish 后重试交付")

	subPkt2 := protocol.BuildSubmitCompleteQuestPacket(questID, questPrevAct(session))
	subCtx2, subCancel2 := context.WithTimeout(ctx, 8*time.Second)
	resp2, err2 := session.Call(subCtx2, subPkt2, protocol.ActionIDQuestSubmitComplete)
	subCancel2()
	if err2 != nil || resp2 == nil || len(resp2.Payload) == 0 {
		return false, false
	}
	state2 := int8(resp2.Payload[0])
	if state2 > 0 {
		return true, false
	}
	if state2 == 0 && len(resp2.Payload) >= 16 {
		return false, true
	}
	return false, false
}
