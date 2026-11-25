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

// TestRoleSession_ReturnToTown_FallbackOnInvalidTownMapID 验证当 TownMapID (如 153)
// 返回 FAILED(1) 时，ReturnToTown 自动回退至有效主城 (3/2/1) 并缓存成功 ID。
func TestRoleSession_ReturnToTown_FallbackOnInvalidTownMapID(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	var seenIDs []int32
	go func() {
		for {
			pkt, err := protocol.ReadPacket(c2)
			if err != nil {
				return
			}
			if pkt.ActionID == protocol.ActionIDTownEnter {
				r := protocol.NewReader(pkt.Payload)
				tid, _ := r.ReadInt32()
				seenIDs = append(seenIDs, tid)
				code := byte(1) // FAILED
				if tid == 3 {
					code = 0 // SUCCESS
				}
				_ = protocol.WritePacket(c2, protocol.NewPacket(pkt.ActionID, []byte{code}))
			}
		}
	}()

	sess := client.NewRoleSession(client.SessionConfig{
		RoleID:        "role-fallback-town",
		ServerAddr:    "mock-pipe",
		Dialer:        func(context.Context, string, string) (net.Conn, error) { return c1, nil },
		Authenticator: func(*client.RoleSession) error { return nil },
	})
	sess.UpdatePlayerState(func(ps *client.PlayerState) {
		ps.TownMapID = 153
	})
	if err := sess.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sess.Close()
	deadline := time.Now().Add(2 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	if !sess.ReturnToTown(context.Background(), 2*time.Second) {
		t.Fatal("153 返回 FAILED 后应自动回退至 3 并成功")
	}
	if len(seenIDs) != 2 || seenIDs[0] != 153 || seenIDs[1] != 3 {
		t.Fatalf("期望依次尝试 [153, 3], 实际 %v", seenIDs)
	}

	// 第二次调用应直接命中已缓存的有效主城 3。
	if !sess.ReturnToTown(context.Background(), 2*time.Second) {
		t.Fatal("第二次 ReturnToTown 应直接命中缓存成功")
	}
	if len(seenIDs) != 3 || seenIDs[2] != 3 {
		t.Fatalf("第二次应直接发送 townID=3, 实际 %v", seenIDs)
	}
}
