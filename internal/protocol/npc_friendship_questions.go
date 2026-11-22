// Package protocol 提供 NPC 结交每日问答的最优选项查询。
//
// 数据来源: data/gamedata.db :: NPCQuest (由 scripts/rebuild_dict.py 从官方
// Main.swf :: com.assist.server.NpcFriendShipType.QuestList 提取)。
package protocol

import "sxd-pie-ng/internal/dictionary"

// NpcFriendshipAnswer 表示某个 NPC 问答的最优选项。
type NpcFriendshipAnswer struct {
	// Answer 答案编号 (ANSWER_A=13 / ANSWER_B=14 / ANSWER_C=15)
	Answer uint8
	// Award 该选项获得的好感度经验
	Award int32
}

// NpcFriendshipBestAnswer 从统一静态数据字典查询指定问答的最优答案。
// 未知问题返回 ok=false，调用方必须跳过，避免随机作答触发负面奖励。
func NpcFriendshipBestAnswer(questionID int32) (NpcFriendshipAnswer, bool) {
	repo := dictionary.Default()
	if repo == nil {
		return NpcFriendshipAnswer{}, false
	}
	q, err := repo.GetNPCQuestAnswer(int(questionID))
	if err != nil || q == nil || q.BestAward < 0 || q.BestAnswer == 0 {
		return NpcFriendshipAnswer{}, false
	}
	return NpcFriendshipAnswer{
		Answer: q.BestAnswer,
		Award:  q.BestAward,
	}, true
}
