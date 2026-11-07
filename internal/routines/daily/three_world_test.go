package daily

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func humanWorldPracticePayload(result uint8, state int32) []byte {
	w := protocol.NewWriter().WriteUint8(result)
	vals := []int32{0, 0, 0, 0, state, 3, 40}
	for _, v := range vals {
		w.WriteInt32(v)
	}
	return w.Bytes()
}

func TestThreeWorldRoutine_IdleStartsPractice(t *testing.T) {
	var startCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDThreeWorldStatus:
			w := protocol.NewWriter().WriteUint8(protocol.ThreeWorldOpen).WriteUint8(uint8(protocol.ThreeWorldIdle))
			w.WriteInt32(0).WriteInt32(0).WriteInt32(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDHumanWorldPractice:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, humanWorldPracticePayload(0, 0)))
		case protocol.ActionIDHumanWorldStart:
			startCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.ThreeWorldSuccess}))
		}
	})
	defer sess.Close()

	if err := NewThreeWorldRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("three world: %v", err)
	}
	if startCalls.Load() != 1 {
		t.Fatalf("空闲时应开始修炼, start calls = %d", startCalls.Load())
	}
}

func TestThreeWorldRoutine_CompletedStops(t *testing.T) {
	var stopCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDThreeWorldStatus:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.ThreeWorldOpen, uint8(protocol.ThreeWorldComplete), 0, 0, 0, 0}))
		case protocol.ActionIDHumanWorldPractice:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, humanWorldPracticePayload(0, 6)))
		case protocol.ActionIDHumanWorldStop:
			stopCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.ThreeWorldSuccess}))
		}
	})
	defer sess.Close()

	if err := NewThreeWorldRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("three world: %v", err)
	}
	if stopCalls.Load() != 1 {
		t.Fatalf("完成时应结算, stop calls = %d", stopCalls.Load())
	}
}

func TestThreeWorldRoutine_PracticeFailedNoStart(t *testing.T) {
	var startCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDThreeWorldStatus:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.ThreeWorldClose, 0, 0, 0, 0, 0}))
		case protocol.ActionIDHumanWorldPractice:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.ThreeWorldFailed, 0, 0, 0, 0}))
		case protocol.ActionIDHumanWorldStart:
			startCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewThreeWorldRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("three world: %v", err)
	}
	if startCalls.Load() != 0 {
		t.Fatalf("面板失败时不应开始修炼, start calls = %d", startCalls.Load())
	}
}
