package daily

import (
	"context"
	"encoding/binary"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

// buildSystemAwardListPayload 构造 2/6 回包，含两条奖励，覆盖明细与空明细两种形态。
func buildSystemAwardListPayload() []byte {
	w := protocol.NewWriter()
	w.WriteUint16(2)

	// award 1: id=0x0A，3 字节文本，2 条明细。
	w.WriteUint32(0x0000000A)
	w.WriteInt32(1)
	w.WriteUint16(3)
	w.WriteBytes([]byte("abc"))
	w.WriteInt32(2)
	w.WriteUint8(3)
	w.WriteInt32(100)
	w.WriteInt32(0)
	w.WriteUint8(4)
	w.WriteInt32(200)
	w.WriteInt32(5)

	// award 2: id=0x0B，无明细。
	w.WriteUint32(0x0000000B)
	w.WriteInt32(2)
	w.WriteUint16(2)
	w.WriteBytes([]byte("xy"))
	w.WriteInt32(0)
	return w.Bytes()
}

// TestClaimSystemAwards_ClaimsAllAndHandlesFailureCode 验证 2/6 -> 2/29 全流程：
// 成功码(20)计入领取，失败码(1)只记录不中断，且不会无限重试。
func TestClaimSystemAwards_ClaimsAllAndHandlesFailureCode(t *testing.T) {
	var listCalls, claimCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDSystemAwardList:
			listCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, buildSystemAwardListPayload()))
		case protocol.ActionIDItemClaimAward:
			n := claimCalls.Add(1)
			if len(pkt.Payload) < 4 {
				t.Errorf("2/29 载荷过短: %d", len(pkt.Payload))
			}
			id := binary.BigEndian.Uint32(pkt.Payload[:4])
			code := byte(protocol.ItemSuccess)
			if id == 0x0000000B {
				code = 1 // 模拟“已领取/条件不满足”失败码
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{code}))
			if n > 2 {
				t.Errorf("失败码后出现多余重试: %d", n)
			}
		}
	})
	defer sess.Close()

	if err := claimSystemAwards(context.Background(), sess); err != nil {
		t.Fatalf("claimSystemAwards: %v", err)
	}
	if listCalls.Load() != 1 {
		t.Fatalf("2/6 列表查询次数 = %d, want 1", listCalls.Load())
	}
	if claimCalls.Load() != 2 {
		t.Fatalf("2/29 领取次数 = %d, want 2", claimCalls.Load())
	}
}

// TestClaimSystemAwards_NoResponseStops 验证列表无回包时快速返回错误，不继续领取。
func TestClaimSystemAwards_NoResponseStops(t *testing.T) {
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		// 故意不回包。
	})
	defer sess.Close()

	if err := claimSystemAwards(context.Background(), sess); err == nil {
		t.Fatal("列表无回包时应返回错误")
	}
}

// TestClaimSystemAwards_SkipsDroppedAward 验证单条领取被静默丢弃时：
// 回退链重试一次，仍失败则跳过该奖励并继续领取后续奖励（不中断整批）。
func TestClaimSystemAwards_SkipsDroppedAward(t *testing.T) {
	var listCalls, firstAwardCalls, secondAwardCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDSystemAwardList:
			listCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, buildSystemAwardListPayload()))
		case protocol.ActionIDItemClaimAward:
			id := binary.BigEndian.Uint32(pkt.Payload[:4])
			if id == 0x0000000A {
				firstAwardCalls.Add(1)
				// 故意不回包，模拟静默丢弃。
				return
			}
			secondAwardCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.ItemSuccess}))
		}
	})
	defer sess.Close()

	if err := claimSystemAwards(context.Background(), sess); err != nil {
		t.Fatalf("claimSystemAwards: %v", err)
	}
	if firstAwardCalls.Load() != 2 {
		t.Fatalf("丢包奖励应重试 1 次 (共 2 次), 实际 %d", firstAwardCalls.Load())
	}
	if secondAwardCalls.Load() != 1 {
		t.Fatalf("后续奖励应继续领取 1 次, 实际 %d", secondAwardCalls.Load())
	}
	if listCalls.Load() != 1 {
		t.Fatalf("列表查询次数 = %d, want 1", listCalls.Load())
	}
}
