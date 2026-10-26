// Package routines_test 提供玩法活动注册中心与子域玩法的测试用例。
//
// @author Ateng
// @since 2026-10-08
package routines_test

import (
	"testing"

	"sxd-pie-ng/internal/routines"
	"sxd-pie-ng/internal/routines/daily"
	"sxd-pie-ng/internal/routines/dungeon"
	"sxd-pie-ng/internal/routines/farming"
	"sxd-pie-ng/internal/routines/minigame"
	"sxd-pie-ng/internal/routines/pvp"
	"sxd-pie-ng/internal/routines/social"
	"sxd-pie-ng/internal/scheduler"
)

func TestRoutines_DefinitionsAndRegistry(t *testing.T) {
	defs := routines.GetAllDefinitions()
	if len(defs) < 30 {
		t.Fatalf("预期玩法定义数量 >= 30, 实际为 %d", len(defs))
	}

	// 验证涵盖六大领域
	domainSet := make(map[string]bool)
	for _, d := range defs {
		domainSet[d.Domain] = true
	}

	expectedDomains := []string{"daily", "farming", "social", "dungeon", "pvp", "minigame"}
	for _, exp := range expectedDomains {
		if !domainSet[exp] {
			t.Errorf("缺少领域定义: %s", exp)
		}
	}

	// 验证批量注册
	factory := routines.NewRegistryFactory(nil, nil)
	fastJitter := scheduler.NewJitter(scheduler.JitterConfig{})
	sched := scheduler.NewRoleScheduler("role-test", nil, fastJitter)

	// 注册默认启用的玩法
	registered := factory.RegisterAll(sched, nil)
	if registered == 0 {
		t.Fatal("未成功注册任何默认玩法")
	}

	// 验证特定玩法自定义开关
	customMap := map[string]bool{
		"herb_garden": false,
		"dungeon":     false,
	}
	sched2 := scheduler.NewRoleScheduler("role-test-2", nil, fastJitter)
	regCount2 := factory.RegisterAll(sched2, customMap)
	if regCount2 != registered-2 {
		t.Errorf("预期关闭 2 个玩法后注册数为 %d, 实际为 %d", registered-2, regCount2)
	}
}

func TestSubdomains_Instances(t *testing.T) {
	d := daily.NewSignInRoutine()
	if d.Name() != "daily_sign_in" {
		t.Errorf("daily_sign_in name mismatch: %s", d.Name())
	}

	f := farming.NewHerbGardenRoutine()
	if f.Name() != "herb_garden" {
		t.Errorf("herb_garden name mismatch: %s", f.Name())
	}

	s := social.NewGuildActivitiesRoutine()
	if s.Name() != "guild_activities" {
		t.Errorf("guild_activities name mismatch: %s", s.Name())
	}

	dg := dungeon.NewDungeonSweepRoutine(nil)
	if dg.Name() != "dungeon" {
		t.Errorf("dungeon name mismatch: %s", dg.Name())
	}

	p := pvp.NewArenaRoutine()
	if p.Name() != "arena" {
		t.Errorf("arena name mismatch: %s", p.Name())
	}

	m := minigame.NewLuckyStarRoutine()
	if m.Name() != "lucky_star" {
		t.Errorf("lucky_star name mismatch: %s", m.Name())
	}

	gr := farming.NewGourdRealmRoutine(nil)
	if gr.Name() != "gourd_realm" {
		t.Errorf("gourd_realm name mismatch: %s", gr.Name())
	}

	ie := minigame.NewImperialExamRoutine(nil)
	if ie.Name() != "imperial_exam" {
		t.Errorf("imperial_exam name mismatch: %s", ie.Name())
	}

	// 验证 16 大抓包新功能实例正常
	checks := []struct {
		r    scheduler.ActivityRoutine
		want string
	}{
		{social.NewJieYuanBoxRoutine(), "jieyuan_box"},
		{daily.NewGodBlessRoutine(), "god_bless"},
		{daily.NewOnlineGiftRoutine(), "online_gift"},
		{daily.NewDailyQuestRoutine(), "daily_quest"},
		{farming.NewSpiritPoolRoutine(), "spirit_pool"},
		{pvp.NewCaravanHijackRoutine(), "caravan_hijack"},
		{dungeon.NewQiMenRoutine(), "qimen_dunjia"},
		{dungeon.NewXunXianRoutine(), "xunxian_road"},
		{dungeon.NewDemonTowerRoutine(), "demon_tower"},
		{minigame.NewEraTravelRoutine(), "era_travel"},
		{minigame.NewLingDaoRoutine(), "lingdao_practice"},
		{minigame.NewHuanshouRoutine(), "huanshou_invade"},
		{minigame.NewWuFaRoutine(), "wufa_field"},
		{minigame.NewMasterPracticeRoutine(), "master_practice"},
		{minigame.NewTreasureHouseRoutine(), "treasure_house"},
	}

	for _, tc := range checks {
		if tc.r == nil || tc.r.Name() != tc.want {
			t.Errorf("玩法实例不符或为空: want=%s, got=%v", tc.want, tc.r)
		}
	}
}

