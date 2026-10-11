// Package daily 提供每日签到、日常任务与邮件福利自动化逻辑。
//
// @author Ateng
// @since 2026-10-11
package daily

import (
	"context"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/scheduler"
)

func TestDailyRoutines_Execute(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_test_daily",
		RoleName:   "测试日常角色",
		ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()

	// 等待会话完成认证并进入激活态
	time.Sleep(100 * time.Millisecond)

	// 设置背包余量
	session.UpdatePlayerState(func(ps *client.PlayerState) {
		ps.BagCapacity = 15
	})

	t.Run("每日签到全链路", func(t *testing.T) {
		r1 := NewSignInRoutine()
		if dr, ok := r1.(scheduler.DetailedRoutine); ok {
			if dr.Domain() != "daily" {
				t.Fatalf("领域不匹配: %s", dr.Domain())
			}
		}
		if err := r1.Execute(context.Background(), session, nil); err != nil {
			t.Fatalf("签到任务执行失败: %v", err)
		}
		state := session.GetPlayerState()
		if state.Coins <= 0 || state.Ingots <= 0 {
			t.Errorf("签到后未获得铜钱或元宝奖励: coins=%d, ingots=%d", state.Coins, state.Ingots)
		}
	})

	t.Run("日常小助手", func(t *testing.T) {
		r2 := NewDailyQuestRoutine()
		if err := r2.Execute(context.Background(), session, nil); err != nil {
			t.Fatalf("日常任务执行失败: %v", err)
		}
	})

	t.Run("邮件附件一键收取", func(t *testing.T) {
		r3 := NewMailCollectRoutine()
		if err := r3.Execute(context.Background(), session, nil); err != nil {
			t.Fatalf("邮件收取执行失败: %v", err)
		}
	})

	t.Run("背包已满防御拦截", func(t *testing.T) {
		session.UpdatePlayerState(func(ps *client.PlayerState) {
			ps.BagCapacity = 0
		})
		r3 := NewMailCollectRoutine()
		err := r3.Execute(context.Background(), session, nil)
		if err != scheduler.ErrBagFull {
			t.Errorf("期望返回 ErrBagFull, 实际: %v", err)
		}
	})

	t.Run("空会话防御", func(t *testing.T) {
		r1 := NewSignInRoutine()
		if err := r1.Execute(context.Background(), nil, nil); err != client.ErrNotConnected {
			t.Errorf("期望 ErrNotConnected, 实际: %v", err)
		}
	})
}
