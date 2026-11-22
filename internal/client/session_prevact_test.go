package client_test

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
)

// TestCallWithPrevActResync 验证首个请求被静默丢弃后，用 LastAckActionID
// 作为显式锚点重试一次，且重试载荷尾部为该锚点。
func TestCallWithPrevActResync(t *testing.T) {
	const (
		primeAct  uint32 = 0x0000002A
		targetAct uint32 = 0x03530005
	)
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	var targetCalls atomic.Int32
	var retryPayload []byte
	go func() {
		for {
			pkt, err := protocol.ReadPacket(c2)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case primeAct:
				_ = protocol.WritePacket(c2, protocol.NewPacket(primeAct, []byte{0}))
			case targetAct:
				if targetCalls.Add(1) == 1 {
					continue // 模拟服务端静默丢弃
				}
				retryPayload = append([]byte(nil), pkt.Payload...)
				_ = protocol.WritePacket(c2, protocol.NewPacket(targetAct, []byte{0}))
			}
		}
	}()

	sess := client.NewRoleSession(client.SessionConfig{
		RoleID: "role-prevact", RoleName: "PrevAct 测试", ServerAddr: "prevact-pipe",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return c1, nil
		},
		Authenticator: func(*client.RoleSession) error { return nil },
	})
	if err := sess.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sess.Close()
	deadline := time.Now().Add(2 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := sess.Call(ctx, protocol.NewPacket(primeAct, nil), primeAct); err != nil {
		t.Fatalf("prime call: %v", err)
	}

	req := &protocol.Packet{ActionID: targetAct, Payload: []byte{0, 0, 0, 1, 0, 0, 0, 0}, HasPrevAct: true}
	resp, err := sess.CallWithPrevActResync(ctx, 200*time.Millisecond, req, targetAct)
	if err != nil || resp == nil {
		t.Fatalf("resync call failed: resp=%v err=%v", resp, err)
	}
	if len(retryPayload) != 8 {
		t.Fatalf("retry payload len = %d, want 8", len(retryPayload))
	}
	wantAnchor := primeAct
	got := uint32(retryPayload[4])<<24 | uint32(retryPayload[5])<<16 |
		uint32(retryPayload[6])<<8 | uint32(retryPayload[7])
	if got != wantAnchor {
		t.Fatalf("retry anchor = 0x%08X, want 0x%08X", got, wantAnchor)
	}
}

// TestSendUsesLastReceivedPushAsPrevAct 验证 PrevAct 链取「最后收到的包」而不是
// 「最后发出的包」：服务端在两次请求之间推送通知后，下一次请求的尾部必须携带
// 该推送的 ActionID。抓包 /tmp/wuliangjiehai.pcapng 中 853/2 的 PrevAct 就是
// 前一轮收到的 52/0 回包/推送；带参数请求若链不一致会被静默丢弃。
func TestSendUsesLastBusinessSendAsPrevAct(t *testing.T) {
	const (
		firstAct uint32 = 0x03530004
		pushAct  uint32 = 0x000D0019
		nextAct  uint32 = 0x03530005
	)
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	var nextPayload []byte
	go func() {
		for {
			pkt, err := protocol.ReadPacket(c2)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case firstAct:
				_ = protocol.WritePacket(c2, protocol.NewPacket(firstAct, []byte{0}))
			case nextAct:
				nextPayload = append([]byte(nil), pkt.Payload...)
				_ = protocol.WritePacket(c2, protocol.NewPacket(nextAct, []byte{0}))
			}
		}
	}()

	sess := client.NewRoleSession(client.SessionConfig{
		RoleID: "role-prevact-rx", RoleName: "PrevAct 收包链测试", ServerAddr: "prevact-rx-pipe",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return c1, nil
		},
		Authenticator: func(*client.RoleSession) error { return nil },
	})
	if err := sess.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sess.Close()
	deadline := time.Now().Add(2 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := sess.Call(ctx, protocol.NewPacket(firstAct, []byte{0, 0, 0, 0}), firstAct); err != nil {
		t.Fatalf("first call: %v", err)
	}

	// 服务端主动推送：应更新 LastRxActionID，但不得影响业务 PrevAct 锚点。
	_ = protocol.WritePacket(c2, protocol.NewPacket(pushAct, []byte{0}))
	waitDeadline := time.Now().Add(time.Second)
	for sess.LastRxActionID() != pushAct && time.Now().Before(waitDeadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := sess.LastRxActionID(); got != pushAct {
		t.Fatalf("最后收包 = 0x%08X, want 0x%08X", got, pushAct)
	}

	req := &protocol.Packet{ActionID: nextAct, Payload: []byte{0, 0, 0, 1, 0, 0, 0, 0}, HasPrevAct: true}
	if _, err := sess.Call(ctx, req, nextAct); err != nil {
		t.Fatalf("next call: %v", err)
	}
	if len(nextPayload) != 8 {
		t.Fatalf("next payload len = %d, want 8", len(nextPayload))
	}
	got := uint32(nextPayload[4])<<24 | uint32(nextPayload[5])<<16 |
		uint32(nextPayload[6])<<8 | uint32(nextPayload[7])
	// PrevAct 取最后一条发送的业务动作 firstAct，而不是推送动作。
	if got != firstAct {
		t.Fatalf("PrevAct = 0x%08X, want 最后业务动作 0x%08X (push=0x%08X)", got, firstAct, pushAct)
	}
}

// TestCallWithPrevActResync_FallsBackToPreSendAnchorWhenNoRx 验证重连后
// lastRxActionID 为 0 时，重试锚点取「首次发送前的上一条发送动作」，
// 而不是 LastAckActionID（可能是旧连接上其他模块的回包，如 429/4）。
func TestCallWithPrevActResync_FallsBackToPreSendAnchorWhenNoRx(t *testing.T) {
	const (
		preSentAct uint32 = 0x00020029
		targetAct  uint32 = 0x03550002
	)
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	var targetCalls atomic.Int32
	var retryPayload []byte
	go func() {
		for {
			pkt, err := protocol.ReadPacket(c2)
			if err != nil {
				return
			}
			if pkt.ActionID != targetAct {
				continue
			}
			if targetCalls.Add(1) == 1 {
				continue // 首次静默丢弃
			}
			retryPayload = append([]byte(nil), pkt.Payload...)
			_ = protocol.WritePacket(c2, protocol.NewPacket(targetAct, []byte{0}))
		}
	}()

	sess := client.NewRoleSession(client.SessionConfig{
		RoleID: "role-prevact-fallback", RoleName: "PrevAct 无收包兜底", ServerAddr: "prevact-fallback-pipe",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return c1, nil
		},
		Authenticator: func(*client.RoleSession) error { return nil },
	})
	if err := sess.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sess.Close()
	deadline := time.Now().Add(2 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	// 模拟重连后尚未收到任何包：lastRx=0，上一条发送动作是握手/前序业务包。
	sess.SetLastActionID(preSentAct)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req := &protocol.Packet{ActionID: targetAct, Payload: []byte{0, 0, 0, 1, 0, 0, 0, 0}, HasPrevAct: true}
	resp, err := sess.CallWithPrevActResync(ctx, 200*time.Millisecond, req, targetAct)
	if err != nil || resp == nil {
		t.Fatalf("resync call failed: resp=%v err=%v", resp, err)
	}
	if len(retryPayload) != 8 {
		t.Fatalf("retry payload len = %d, want 8", len(retryPayload))
	}
	got := uint32(retryPayload[4])<<24 | uint32(retryPayload[5])<<16 |
		uint32(retryPayload[6])<<8 | uint32(retryPayload[7])
	if got != preSentAct {
		t.Fatalf("retry anchor = 0x%08X, want 上一条发送动作 0x%08X", got, preSentAct)
	}
}
