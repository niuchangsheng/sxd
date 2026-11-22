package system

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
)

// TestSessionRefreshRoutine_Reconnects 验证任务会主动重建连接并在会话重新
// 激活后返回 nil；重建期间调度器会暂停其他任务。
func TestSessionRefreshRoutine_Reconnects(t *testing.T) {
	var mu sync.Mutex
	dials := 0
	dialer := func(ctx context.Context, network, addr string) (net.Conn, error) {
		mu.Lock()
		dials++
		mu.Unlock()
		c1, c2 := net.Pipe()
		go func() {
			buf := make([]byte, 1024)
			for {
				if _, err := c2.Read(buf); err != nil {
					_ = c2.Close()
					return
				}
			}
		}()
		return c1, nil
	}

	sess := client.NewRoleSession(client.SessionConfig{
		RoleID:        "role-session-refresh",
		RoleName:      "会话刷新测试",
		ServerAddr:    "mock-pipe",
		Dialer:        dialer,
		Authenticator: func(*client.RoleSession) error { return nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sess.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sess.Close()

	deadline := time.Now().Add(2 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		t.Fatalf("session not active: %s", sess.State())
	}

	routine := NewSessionRefreshRoutine()
	// 首次执行只建立周期基线，不重连。
	if err := routine.Execute(ctx, sess, nil); err != nil {
		t.Fatalf("first execute: %v", err)
	}
	if err := routine.Execute(ctx, sess, nil); err != nil {
		t.Fatalf("session refresh: %v", err)
	}
	if sess.State() != client.StateActive {
		t.Fatalf("刷新后会话未重新激活: %s", sess.State())
	}
	mu.Lock()
	got := dials
	mu.Unlock()
	if got < 2 {
		t.Fatalf("应至少重连一次, dials = %d", got)
	}
}

// TestSessionRefreshRoutine_InactiveReturnsNotConnected 验证会话未激活时
// BaseRoutine 直接返回 ErrNotConnected，避免在重连过程中重复触发。
func TestSessionRefreshRoutine_SkipsWhenInactive(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	sess := client.NewRoleSession(client.SessionConfig{
		RoleID:        "role-session-refresh-idle",
		ServerAddr:    "mock-pipe",
		Dialer:        func(context.Context, string, string) (net.Conn, error) { return c1, nil },
		Authenticator: func(*client.RoleSession) error { return nil },
	})
	// 不 Start，会话处于 closed/连接前状态。
	if err := NewSessionRefreshRoutine().Execute(context.Background(), sess, nil); !errors.Is(err, client.ErrNotConnected) {
		t.Fatalf("未激活会话应返回 ErrNotConnected, got %v", err)
	}
}
