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
			// 圣域竞技场先查圣域开启状态，Mock 默认返回"已开启、无关闭窗口"。
			if pkt.ActionID == protocol.ActionIDSaintAreaStatus {
				w := protocol.NewWriter()
				w.WriteUint8(protocol.SaintAreaStatusOpen)
				w.WriteUint16(0)
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDSaintAreaStatus, w.Bytes()))
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

// stubSaintAreaSession 让圣域会话建立返回主会话本身，便于单测验证协议流程。
func stubSaintAreaSession(t *testing.T) {
	t.Helper()
	prev := openSaintAreaSession
	openSaintAreaSession = func(ctx context.Context, s *client.RoleSession) (*client.RoleSession, error) {
		return s, nil
	}
	t.Cleanup(func() { openSaintAreaSession = prev })
}

func TestHolyArenaRoutine_ClaimsEmptySeat(t *testing.T) {
	stubSaintAreaSession(t)
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
	stubSaintAreaSession(t)
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

// TestHolyArenaRoutine_SaintSessionUnavailableSkips 验证：圣域连接无法建立时
// 不应回退到主服发起抢占（主服 rob 会固定返回 ACTIVITY_CLOSE）。
func TestHolyArenaRoutine_SaintSessionUnavailableSkips(t *testing.T) {
	var robCalls, panelCalls atomic.Int32
	prev := openSaintAreaSession
	openSaintAreaSession = func(ctx context.Context, s *client.RoleSession) (*client.RoleSession, error) {
		return nil, context.DeadlineExceeded
	}
	t.Cleanup(func() { openSaintAreaSession = prev })

	sess := newPvPMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDSaArenaGetInfo:
			panelCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildSaArenaGetInfoResultPacket(protocol.SaArenaInfo{
				Seats:          []protocol.SaArenaSeat{{Seq: 1}},
				AvailableCount: 1,
			}))
		case protocol.ActionIDSaArenaRobSeat:
			robCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewHolyArenaRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("holy arena: %v", err)
	}
	if panelCalls.Load() != 0 || robCalls.Load() != 0 {
		t.Fatalf("expected skip when saint session unavailable, panel=%d rob=%d",
			panelCalls.Load(), robCalls.Load())
	}
}
