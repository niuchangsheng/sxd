package minigame

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestBaiLianRoutine_OneKeyBoth(t *testing.T) {
	var oneCalls, twoCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDBaiLianOneInfo:
			w := protocol.NewWriter()
			w.WriteInt8(100)
			w.WriteUint8(0)
			w.WriteUint8(2)
			w.WriteUint8(5)
			w.WriteInt32(965)
			w.WriteInt16(500)
			w.WriteInt16(100)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDBaiLianOneKey:
			oneCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.BaiLianOneSuccess}))
		case protocol.ActionIDBaiLianTwoInfo:
			w := protocol.NewWriter()
			w.WriteUint8(protocol.BaiLianTwoSuccess)
			w.WriteInt8(100)
			w.WriteUint8(0)
			w.WriteUint8(2)
			w.WriteUint8(5)
			w.WriteInt32(896)
			w.WriteInt16(500)
			w.WriteInt16(100)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDBaiLianTwoKey:
			twoCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.BaiLianTwoFailed}))
		case protocol.ActionIDBaiLianTwoFight:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.BaiLianTwoFailed}))
		}
	})
	defer sess.Close()

	if err := NewBaiLianRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("bailian: %v", err)
	}
	if oneCalls.Load() != 1 || twoCalls.Load() != 1 {
		t.Fatalf("one_key calls=(%d,%d), want (1,1)", oneCalls.Load(), twoCalls.Load())
	}
}

func TestBaiLianRoutine_FuncUnopenStops(t *testing.T) {
	var keyCalls, fightCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDBaiLianOneInfo:
			w := protocol.NewWriter()
			w.WriteInt8(1)
			w.WriteUint8(0)
			w.WriteUint8(0)
			w.WriteUint8(0)
			w.WriteInt32(0)
			w.WriteInt16(0)
			w.WriteInt16(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDBaiLianOneKey:
			keyCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.BaiLianOneFailed}))
		case protocol.ActionIDBaiLianOneFight:
			fightCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.BaiLianOneFailed}))
		}
	})
	defer sess.Close()

	if err := NewBaiLianRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("bailian: %v", err)
	}
	if keyCalls.Load() != 1 {
		t.Fatalf("one_key calls = %d, want 1", keyCalls.Load())
	}
	if fightCalls.Load() != 1 {
		t.Fatalf("一键扫荡失败后应尝试单次挑战, fight calls = %d", fightCalls.Load())
	}
}
