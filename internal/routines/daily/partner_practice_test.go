package daily

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestPartnerPracticeRoutine_StartsPractice(t *testing.T) {
	var startCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPartnerPracticeInfo:
			w := protocol.NewWriter().WriteInt32(1216785)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDPartnerPracticeStart:
			startCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.PartnerPracticeSuccess, 0, 0, 0, 0}))
		}
	})
	defer sess.Close()

	if err := NewPartnerPracticeRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("partner practice: %v", err)
	}
	if startCalls.Load() != 1 {
		t.Fatalf("start calls = %d, want 1", startCalls.Load())
	}
}

func TestPartnerPracticeRoutine_FinishedThenFight(t *testing.T) {
	var fightCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPartnerPracticeInfo:
			w := protocol.NewWriter().WriteInt32(1216785)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDPartnerPracticeStart:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.PartnerPracticeFinished, 0, 0, 0, 0}))
		case protocol.ActionIDPartnerPracticeEnterFight:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.PartnerPracticeSuccess}))
		case protocol.ActionIDPartnerPracticeFight:
			fightCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.PartnerPracticeSuccess, 0, 0, 0}))
		}
	})
	defer sess.Close()

	if err := NewPartnerPracticeRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("partner practice: %v", err)
	}
	if fightCalls.Load() != 1 {
		t.Fatalf("FINISHED 后应进入镜像战斗，fight calls = %d", fightCalls.Load())
	}
}

func TestPartnerPracticeRoutine_NoPartnerSkips(t *testing.T) {
	var startCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPartnerPracticeInfo:
			w := protocol.NewWriter().WriteInt32(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDPartnerPracticeStart:
			startCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewPartnerPracticeRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("partner practice: %v", err)
	}
	if startCalls.Load() != 0 {
		t.Fatalf("未选伙伴时不应开始修炼，start calls = %d", startCalls.Load())
	}
}

func TestPartnerPracticeRoutine_FuncNoOpenStops(t *testing.T) {
	var stopCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPartnerPracticeInfo:
			w := protocol.NewWriter().WriteInt32(1216785)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDPartnerPracticeStart:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.PartnerPracticeFuncNoOpen, 0, 0, 0, 0}))
		case protocol.ActionIDPartnerPracticeStop:
			stopCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewPartnerPracticeRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("partner practice: %v", err)
	}
	if stopCalls.Load() != 0 {
		t.Fatalf("功能未开放时不应结算，stop calls = %d", stopCalls.Load())
	}
}
