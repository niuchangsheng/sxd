package pvp

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
)

// pvpAutomationFunctionIDs Mock 环境视为已开放的玩法功能编号。
var pvpAutomationFunctionIDs = []int32{
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

func newPvPMockSession(t *testing.T, handler func(net.Conn, *protocol.Packet)) *client.RoleSession {
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
				_ = protocol.WritePacket(serverConn, protocol.BuildPlayerFunctionsResultPacket(pvpAutomationFunctionIDs))
				continue
			}
			handler(serverConn, pkt)
		}
	}()
	sess := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role-holy-arena-test",
		RoleName:   "圣域测试",
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
		ps.PlayerID = 999
	})
	return sess
}

func TestHolyArenaRoutine_ClaimsEmptySeat(t *testing.T) {
	var panelCalls, robCalls atomic.Int32
	sess := newPvPMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDSaArenaGetInfo:
			n := panelCalls.Add(1)
			info := protocol.SaArenaInfo{
				Seats:          []protocol.SaArenaSeat{{Seq: 1}},
				AvailableCount: 0,
			}
			if n == 1 {
				info.AvailableCount = 1
			}
			_ = protocol.WritePacket(conn, protocol.BuildSaArenaGetInfoResultPacket(info))
		case protocol.ActionIDSaArenaRobSeat:
			robCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildSaArenaRobResultPacket(protocol.SaArenaRobResult{
				Result: protocol.SaArenaResultSuccessFree,
			}))
		}
	})
	defer sess.Close()

	if err := NewHolyArenaRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("holy arena: %v", err)
	}
	if robCalls.Load() != 1 {
		t.Fatalf("expected 1 rob, got %d", robCalls.Load())
	}
}

func TestHolyArenaRoutine_ActivityCloseStops(t *testing.T) {
	var robCalls atomic.Int32
	sess := newPvPMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDSaArenaGetInfo:
			_ = protocol.WritePacket(conn, protocol.BuildSaArenaGetInfoResultPacket(protocol.SaArenaInfo{
				Seats:          []protocol.SaArenaSeat{{Seq: 1}},
				AvailableCount: 3,
			}))
		case protocol.ActionIDSaArenaRobSeat:
			robCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildSaArenaRobResultPacket(protocol.SaArenaRobResult{
				Result: protocol.SaArenaResultActivityClose,
			}))
		}
	})
	defer sess.Close()

	if err := NewHolyArenaRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("holy arena: %v", err)
	}
	if robCalls.Load() != 1 {
		t.Fatalf("expected stop after ACTIVITY_CLOSE, got %d robs", robCalls.Load())
	}
}
