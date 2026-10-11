// Package minigame 提供吉星高照、仙履奇缘答题、刮刮卡、钓鱼等益智趣味活动。
//
// @author Ateng
// @since 2026-10-11
package minigame

import (
	"context"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
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
}
