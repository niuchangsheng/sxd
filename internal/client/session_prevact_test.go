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
