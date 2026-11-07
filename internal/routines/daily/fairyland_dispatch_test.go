package daily

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestFairylandDispatchRoutine_NotOpenSkips(t *testing.T) {
	var taskCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDFairylandMainPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.FairylandFailed, 0}))
		case protocol.ActionIDFairylandTaskPanel:
			taskCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewFairylandDispatchRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("fairyland: %v", err)
	}
	if taskCalls.Load() != 0 {
		t.Fatalf("未开放时不应查询任务, calls = %d", taskCalls.Load())
	}
}

func TestFairylandDispatchRoutine_Dispatches(t *testing.T) {
	var dispatchCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDFairylandMainPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.FairylandSuccess, 0}))
		case protocol.ActionIDFairylandTaskPanel:
			w := protocol.NewWriter().WriteUint8(protocol.FairylandSuccess).WriteUint16(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDFairylandDispatch:
			dispatchCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.FairylandSuccess}))
		}
	})
	defer sess.Close()

	if err := NewFairylandDispatchRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("fairyland: %v", err)
	}
	if dispatchCalls.Load() == 0 {
		t.Fatal("开放时应派遣游历")
	}
}

func TestFairylandDispatchRoutine_OrderCompletedStops(t *testing.T) {
	var dispatchCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDFairylandMainPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.FairylandSuccess, 0}))
		case protocol.ActionIDFairylandTaskPanel:
			w := protocol.NewWriter().WriteUint8(protocol.FairylandSuccess).WriteUint16(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDFairylandDispatch:
			dispatchCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.FairylandOrderCompleted}))
		}
	})
	defer sess.Close()

	if err := NewFairylandDispatchRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("fairyland: %v", err)
	}
	if dispatchCalls.Load() != 1 {
		t.Fatalf("订单完成后应停止, dispatch calls = %d", dispatchCalls.Load())
	}
}
