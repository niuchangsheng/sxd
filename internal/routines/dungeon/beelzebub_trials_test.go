package dungeon_test

import (
	"context"
	"net"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/dungeon"
	"sxd-pie-ng/internal/scheduler"
)

func TestBeelzebubTrialsRoutine_IdentityAndConfig(t *testing.T) {
	cfg := dungeon.DefaultBeelzebubTrialsConfig()
	if cfg.MinLevel != 0 || !cfg.AutoMoralAward || cfg.PreferBossID != 0 {
		t.Fatalf("unexpected default config: %+v", cfg)
	}

	rt := dungeon.NewBeelzebubTrialsRoutine(cfg)
	if rt.Name() != "beelzebub_trials" {
		t.Fatalf("expected routine name beelzebub_trials, got %s", rt.Name())
	}
	if dr, ok := rt.(scheduler.DetailedRoutine); ok {
		if dr.Domain() != "dungeon" {
			t.Fatalf("expected domain dungeon, got %s", dr.Domain())
		}
		if dr.ScheduleType() != scheduler.ScheduleCron {
			t.Fatalf("expected ScheduleCron, got %v", dr.ScheduleType())
		}
	}
}

func TestBeelzebubTrialsRoutine_LevelGate(t *testing.T) {
	cfg := dungeon.BeelzebubTrialsConfig{
		MinLevel:       70,
		AutoMoralAward: true,
	}
	rt := dungeon.NewBeelzebubTrialsRoutine(cfg)

	s := client.NewRoleSession(client.SessionConfig{
		ServerAddr: "sandbox",
		RoleID:     "1001",
		Authenticator: func(s *client.RoleSession) error {
			return nil
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_ = s.Start(ctx)
	defer s.Close()

	// 角色等级 60 < 门槛 70
	s.UpdatePlayerState(func(ps *client.PlayerState) {
		ps.Level = 60
	})

	err := rt.Execute(ctx, s, nil)
	if err != nil {
		t.Fatalf("expected nil when level gate not met, got: %v", err)
	}
}

func TestBeelzebubTrialsRoutine_FullExecutionFlow(t *testing.T) {
	cfg := dungeon.BeelzebubTrialsConfig{
		PreferBossID:   72, // 魔掌门神识
		AutoMoralAward: true,
		MinLevel:       70,
	}
	rt := dungeon.NewBeelzebubTrialsRoutine(cfg)

	s := client.NewRoleSession(client.SessionConfig{
		ServerAddr: "sandbox",
		RoleID:     "1001",
		Authenticator: func(s *client.RoleSession) error {
			return nil
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.Start(ctx); err != nil {
		t.Fatalf("session start failed: %v", err)
	}
	defer s.Close()

	deadline := time.Now().Add(2 * time.Second)
	for s.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	s.UpdatePlayerState(func(ps *client.PlayerState) {
		ps.Level = 100
	})

	err := rt.Execute(ctx, s, nil)
	if err != nil {
		t.Fatalf("BeelzebubTrialsRoutine.Execute failed: %v", err)
	}
}

func TestBeelzebubTrialsRoutine_NoAttempts(t *testing.T) {
	// 模拟已无挑战次数的情况
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	go func() {
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDBeelzebubTrialsCheckCanChallenge:
				// 返回无法挑战 (Failed = 14)
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsCheckCanChallenge,
					[]byte{protocol.BeelzebubTrialsFailed},
				))
			case protocol.ActionIDBeelzebubTrialsOpenMoral:
				// 返回道行无法领取
				mw := protocol.NewWriter()
				mw.WriteInt32(100)
				mw.WriteInt32(1)
				mw.WriteInt32(0) // CanClaim = false
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsOpenMoral,
					mw.Bytes(),
				))
			}
		}
	}()

	s := client.NewRoleSession(client.SessionConfig{
		ServerAddr: "mock-pipe",
		RoleID:     "1001",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(s *client.RoleSession) error {
			return nil
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_ = s.Start(ctx)
	defer s.Close()

	deadline := time.Now().Add(1 * time.Second)
	for s.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	cfg := dungeon.DefaultBeelzebubTrialsConfig()
	rt := dungeon.NewBeelzebubTrialsRoutine(cfg)

	err := rt.Execute(ctx, s, nil)
	if err != nil {
		t.Fatalf("expected nil when no attempts, got: %v", err)
	}
}
