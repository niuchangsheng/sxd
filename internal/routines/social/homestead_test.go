package social

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// newHomesteadTestSession 构造只响应住宅状态与家具效果领取的测试会话。
func newHomesteadTestSession(t *testing.T, selfResult uint8, effects []protocol.MarryHomeFurnitureEffect) (*client.RoleSession, *[]uint32) {
	t.Helper()

	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})

	var mu sync.Mutex
	var actions []uint32
	go func() {
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			mu.Lock()
			actions = append(actions, pkt.ActionID)
			mu.Unlock()
			switch pkt.ActionID {
			case protocol.ActionIDMarryHomeSelfInfo:
				_ = protocol.WritePacket(serverConn, protocol.BuildMarryHomeSelfInfoResultPacket(selfResult))
			case protocol.ActionIDMarryHomeBatchFurnitureEffect:
				_ = protocol.WritePacket(serverConn,
					protocol.BuildMarryHomeBatchFurnitureEffectResultPacket(effects))
			}
		}
	}()

	s := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role-homestead-test",
		RoleName:   "住宅测试",
		ServerAddr: "mock-pipe",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(*client.RoleSession) error { return nil },
	})
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	t.Cleanup(s.Close)

	deadline := time.Now().Add(2 * time.Second)
	for s.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if s.State() != client.StateActive {
		t.Fatalf("session not active, state=%v", s.State())
	}
	return s, &actions
}

func TestHomesteadRoutine_NonMarrySkipsFurnitureEffect(t *testing.T) {
	s, actions := newHomesteadTestSession(t, protocol.MarryHomeResultNonMarry, nil)

	if err := NewHomesteadRoutine().Execute(context.Background(), s, nil); err != nil && !errors.Is(err, scheduler.ErrAttemptsExhausted) {
		t.Fatalf("execute: %v", err)
	}

	for _, act := range *actions {
		if act == protocol.ActionIDMarryHomeBatchFurnitureEffect {
			t.Fatal("未结婚角色不应发送 210/18 家具效果请求")
		}
	}
}

func TestHomesteadRoutine_MarriedClaimsFurnitureEffect(t *testing.T) {
	want := []protocol.MarryHomeFurnitureEffect{{ItemID: 1001, Effect: 5}}
	s, actions := newHomesteadTestSession(t, protocol.MarryHomeResultSuccess, want)

	if err := NewHomesteadRoutine().Execute(context.Background(), s, nil); err != nil {
		t.Fatalf("execute: %v", err)
	}

	called := false
	for _, act := range *actions {
		if act == protocol.ActionIDMarryHomeBatchFurnitureEffect {
			called = true
		}
	}
	if !called {
		t.Fatal("已婚角色应发送 210/18 家具效果请求")
	}
}
