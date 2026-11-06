package pvp

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestCaravanHijackRoutine_RobsSearchedTarget(t *testing.T) {
	var robCalls atomic.Int32
	sess := newPvPMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDCaravanGetDetail:
			_ = protocol.WritePacket(conn, protocol.BuildCaravanInfoResultPacket(protocol.CaravanStateSearched))
		case protocol.ActionIDCaravanRob:
			robCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildCaravanRobResultPacket(
				protocol.CaravanResultSuccess, [2]int32{3, 1000}))
		}
	})
	defer sess.Close()

	if err := NewCaravanHijackRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("caravan: %v", err)
	}
	if got := robCalls.Load(); got != 1 {
		t.Fatalf("rob calls = %d, want 1", got)
	}
}

func TestCaravanHijackRoutine_SkipsWhenNotSearched(t *testing.T) {
	var robCalls atomic.Int32
	sess := newPvPMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDCaravanGetDetail:
			_ = protocol.WritePacket(conn, protocol.BuildCaravanInfoResultPacket(protocol.CaravanStateNotSearch))
		case protocol.ActionIDCaravanRob:
			robCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewCaravanHijackRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("caravan: %v", err)
	}
	if got := robCalls.Load(); got != 0 {
		t.Fatalf("rob calls = %d, want 0", got)
	}
}

func TestCaravanHijackRoutine_StopsOnFailure(t *testing.T) {
	var robCalls atomic.Int32
	sess := newPvPMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDCaravanGetDetail:
			_ = protocol.WritePacket(conn, protocol.BuildCaravanInfoResultPacket(protocol.CaravanStateSearched))
		case protocol.ActionIDCaravanRob:
			robCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildCaravanRobResultPacket(
				protocol.CaravanResultFailure, [2]int32{}))
		}
	})
	defer sess.Close()

	if err := NewCaravanHijackRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("caravan: %v", err)
	}
	if got := robCalls.Load(); got != 1 {
		t.Fatalf("rob calls = %d, want 1", got)
	}
}
