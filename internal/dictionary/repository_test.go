// Package dictionary_test 提供数据字典仓储模块的黑盒行为测试。
//
// @author Ateng
// @since 2026-10-08
package dictionary_test

import (
	"errors"
	"testing"

	"sxd-pie-ng/internal/dictionary"
)

func TestRepository_Queries(t *testing.T) {
	repo, err := dictionary.NewRepository("")
	if err != nil {
		t.Fatalf("初始化数据字典仓储失败: %v", err)
	}
	defer repo.Close()

	t.Run("GetItem", func(t *testing.T) {
		item, err := repo.GetItem(7)
		if err != nil {
			t.Fatalf("获取道具失败: %v", err)
		}
		if item == nil || item.Name != "气血包" {
			t.Errorf("期望道具名称为 '气血包', 实际为 %+v", item)
		}
		if item.TypeID != 10001 {
			t.Errorf("期望道具 TypeID 为 10001, 实际为 %d", item.TypeID)
		}

		// 测试不存在的道具
		_, err = repo.GetItem(99999999)
		if !errors.Is(err, dictionary.ErrNotFound) {
			t.Errorf("期望返回 ErrNotFound, 实际返回 %v", err)
		}
	})

	t.Run("GetNPC", func(t *testing.T) {
		npc, err := repo.GetNPC(1)
		if err != nil {
			t.Fatalf("获取 NPC 失败: %v", err)
		}
		if npc == nil || npc.Name != "村长" {
			t.Errorf("期望 NPC 名称为 '村长', 实际为 %+v", npc)
		}
	})

	t.Run("GetRoleType", func(t *testing.T) {
		role, err := repo.GetRoleType(1)
		if err != nil {
			t.Fatalf("获取角色伙伴失败: %v", err)
		}
		if role == nil || role.Name != "剑灵男" {
			t.Errorf("期望伙伴名称为 '剑灵男', 实际为 %+v", role)
		}
	})

	t.Run("SearchItems", func(t *testing.T) {
		items, err := repo.SearchItems("气血")
		if err != nil {
			t.Fatalf("模糊搜索道具失败: %v", err)
		}
		if len(items) == 0 {
			t.Fatal("搜索 '气血' 返回结果为空")
		}
		found := false
		for _, it := range items {
			if it.Name == "气血包" {
				found = true
				break
			}
		}
		if !found {
			t.Error("搜索结果中未包含 '气血包'")
		}
	})

	t.Run("GetMission", func(t *testing.T) {
		m, err := repo.GetMission(1)
		if err != nil {
			t.Fatalf("获取副本关卡失败: %v", err)
		}
		if m == nil || m.Name != "浮月林道(1)" {
			t.Errorf("期望关卡名称为 '浮月林道(1)', 实际为 %+v", m)
		}
		if m.Power != 5 {
			t.Errorf("期望单次体力消耗为 5, 实际为 %d", m.Power)
		}

		highest, err := repo.GetHighestMission(false)
		if err != nil {
			t.Fatalf("获取最高等级副本失败: %v", err)
		}
		if highest == nil || highest.ID <= 0 {
			t.Fatalf("期望返回有效的最高等级关卡, 实际为 %+v", highest)
		}

		// 验证根据等级寻址最优关卡
		lvl1Mission, err := repo.GetHighestMissionByLevel(1, false)
		if err != nil {
			t.Fatalf("获取 1 级推荐关卡失败: %v", err)
		}
		if lvl1Mission == nil || lvl1Mission.ID <= 0 {
			t.Errorf("1 级推荐关卡应有效: %+v", lvl1Mission)
		}

		lvl50Mission, err := repo.GetHighestMissionByLevel(50, false)
		if err != nil {
			t.Fatalf("获取 50 级推荐关卡失败: %v", err)
		}
		if lvl50Mission.MissionLock > 50*1000 {
			t.Errorf("50 级推荐关卡 Lock (%d) 不应超出 50000", lvl50Mission.MissionLock)
		}
		if lvl50Mission.MissionLock <= lvl1Mission.MissionLock {
			t.Errorf("50 级推荐关卡 Lock 应高于 1 级关卡: %d vs %d", lvl50Mission.MissionLock, lvl1Mission.MissionLock)
		}

		// 验证三类副本 (主线 0, 精英 1, 支线 2) 查询
		mainMission, err := repo.GetHighestMissionByTypeAndLevel(50, 0)
		if err != nil || mainMission == nil || mainMission.Type != 0 {
			t.Errorf("主线关卡查询失败: %v, %+v", err, mainMission)
		}

		eliteMission, err := repo.GetHighestMissionByTypeAndLevel(50, 1)
		if err != nil || eliteMission == nil || eliteMission.Type != 1 {
			t.Errorf("精英关卡查询失败: %v, %+v", err, eliteMission)
		}

		sideMission, err := repo.GetHighestMissionByTypeAndLevel(50, 2)
		// 支线若库中存在则必须 Type=2，若不存在返回 ErrNotFound 均可接受
		if err == nil && sideMission != nil && sideMission.Type != 2 {
			t.Errorf("支线关卡查询预期 Type=2，实际为 %d", sideMission.Type)
		}
	})
}
