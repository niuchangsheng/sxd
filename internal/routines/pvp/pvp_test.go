// Package pvp 提供本服竞技场、仙界竞技场、神魔大战与劫镖等对抗性玩法。
//
// @author Ateng
// @since 2026-10-11
package pvp

import (
	"context"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func TestArenaRoutine_Execute(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_test_pvp",
		RoleName:   "测试竞技场角色",
		ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()

	time.Sleep(100 * time.Millisecond)

	t.Run("竞技场挑战全链路", func(t *testing.T) {
		routine := NewArenaRoutine()
		if routine.Name() != "arena" {
			t.Fatalf("任务名称不匹配: %s", routine.Name())
		}
		if dr, ok := routine.(scheduler.DetailedRoutine); ok {
			if dr.Domain() != "pvp" {
				t.Fatalf("领域不匹配: %s", dr.Domain())
			}
		}

		err := routine.Execute(context.Background(), session, nil)
		if err != nil {
			t.Fatalf("竞技场例程执行失败: %v", err)
		}
	})

	t.Run("软柿子筛选算法单测", func(t *testing.T) {
		opponents := []protocol.ArenaOpponent{
			{Rank: 10, RoleID: 101, Name: "强力剑宗", FightValue: 120000},
			{Rank: 8, RoleID: 102, Name: "挂机小号", FightValue: 32000},
			{Rank: 7, RoleID: 103, Name: "平民飞羽", FightValue: 55000},
			{Rank: 6, RoleID: 104, Name: "老玩家", FightValue: 32000}, // 战力同为 32000，但 Rank 6 更靠前
		}

		target := SelectSoftPersimmonOpponent(opponents)
		if target.Rank != 6 || target.FightValue != 32000 {
			t.Errorf("软柿子算法未能精准选出战力最低且排名最靠前者: %+v", target)
		}
	})

	t.Run("空会话防御", func(t *testing.T) {
		routine := NewArenaRoutine()
		if err := routine.Execute(context.Background(), nil, nil); err != client.ErrNotConnected {
			t.Errorf("期望 ErrNotConnected, 实际: %v", err)
		}
	})
}
