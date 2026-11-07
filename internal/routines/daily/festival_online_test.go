package daily

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func expiredOnlinePanel() []byte {
	w := protocol.NewWriter()
	w.WriteInt32(1).WriteInt32(2).WriteUint8(1).WriteUint16(0).WriteInt32(0)
	return w.Bytes()
}

func festivalOnlineInfo(index uint8) []byte {
	w := protocol.NewWriter().WriteInt32(579).WriteUint16(1)
	w.WriteUint8(index).WriteUint8(47).WriteUint16(1).WriteInt32(1740).WriteInt32(10)
	w.WriteInt32(0).WriteInt32(0)
	return w.Bytes()
}

func TestOnlineRewardRoutine_FallsBackToFestival218(t *testing.T) {
	var awardCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDOnlineRewardPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, expiredOnlinePanel()))
		case protocol.ActionIDFestivalOnlineInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, festivalOnlineInfo(1)))
		case protocol.ActionIDFestivalOnlineAward:
			awardCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.FestivalOnlineSuccess}))
		}
	})
	defer sess.Close()

	if err := NewOnlineRewardRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("online reward: %v", err)
	}
	if awardCalls.Load() != 1 {
		t.Fatalf("festival award calls = %d, want 1", awardCalls.Load())
	}
}

func TestOnlineRewardRoutine_FestivalNonActivityStops(t *testing.T) {
	var awardCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDOnlineRewardPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, expiredOnlinePanel()))
		case protocol.ActionIDFestivalOnlineInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, festivalOnlineInfo(1)))
		case protocol.ActionIDFestivalOnlineAward:
			awardCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.FestivalOnlineNonActivityTime}))
		}
	})
	defer sess.Close()

	if err := NewOnlineRewardRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("online reward: %v", err)
	}
	if awardCalls.Load() != 1 {
		t.Fatalf("非活动时间后应停止, calls = %d", awardCalls.Load())
	}
}
