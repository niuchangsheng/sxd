// Package social 提供仙盟、好友、住宅、收藏册等社交玩法自动化。
//
// 本文件实现「NPC 结交」(Mod_NpcFriendship_Base, Module 436) 自动化。
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

// maxNpcFriendshipMakeFriends 单轮最多结交的 NPC 数量。
const maxNpcFriendshipMakeFriends = 20

// NewNpcFriendshipRoutine 构造 NPC 结交日常任务。
//
// 官方流程 (com.datas.NpcFriendShipData / com.views.NpcFriendShipView)：
//  1. get_info 查询全部 NPC 好感度、结交状态与任务状态；
//  2. 对 is_complete=1 且 is_receive=0 的任务调用 gain_mission_award；
//  3. 对未结交 NPC 调用 make_friend；
//  4. 按 cur_exp 从高到低给 NPC 赠送伴手礼 (present_id=1，铜钱系道具)。
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

			resp, err := call(protocol.BuildNpcFriendshipInfoPacket(), protocol.ActionIDNpcFriendshipInfo)
			if err != nil || resp == nil {
				slog.Info("NPC 结交功能不可用或未开放，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			snap, err := protocol.ParseNpcFriendshipInfoResult(resp.Payload)
			if err != nil {
				slog.Info("解析 NPC 结交面板失败，跳过", "role_id", session.RoleID(), "err", err)
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

			// 1. 领取已完成的结交任务奖励。
			for _, npc := range snap.Npcs {
				if npc.IsComplete != 1 || npc.IsReceive != 0 {
					continue
				}
				if err := ctx.Err(); err != nil {
					return nil
				}
				aResp, aErr := call(
					protocol.BuildNpcFriendshipGainMissionAwardPacket(npc.NpcID),
					protocol.ActionIDNpcFriendshipGainMissionAward,
				)
				if aErr != nil || aResp == nil {
					slog.Info("领取 NPC 结交任务奖励无回包", "role_id", session.RoleID(), "npc_id", npc.NpcID, "err", aErr)
					continue
				}
				if len(aResp.Payload) == 0 {
					continue
				}
				code := aResp.Payload[0]
				switch code {
				case protocol.NpcFriendshipResultSuccess:
					slog.Info("NPC 结交任务奖励领取成功", "role_id", session.RoleID(), "npc_id", npc.NpcID)
				case protocol.NpcFriendshipResultPackFull:
					return scheduler.ErrBagFull
				default:
					slog.Info("NPC 结交任务奖励未领取", "role_id", session.RoleID(), "npc_id", npc.NpcID, "result", code)
				}
			}

			// 2. 结交尚未成为好友的 NPC (免费)。
			made := 0
			for _, npc := range snap.Npcs {
				if made >= maxNpcFriendshipMakeFriends {
					break
				}
				if npc.IsFriend == 1 {
					continue
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
				default:
					slog.Info("NPC 结交未成功", "role_id", session.RoleID(), "npc_id", npc.NpcID, "result", got.Result)
				}
			}

			// 3. 赠送伴手礼 (铜钱系礼物)。
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
				return nil
			}

			// 好感度从高到低排序；满级 NPC 由服务端 EXP_MAX 分支跳过。
			candidates := make([]protocol.NpcFriendshipInfo, 0, len(snap.Npcs))
			for _, npc := range snap.Npcs {
				if npc.IsFriend == 1 {
					candidates = append(candidates, npc)
				}
			}
			sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].CurExp > candidates[j].CurExp })

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
					return nil
				}
				gResp, gErr := call(
					protocol.BuildNpcFriendshipGivePresentPacket(protocol.NpcFriendshipPresentSouvenir, npc.NpcID),
					protocol.ActionIDNpcFriendshipGivePresent,
				)
				if gErr != nil || gResp == nil {
					slog.Info("赠送 NPC 伴手礼无回包", "role_id", session.RoleID(), "npc_id", npc.NpcID, "err", gErr)
					return nil
				}
				got, pParseErr := protocol.ParseNpcFriendshipGiveResult(gResp.Payload)
				if pParseErr != nil {
					slog.Warn("解析赠送 NPC 伴手礼回包失败", "role_id", session.RoleID(), "err", pParseErr)
					return nil
				}
				switch got.Result {
				case protocol.NpcFriendshipResultSuccess:
					given++
					slog.Info("赠送 NPC 伴手礼成功",
						"role_id", session.RoleID(),
						"npc_id", npc.NpcID,
						"new_exp", got.NewExp,
						"given", given,
					)
				case protocol.NpcFriendshipResultCountLimit, protocol.NpcFriendshipResultMaxCountLimit:
					slog.Info("NPC 结交送礼次数已达上限", "role_id", session.RoleID())
					return nil
				case protocol.NpcFriendshipResultExpMax:
					slog.Debug("NPC 好感度已满级，跳过", "role_id", session.RoleID(), "npc_id", npc.NpcID)
					continue
				case protocol.NpcFriendshipResultNoEnough:
					slog.Info("NPC 结交礼物不足，停止赠送", "role_id", session.RoleID())
					return nil
				default:
					slog.Info("赠送 NPC 伴手礼未成功", "role_id", session.RoleID(), "npc_id", npc.NpcID, "result", got.Result)
					return nil
				}
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(200 * time.Millisecond):
				}
			}
			slog.Info("NPC 结交巡检完成", "role_id", session.RoleID(), "made_friends", made, "gifts", given)
			return nil
		},
	)
}
