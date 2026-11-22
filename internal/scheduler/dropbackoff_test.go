package scheduler_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/scheduler"
)

// newActiveSchedulerSession 构造一个已激活的 mock 会话，用于验证静默丢包退避。
func newActiveSchedulerSession(t *testing.T, roleID string) *client.RoleSession {
	t.Helper()
	c1, c2 := net.Pipe()
	t.Cleanup(func() {
		_ = c1.Close()
		_ = c2.Close()
	})
	go func() {
		buf := make([]byte, 1024)
		for {
			if _, err := c2.Read(buf); err != nil {
				return
			}
		}
	}()
	sess := client.NewRoleSession(client.SessionConfig{
		RoleID:        roleID,
		ServerAddr:    "mock-pipe",
		Dialer:        func(context.Context, string, string) (net.Conn, error) { return c1, nil },
		Authenticator: func(*client.RoleSession) error { return nil },
	})
	if err := sess.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	t.Cleanup(sess.Close)
	deadline := time.Now().Add(2 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		t.Fatalf("session not active: %s", sess.State())
	}
	return sess
}

// TestHandleSilentDrop_BacksOffToDailyReset 验证连续静默丢包：未达阈值按短冷却
// 重试，达到阈值后挂起到次日 00:05。
func TestHandleSilentDrop_BacksOffToDailyReset(t *testing.T) {
	const routine = "silent_drop_test_routine"
	sess := newActiveSchedulerSession(t, "role-silent-drop")
	scheduler.ResetSilentDrops(sess, routine)
	defer scheduler.ResetSilentDrops(sess, routine)

	for i := 1; i <= 2; i++ {
		err := scheduler.HandleSilentDrop(sess, routine, 3, 10*time.Minute, "测试无回包")
		var cd *scheduler.CooldownError
		if !errors.As(err, &cd) {
			t.Fatalf("第 %d 次应返回 CooldownError, got %v", i, err)
		}
		if got := scheduler.SilentDropCount(sess, routine); got != i {
			t.Fatalf("连续次数 = %d, want %d", got, i)
		}
		// 未达阈值：应落在短冷却范围内（<= 10 分钟 + 少量误差）。
		if d := time.Until(cd.Until); d > 11*time.Minute || d <= 0 {
			t.Fatalf("第 %d 次冷却 = %v, want ~10m", i, d)
		}
	}

	err := scheduler.HandleSilentDrop(sess, routine, 3, 10*time.Minute, "测试无回包")
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("达到阈值应返回 CooldownError, got %v", err)
	}
	want := scheduler.NextDailyReset(time.Now())
	if cap := time.Now().Add(6 * time.Hour); want.After(cap) {
		want = cap
	}
	if diff := cd.Until.Sub(want); diff > time.Second || diff < -time.Second {
		t.Fatalf("达到阈值应挂起到 %v, got %v", want, cd.Until)
	}
	if d := time.Until(cd.Until); d > 6*time.Hour+time.Second {
		t.Fatalf("最长挂起不应超过 6 小时, got %v", d)
	}

	scheduler.ResetSilentDrops(sess, routine)
	if got := scheduler.SilentDropCount(sess, routine); got != 0 {
		t.Fatalf("重置后连续次数 = %d, want 0", got)
	}
}

// TestNextDailyReset 验证 00:05 重置时刻的边界。
func TestNextDailyReset(t *testing.T) {
	loc := time.Local
	before := time.Date(2026, 11, 15, 0, 4, 59, 0, loc)
	if got := scheduler.NextDailyReset(before); !got.Equal(time.Date(2026, 11, 15, 0, 5, 0, 0, loc)) {
		t.Fatalf("00:04:59 的下一次重置 = %v", got)
	}
	after := time.Date(2026, 11, 15, 0, 5, 0, 0, loc)
	if got := scheduler.NextDailyReset(after); !got.Equal(time.Date(2026, 11, 16, 0, 5, 0, 0, loc)) {
		t.Fatalf("00:05:00 的下一次重置 = %v", got)
	}
}

// TestHandleSilentDrop_InactiveSessionReturnsNotConnected 验证会话未激活时
// 不累计丢包、不进入长冷却，交由调度器按 ErrNotConnected 重连重试。
func TestHandleSilentDrop_InactiveSessionReturnsNotConnected(t *testing.T) {
	sess := client.NewRoleSession(client.SessionConfig{RoleID: "role-drop-inactive"})
	err := scheduler.HandleSilentDrop(sess, "routine_x", 3, time.Minute, "test")
	if !errors.Is(err, client.ErrNotConnected) {
		t.Fatalf("未激活会话应返回 ErrNotConnected, got %v", err)
	}
	if n := scheduler.SilentDropCount(sess, "routine_x"); n != 0 {
		t.Fatalf("未激活会话不应累计丢包, got %d", n)
	}
}

// TestHandleSilentDrop_NilSessionReturnsNotConnected 验证 nil 会话同样安全。
func TestHandleSilentDrop_NilSessionReturnsNotConnected(t *testing.T) {
	if err := scheduler.HandleSilentDrop(nil, "routine_y", 3, time.Minute, "test"); !errors.Is(err, client.ErrNotConnected) {
		t.Fatalf("nil 会话应返回 ErrNotConnected, got %v", err)
	}
}
