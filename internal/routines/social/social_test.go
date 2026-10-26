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
