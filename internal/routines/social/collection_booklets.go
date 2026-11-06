// Package social 提供仙盟、好友、住宅、收藏册等社交玩法自动化。
//
// 本文件实现「神仙收藏册」(Mod_CollectionBooklets_Base, Module 815) 自动化。
package social

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// maxCollectionBookletsApprovals 单轮最多拜访点赞的好友数量。
const maxCollectionBookletsApprovals = 30

// NewCollectionBookletsRoutine 构造神仙收藏册日常任务。
//
// 官方 com.views.collection_booklets.CollectionBookletsView 的免费产出闭环：
//  1. popularity_award_info 读取当前声望与已领取档位；
//  2. 对声望已达标且未领取的档位调用 get_popularity_award；
//  3. my_friend 读取好友列表，对 is_open_visit=1 的好友调用 approve_player 获得声望。
//
// 自动化不发送留言、不修改展示角色、不消耗元宝。
func NewCollectionBookletsRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"collection_booklets",
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
			slog.Info("正在执行社交任务: 神仙收藏册", "role_id", session.RoleID())

			call := func(pkt *protocol.Packet, act uint32) (*protocol.Packet, error) {
				cCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				return session.Call(cCtx, pkt, act)
			}

			// 1. 声望档位奖励。
			resp, err := call(protocol.BuildCollectionBookletsPopularityInfoPacket(), protocol.ActionIDCollectionBookletsPopularityInfo)
			if err != nil || resp == nil {
				slog.Info("神仙收藏册功能不可用或未开放，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			popInfo, err := protocol.ParseCollectionBookletsPopularityInfoResult(resp.Payload)
			if err != nil {
				slog.Info("解析神仙收藏册声望信息失败，跳过", "role_id", session.RoleID(), "err", err)
				return nil
			}
			claimed := make(map[int32]bool, len(popInfo.ClaimedIDs))
			for _, id := range popInfo.ClaimedIDs {
				claimed[id] = true
			}
			slog.Info("神仙收藏册声望",
				"role_id", session.RoleID(),
				"popularity", popInfo.Popularity,
				"claimed", len(popInfo.ClaimedIDs),
			)
			for _, award := range protocol.CollectionBookletsPopularityThresholds {
				if claimed[award.ID] || popInfo.Popularity < award.Popularity {
					continue
				}
				if err := ctx.Err(); err != nil {
					return nil
				}
				aResp, aErr := call(
					protocol.BuildCollectionBookletsGetPopularityAwardPacket(award.ID),
					protocol.ActionIDCollectionBookletsGetPopularityAward,
				)
				if aErr != nil || aResp == nil {
					slog.Warn("领取神仙收藏册声望奖励无回包", "role_id", session.RoleID(), "award_id", award.ID, "err", aErr)
					return nil
				}
				code, pErr := protocol.ParseCollectionBookletsActionCode(aResp.Payload)
				if pErr != nil {
					slog.Warn("解析声望奖励回包失败", "role_id", session.RoleID(), "err", pErr)
					return nil
				}
				switch code {
				case protocol.CollectionBookletsResultSuccess:
					slog.Info("神仙收藏册声望奖励领取成功", "role_id", session.RoleID(), "award_id", award.ID)
				case protocol.CollectionBookletsResultIsGet:
					slog.Debug("神仙收藏册声望奖励已领取", "role_id", session.RoleID(), "award_id", award.ID)
				case protocol.CollectionBookletsResultNotEnoughPopularity:
					slog.Info("神仙收藏册声望不足，停止领取", "role_id", session.RoleID(), "award_id", award.ID)
					return nil
				case protocol.CollectionBookletsResultItemPackFull,
					protocol.CollectionBookletsResultFatePackFull,
					protocol.CollectionBookletsResultDragonBallPackFull:
					return scheduler.ErrBagFull
				default:
					slog.Info("神仙收藏册声望奖励未领取", "role_id", session.RoleID(), "award_id", award.ID, "result", code)
					return nil
				}
			}

			// 2. 拜访好友获得声望。
			fResp, fErr := call(protocol.BuildCollectionBookletsMyFriendPacket(), protocol.ActionIDCollectionBookletsMyFriend)
			if fErr != nil || fResp == nil {
				slog.Warn("查询神仙收藏册好友列表无回包", "role_id", session.RoleID(), "err", fErr)
				return nil
			}
			friendResult, friends, pErr := protocol.ParseCollectionBookletsMyFriendResult(fResp.Payload)
			if pErr != nil {
				slog.Warn("解析神仙收藏册好友列表失败", "role_id", session.RoleID(), "err", pErr)
				return nil
			}
			if friendResult != protocol.CollectionBookletsResultSuccess {
				slog.Info("神仙收藏册好友列表不可用", "role_id", session.RoleID(), "result", friendResult)
				return nil
			}
			approvals := 0
			for _, friend := range friends {
				if approvals >= maxCollectionBookletsApprovals {
					break
				}
				if friend.IsOpenVisit != 1 || friend.PlayerID == session.PlayerID() {
					continue
				}
				if err := ctx.Err(); err != nil {
					return nil
				}
				approveResp, apErr := call(
					protocol.BuildCollectionBookletsApprovePacket(friend.PlayerID, friend.ServerName),
					protocol.ActionIDCollectionBookletsApprove,
				)
				if apErr != nil || approveResp == nil {
					slog.Warn("拜访神仙收藏册好友无回包", "role_id", session.RoleID(), "player_id", friend.PlayerID, "err", apErr)
					return nil
				}
				got, apParseErr := protocol.ParseCollectionBookletsApproveResult(approveResp.Payload)
				if apParseErr != nil {
					slog.Warn("解析拜访回包失败", "role_id", session.RoleID(), "err", apParseErr)
					return nil
				}
				switch got.Result {
				case protocol.CollectionBookletsResultSuccess:
					approvals++
					slog.Info("神仙收藏册拜访点赞成功",
						"role_id", session.RoleID(),
						"friend", friend.Nickname,
						"player_id", friend.PlayerID,
						"left_times", got.LeftApproveTimes,
						"award_items", got.AwardItemIDs,
					)
				case protocol.CollectionBookletsResultNotEnoughApproveTimes:
					slog.Info("神仙收藏册今日拜访次数已用尽", "role_id", session.RoleID())
					return nil
				case protocol.CollectionBookletsResultNotApproveSelf:
					continue
				case protocol.CollectionBookletsResultNoOpenVisit:
					continue
				case protocol.CollectionBookletsResultItemPackFull,
					protocol.CollectionBookletsResultFatePackFull,
					protocol.CollectionBookletsResultDragonBallPackFull:
					return scheduler.ErrBagFull
				default:
					slog.Info("神仙收藏册拜访未成功", "role_id", session.RoleID(), "player_id", friend.PlayerID, "result", got.Result)
				}
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(200 * time.Millisecond):
				}
			}
			// 重新读取声望面板，便于日志观察最终声望。
			if again, aErr := call(protocol.BuildCollectionBookletsPopularityInfoPacket(), protocol.ActionIDCollectionBookletsPopularityInfo); aErr == nil && again != nil {
				if finalInfo, fpErr := protocol.ParseCollectionBookletsPopularityInfoResult(again.Payload); fpErr == nil {
					slog.Info("神仙收藏册巡检完成",
						"role_id", session.RoleID(),
						"approvals", approvals,
						"popularity", finalInfo.Popularity,
					)
					return nil
				}
			}
			slog.Info("神仙收藏册巡检完成", "role_id", session.RoleID(), "approvals", approvals)
			return nil
		},
	)
}
