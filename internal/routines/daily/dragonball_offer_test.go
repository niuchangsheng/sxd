package daily

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestDragonballOfferRoutine_StopsWhenTimesUp(t *testing.T) {
	var calls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		if pkt.ActionID != protocol.ActionIDDragonballSacrifice {
			return
		}
		n := calls.Add(1)
		code := protocol.DragonballResultSuccess
		remain := uint8(3 - n)
		if n >= 3 {
			code = protocol.DragonballResultTimesUseUp
		}
		_ = protocol.WritePacket(conn, protocol.BuildDragonballSacrificeResultPacket(protocol.DragonballSacrificeResult{
			Result: code, RemainTimes: remain,
		}))
	})
	defer sess.Close()

	if err := NewDragonballOfferRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("dragonball offer: %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("expected 3 calls before TIMES_USE_UP, got %d", calls.Load())
	}
}

func TestDragonballOfferRoutine_NoCoinStops(t *testing.T) {
	var calls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		if pkt.ActionID != protocol.ActionIDDragonballSacrifice {
			return
		}
		calls.Add(1)
		_ = protocol.WritePacket(conn, protocol.BuildDragonballSacrificeResultPacket(protocol.DragonballSacrificeResult{
			Result: protocol.DragonballResultNoEnoughCoin,
		}))
	})
	defer sess.Close()

	if err := NewDragonballOfferRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("dragonball offer: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected stop after no coin, got %d", calls.Load())
	}
}

func TestDragonballOfferRoutine_TmpGridFullCollectsAndRetries(t *testing.T) {
	var sacrificeCalls, collectCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDDragonballSacrifice:
			n := sacrificeCalls.Add(1)
			code := protocol.DragonballResultTmpGridFull
			if n > 1 {
				code = protocol.DragonballResultTimesUseUp
			}
			_ = protocol.WritePacket(conn, protocol.BuildDragonballSacrificeResultPacket(
				protocol.DragonballSacrificeResult{Result: code}))
		case protocol.ActionIDDragonballTmpInfo:
			_ = protocol.WritePacket(conn, protocol.BuildDragonballTmpInfoResultPacket(
				[]protocol.DragonballTmpBall{{PlayerDBID: 1001, Level: 5, Quality: 3}}))
		case protocol.ActionIDDragonballCollect:
			collectCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildDragonballCollectResultPacket(
				protocol.DragonballResultSuccess, []int32{1001}))
		}
	})
	defer sess.Close()

	if err := NewDragonballOfferRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("dragonball offer: %v", err)
	}
	if sacrificeCalls.Load() != 2 {
		t.Fatalf("expected retry after collect, got %d sacrifice calls", sacrificeCalls.Load())
	}
	if collectCalls.Load() != 1 {
		t.Fatalf("expected 1 collect, got %d", collectCalls.Load())
	}
}
