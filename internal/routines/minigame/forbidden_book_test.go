package minigame

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
)

// minigameAutomationFunctionIDs Mock 环境视为已开放的玩法功能编号。
var minigameAutomationFunctionIDs = []int32{
	protocol.FunctionIDZodiac, protocol.FunctionIDBeelzebubTrials,
	protocol.FunctionIDMiracleFighters, protocol.FunctionIDRepressDemonTower,
	protocol.FunctionIDShanhaiWorld, protocol.FunctionIDEudemons,
	protocol.FunctionIDDestinySword, protocol.FunctionIDLinkFate,
	protocol.FunctionIDPartnerLink, protocol.FunctionIDSaintArea,
	protocol.FunctionIDFaction, protocol.FunctionIDTreasureHouse,
	protocol.FunctionIDStTrials, protocol.FunctionIDStPracticeRoom,
	protocol.FunctionIDTravelEvent, protocol.FunctionIDRollCake,
	protocol.FunctionIDFindImmortal, protocol.FunctionIDFishing,
}

func newMinigameMockSession(t *testing.T, handler func(net.Conn, *protocol.Packet)) *client.RoleSession {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})
	go func() {
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			// 参与自动化功能门槛的玩法在 Mock 下一律视为已开放。
			if pkt.ActionID == protocol.ActionIDPlayerGetFunctions {
				_ = protocol.WritePacket(serverConn, protocol.BuildPlayerFunctionsResultPacket(minigameAutomationFunctionIDs))
				continue
			}
			handler(serverConn, pkt)
		}
	}()
	sess := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role-forbidden-book-test",
		RoleName:   "禁地测试",
		ServerAddr: "mock-pipe",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(*client.RoleSession) error { return nil },
	})
	if err := sess.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	sess.UpdatePlayerState(func(ps *client.PlayerState) {
		ps.Coins = 1_000_000
	})
	return sess
}

func TestForbiddenBookRoutine_CopperExchangeThenExplore(t *testing.T) {
	var exchangeCalls, exploreCalls atomic.Int32
	var exploreType atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDForbiddenBookInfo:
			info := protocol.ForbiddenBookInfo{Areas: []protocol.ForbiddenBookArea{
				{ForbiddenType: 19, ExploreType: protocol.ForbiddenBookMulti, CurNumber: 0, CostIngot: 200},
				{ForbiddenType: 19, ExploreType: protocol.ForbiddenBookOnce, CurNumber: 0, CostIngot: 0},
				{ForbiddenType: 20, ExploreType: protocol.ForbiddenBookMulti, CurNumber: 0, CostIngot: 300},
				{ForbiddenType: 20, ExploreType: protocol.ForbiddenBookOnce, CurNumber: 0, CostIngot: 0},
			}}
			_ = protocol.WritePacket(conn, protocol.BuildForbiddenBookInfoResultPacket(info))
		case protocol.ActionIDForbiddenBookExchangeInfo:
			_ = protocol.WritePacket(conn, protocol.BuildForbiddenBookExchangeInfoResultPacket([]protocol.ForbiddenBookExchangeInfo{
				{ForbiddenType: 19, CostCoin: 50, CostIngot: 0, Count: 2},
				{ForbiddenType: 20, CostCoin: 100, CostIngot: 0, Count: 1},
				{ForbiddenType: 18, CostCoin: 10, CostIngot: 1, Count: 5}, // 含元宝，必须跳过
			}))
		case protocol.ActionIDForbiddenBookExchange:
			exchangeCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildForbiddenBookExchangeResultPacket(protocol.AwakeResultSuccess))
		case protocol.ActionIDForbiddenBookExplore:
			exploreCalls.Add(1)
			if len(pkt.Payload) >= 2 {
				exploreType.Store(int32(pkt.Payload[1]))
			}
			_ = protocol.WritePacket(conn, protocol.BuildForbiddenBookExploreResultPacket(protocol.AwakeResultSuccess))
		}
	})
	defer sess.Close()

	if err := NewForbiddenBookRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("forbidden book: %v", err)
	}
	if got := exchangeCalls.Load(); got != 3 {
		t.Fatalf("expected 3 copper exchanges (2x19 + 1x20), got %d", got)
	}
	if got := exploreCalls.Load(); got != 3 {
		t.Fatalf("expected 3 explorations after exchange, got %d", got)
	}
	if got := exploreType.Load(); got != int32(protocol.ForbiddenBookOnce) {
		t.Fatalf("expected single explore to avoid ingots, got explore_type=%d", got)
	}
	if coins := sess.GetPlayerState().Coins; coins != 1_000_000-2*50-100 {
		t.Fatalf("unexpected coins after exchange: %d", coins)
	}
}

func TestForbiddenBookRoutine_ExchangeFailureStop(t *testing.T) {
	var exploreCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDForbiddenBookInfo:
			_ = protocol.WritePacket(conn, protocol.BuildForbiddenBookInfoResultPacket(protocol.ForbiddenBookInfo{
				Areas: []protocol.ForbiddenBookArea{{ForbiddenType: 19, ExploreType: protocol.ForbiddenBookMulti, CurNumber: 0}},
			}))
		case protocol.ActionIDForbiddenBookExchangeInfo:
			_ = protocol.WritePacket(conn, protocol.BuildForbiddenBookExchangeInfoResultPacket([]protocol.ForbiddenBookExchangeInfo{
				{ForbiddenType: 19, CostCoin: 50, CostIngot: 0, Count: 2},
			}))
		case protocol.ActionIDForbiddenBookExchange:
			_ = protocol.WritePacket(conn, protocol.BuildForbiddenBookExchangeResultPacket(protocol.AwakeResultNoEnoughCoin))
		case protocol.ActionIDForbiddenBookExplore:
			exploreCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildForbiddenBookExploreResultPacket(protocol.AwakeResultSuccess))
		}
	})
	defer sess.Close()

	if err := NewForbiddenBookRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("forbidden book: %v", err)
	}
	if exploreCalls.Load() != 0 {
		t.Fatalf("exchange failure should not trigger exploration, got %d", exploreCalls.Load())
	}
}
