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

func TestCaravanHijackRoutine_SearchesThenRobs(t *testing.T) {
	var detailCalls, searchCalls, robCalls atomic.Int32
	sess := newPvPMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDCaravanGetDetail:
			detailCalls.Add(1)
			state := protocol.CaravanStateNotSearch
			if detailCalls.Load() > 1 {
				state = protocol.CaravanStateSearched
			}
			_ = protocol.WritePacket(conn, protocol.BuildCaravanInfoResultPacket(state))
		case protocol.ActionIDCaravanGetList:
			searchCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildCaravanSearchResultPacket(
				[]int32{1001}, protocol.CaravanResultSuccess))
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
	if got := searchCalls.Load(); got != 1 {
		t.Fatalf("search calls = %d, want 1", got)
	}
	if got := robCalls.Load(); got != 1 {
		t.Fatalf("rob calls = %d, want 1", got)
	}
}

// TestCaravanHijackRoutine_StopsWhenSearchNeedsIngot 验证勘察返回元宝/购买次数
// 不足时立即停止，不调用 rob，也不调用 buy_rob_times。
func TestCaravanHijackRoutine_StopsWhenSearchNeedsIngot(t *testing.T) {
	var robCalls, buyCalls atomic.Int32
	sess := newPvPMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDCaravanGetDetail:
			_ = protocol.WritePacket(conn, protocol.BuildCaravanInfoResultPacket(protocol.CaravanStateNotSearch))
		case protocol.ActionIDCaravanGetList:
			_ = protocol.WritePacket(conn, protocol.BuildCaravanSearchResultPacket(
				nil, protocol.CaravanResultNotEnoughIngot))
		case protocol.ActionIDCaravanRob:
			robCalls.Add(1)
		case 0x013A0003: // buy_rob_times
			buyCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewCaravanHijackRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("caravan: %v", err)
	}
	if got := robCalls.Load(); got != 0 {
		t.Fatalf("勘察付费时不应 rob, 实际 %d 次", got)
	}
	if got := buyCalls.Load(); got != 0 {
		t.Fatalf("严禁调用 buy_rob_times, 实际 %d 次", got)
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
