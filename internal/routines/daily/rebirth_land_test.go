package daily

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestRebirthLandRoutine_ClaimsFreeReward(t *testing.T) {
	var claimCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGameAssistantInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: DailyTaskRebirthLand, CurTimes: 0, MaxTimes: 1, PerActiveAdd: 20},
			}))
		case protocol.ActionIDEraRebirthLandFreeBack:
			claimCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildEraRebirthFreeBackResultPacket(
				protocol.EraRebirthResultSuccess, []protocol.EraRebirthReward{{ItemID: 10994, Num: 72000}}))
		}
	})
	defer sess.Close()

	if err := NewRebirthLandRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("rebirth land: %v", err)
	}
	if got := claimCalls.Load(); got != 1 {
		t.Fatalf("claim calls = %d, want 1", got)
	}
}

func TestRebirthLandRoutine_SkipsWhenTaskComplete(t *testing.T) {
	var claimCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGameAssistantInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: DailyTaskRebirthLand, CurTimes: 1, MaxTimes: 1, PerActiveAdd: 20},
			}))
		case protocol.ActionIDEraRebirthLandFreeBack:
			claimCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewRebirthLandRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("rebirth land: %v", err)
	}
	if got := claimCalls.Load(); got != 0 {
		t.Fatalf("claim calls = %d, want 0", got)
	}
}

func TestRebirthLandRoutine_NoGiftStops(t *testing.T) {
	var claimCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGameAssistantInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: DailyTaskRebirthLand, CurTimes: 0, MaxTimes: 1, PerActiveAdd: 20},
			}))
		case protocol.ActionIDEraRebirthLandFreeBack:
			claimCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildEraRebirthFreeBackResultPacket(
				protocol.EraRebirthResultNoCanGetGift, nil))
		}
	})
	defer sess.Close()

	if err := NewRebirthLandRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("rebirth land: %v", err)
	}
	if got := claimCalls.Load(); got != 1 {
		t.Fatalf("claim calls = %d, want 1", got)
	}
}
