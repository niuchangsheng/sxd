package dungeon

import (
	"testing"

	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/protocol"
)

type stubQuestRepo struct {
	dictionary.Repository
	quests map[int]*dictionary.Quest
}

func (r *stubQuestRepo) GetQuest(id int) (*dictionary.Quest, error) {
	return r.quests[id], nil
}

// TestStaminaRoutinePriorityOrder 验证体力消耗玩法的调度优先级顺序：
// 主支线/精英首通推图 (dungeon) > 通天塔 (tong_tian_tower) > 精英副本扫荡 (dungeon_sweep)。
func TestStaminaRoutinePriorityOrder(t *testing.T) {
	questPush := NewQuestPushRoutine(nil)
	tower := NewTongTianTowerRoutine()
	eliteSweep := NewEliteSweepRoutine(nil)

	if questPush.Name() != "dungeon" {
		t.Fatalf("questPush.Name() = %q, want dungeon", questPush.Name())
	}
	if eliteSweep.Name() != "dungeon_sweep" {
		t.Fatalf("eliteSweep.Name() = %q, want dungeon_sweep", eliteSweep.Name())
	}
	if !(questPush.Priority() > tower.Priority()) {
		t.Fatalf("主支线/精英推图优先级 (%d) 必须高于通天塔 (%d)", questPush.Priority(), tower.Priority())
	}
	if !(tower.Priority() > eliteSweep.Priority()) {
		t.Fatalf("通天塔优先级 (%d) 必须高于精英副本扫荡 (%d)", tower.Priority(), eliteSweep.Priority())
	}
}

// TestSortQuestItemsByPriority 验证任务接取与推进均严格遵循：
// 主线副本 (Type=1) > 支线副本 (Type=2) > 精英副本 (Type=4) > 其他。
func TestSortQuestItemsByPriority(t *testing.T) {
	repo := &stubQuestRepo{
		quests: map[int]*dictionary.Quest{
			101: {ID: 101, Type: 4, MissionID: 501, Lock: 10}, // 精英副本
			102: {ID: 102, Type: 2, MissionID: 401, Lock: 20}, // 支线副本
			103: {ID: 103, Type: 1, MissionID: 301, Lock: 30}, // 主线副本
			104: {ID: 104, Type: 9, MissionID: 601, Lock: 5},  // 其他任务
		},
	}

	// 1. 接取阶段 (talkFirst=false): 严格按 主线 > 支线 > 精英 > 其他
	acceptable := []protocol.PlayerQuestItem{
		{QuestID: 104, State: 0},
		{QuestID: 101, State: 0},
		{QuestID: 102, State: 0},
		{QuestID: 103, State: 0},
	}
	sortQuestItemsByPriority(acceptable, repo, false)
	want := []int32{103, 102, 101, 104}
	for i, w := range want {
		if acceptable[i].QuestID != w {
			t.Fatalf("接取排序位置 %d: got QuestID=%d, want %d (full=%+v)", i, acceptable[i].QuestID, w, acceptable)
		}
	}

	// 2. 推进阶段 (talkFirst=true): 同为副本战斗任务时仍严格按 主线 > 支线 > 精英 > 其他
	inProgress := []protocol.PlayerQuestItem{
		{QuestID: 101, State: 1},
		{QuestID: 102, State: 1},
		{QuestID: 104, State: 1},
		{QuestID: 103, State: 1},
	}
	sortQuestItemsByPriority(inProgress, repo, true)
	for i, w := range want {
		if inProgress[i].QuestID != w {
			t.Fatalf("推进排序位置 %d: got QuestID=%d, want %d (full=%+v)", i, inProgress[i].QuestID, w, inProgress)
		}
	}
}
