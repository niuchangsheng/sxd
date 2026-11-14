package social

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
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
			case protocol.ActionIDMarryHomeGetCake:
				// 家园蛋糕探测婚姻状态，返回成功（已结婚/有家园）。
				_ = protocol.WritePacket(serverConn, protocol.BuildMarryHomeCakeResultPacket(
					protocol.MarryHomeCakeResult{Result: protocol.MarryHomeResultSuccess}))
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

// TestHomesteadRoutine_SelfInfo3StillAttempts 验证 210/0 返回 3 时不再直接判定
// “未结婚”（qq s0 已婚账号同样返回 3），仍按 210/2 -> 210/18 顺序尝试。
func TestHomesteadRoutine_SelfInfo3StillAttempts(t *testing.T) {
	want := []protocol.MarryHomeFurnitureEffect{{ItemID: 1001, Effect: 5}}
	s, actions := newHomesteadTestSession(t, protocol.MarryHomeResultNonMarry, want)

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
		t.Fatal("210/0 返回 3 时仍应尝试 210/18（该码不代表未结婚）")
	}
}

func TestHomesteadRoutine_MarriedClaimsFurnitureEffect(t *testing.T) {
	want := []protocol.MarryHomeFurnitureEffect{{ItemID: 1001, Effect: 5}}
	s, actions := newHomesteadTestSession(t, protocol.MarryHomeResultSuccess, want)

	if err := NewHomesteadRoutine().Execute(context.Background(), s, nil); err != nil {
		t.Fatalf("execute: %v", err)
	}

	cakeIdx, effectIdx := -1, -1
	for i, act := range *actions {
		switch act {
		case protocol.ActionIDMarryHomeGetCake:
			cakeIdx = i
		case protocol.ActionIDMarryHomeBatchFurnitureEffect:
			effectIdx = i
		}
	}
	if cakeIdx < 0 || effectIdx < 0 || cakeIdx > effectIdx {
		t.Fatalf("已婚角色应先发 210/19 探测再发 210/18, actions=%v", *actions)
	}
}
