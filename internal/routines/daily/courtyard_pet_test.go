package daily

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func petInfoPayload(talisman int16) []byte {
	w := protocol.NewWriter()
	w.WriteInt16(talisman)
	w.WriteBytes([]byte{0, 0, 0, 0, 0, 0})
	return w.Bytes()
}

func TestCourtyardPetRoutine_ConsumesAllTalismans(t *testing.T) {
	var callCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDCourtyardPetInfo:
			remain := int16(3) - int16(callCalls.Load())
			if remain < 0 {
				remain = 0
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, petInfoPayload(remain)))
		case protocol.ActionIDCourtyardPetCall:
			callCalls.Add(1)
			w := protocol.NewWriter()
			w.WriteUint8(protocol.CourtyardPetResultSuccess)
			w.WriteInt16(0)
			w.WriteUint8(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		}
	})
	defer sess.Close()

	if err := NewCourtyardPetRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("courtyard pet: %v", err)
	}
	if got := callCalls.Load(); got != 3 {
		t.Fatalf("灵符=3 时应召唤 3 次, 实际 %d 次", got)
	}
}

func TestCourtyardPetRoutine_SkipsWithoutTalisman(t *testing.T) {
	var callCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDCourtyardPetInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, petInfoPayload(0)))
		case protocol.ActionIDCourtyardPetCall:
			callCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewCourtyardPetRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("courtyard pet: %v", err)
	}
	if got := callCalls.Load(); got != 0 {
		t.Fatalf("无灵符时不应召唤, 实际 %d 次", got)
	}
}

func TestCourtyardPetRoutine_StopsWhenCountNotDecrease(t *testing.T) {
	var callCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDCourtyardPetInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, petInfoPayload(5)))
		case protocol.ActionIDCourtyardPetCall:
			callCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.CourtyardPetResultSuccess}))
		}
	})
	defer sess.Close()

	if err := NewCourtyardPetRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("courtyard pet: %v", err)
	}
	if got := callCalls.Load(); got != 1 {
		t.Fatalf("灵符数量未递减时应熔断, 实际召唤 %d 次", got)
	}
}
