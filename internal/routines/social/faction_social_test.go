// Package social 提供仙盟、圣盟、住宅与好友互动的帮派社交自动化玩法。
//
// @author Ateng
// @since 2026-11-02
package social

import (
	"context"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func TestFactionSocialRoutines_MockExecute(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_test_faction_social",
		RoleName:   "测试帮派角色",
		ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()

	deadline := time.Now().Add(2 * time.Second)
	for session.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if session.State() != client.StateActive {
		t.Fatalf("测试会话未进入激活状态: %s", session.State())
	}

	t.Run("帮派祭神循环上香", func(t *testing.T) {
		routine := NewFactionBlessingRoutine()
		if routine.Name() != "faction_blessing" {
			t.Fatalf("任务名称不匹配: %s", routine.Name())
		}
		if dr, ok := routine.(scheduler.DetailedRoutine); ok {
			if dr.Domain() != "social" {
				t.Fatalf("领域不匹配: %s", dr.Domain())
			}
		}

		if err := routine.Execute(context.Background(), session, nil); err != nil {
			t.Fatalf("帮派祭神执行失败: %v", err)
		}
		if got := session.LastActionID(); got != protocol.ActionIDFactionIncense {
			t.Errorf("期望最后动作为上香 0x%08X, 实际 0x%08X", protocol.ActionIDFactionIncense, got)
		}
	})

	t.Run("帮派转盘循环抽奖", func(t *testing.T) {
		routine := NewFactionLuckyWheelRoutine()
		if routine.Name() != "faction_lucky_wheel" {
			t.Fatalf("任务名称不匹配: %s", routine.Name())
		}
		if dr, ok := routine.(scheduler.DetailedRoutine); ok {
			if dr.Domain() != "social" {
				t.Fatalf("领域不匹配: %s", dr.Domain())
			}
		}

		if err := routine.Execute(context.Background(), session, nil); err != nil {
			t.Fatalf("帮派转盘执行失败: %v", err)
		}
		if got := session.LastActionID(); got != protocol.ActionIDLuckyWheelStart {
			t.Errorf("期望最后动作为转盘抽奖 0x%08X, 实际 0x%08X", protocol.ActionIDLuckyWheelStart, got)
		}
	})

	t.Run("空会话防御", func(t *testing.T) {
		if err := NewFactionBlessingRoutine().Execute(context.Background(), nil, nil); err != client.ErrNotConnected {
			t.Errorf("帮派祭神空会话应返回 ErrNotConnected, 实际: %v", err)
		}
		if err := NewFactionLuckyWheelRoutine().Execute(context.Background(), nil, nil); err != client.ErrNotConnected {
			t.Errorf("帮派转盘空会话应返回 ErrNotConnected, 实际: %v", err)
		}
	})
}
