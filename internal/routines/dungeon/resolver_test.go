// Package dungeon_test 针对关卡智能寻址引擎与双模配置策略进行单元测试。
//
// @author Ateng
// @since 2026-10-11
package dungeon_test

import (
	"errors"
	"testing"

	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/routines/dungeon"
)

// mockDictionaryRepo 内存模拟字典仓储，供无 SQLite 依赖的单元测试隔离使用。
type mockDictionaryRepo struct {
	missions map[int]*dictionary.Mission
}

func (m *mockDictionaryRepo) GetItem(id int) (*dictionary.Item, error) {
	return nil, dictionary.ErrNotFound
}

func (m *mockDictionaryRepo) GetNPC(id int) (*dictionary.NPC, error) {
	return nil, dictionary.ErrNotFound
}

func (m *mockDictionaryRepo) GetRoleType(id int) (*dictionary.RoleType, error) {
	return nil, dictionary.ErrNotFound
}

func (m *mockDictionaryRepo) SearchItems(keyword string) ([]*dictionary.Item, error) {
	return []*dictionary.Item{}, nil
}

func (m *mockDictionaryRepo) Close() error {
	return nil
}

func (m *mockDictionaryRepo) GetMission(id int) (*dictionary.Mission, error) {
	if ms, ok := m.missions[id]; ok {
		return ms, nil
	}
	return nil, dictionary.ErrNotFound
}

func (m *mockDictionaryRepo) GetHighestMission(sweepElite bool) (*dictionary.Mission, error) {
	var highest *dictionary.Mission
	for _, ms := range m.missions {
		if !sweepElite && ms.Type == 1 {
			continue
		}
		if highest == nil || ms.ID > highest.ID {
			highest = ms
		}
	}
	if highest == nil {
		return nil, dictionary.ErrNotFound
	}
	return highest, nil
}

func (m *mockDictionaryRepo) GetHighestMissionByLevel(level int, sweepElite bool) (*dictionary.Mission, error) {
	maxLock := level * 1000
	if level <= 1 {
		maxLock = 1020
	}
	var best *dictionary.Mission
	for _, ms := range m.missions {
		if !sweepElite && ms.Type == 1 {
			continue
		}
		if ms.MissionLock <= maxLock {
			if best == nil || ms.MissionLock > best.MissionLock || (ms.MissionLock == best.MissionLock && ms.ID > best.ID) {
				best = ms
			}
		}
	}
	if best == nil {
		return nil, dictionary.ErrNotFound
	}
	return best, nil
}

func TestMissionResolver_AutoRecommend(t *testing.T) {
	mockRepo := &mockDictionaryRepo{
		missions: map[int]*dictionary.Mission{
			1: {
				ID:          1,
				SectionID:   1,
				MissionLock: 1010,
				Power:       5,
				Name:        "小竹林(1)",
				Type:        0,
			},
			11: {
				ID:          11,
				SectionID:   4,
				MissionLock: 4020,
				Power:       5,
				Name:        "沉没之船(2)",
				Type:        0,
			},
			854: {
				ID:          854,
				SectionID:   63,
				MissionLock: 52090,
				Power:       5,
				Name:        "幽冥地府(9)",
				Type:        0,
			},
			862: {
				ID:          862,
				SectionID:   63,
				MissionLock: 52061,
				Power:       5,
				Name:        "精英幽冥地府(1)",
				Type:        1,
			},
		},
	}

	resolver := dungeon.NewMissionResolver(mockRepo)

	// 1. 测试 1 级新手角色推荐 (应命中 1 号小竹林)
	t.Run("Level1_Beginner", func(t *testing.T) {
		cfg := dungeon.DefaultDungeonSweepConfig()
		res, err := resolver.Resolve(cfg, 1, 100)
		if err != nil {
			t.Fatalf("Resolve 失败: %v", err)
		}
		if res.MissionID != 1 {
			t.Errorf("期望关卡 ID 1, 实际 %d", res.MissionID)
		}
		if res.Mode != dungeon.ModeAutoRecommend {
			t.Errorf("期望模式 ModeAutoRecommend, 实际 %v", res.Mode)
		}
		if res.PowerCost != 5 {
			t.Errorf("期望单次体力消耗 5, 实际 %d", res.PowerCost)
		}
	})

	// 2. 测试 5 级角色推荐 (应命中 11 号沉没之船)
	t.Run("Level5_Intermediate", func(t *testing.T) {
		cfg := dungeon.DefaultDungeonSweepConfig()
		res, err := resolver.Resolve(cfg, 5, 100)
		if err != nil {
			t.Fatalf("Resolve 失败: %v", err)
		}
		if res.MissionID != 11 {
			t.Errorf("期望关卡 ID 11, 实际 %d", res.MissionID)
		}
	})

	// 3. 测试 60 级角色推荐 (应命中 854 号幽冥地府)
	t.Run("Level60_HighTier", func(t *testing.T) {
		cfg := dungeon.DefaultDungeonSweepConfig()
		res, err := resolver.Resolve(cfg, 60, 100)
		if err != nil {
			t.Fatalf("Resolve 失败: %v", err)
		}
		if res.MissionID != 854 {
			t.Errorf("期望关卡 ID 854, 实际 %d", res.MissionID)
		}
		if res.IsElite {
			t.Errorf("默认推荐不应为精英关卡")
		}
	})
}

func TestMissionResolver_ExpertOverride(t *testing.T) {
	mockRepo := &mockDictionaryRepo{
		missions: map[int]*dictionary.Mission{
			105: {
				ID:          105,
				SectionID:   12,
				MissionLock: 15000,
				Power:       5,
				Name:        "即墨郊外(5)",
				Type:        0,
			},
			854: {
				ID:          854,
				SectionID:   63,
				MissionLock: 52090,
				Power:       5,
				Name:        "幽冥地府(9)",
				Type:        0,
			},
		},
	}

	resolver := dungeon.NewMissionResolver(mockRepo)

	// 1. 专家模式合法关卡解析
	t.Run("ValidOverride", func(t *testing.T) {
		cfg := dungeon.ExpertDungeonSweepConfig(105, 20)
		res, err := resolver.Resolve(cfg, 20, 50)
		if err != nil {
			t.Fatalf("Resolve 失败: %v", err)
		}
		if res.MissionID != 105 {
			t.Errorf("期望 MissionID 105, 实际 %d", res.MissionID)
		}
		if res.Mode != dungeon.ModeExpertOverride {
			t.Errorf("期望模式 ModeExpertOverride, 实际 %v", res.Mode)
		}
		if res.MissionName != "即墨郊外(5)" {
			t.Errorf("期望关卡名 '即墨郊外(5)', 实际 %s", res.MissionName)
		}
	})

	// 2. 专家模式越级挑战防护拦截
	t.Run("LevelTooLow_Protection", func(t *testing.T) {
		// 角色仅 5 级 (maxLock=5000)，却试图指定 854 关卡 (lock=52090)
		cfg := dungeon.ExpertDungeonSweepConfig(854, 10)
		_, err := resolver.Resolve(cfg, 5, 50)
		if !errors.Is(err, dungeon.ErrLevelTooLow) {
			t.Errorf("期望返回 ErrLevelTooLow, 实际返回: %v", err)
		}
	})

	// 3. 专家模式指定不存在的关卡拦截
	t.Run("InvalidMissionID", func(t *testing.T) {
		cfg := dungeon.ExpertDungeonSweepConfig(999999, 10)
		_, err := resolver.Resolve(cfg, 100, 50)
		if !errors.Is(err, dungeon.ErrInvalidMission) {
			t.Errorf("期望返回 ErrInvalidMission, 实际返回: %v", err)
		}
	})
}

func TestMissionResolver_StaminaGuard(t *testing.T) {
	resolver := dungeon.NewMissionResolver(nil)

	// 1. 体力不足 5 点时拦截
	cfg := dungeon.DefaultDungeonSweepConfig()
	_, err := resolver.Resolve(cfg, 50, 4)
	if !errors.Is(err, dungeon.ErrInsufficientStamina) {
		t.Errorf("期望返回 ErrInsufficientStamina, 实际返回: %v", err)
	}

	// 2. 体力恰好 5 点时通过
	res, err := resolver.Resolve(cfg, 50, 5)
	if err != nil {
		t.Fatalf("5 点体力应当通过卫语句: %v", err)
	}
	if res == nil || res.PowerCost != 5 {
		t.Errorf("期望返回有效解析结果且消耗 5 点体力")
	}
}

func TestMissionResolver_RealDatabaseIntegration(t *testing.T) {
	repo, err := dictionary.NewRepository("")
	if err != nil {
		t.Skipf("本地缺少 Pieb.db 数据字典，跳过真实数据库集成测试: %v", err)
	}
	defer repo.Close()

	resolver := dungeon.NewMissionResolver(repo)

	// 1. 验证 300 级顶级角色自动推荐最优关卡
	cfg := dungeon.DefaultDungeonSweepConfig()
	res, err := resolver.Resolve(cfg, 300, 100)
	if err != nil {
		t.Fatalf("真实数据库 Resolve 失败: %v", err)
	}
	if res.MissionID <= 0 || res.MissionName == "" {
		t.Fatalf("期望得到有效关卡，实际: %+v", res)
	}
	if res.PowerCost != 5 {
		t.Errorf("单次扫荡体力消耗必须为 5, 实际 %d", res.PowerCost)
	}

	// 2. 验证专家模式指定 854 关卡
	expertCfg := dungeon.ExpertDungeonSweepConfig(854, 30)
	res854, err := resolver.Resolve(expertCfg, 300, 100)
	if err != nil {
		t.Fatalf("专家模式指定 854 关卡失败: %v", err)
	}
	if res854.MissionID != 854 {
		t.Errorf("期望关卡 854, 实际 %d", res854.MissionID)
	}
}
