package daily

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
)

// newGodBlessTestSession 构造一个只响应天神赐福面板/等级奖励/领取的测试会话。
func newGodBlessTestSession(t *testing.T, playerLevel int, info protocol.GodWelfareInfo, awards []protocol.GodWelfareLevelAward) (*client.RoleSession, *atomic.Int32) {
	t.Helper()

	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})

	var claimedLevel atomic.Int32
	claimedLevel.Store(-1)
	go func() {
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDGodWelfareInfo:
				_ = protocol.WritePacket(serverConn, protocol.BuildGodWelfareInfoResultPacket(info))
			case protocol.ActionIDGodWelfareLevelAwardInfo:
				_ = protocol.WritePacket(serverConn, protocol.BuildGodWelfareLevelAwardInfoResultPacket(awards))
			case protocol.ActionIDGodWelfareGetAward:
				r := protocol.NewReader(pkt.Payload)
				level, _ := r.ReadInt16()
				claimedLevel.Store(int32(level))
				_ = protocol.WritePacket(serverConn,
					protocol.BuildGodWelfareGetAwardResultPacket(protocol.GodWelfareResultSuccess))
			}
		}
	}()

	s := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role-god-bless-test",
		RoleName:   "天神赐福测试",
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
	s.UpdatePlayerState(func(ps *client.PlayerState) { ps.Level = playerLevel })
	return s, &claimedLevel
}

func TestGodBlessRoutine_SkipsWhenPlayerLevelAboveCap(t *testing.T) {
	info := protocol.GodWelfareInfo{Level: 260, IsGeted: 0}
	awards := []protocol.GodWelfareLevelAward{{Level: 90, NeedValue: 1, AwardValue: 1, IsGet: 0}}
	s, claimedLevel := newGodBlessTestSession(t, godWelfareMaxPlayerLevel+1, info, awards)

	if err := NewGodBlessRoutine().Execute(context.Background(), s, nil); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := claimedLevel.Load(); got != -1 {
		t.Fatalf("player level above cap must not claim, got level=%d", got)
	}
}

func TestGodBlessRoutine_SkipsWhenAlreadyGeted(t *testing.T) {
	info := protocol.GodWelfareInfo{Level: 260, IsGeted: 1}
	awards := []protocol.GodWelfareLevelAward{{Level: 90, NeedValue: 1, AwardValue: 1, IsGet: 0}}
	s, claimedLevel := newGodBlessTestSession(t, 100, info, awards)

	if err := NewGodBlessRoutine().Execute(context.Background(), s, nil); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := claimedLevel.Load(); got != -1 {
		t.Fatalf("is_geted=1 must not claim, got level=%d", got)
	}
}

func TestGodBlessRoutine_ClaimsEligibleLevelAward(t *testing.T) {
	info := protocol.GodWelfareInfo{Level: 260, IsGeted: 0}
	awards := []protocol.GodWelfareLevelAward{
		{Level: 80, NeedValue: 1, AwardValue: 1, IsGet: 1},
		{Level: 90, NeedValue: 2, AwardValue: 2, IsGet: 0},
	}
	s, claimedLevel := newGodBlessTestSession(t, 100, info, awards)

	if err := NewGodBlessRoutine().Execute(context.Background(), s, nil); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := claimedLevel.Load(); got != 90 {
		t.Fatalf("expected claim level 90, got %d", got)
	}
}
