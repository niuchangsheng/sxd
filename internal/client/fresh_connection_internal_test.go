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
