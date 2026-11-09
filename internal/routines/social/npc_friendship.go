// Package social 提供仙盟、好友、住宅、收藏册等社交玩法自动化。
//
// 本文件实现「NPC 结交」(Mod_NpcFriendship_Base, Module 436) 自动化：
// 每日问答、接取/完成结交任务、领取任务奖励、赠送伴手礼提升好感度、达到
// 「义结金兰」门槛后结义。
package social

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// npcFriendshipSouvenirCoinCost 伴手礼 (present_id=1) 的单次铜钱成本。
//
// 官方 NpcFriendShipType.PresentList: id=1 item_id=1747 item_count=500000。
// 1747 为铜钱系道具，等价于 50 万铜钱；礼物 2/3 分别消耗同心结与元宝，自动化不使用。
const npcFriendshipSouvenirCoinCost int64 = 500000

const (
	// maxNpcFriendshipMakeFriends 单轮最多结交的 NPC 数量。
	maxNpcFriendshipMakeFriends = 20
	// maxNpcFriendshipQuestions 单轮最多回答的 NPC 问答数量。
	maxNpcFriendshipQuestions = 20
	// maxNpcFriendshipTakeMissions 单轮最多接取的结交任务数量。
	maxNpcFriendshipTakeMissions = 20
	// maxNpcFriendshipMissionActions 单轮最多执行的结任务动作数量。
	maxNpcFriendshipMissionActions = 20
	// maxNpcFriendshipGuessAttempts 猜拳/掷骰子单任务最大尝试次数。
	// 两种玩法均为免费随机事件，失败可立即重试。
	maxNpcFriendshipGuessAttempts = 12
	// npcFriendshipActionSleep 连续业务请求之间的拟人间隔。
	npcFriendshipActionSleep = 150 * time.Millisecond
)

// 结交任务类型 (NpcFriendShipType.MissionTypeList)。
const (
	npcMissionFingerGuess int32 = 2  // 猜拳
	npcMissionDiceGuess   int32 = 13 // 掷骰子
)

// NewNpcFriendshipRoutine 构造 NPC 结交日常任务。
//
// 官方流程 (com.datas.NpcFriendShipData / com.views.NpcFriendShipView)：
//  1. get_info 查询全部 NPC 好感度、问答、结交状态与任务状态；
//  2. question_id > 0 时调用 answer_question 回答每日问答 (直接增加好感度)；
//  3. mission_type > 0 且未接取时调用 take_mission；
//  4. 已接取未完成的任务：
//     - task2 猜拳 (finger_guess)、task13 掷骰子 (dice_guess) 反复尝试直到胜利；
//     - 其余任务调用 complete_mission (交付/进度型任务由服务端校验)；
//  5. 对 is_complete=1 的任务调用 gain_mission_award 领取好感度奖励；
//  6. 赠送伴手礼 (present_id=1，铜钱系) 提升好感度，未结义 NPC 优先；
//  7. 好感度达到 1071 (Lv5 义结金兰) 后调用 make_friend 结义。
//
// 自动化绝不使用 present_id=3 (金兰花/元宝)，也不消耗元宝。
func NewNpcFriendshipRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"npc_friendship",
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
			slog.Info("正在执行社交任务: NPC 结交", "role_id", session.RoleID())

			call := func(pkt *protocol.Packet, act uint32) (*protocol.Packet, error) {
				cCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				return session.Call(cCtx, pkt, act)
			}
			sleep := func() bool {
				select {
				case <-ctx.Done():
					return false
				case <-time.After(npcFriendshipActionSleep):
					return true
				}
			}
			fetchDropped := false
			fetch := func() *protocol.NpcFriendshipSnapshot {
				resp, err := call(protocol.BuildNpcFriendshipInfoPacket(), protocol.ActionIDNpcFriendshipInfo)
				if err != nil || resp == nil {
					fetchDropped = true
					slog.Info("NPC 结交信息查询无回包", "role_id", session.RoleID(), "err", err)
					return nil
				}
				snap, pErr := protocol.ParseNpcFriendshipInfoResult(resp.Payload)
				if pErr != nil {
					slog.Warn("解析 NPC 结交面板失败", "role_id", session.RoleID(), "err", pErr)
					return nil
				}
				return snap
			}

			snap := fetch()
			if snap == nil {
				if fetchDropped {
					slog.Info("NPC 结交面板无回包，稍后重试", "role_id", session.RoleID())
					return scheduler.NewCooldown(30*time.Minute, "NPC结交面板无回包")
				}
				slog.Info("NPC 结交功能不可用或未开放，跳过", "role_id", session.RoleID())
				return nil
			}
			slog.Info("NPC 结交面板",
				"role_id", session.RoleID(),
				"npcs", len(snap.Npcs),
				"friend_count", snap.FriendCount,
				"mission_count", snap.MissionCount,
			)
			if len(snap.Npcs) == 0 {
				return nil
			}

			// 1. 回答每日问答：QuestList 中选奖励最高的选项。
			answered := 0
			for i := range snap.Npcs {
				if answered >= maxNpcFriendshipQuestions {
					break
				}
				npc := &snap.Npcs[i]
				if npc.QuestionID <= 0 {
					continue
				}
				questionID := npc.QuestionID
				answer, ok := protocol.NpcFriendshipBestAnswer(questionID)
				if !ok {
					slog.Info("NPC 问答编号未知，跳过",
						"role_id", session.RoleID(), "npc_id", npc.NpcID, "question_id", npc.QuestionID)
					continue
				}
				resp, err := call(
					protocol.BuildNpcFriendshipAnswerQuestionPacket(questionID, npc.NpcID, answer.Answer),
					protocol.ActionIDNpcFriendshipAnswerQuestion,
				)
				if err != nil || resp == nil {
					slog.Info("NPC 问答无回包", "role_id", session.RoleID(),
						"npc_id", npc.NpcID, "question_id", questionID, "err", err)
					continue
				}
				got, pErr := protocol.ParseNpcFriendshipAnswerResult(resp.Payload)
				if pErr != nil {
					slog.Warn("解析 NPC 问答回包失败", "role_id", session.RoleID(), "err", pErr)
					continue
				}
				if got.Result != protocol.NpcFriendshipResultSuccess {
					slog.Info("NPC 问答未成功", "role_id", session.RoleID(),
						"npc_id", npc.NpcID, "question_id", questionID, "result", got.Result)
					continue
				}
				answered++
				npc.QuestionID = 0
				if got.NewExp > 0 {
					npc.CurExp = got.NewExp
				} else {
					npc.CurExp += answer.Award
				}
				slog.Info("NPC 问答成功", "role_id", session.RoleID(),
					"npc_id", npc.NpcID, "question_id", questionID,
					"answer", answer.Answer, "new_exp", npc.CurExp)
				if !sleep() {
					return nil
				}
			}

			// 2. 接取可接的结交任务。
			taken := 0
			takeLimitReached := false
			for i := range snap.Npcs {
				if taken >= maxNpcFriendshipTakeMissions || takeLimitReached {
					break
				}
				npc := &snap.Npcs[i]
				if npc.MissionType <= 0 || npc.HasTakeMission == 1 || npc.IsComplete == 1 {
					continue
				}
				resp, err := call(
					protocol.BuildNpcFriendshipTakeMissionPacket(npc.NpcID),
					protocol.ActionIDNpcFriendshipTakeMission,
				)
				if err != nil || resp == nil {
					slog.Info("NPC 接取任务无回包", "role_id", session.RoleID(), "npc_id", npc.NpcID, "err", err)
					continue
				}
				got, pErr := protocol.ParseNpcFriendshipTakeMissionResult(resp.Payload)
				if pErr != nil {
					slog.Warn("解析 NPC 接取任务回包失败", "role_id", session.RoleID(), "err", pErr)
					continue
				}
				switch got.Result {
				case protocol.NpcFriendshipResultSuccess:
					npc.HasTakeMission = got.HasTakeMission
					if npc.HasTakeMission == 0 {
						npc.HasTakeMission = 1
					}
					taken++
					slog.Info("NPC 接取任务成功", "role_id", session.RoleID(),
						"npc_id", npc.NpcID, "mission_type", npc.MissionType)
				case protocol.NpcFriendshipResultAlreadyTake:
					npc.HasTakeMission = 1
				case protocol.NpcFriendshipResultMaxCountLimit, protocol.NpcFriendshipResultMakeFriendCountLimit:
					slog.Info("NPC 结交任务数量已达上限，停止接取", "role_id", session.RoleID())
					takeLimitReached = true
				default:
					slog.Info("NPC 接取任务未成功", "role_id", session.RoleID(),
						"npc_id", npc.NpcID, "result", got.Result)
				}
				if !sleep() {
					return nil
				}
			}

			// 3. 完成已接取的任务。
			completeMission := func(npc *protocol.NpcFriendshipInfo) bool {
				resp, err := call(
					protocol.BuildNpcFriendshipCompleteMissionPacket(npc.NpcID, npc.MissionType, npc.TownNpcID),
					protocol.ActionIDNpcFriendshipCompleteMission,
				)
				if err != nil || resp == nil {
					slog.Info("NPC 完成任务无回包", "role_id", session.RoleID(),
						"npc_id", npc.NpcID, "mission_type", npc.MissionType, "err", err)
					return false
				}
				got, pErr := protocol.ParseNpcFriendshipCompleteResult(resp.Payload)
				if pErr != nil {
					slog.Warn("解析 NPC 完成任务回包失败", "role_id", session.RoleID(), "err", pErr)
					return false
				}
				switch got.Result {
				case protocol.NpcFriendshipResultSuccess, protocol.NpcFriendshipResultHasAlreadyComplete:
					slog.Info("NPC 任务完成", "role_id", session.RoleID(),
						"npc_id", npc.NpcID, "mission_type", npc.MissionType)
					return true
				case protocol.NpcFriendshipResultHasNotComplete:
					slog.Info("NPC 任务进度未满足，保留待后续完成", "role_id", session.RoleID(),
						"npc_id", npc.NpcID, "mission_type", npc.MissionType)
				case protocol.NpcFriendshipResultCompleteNoEnough:
					slog.Info("NPC 任务交付物/进度不足", "role_id", session.RoleID(),
						"npc_id", npc.NpcID, "mission_type", npc.MissionType,
						"mission_item_id", npc.MissionItemID)
				default:
					slog.Info("NPC 任务未完成", "role_id", session.RoleID(),
						"npc_id", npc.NpcID, "mission_type", npc.MissionType, "result", got.Result)
				}
				return false
			}

			// playGuess 反复执行猜拳/掷骰子直到胜利。
			playGuess := func(npc *protocol.NpcFriendshipInfo) bool {
				for attempt := 0; attempt < maxNpcFriendshipGuessAttempts; attempt++ {
					if err := ctx.Err(); err != nil {
						return false
					}
					var (
						resp *protocol.Packet
						err  error
					)
					if npc.MissionType == npcMissionFingerGuess {
						resp, err = call(
							protocol.BuildNpcFriendshipFingerGuessPacket(
								npc.NpcID, npc.MissionType, npc.TownNpcID, int32(attempt%3)),
							protocol.ActionIDNpcFriendshipFingerGuess,
						)
					} else {
						resp, err = call(
							protocol.BuildNpcFriendshipDiceGuessPacket(
								npc.NpcID, npc.MissionType, npc.TownNpcID),
							protocol.ActionIDNpcFriendshipDiceGuess,
						)
					}
					if err != nil || resp == nil {
						slog.Info("NPC 猜拳/骰子无回包", "role_id", session.RoleID(),
							"npc_id", npc.NpcID, "mission_type", npc.MissionType, "err", err)
						return false
					}
					got, pErr := protocol.ParseNpcFriendshipGuessResult(resp.Payload)
					if pErr != nil {
						slog.Warn("解析 NPC 猜拳/骰子回包失败", "role_id", session.RoleID(), "err", pErr)
						return false
					}
					slog.Info("NPC 猜拳/骰子结果", "role_id", session.RoleID(),
						"npc_id", npc.NpcID, "mission_type", npc.MissionType,
						"attempt", attempt+1, "result", got.Result,
						"result_text", protocol.NpcFriendshipGuessResultText(got.Result),
						"my_guess", got.MyGuess, "npc_guess", got.NpcGuess)
					if got.Result == protocol.NpcFriendshipResultWin {
						return true
					}
					if got.Result != protocol.NpcFriendshipResultLose && got.Result != protocol.NpcFriendshipResultTie {
						return false
					}
					if !sleep() {
						return false
					}
				}
				return false
			}

			actions := 0
			for i := range snap.Npcs {
				if actions >= maxNpcFriendshipMissionActions {
					break
				}
				npc := &snap.Npcs[i]
				if npc.HasTakeMission != 1 || npc.IsComplete == 1 {
					continue
				}
				if npc.MissionType == npcMissionFingerGuess || npc.MissionType == npcMissionDiceGuess {
					if playGuess(npc) {
						// 胜利后服务端通常已置为完成；再调用 complete_mission 固化状态，
						// 最终以第 4 步重新拉取的面板为准。
						completeMission(npc)
					}
				} else if completeMission(npc) {
					npc.IsComplete = 1
				}
				actions++
				if !sleep() {
					return nil
				}
			}

			// 4. 重新拉取面板，领取已完成任务的好感度奖励。
			final := fetch()
			if final == nil {
				final = snap
			}
			for i := range final.Npcs {
				npc := &final.Npcs[i]
				if npc.IsComplete != 1 || npc.IsReceive != 0 {
					continue
				}
				resp, err := call(
					protocol.BuildNpcFriendshipGainMissionAwardPacket(npc.NpcID),
					protocol.ActionIDNpcFriendshipGainMissionAward,
				)
				if err != nil || resp == nil {
					slog.Info("领取 NPC 结交任务奖励无回包", "role_id", session.RoleID(), "npc_id", npc.NpcID, "err", err)
					continue
				}
				got, pErr := protocol.ParseNpcFriendshipMissionAwardResult(resp.Payload)
				if pErr != nil {
					slog.Warn("解析 NPC 结交任务奖励回包失败", "role_id", session.RoleID(), "err", pErr)
					continue
				}
				switch got.Result {
				case protocol.NpcFriendshipResultSuccess:
					npc.IsReceive = 1
					npc.MissionType = 0
					if got.NewExp > 0 {
						npc.CurExp = got.NewExp
					}
					slog.Info("NPC 结交任务奖励领取成功", "role_id", session.RoleID(),
						"npc_id", npc.NpcID, "new_exp", npc.CurExp)
				case protocol.NpcFriendshipResultPackFull:
					return scheduler.ErrBagFull
				case protocol.NpcFriendshipResultHasAlreadyReceive, protocol.NpcFriendshipResultAlreadyTake:
					npc.IsReceive = 1
				default:
					slog.Info("NPC 结交任务奖励未领取", "role_id", session.RoleID(),
						"npc_id", npc.NpcID, "result", got.Result)
				}
				if !sleep() {
					return nil
				}
			}

			// 5. 赠送伴手礼 (铜钱系礼物)。
			pResp, pErr := call(protocol.BuildNpcFriendshipPresentInfoPacket(), protocol.ActionIDNpcFriendshipPresentInfo)
			if pErr != nil || pResp == nil {
				slog.Info("查询 NPC 送礼信息无回包", "role_id", session.RoleID(), "err", pErr)
				return nil
			}
			used, maxGive, pErr := protocol.ParseNpcFriendshipPresentInfoResult(pResp.Payload)
			if pErr != nil {
				slog.Warn("解析 NPC 送礼信息失败", "role_id", session.RoleID(), "err", pErr)
				return nil
			}
			remaining := int(maxGive) - int(used[protocol.NpcFriendshipPresentSouvenir])
			if remaining <= 0 {
				slog.Info("NPC 结交今日送礼次数已用尽", "role_id", session.RoleID(), "max_give", maxGive)
			} else {
				// 未结义 NPC 优先 (送礼提升好感度直至可结义)，其次按好感度从高到低。
				// 官方 NpcFriendShipPresentView 允许对任意 NPC 送礼，不要求已结义。
				candidates := make([]*protocol.NpcFriendshipInfo, 0, len(final.Npcs))
				for i := range final.Npcs {
					npc := &final.Npcs[i]
					if npc.IsFriend == 0 && npc.CurExp < protocol.NpcFriendShipMakeFriendMinExp {
						candidates = append(candidates, npc)
						continue
					}
					if npc.IsFriend == 1 {
						candidates = append(candidates, npc)
					}
				}
				sort.SliceStable(candidates, func(i, j int) bool {
					if (candidates[i].IsFriend == 1) != (candidates[j].IsFriend == 1) {
						// IsFriend=0 的未结义 NPC 排在前面。
						return candidates[i].IsFriend < candidates[j].IsFriend
					}
					return candidates[i].CurExp > candidates[j].CurExp
				})

				given := 0
				for _, npc := range candidates {
					if given >= remaining {
						break
					}
					if err := ctx.Err(); err != nil {
						return nil
					}
					if session.GetPlayerState().Coins < npcFriendshipSouvenirCoinCost {
						slog.Info("铜钱不足，停止赠送 NPC 伴手礼", "role_id", session.RoleID(),
							"coins", session.GetPlayerState().Coins)
						break
					}
					gResp, gErr := call(
						protocol.BuildNpcFriendshipGivePresentPacket(protocol.NpcFriendshipPresentSouvenir, npc.NpcID),
						protocol.ActionIDNpcFriendshipGivePresent,
					)
					if gErr != nil || gResp == nil {
						slog.Info("赠送 NPC 伴手礼无回包", "role_id", session.RoleID(), "npc_id", npc.NpcID, "err", gErr)
						break
					}
					got, pParseErr := protocol.ParseNpcFriendshipGiveResult(gResp.Payload)
					if pParseErr != nil {
						slog.Warn("解析赠送 NPC 伴手礼回包失败", "role_id", session.RoleID(), "err", pParseErr)
						break
					}
					switch got.Result {
					case protocol.NpcFriendshipResultSuccess:
						given++
						if got.NewExp > 0 {
							npc.CurExp = got.NewExp
						}
						slog.Info("赠送 NPC 伴手礼成功",
							"role_id", session.RoleID(),
							"npc_id", npc.NpcID,
							"new_exp", got.NewExp,
							"given", given,
						)
					case protocol.NpcFriendshipResultCountLimit, protocol.NpcFriendshipResultMaxCountLimit:
						slog.Info("NPC 结交送礼次数已达上限", "role_id", session.RoleID())
						given = remaining
					case protocol.NpcFriendshipResultExpMax:
						slog.Debug("NPC 好感度已满级，跳过", "role_id", session.RoleID(), "npc_id", npc.NpcID)
						continue
					case protocol.NpcFriendshipResultNoEnough:
						slog.Info("NPC 结交礼物不足，停止赠送", "role_id", session.RoleID())
						given = remaining
					default:
						slog.Info("赠送 NPC 伴手礼未成功", "role_id", session.RoleID(),
							"npc_id", npc.NpcID, "result", got.Result)
						given = remaining
					}
					if !sleep() {
						return nil
					}
				}
			}

			// 6. 好感度达到 Lv5「义结金兰」后结义。
			belowThreshold := 0
			made := 0
			for i := range final.Npcs {
				npc := &final.Npcs[i]
				if npc.IsFriend == 1 {
					continue
				}
				if npc.CurExp < protocol.NpcFriendShipMakeFriendMinExp {
					belowThreshold++
					continue
				}
				if made >= maxNpcFriendshipMakeFriends {
					break
				}
				if err := ctx.Err(); err != nil {
					return nil
				}
				mResp, mErr := call(
					protocol.BuildNpcFriendshipMakeFriendPacket(npc.NpcID),
					protocol.ActionIDNpcFriendshipMakeFriend,
				)
				if mErr != nil || mResp == nil {
					slog.Info("结交 NPC 无回包", "role_id", session.RoleID(), "npc_id", npc.NpcID, "err", mErr)
					continue
				}
				got, pErr := protocol.ParseNpcFriendshipFriendResult(mResp.Payload)
				if pErr != nil {
					slog.Warn("解析结交 NPC 回包失败", "role_id", session.RoleID(), "err", pErr)
					continue
				}
				switch got.Result {
				case protocol.NpcFriendshipResultSuccess:
					made++
					npc.IsFriend = got.IsFriend
					slog.Info("NPC 结交成功", "role_id", session.RoleID(), "npc_id", npc.NpcID)
				case protocol.NpcFriendshipResultMakeFriendCountLimit, protocol.NpcFriendshipResultMaxCountLimit:
					slog.Info("NPC 结交人数已达上限", "role_id", session.RoleID())
					made = maxNpcFriendshipMakeFriends
				case protocol.NpcFriendshipResultExpMax:
					// 已满级 NPC 不需要再结交。
				case protocol.NpcFriendshipResultExpNoEnough:
					// 服务端仍未认可好感度，保留到下一轮继续送礼。
					slog.Info("NPC 结义好感度仍不足", "role_id", session.RoleID(),
						"npc_id", npc.NpcID, "cur_exp", npc.CurExp)
				default:
					slog.Info("NPC 结交未成功", "role_id", session.RoleID(), "npc_id", npc.NpcID, "result", got.Result)
				}
				if !sleep() {
					return nil
				}
			}

			slog.Info("NPC 结交巡检完成", "role_id", session.RoleID(),
				"answered", answered,
				"missions_taken", taken,
				"made_friends", made,
				"below_threshold", belowThreshold,
			)
			return nil
		},
	)
}
