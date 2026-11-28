package client

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// TestEnsureFreshConnection 验证会话超过健康窗口时会主动重建连接，
// 未超过窗口时保持原连接不动。
func TestEnsureFreshConnection(t *testing.T) {
	pipes := make([][2]net.Conn, 4)
	for i := range pipes {
		pipes[i][0], pipes[i][1] = net.Pipe()
		defer pipes[i][0].Close()
		defer pipes[i][1].Close()
	}
	dial := 0
	s := NewRoleSession(SessionConfig{
		RoleID:     "role-fresh",
		ServerAddr: "mock-pipe",
		Dialer: func(context.Context, string, string) (net.Conn, error) {
			c := pipes[dial]
			dial++
			return c[0], nil
		},
		Authenticator: func(*RoleSession) error { return nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close()
	deadline := time.Now().Add(2 * time.Second)
	for s.State() != StateActive && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if s.State() != StateActive {
		t.Fatalf("session not active: %s", s.State())
	}

	// 新会话：不重建。
	gen := s.ConnectionGeneration()
	if err := s.EnsureFreshConnection(ctx, HealthySessionAge); err != nil {
		t.Fatalf("fresh session: %v", err)
	}
	if s.ConnectionGeneration() != gen {
		t.Fatal("新会话不应触发重建")
	}

	// 伪造旧会话：应重建到新一代连接。
	s.connSince.Store(time.Now().Add(-10 * time.Minute).UnixNano())
	if err := s.EnsureFreshConnection(ctx, HealthySessionAge); err != nil {
		t.Fatalf("old session should reconnect: %v", err)
	}
	if s.ConnectionGeneration() <= gen {
		t.Fatalf("应建立新连接, generation %d -> %d", gen, s.ConnectionGeneration())
	}
	if s.State() != StateActive {
		t.Fatalf("重建后应 Active, got %s", s.State())
	}
}

// TestEnsureFreshConnection_RefreshesCredentialsBeforeReconnect 验证主动重建前
// 会先调用换票回调，避免旧票据在服务端释放旧会话前连续握手超时。
func TestEnsureFreshConnection_RefreshesCredentialsBeforeReconnect(t *testing.T) {
	pipes := make([][2]net.Conn, 4)
	for i := range pipes {
		pipes[i][0], pipes[i][1] = net.Pipe()
		defer pipes[i][0].Close()
		defer pipes[i][1].Close()
	}
	dial := 0
	refreshed := 0
	s := NewRoleSession(SessionConfig{
		RoleID:     "role-fresh-refresh",
		ServerAddr: "mock-pipe",
		Dialer: func(context.Context, string, string) (net.Conn, error) {
			c := pipes[dial]
			dial++
			return c[0], nil
		},
		Authenticator: func(*RoleSession) error { return nil },
		RefreshCredentials: func(context.Context, *SessionConfig) error {
			refreshed++
			return nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Close()
	deadline := time.Now().Add(2 * time.Second)
	for s.State() != StateActive && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if s.State() != StateActive {
		t.Fatalf("session not active: %s", s.State())
	}

	gen := s.ConnectionGeneration()
	s.connSince.Store(time.Now().Add(-10 * time.Minute).UnixNano())
	s.credRefreshMu.Lock()
	s.lastCredRefresh = time.Now().Add(-10 * time.Minute)
	s.credRefreshMu.Unlock()
	if err := s.EnsureFreshConnection(ctx, HealthySessionAge); err != nil {
		t.Fatalf("old session should reconnect: %v", err)
	}
	if refreshed != 1 {
		t.Fatalf("重建前应换票 1 次, got %d", refreshed)
	}
	if s.ConnectionGeneration() <= gen {
		t.Fatalf("应建立新连接, generation %d -> %d", gen, s.ConnectionGeneration())
	}

	// 新会话未超窗：不应再次换票。
	if err := s.EnsureFreshConnection(ctx, HealthySessionAge); err != nil {
		t.Fatalf("fresh session: %v", err)
	}
	if refreshed != 1 {
		t.Fatalf("未超窗时不应换票, got %d", refreshed)
	}
}

// newPipeSessionForEnsure 构造一个基于 net.Pipe 的已激活会话（自定义登录握手）。
func newPipeSessionForEnsure(t *testing.T, roleID string) *RoleSession {
	t.Helper()
	pipes := make([][2]net.Conn, 4)
	for i := range pipes {
		pipes[i][0], pipes[i][1] = net.Pipe()
		t.Cleanup(func() {
			_ = pipes[i][0].Close()
			_ = pipes[i][1].Close()
		})
	}
	dial := 0
	s := NewRoleSession(SessionConfig{
		RoleID:     roleID,
		ServerAddr: "mock-pipe",
		Dialer: func(context.Context, string, string) (net.Conn, error) {
			if dial >= len(pipes) {
				return nil, ErrNotConnected
			}
			c := pipes[dial]
			dial++
			return c[0], nil
		},
		Authenticator: func(*RoleSession) error { return nil },
	})
	t.Cleanup(s.Close)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for s.State() != StateActive && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if s.State() != StateActive {
		t.Fatalf("session not active: %s", s.State())
	}
	return s
}

// TestEnsureFreshConnection_WaitsForInFlightReconnect 验证会话正在重连时，
// EnsureFreshConnection 等待状态机恢复而不是立即返回 ErrNotConnected。
func TestEnsureFreshConnection_WaitsForInFlightReconnect(t *testing.T) {
	s := newPipeSessionForEnsure(t, "role-wait-reconnect")
	gen := s.ConnectionGeneration()

	// 模拟状态机正在重连：100ms 后恢复 Active。
	s.setState(StateReconnecting)
	go func() {
		time.Sleep(100 * time.Millisecond)
		s.setState(StateActive)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.EnsureFreshConnection(ctx, HealthySessionAge); err != nil {
		t.Fatalf("应等待重连完成而不是报未连接: %v", err)
	}
	if s.ConnectionGeneration() != gen {
		t.Fatalf("恢复后的新连接足够新，不应再次重建 (gen %d -> %d)", gen, s.ConnectionGeneration())
	}
}

// TestEnsureFreshConnection_NotRecoveringFailsFast 验证未启动/已关闭的会话
// 仍立即返回错误，不会被「等待重连」拖住。
func TestEnsureFreshConnection_NotRecoveringFailsFast(t *testing.T) {
	s := NewRoleSession(SessionConfig{RoleID: "role-disconnected", ServerAddr: "mock"})
	start := time.Now()
	err := s.EnsureFreshConnection(context.Background(), HealthySessionAge)
	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("err = %v, want ErrNotConnected", err)
	}
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Fatalf("未启动会话应立即失败, took %v", elapsed)
	}
}

// TestEnsureFreshConnection_ForceRebuildCoalesced 验证强制重建在刚建立的
// 连接上会被合并，避免每个玩法都拆一次连接。
func TestEnsureFreshConnection_ForceRebuildCoalesced(t *testing.T) {
	s := newPipeSessionForEnsure(t, "role-force-coalesce")

	// 伪造旧连接，触发第一次强制重建。
	s.connSince.Store(time.Now().Add(-time.Minute).UnixNano())
	gen := s.ConnectionGeneration()
	if err := s.EnsureFreshConnection(context.Background(), 0); err != nil {
		t.Fatalf("force rebuild: %v", err)
	}
	if s.ConnectionGeneration() <= gen {
		t.Fatalf("应建立新连接, gen %d -> %d", gen, s.ConnectionGeneration())
	}

	// 紧接着的第二次强制重建应被合并（连接建立不足 3 秒）。
	gen = s.ConnectionGeneration()
	if err := s.EnsureFreshConnection(context.Background(), 0); err != nil {
		t.Fatalf("coalesced force rebuild: %v", err)
	}
	if s.ConnectionGeneration() != gen {
		t.Fatalf("刚重建过的连接不应再次拆迁, gen %d -> %d", gen, s.ConnectionGeneration())
	}
}

// TestAwaitActive 验证状态等待语义：Active 立即返回、Closed 立即报错、ctx 到期返回。
func TestAwaitActive(t *testing.T) {
	s := NewRoleSession(SessionConfig{RoleID: "role-await", ServerAddr: "mock"})

	// 1. Active 立即返回。
	s.setState(StateActive)
	if err := s.AwaitActive(context.Background()); err != nil {
		t.Fatalf("active session: %v", err)
	}

	// 2. Closed 立即返回 ErrSessionClosed。
	s.setState(StateClosed)
	if err := s.AwaitActive(context.Background()); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("closed session: %v, want ErrSessionClosed", err)
	}

	// 3. Reconnecting 状态下 ctx 到期返回。
	s2 := NewRoleSession(SessionConfig{RoleID: "role-await-timeout", ServerAddr: "mock"})
	s2.setState(StateReconnecting)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := s2.AwaitActive(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v, want DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("AwaitActive 未在 ctx 到期时返回, took %v", elapsed)
	}

	// 4. 等待期间状态恢复为 Active 时返回 nil。
	done := make(chan error, 1)
	go func() { done <- s2.AwaitActive(context.Background()) }()
	time.Sleep(20 * time.Millisecond)
	s2.setState(StateActive)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("recovered session: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("AwaitActive 未在状态恢复后返回")
	}
}

// TestAwaitActive_WakesOnClose 验证 Close 会立即唤醒 AwaitActive 等待者，
// 而不是让其一直等到调用方 ctx 到期。
func TestAwaitActive_WakesOnClose(t *testing.T) {
	s := NewRoleSession(SessionConfig{RoleID: "role-await-close", ServerAddr: "mock"})
	s.setState(StateReconnecting)
	done := make(chan error, 1)
	go func() { done <- s.AwaitActive(context.Background()) }()
	time.Sleep(20 * time.Millisecond)
	s.Close()
	select {
	case err := <-done:
		if !errors.Is(err, ErrSessionClosed) {
			t.Fatalf("err = %v, want ErrSessionClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close 未唤醒 AwaitActive")
	}
}
