// Package protocol 提供 NPC 结交每日问答的静态题库。
//
// 数据来源: 官方 Main.swf 的 com.assist.server.NpcFriendShipType.QuestList。
// 每题三个选项的好感度奖励不同，自动化选择奖励最高的选项。
// 奖励为 -1 的选项属于负面答案，任何情况下都不选。
package protocol

// NpcFriendshipAnswer 表示某个 NPC 问答的最优选项。
type NpcFriendshipAnswer struct {
	// Answer 答案编号 (ANSWER_A=13 / ANSWER_B=14 / ANSWER_C=15)
	Answer uint8
	// Award 该选项获得的好感度经验
	Award int32
}

// npcFriendshipBestAnswers 问题编号 -> 最优答案。
//
// QuestList 共 20 题；同一档位取奖励更高者，-1 永不选取。
var npcFriendshipBestAnswers = map[int32]NpcFriendshipAnswer{
	1:  {Answer: NpcFriendshipResultAnswerC, Award: 12},
	2:  {Answer: NpcFriendshipResultAnswerB, Award: 12},
	3:  {Answer: NpcFriendshipResultAnswerB, Award: 12},
	4:  {Answer: NpcFriendshipResultAnswerA, Award: 12},
	5:  {Answer: NpcFriendshipResultAnswerB, Award: 12},
	6:  {Answer: NpcFriendshipResultAnswerA, Award: 12},
	7:  {Answer: NpcFriendshipResultAnswerB, Award: 12},
	8:  {Answer: NpcFriendshipResultAnswerB, Award: 12},
	9:  {Answer: NpcFriendshipResultAnswerC, Award: 12},
	10: {Answer: NpcFriendshipResultAnswerB, Award: 12},
	11: {Answer: NpcFriendshipResultAnswerA, Award: 12},
	12: {Answer: NpcFriendshipResultAnswerB, Award: 12},
	13: {Answer: NpcFriendshipResultAnswerC, Award: 12},
	14: {Answer: NpcFriendshipResultAnswerC, Award: 12},
	15: {Answer: NpcFriendshipResultAnswerA, Award: 12},
	16: {Answer: NpcFriendshipResultAnswerA, Award: 12},
	17: {Answer: NpcFriendshipResultAnswerB, Award: 12},
	18: {Answer: NpcFriendshipResultAnswerB, Award: 10},
	19: {Answer: NpcFriendshipResultAnswerC, Award: 12},
	20: {Answer: NpcFriendshipResultAnswerA, Award: 12},
}

// NpcFriendshipBestAnswer 查询指定问答的最优答案。
// 未知问题返回 ok=false，调用方必须跳过，避免随机作答触发负面奖励。
func NpcFriendshipBestAnswer(questionID int32) (NpcFriendshipAnswer, bool) {
	ans, ok := npcFriendshipBestAnswers[questionID]
	return ans, ok
}
