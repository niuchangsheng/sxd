package daily_test

import (
	"context"
	"net"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/daily"
	"sxd-pie-ng/internal/scheduler"
)

func TestTownPracticeRoutine_GuardStatements(t *testing.T) {
	r := daily.NewTownPracticeRoutine()
	if r.Name() != "town_practice" {
		t.Fatalf("unexpected routine name: %s", r.Name())
	}
	if dr, ok := r.(scheduler.DetailedRoutine); ok {
		if dr.Domain() != "daily" {
			t.Errorf("unexpected domain: %s", dr.Domain())
		}
	}

	ctx := context.Background()

	// 1. nil session
	if err := r.Execute(ctx, nil, nil); err == nil {
		t.Errorf("expected error for nil session, got nil")
	}

	// 2. TownID <= 0 (not in town)
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID:            "role-guard-test",
		ServerAddr:        "mock-pipe",
		HeartbeatInterval: 1 * time.Second,
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(s *client.RoleSession) error {
			return nil
		},
	})
	_ = session.Start(ctx)
	defer session.Close()

	// TownID is default 0
	if err := r.Execute(ctx, session, nil); err != nil {
		t.Errorf("expected nil when TownID <= 0, got %v", err)
	}
}

func TestTownPracticeRoutine_ExecutionFlow(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	cancelReceived := false
	signReceived := false

	go func() {
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDPlayerCancelPractice:
				cancelReceived = true
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDPlayerCancelPractice, []byte{}))
			case protocol.ActionIDPlayerSignFinishedPractice:
				signReceived = true
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDPlayerSignFinishedPractice, []byte{}))
			}
		}
	}()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID:            "role-practice-routine",
		ServerAddr:        "mock-pipe",
		HeartbeatInterval: 1 * time.Second,
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(s *client.RoleSession) error {
			return nil
		},
	})

	ctx := context.Background()
	_ = session.Start(ctx)
	defer session.Close()

	// 模拟已进入主城场景
	session.UpdatePlayerState(func(ps *client.PlayerState) {
		ps.TownID = 4879
	})

	r := daily.NewTownPracticeRoutine()
	jitter := scheduler.NewJitter(scheduler.JitterConfig{
		MinDuration: 5 * time.Millisecond,
		MaxDuration: 10 * time.Millisecond,
	})

	// 执行 routine: 上线直接结算并领取离线经验
	if err := r.Execute(ctx, session, jitter); err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	if !cancelReceived {
		t.Errorf("expected cancel_practice packet to be sent")
	}
	if !signReceived {
		t.Errorf("expected sign_finished_practice packet to be sent")
	}
}
