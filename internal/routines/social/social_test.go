// Package social 提供仙盟、圣盟、住宅与好友互动的帮派社交自动化玩法。
//
// @author Ateng
// @since 2026-10-09
package social

import (
	"context"
	"net"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/scheduler"
)

func TestSocialRoutines_Execute(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	go func() {
		buf := make([]byte, 1024)
		for {
			_, err := c2.Read(buf)
			if err != nil {
				return
			}
		}
	}()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID:   "role_test_social",
		RoleName: "测试社交角色",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return c1, nil
		},
	})
	_ = session.Start(context.Background())
	defer session.Close()

	time.Sleep(50 * time.Millisecond)

	r1 := NewGuildActivitiesRoutine()
	if dr, ok := r1.(scheduler.DetailedRoutine); ok {
		if dr.Domain() != "social" {
			t.Fatalf("领域不匹配: %s", dr.Domain())
		}
	}
	if err := r1.Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("仙盟任务执行失败: %v", err)
	}

	r2 := NewHomesteadRoutine()
	if err := r2.Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("住宅任务执行失败: %v", err)
	}

	t.Run("结缘宝箱每批最多10个配置", func(t *testing.T) {
		cfg := JieYuanBoxConfig{MaxBatchSize: 10}
		r3 := NewJieYuanBoxRoutine(cfg)
		if r3.Name() != "jieyuan_box" {
			t.Fatalf("结缘宝箱名称不匹配: %s", r3.Name())
		}
		if err := r3.Execute(context.Background(), session, nil); err != nil {
			t.Fatalf("结缘宝箱任务执行异常: %v", err)
		}
	})
}

// TestFameBlessingRoutine_Execute 覆盖免费白檀香膜拜成功路径。
func TestFameBlessingRoutine_Execute(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_test_fame_blessing",
		RoleName:   "测试膜拜角色",
		ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()
	time.Sleep(100 * time.Millisecond)

	r := NewFameBlessingRoutine()
	if err := r.Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("膜拜任务执行失败: %v", err)
	}
}

// TestFriendshipRoutine_Execute 覆盖好友来信自动阅读并领取奖励路径。
func TestFriendshipRoutine_Execute(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_test_friendship",
		RoleName:   "测试好友来信角色",
		ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()
	time.Sleep(100 * time.Millisecond)

	r := NewFriendshipRoutine()
	if err := r.Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("好友来信任务执行失败: %v", err)
	}
}

// TestGuildActivitiesRoutine_Execute 覆盖仙盟开放状态与入盟检测路径。
func TestGuildActivitiesRoutine_Execute(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID: "role_test_guild", RoleName: "测试仙盟角色", ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()
	time.Sleep(100 * time.Millisecond)
	if err := NewGuildActivitiesRoutine().Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("仙盟任务执行失败: %v", err)
	}
}

// TestSacredAllianceRoutine_Execute 覆盖圣盟赛季探测路径。
func TestSacredAllianceRoutine_Execute(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID: "role_test_alliance", RoleName: "测试圣盟角色", ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()
	time.Sleep(100 * time.Millisecond)
	if err := NewSacredAllianceRoutine().Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("圣盟任务执行失败: %v", err)
	}
}

// TestHomesteadRoutine_Execute 覆盖住宅状态探测路径。
func TestHomesteadRoutine_Execute(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID: "role_test_homestead", RoleName: "测试住宅角色", ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()
	time.Sleep(100 * time.Millisecond)
	if err := NewHomesteadRoutine().Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("住宅任务执行失败: %v", err)
	}
}
