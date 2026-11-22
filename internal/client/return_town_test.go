package client_test

import (
	"context"
	"net"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
)

// TestRoleSession_ReturnToTown 验证战斗结束后发送 enter_town 并在 result=0 时返回 true。
func TestRoleSession_ReturnToTown(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	var enterCalls int
	go func() {
		for {
			pkt, err := protocol.ReadPacket(c2)
			if err != nil {
				return
			}
			if pkt.ActionID == protocol.ActionIDTownEnter {
				enterCalls++
				_ = protocol.WritePacket(c2, protocol.NewPacket(pkt.ActionID, []byte{0}))
			}
		}
	}()

	sess := client.NewRoleSession(client.SessionConfig{
		RoleID:        "role-return-town",
		ServerAddr:    "mock-pipe",
		Dialer:        func(context.Context, string, string) (net.Conn, error) { return c1, nil },
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
	if sess.State() != client.StateActive {
		t.Fatalf("session not active: %s", sess.State())
	}

	if !sess.ReturnToTown(context.Background(), 2*time.Second) {
		t.Fatal("ReturnToTown 应返回 true")
	}
	if enterCalls != 1 {
		t.Fatalf("应发送一次 enter_town, got %d", enterCalls)
	}
}
