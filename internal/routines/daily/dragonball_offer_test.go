package daily

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// respondDragonballMerge 解析一键合成请求中的品质列表并回成功。
func respondDragonballMerge(conn net.Conn, pkt *protocol.Packet) []int32 {
	r := protocol.NewReader(pkt.Payload)
	n, _ := r.ReadUint16()
	quals := make([]int32, 0, n)
	for i := 0; i < int(n); i++ {
		q, _ := r.ReadInt32()
		quals = append(quals, q)
	}
	_ = protocol.WritePacket(conn, protocol.BuildDragonballOneKeyMergeResultPacket(protocol.DragonballResultSuccess, nil))
	return quals
}

func TestDragonballOfferRoutine_StopsWhenTimesUp(t *testing.T) {
	var calls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDDragonballTmpInfo:
			_ = protocol.WritePacket(conn, protocol.BuildDragonballTmpInfoDetailResultPacket(
				protocol.DragonballTmpInfo{NormalCount: 20, RemainNormalCount: 20}))
		case protocol.ActionIDDragonballSacrifice:
			n := calls.Add(1)
			code := protocol.DragonballResultSuccess
			if n >= 3 {
				code = protocol.DragonballResultTimesUseUp
			}
			_ = protocol.WritePacket(conn, protocol.BuildDragonballSacrificeResultPacket(protocol.DragonballSacrificeResult{
				Result: code, PlayFlag: protocol.DragonballNotPlay,
			}))
		}
	})
	defer sess.Close()

	if err := NewDragonballOfferRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("dragonball offer: %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("expected 3 calls before TIMES_USE_UP, got %d", calls.Load())
	}
}

// TestDragonballOfferRoutine_ContinuesWhenPlayFlagNotPlay 验证 sacrifice 回包中的
// 第三个 UByte 是动画标记 (PLAY=14/NOT_PLAY=15)，不是剩余次数。旧实现把它当
// remain_times，NOT_PLAY(15) 或 0 会误判为次数耗尽，导致只上供一次。
func TestDragonballOfferRoutine_ContinuesWhenPlayFlagNotPlay(t *testing.T) {
	var calls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDDragonballTmpInfo:
			_ = protocol.WritePacket(conn, protocol.BuildDragonballTmpInfoDetailResultPacket(
				protocol.DragonballTmpInfo{NormalCount: 20, RemainNormalCount: 20}))
		case protocol.ActionIDDragonballSacrifice:
			n := calls.Add(1)
			code := protocol.DragonballResultSuccess
			if n >= 20 {
				code = protocol.DragonballResultTimesUseUp
			}
			_ = protocol.WritePacket(conn, protocol.BuildDragonballSacrificeResultPacket(protocol.DragonballSacrificeResult{
				Result: code, PlayFlag: protocol.DragonballNotPlay,
			}))
		}
	})
	defer sess.Close()

	if err := NewDragonballOfferRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("dragonball offer: %v", err)
	}
	if got := calls.Load(); got != 20 {
		t.Fatalf("应连续上供至 20 次, 实际 %d 次", got)
	}
}

func TestDragonballOfferRoutine_NoCoinStops(t *testing.T) {
	var calls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDDragonballTmpInfo:
			_ = protocol.WritePacket(conn, protocol.BuildDragonballTmpInfoDetailResultPacket(
				protocol.DragonballTmpInfo{NormalCount: 20, RemainNormalCount: 20}))
		case protocol.ActionIDDragonballSacrifice:
			calls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildDragonballSacrificeResultPacket(protocol.DragonballSacrificeResult{
				Result: protocol.DragonballResultNoEnoughCoin,
			}))
		}
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
		case protocol.ActionIDDragonballOneKeyMerge:
			respondDragonballMerge(conn, pkt)
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
	// 例程启动时先收取一次，临时格满后再次收取，因此共 2 次。
	if collectCalls.Load() != 2 {
		t.Fatalf("expected 2 collects (initial + retry), got %d", collectCalls.Load())
	}
}

func TestDragonballOfferRoutine_SellsInvalidTmpBalls(t *testing.T) {
	var sellCalls atomic.Int32
	var soldIDs []int32
	var mu sync.Mutex
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDDragonballTmpInfo:
			_ = protocol.WritePacket(conn, protocol.BuildDragonballTmpInfoResultPacket(
				[]protocol.DragonballTmpBall{{PlayerDBID: 2001, Level: 3}, {PlayerDBID: 2002, Level: 4}}))
		case protocol.ActionIDDragonballCollect:
			_ = protocol.WritePacket(conn, protocol.BuildDragonballCollectResultPacket(
				protocol.DragonballResultInvalidDragonball, nil))
		case protocol.ActionIDDragonballSell:
			sellCalls.Add(1)
			r := protocol.NewReader(pkt.Payload)
			n, _ := r.ReadUint16()
			mu.Lock()
			for i := 0; i < int(n); i++ {
				id, _ := r.ReadInt32()
				soldIDs = append(soldIDs, id)
			}
			mu.Unlock()
			_ = protocol.WritePacket(conn, protocol.BuildDragonballSellResultPacket(protocol.DragonballResultSuccess))
		case protocol.ActionIDDragonballSacrifice:
			_ = protocol.WritePacket(conn, protocol.BuildDragonballSacrificeResultPacket(
				protocol.DragonballSacrificeResult{Result: protocol.DragonballResultTimesUseUp}))
		}
	})
	defer sess.Close()

	if err := NewDragonballOfferRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("dragonball offer: %v", err)
	}
	if got := sellCalls.Load(); got != 1 {
		t.Fatalf("sell calls = %d, want 1", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(soldIDs) != 2 || soldIDs[0] != 2001 || soldIDs[1] != 2002 {
		t.Fatalf("sold ids = %v", soldIDs)
	}
}

func TestDragonballOfferRoutine_FunctionNoOpenCooldown(t *testing.T) {
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDDragonballTmpInfo:
			_ = protocol.WritePacket(conn, protocol.BuildDragonballTmpInfoDetailResultPacket(
				protocol.DragonballTmpInfo{NormalCount: 20, RemainNormalCount: 20}))
		case protocol.ActionIDDragonballSacrifice:
			_ = protocol.WritePacket(conn, protocol.BuildDragonballSacrificeResultPacket(protocol.DragonballSacrificeResult{
				Result: protocol.DragonballResultFunctionNoOpen, PlayFlag: protocol.DragonballNotPlay,
			}))
		}
	})
	defer sess.Close()

	err := NewDragonballOfferRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("功能未开放应返回 CooldownError, 实际: %v", err)
	}
}
