package daily

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func eraDrawPayload(result uint8) []byte {
	w := protocol.NewWriter()
	w.WriteUint8(result)
	w.WriteInt32(1)
	w.WriteInt32(4)
	return w.Bytes()
}

func TestRebirthLandRoutine_ClaimsFreeReward(t *testing.T) {
	var freeAwardCalls, freeBackCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGameAssistantInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: DailyTaskRebirthLand, CurTimes: 0, MaxTimes: 1, PerActiveAdd: 20},
			}))
		case protocol.ActionIDEraRebirthLandPoolPanel:
			w := protocol.NewWriter()
			for i := 0; i < 6; i++ {
				w.WriteInt32(int32(i))
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDEraRebirthLandDraw:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				eraDrawPayload(protocol.EraRebirthResultNotEnoughFu)))
		case protocol.ActionIDEraRebirthLandFreeAward:
			freeAwardCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.EraRebirthResultSuccess}))
		case protocol.ActionIDEraRebirthLandTodayAward:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.EraRebirthResultNoCanGetGift}))
		case protocol.ActionIDEraRebirthLandFreeBack:
			freeBackCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewRebirthLandRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("rebirth land: %v", err)
	}
	if got := freeAwardCalls.Load(); got != 1 {
		t.Fatalf("渡业仙池免费奖励 calls = %d, want 1", got)
	}
	if got := freeBackCalls.Load(); got != 0 {
		t.Fatalf("免费奖励成功后不应再调 852/6, 实际 %d 次", got)
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
		case protocol.ActionIDEraRebirthLandPoolPanel:
			w := protocol.NewWriter()
			for i := 0; i < 6; i++ {
				w.WriteInt32(0)
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDEraRebirthLandDraw:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				eraDrawPayload(protocol.EraRebirthResultNotEnoughFu)))
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
	var freeBackCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGameAssistantInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: DailyTaskRebirthLand, CurTimes: 0, MaxTimes: 1, PerActiveAdd: 20},
			}))
		case protocol.ActionIDEraRebirthLandPoolPanel:
			w := protocol.NewWriter()
			for i := 0; i < 6; i++ {
				w.WriteInt32(0)
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDEraRebirthLandDraw:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				eraDrawPayload(protocol.EraRebirthResultNotEnoughFu)))
		case protocol.ActionIDEraRebirthLandFreeAward, protocol.ActionIDEraRebirthLandTodayAward:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.EraRebirthResultNoCanGetGift}))
		case protocol.ActionIDEraRebirthLandFreeBack:
			freeBackCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildEraRebirthFreeBackResultPacket(
				protocol.EraRebirthResultNoFreeStorage, nil))
		}
	})
	defer sess.Close()

	if err := NewRebirthLandRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("rebirth land: %v", err)
	}
	if got := freeBackCalls.Load(); got != 1 {
		t.Fatalf("仙池无奖励时应兜底调用 852/6 一次, 实际 %d 次", got)
	}
}
