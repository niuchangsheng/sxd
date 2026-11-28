package kit_test

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
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

// TestCall_WaitsForReconnectAndRetries 验证调用发生在会话重连窗口时，kit.Call
// 等待状态机恢复后重试一次，而不是把 ErrNotConnected 暴露给业务层。
func TestCall_WaitsForReconnectAndRetries(t *testing.T) {
	const okAct uint32 = 0x01010003
	var dialCount atomic.Int32
	secondDialEntered := make(chan struct{})
	releaseReconnect := make(chan struct{})

	sess := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role-kit-reconnect",
		ServerAddr: "mock-pipe",
		Dialer: func(ctx context.Context, _, _ string) (net.Conn, error) {
			if dialCount.Add(1) == 2 {
				close(secondDialEntered)
				select {
				case <-releaseReconnect:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			c1, c2 := net.Pipe()
			go func() {
				defer c2.Close()
				for {
					pkt, err := protocol.ReadPacket(c2)
					if err != nil {
						return
					}
					if pkt.ActionID == okAct {
						_ = protocol.WritePacket(c2, protocol.NewPacket(okAct, []byte{0x09}))
					}
				}
			}()
			return c1, nil
		},
		Authenticator: func(*client.RoleSession) error { return nil },
	})
	if err := sess.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer sess.Close()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer waitCancel()
	if err := sess.AwaitActive(waitCtx); err != nil {
		t.Fatalf("session not active: %v", err)
	}

	// 主动断开，并把第二次建连门控在 Reconnecting 态，确保 kit.Call 确实命中重连等待路径。
	sess.ReconnectNow()
	select {
	case <-secondDialEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("等待状态机进入第二次建连超时")
	}
	if !sess.State().IsRecovering() {
		t.Fatalf("state = %s, want recovering", sess.State())
	}

	// 放行重连并验证 kit.Call 等待恢复后自动重试成功。
	go func() {
		time.Sleep(30 * time.Millisecond)
		close(releaseReconnect)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	resp, err := kit.Call(ctx, sess, protocol.NewPacket(okAct, nil), okAct, time.Second)
	if err != nil || resp == nil || len(resp.Payload) != 1 || resp.Payload[0] != 0x09 {
		t.Fatalf("重连后重试应成功: resp=%v err=%v", resp, err)
	}
}

// TestCall_InFlightDisconnectDoesNotRetryNonIdempotent 验证请求已写入连接后中途断链时，
// 即使状态机立即重连恢复，kit.Call 也绝不自动重发同一个请求（防止非幂等写重复执行），
// 且 kit.IsAbortErr 仍将其归一化为 client.ErrNotConnected 交由调度器短重试。
func TestCall_InFlightDisconnectDoesNotRetryNonIdempotent(t *testing.T) {
	const writeAct uint32 = 0x01010099
	var received atomic.Int32

	sess := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role-kit-no-double-send",
		ServerAddr: "mock-pipe",
		Dialer: func(context.Context, string, string) (net.Conn, error) {
			c1, c2 := net.Pipe()
			go func() {
				defer c2.Close()
				for {
					pkt, err := protocol.ReadPacket(c2)
					if err != nil {
						return
					}
					if pkt.ActionID == writeAct {
						if received.Add(1) == 1 {
							// 首次收到请求后立刻断链不回包，模拟服务端已执行但回包前断链。
							_ = c2.Close()
							return
						}
						_ = protocol.WritePacket(c2, protocol.NewPacket(writeAct, []byte{0x01}))
					}
				}
			}()
			return c1, nil
		},
		Authenticator: func(*client.RoleSession) error { return nil },
	})
	if err := sess.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer sess.Close()

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer waitCancel()
	if err := sess.AwaitActive(waitCtx); err != nil {
		t.Fatalf("AwaitActive: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := kit.Call(ctx, sess, protocol.NewPacket(writeAct, nil), writeAct, 2*time.Second)
	if !errors.Is(err, client.ErrConnectionLost) {
		t.Fatalf("发包后断链应返回 ErrConnectionLost, got %v", err)
	}
	if got := received.Load(); got != 1 {
		t.Fatalf("发包后断链绝不应重发非幂等请求: received = %d, want 1", got)
	}
	abortErr, isAbort := kit.IsAbortErr(err)
	if !isAbort || !errors.Is(abortErr, client.ErrNotConnected) {
		t.Fatalf("IsAbortErr(ErrConnectionLost) = (%v, %v), want (ErrNotConnected, true)", abortErr, isAbort)
	}
}
