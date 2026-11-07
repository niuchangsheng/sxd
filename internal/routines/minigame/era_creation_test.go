package minigame

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestEraCreationRoutine_ExploreFightAward(t *testing.T) {
	var fightID atomic.Int32
	var chooseCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEraCreationPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.EraCreationSuccess}))
		case protocol.ActionIDEraCreationExplore:
			w := protocol.NewWriter().WriteUint8(protocol.EraCreationSuccess)
			w.WriteInt32(244401).WriteInt32(6).WriteInt32(3).WriteInt32(23)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDEraCreationFightEvent:
			w := protocol.NewWriter().WriteUint8(protocol.EraCreationSuccess).WriteUint16(1)
			w.WriteInt32(244401)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDEraCreationFight:
			r := protocol.NewReader(pkt.Payload)
			id, _ := r.ReadInt32()
			fightID.Store(id)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.EraCreationSuccess, 0, 0, 0}))
		case protocol.ActionIDEraCreationAwardPanel:
			w := protocol.NewWriter().WriteUint8(protocol.EraCreationSuccess)
			w.WriteUint16(0)
			w.WriteInt32(0).WriteInt32(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDEraCreationChoose:
			chooseCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.EraCreationSuccess, 0, 0, 0, 0}))
		}
	})
	defer sess.Close()

	if err := NewEraCreationRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("era creation: %v", err)
	}
	if fightID.Load() != 244401 {
		t.Fatalf("fight 事件 id = %d, want 244401", fightID.Load())
	}
	if chooseCalls.Load() != 1 {
		t.Fatalf("choose award calls = %d, want 1", chooseCalls.Load())
	}
}

func TestEraCreationRoutine_FuncNotOpenStops(t *testing.T) {
	var exploreCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEraCreationPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.EraCreationFuncNotOpen}))
		case protocol.ActionIDEraCreationExplore:
			exploreCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewEraCreationRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("era creation: %v", err)
	}
	if exploreCalls.Load() != 0 {
		t.Fatalf("FUNC_NOT_OPEN 时不应探索，explore calls = %d", exploreCalls.Load())
	}
}

func TestEraCreationRoutine_NoExploreStillChecksEvents(t *testing.T) {
	var eventCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEraCreationPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.EraCreationSuccess}))
		case protocol.ActionIDEraCreationExplore:
			w := protocol.NewWriter().WriteUint8(protocol.EraCreationNoExploreNum)
			w.WriteInt32(0).WriteInt32(0).WriteInt32(0).WriteInt32(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDEraCreationFightEvent:
			eventCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.EraCreationSuccess, 0, 0}))
		case protocol.ActionIDEraCreationAwardPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.EraCreationNoFightAward}))
		}
	})
	defer sess.Close()

	if err := NewEraCreationRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("era creation: %v", err)
	}
	if eventCalls.Load() != 1 {
		t.Fatalf("explore 次数不足仍应查询事件，event calls = %d", eventCalls.Load())
	}
}