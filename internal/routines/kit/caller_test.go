package kit_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
)

func newPipeSession(t *testing.T, handler func(conn net.Conn, pkt *protocol.Packet)) *client.RoleSession {
	t.Helper()
	c1, c2 := net.Pipe()
	t.Cleanup(func() {
		_ = c1.Close()
		_ = c2.Close()
	})
	go func() {
		for {
			pkt, err := protocol.ReadPacket(c2)
			if err != nil {
				return
			}
			if handler != nil {
				handler(c2, pkt)
			}
		}
	}()
	sess := client.NewRoleSession(client.SessionConfig{
		RoleID:        "role-kit-test",
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

func TestCaller_SuccessAndTimeoutOverride(t *testing.T) {
	const (
		okAct   uint32 = 0x01010001
		dropAct uint32 = 0x01010002
	)
	sess := newPipeSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		if pkt.ActionID == okAct {
			_ = protocol.WritePacket(conn, protocol.NewPacket(okAct, []byte{0x07}))
		}
		// dropAct 故意不回包以触发超时
	})

	ctx := kit.WithTimeoutOverride(context.Background(), 40*time.Millisecond)
	c := kit.NewCaller(ctx, sess, 10*time.Second)

	resp, err := c.Call(protocol.NewPacket(okAct, nil), okAct)
	if err != nil || resp == nil || len(resp.Payload) != 1 || resp.Payload[0] != 0x07 {
		t.Fatalf("Call okAct failed: resp=%v err=%v", resp, err)
	}
	if c.LastErr() != nil || c.AbortErr() != nil {
		t.Fatalf("expected nil errors after success, got last=%v abort=%v", c.LastErr(), c.AbortErr())
	}

	start := time.Now()
	got := c.TryCall(protocol.NewPacket(dropAct, nil), dropAct)
	elapsed := time.Since(start)
	if got != nil {
		t.Fatalf("expected nil response on drop, got %v", got)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("WithTimeoutOverride did not cap 10s timeout: took %v", elapsed)
	}
	if !errors.Is(c.LastErr(), context.DeadlineExceeded) {
		t.Fatalf("LastErr = %v, want DeadlineExceeded", c.LastErr())
	}
	if c.AbortErr() != nil {
		t.Fatalf("timeout should not be treated as AbortErr, got %v", c.AbortErr())
	}
}

func TestCaller_PropagatesDisconnectAndCancel(t *testing.T) {
	// 1. 未连接会话返回 client.ErrNotConnected 且 AbortErr 非空
	inactive := client.NewRoleSession(client.SessionConfig{RoleID: "role-inactive"})
	c := kit.NewCaller(context.Background(), inactive, time.Second)
	if resp := c.TryCall(protocol.NewPacket(0x01010001, nil), 0x01010001); resp != nil {
		t.Fatalf("expected nil resp on inactive session")
	}
	if !errors.Is(c.AbortErr(), client.ErrNotConnected) {
		t.Fatalf("AbortErr = %v, want ErrNotConnected", c.AbortErr())
	}

	// 2. 上下文取消透传 context.Canceled
	sess := newPipeSession(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c2 := kit.NewCaller(ctx, sess, time.Second)
	if resp := c2.TryCall(protocol.NewPacket(0x01010001, nil), 0x01010001); resp != nil {
		t.Fatalf("expected nil resp on canceled ctx")
	}
	if !errors.Is(c2.AbortErr(), context.Canceled) {
		t.Fatalf("AbortErr = %v, want context.Canceled", c2.AbortErr())
	}
}

func TestSleep_RespectsContextCancel(t *testing.T) {
	if !kit.Sleep(context.Background(), 10*time.Millisecond) {
		t.Fatal("Sleep should return true when completed")
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(15 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	if kit.Sleep(ctx, 5*time.Second) {
		t.Fatal("Sleep should return false on context cancel")
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("Sleep did not abort promptly on cancel: took %v", elapsed)
	}
}
