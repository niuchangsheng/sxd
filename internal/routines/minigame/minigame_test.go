// Package minigame 提供吉星高照、仙履奇缘答题、刮刮卡、钓鱼等益智趣味活动。
//
// @author Ateng
// @since 2026-10-11
package minigame

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/qa"
	"sxd-pie-ng/internal/scheduler"
)

func TestImmortalFantasyRoutine_Execute(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_test_minigame",
		RoleName:   "测试仙履角色",
		ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()

	time.Sleep(100 * time.Millisecond)

	qaEngine, err := qa.NewEngine("")
	if err != nil {
		t.Fatalf("加载 QA 引擎失败: %v", err)
	}

	t.Run("仙履奇缘全链路答题闭环", func(t *testing.T) {
		routine := NewImmortalFantasyRoutine(qaEngine)
		if routine.Name() != "immortal_fantasy" {
			t.Fatalf("任务名称不匹配: %s", routine.Name())
		}
		if dr, ok := routine.(scheduler.DetailedRoutine); ok {
			if dr.Domain() != "minigame" {
				t.Fatalf("领域不匹配: %s", dr.Domain())
			}
		}

		err := routine.Execute(context.Background(), session, nil)
		if err != nil {
			t.Fatalf("仙履奇缘例程执行失败: %v", err)
		}
	})

	t.Run("金榜题名会试答题", func(t *testing.T) {
		r2 := NewImperialExamRoutine(qaEngine)
		if err := r2.Execute(context.Background(), session, nil); err != nil {
			t.Fatalf("金榜题名执行失败: %v", err)
		}
	})

	t.Run("帮派吉星高照摇骰子", func(t *testing.T) {
		r3 := NewLuckyStarRoutine()
		if err := r3.Execute(context.Background(), session, nil); err != nil {
			t.Fatalf("吉星高照执行失败: %v", err)
		}
	})

	t.Run("空会话防御", func(t *testing.T) {
		routine := NewImmortalFantasyRoutine(qaEngine)
		if err := routine.Execute(context.Background(), nil, nil); err != client.ErrNotConnected {
			t.Errorf("期望 ErrNotConnected, 实际: %v", err)
		}
	})

	t.Run("天帝宝库自选三档大奖配置", func(t *testing.T) {
		thCfg := DefaultTreasureHouseConfig()
		if len(thCfg.Awards) != 3 {
			t.Fatalf("期望默认三档大奖, 实际 %d 档", len(thCfg.Awards))
		}
		r := NewTreasureHouseRoutine(thCfg)
		if r.Name() != "treasure_house" {
			t.Fatalf("任务名称不匹配: %s", r.Name())
		}
		if err := r.Execute(context.Background(), session, nil); err != nil {
			t.Fatalf("天帝宝库执行异常: %v", err)
		}
	})
}

func TestTreasureHouse_InteractiveSelection(t *testing.T) {
	pool := &protocol.TreasureHousePool{
		TotalCount: 45,
		Tier1Items: []int32{50, 12, 9, 5, 2, 7, 1, 15, 11, 3, 14, 4, 6, 10, 8},
		Tier2Items: []int32{21, 30, 23, 18, 20, 51, 27, 29, 28, 22, 26, 17, 24, 19, 25},
		Tier3Items: []int32{42, 45, 33, 40, 31, 34, 41, 36, 37, 44, 32, 39, 43, 35, 38},
	}
	defaults := []protocol.TreasureAwardSelect{
		{TierID: 1, ItemID: 50},
		{TierID: 2, ItemID: 25},
		{TierID: 3, ItemID: 42},
	}

	t.Run("按序号交互选择", func(t *testing.T) {
		input := strings.NewReader("2\n1\n3\n")
		var out bytes.Buffer
		chosen := promptUserAwardSelection(context.Background(), input, &out, pool, nil, defaults)
		if len(chosen) != 3 {
			t.Fatalf("期望三档, 实际: %d", len(chosen))
		}
		if chosen[0].ItemID != 12 { // 序号 2 对应 12
			t.Errorf("Tier 1 期望 12, 实际: %d", chosen[0].ItemID)
		}
		if chosen[1].ItemID != 21 { // 序号 1 对应 21
			t.Errorf("Tier 2 期望 21, 实际: %d", chosen[1].ItemID)
		}
		if chosen[2].ItemID != 33 { // 序号 3 对应 33
			t.Errorf("Tier 3 期望 33, 实际: %d", chosen[2].ItemID)
		}
	})

	t.Run("按道具ID精确交互选择", func(t *testing.T) {
		input := strings.NewReader("id:14\n#25\nid:35\n")
		var out bytes.Buffer
		chosen := promptUserAwardSelection(context.Background(), input, &out, pool, nil, defaults)
		if chosen[0].ItemID != 14 || chosen[1].ItemID != 25 || chosen[2].ItemID != 35 {
			t.Errorf("道具ID匹配不符合预期: %+v", chosen)
		}
	})

	t.Run("按高位道具ID直接交互选择", func(t *testing.T) {
		input := strings.NewReader("50\n25\n42\n")
		var out bytes.Buffer
		chosen := promptUserAwardSelection(context.Background(), input, &out, pool, nil, defaults)
		if chosen[0].ItemID != 50 || chosen[1].ItemID != 25 || chosen[2].ItemID != 42 {
			t.Errorf("大号道具ID直接匹配不符合预期: %+v", chosen)
		}
	})

	t.Run("直接回车使用默认配置", func(t *testing.T) {
		input := strings.NewReader("\n\n\n")
		var out bytes.Buffer
		chosen := promptUserAwardSelection(context.Background(), input, &out, pool, nil, defaults)
		if chosen[0].ItemID != 50 || chosen[1].ItemID != 25 || chosen[2].ItemID != 42 {
			t.Errorf("默认回车项不符合预期: %+v", chosen)
		}
	})

	t.Run("非法输入优雅回退默认", func(t *testing.T) {
		input := strings.NewReader("not_a_number\n9999\n-1\n")
		var out bytes.Buffer
		chosen := promptUserAwardSelection(context.Background(), input, &out, pool, nil, defaults)
		if chosen[0].ItemID != 50 || chosen[1].ItemID != 25 || chosen[2].ItemID != 42 {
			t.Errorf("非法输入回退不符合预期: %+v", chosen)
		}
	})

	t.Run("自定义Selector配置", func(t *testing.T) {
		called := false
		cfg := TreasureHouseConfig{
			Selector: func(ctx context.Context, p *protocol.TreasureHousePool, dict dictionary.Repository) ([]protocol.TreasureAwardSelect, error) {
				called = true
				return []protocol.TreasureAwardSelect{
					{TierID: 1, ItemID: 12},
					{TierID: 2, ItemID: 25},
					{TierID: 3, ItemID: 42},
				}, nil
			},
		}
		r := NewTreasureHouseRoutine(cfg)
		if r == nil {
			t.Fatalf("NewTreasureHouseRoutine 返回 nil")
		}
		if cfg.Selector != nil {
			res, err := cfg.Selector(context.Background(), pool, nil)
			if err != nil || len(res) != 3 || !called {
				t.Fatalf("自定义选择器执行异常: res=%v, err=%v", res, err)
			}
		}
	})
}
