package client

import (
	"context"
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
