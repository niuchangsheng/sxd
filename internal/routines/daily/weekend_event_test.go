package daily

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func TestWeekendEventRoutine_ClaimsGiftAndFruit(t *testing.T) {
	var giftCalls, drawCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDWeekendGiftPanel:
			w := protocol.NewWriter()
			w.WriteInt32(1).WriteUint16(1)
			// day=6, received=0, can_receive=1, can_get_back=0
			w.WriteInt32(6).WriteInt32(0).WriteInt32(1).WriteInt32(0)
			w.WriteInt32(6)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDWeekendGiftAward:
			n := giftCalls.Add(1)
			code := protocol.WeekendGiftResultSuccess
			if n > 1 {
				code = protocol.WeekendGiftResultHasGet
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{code}))
		case protocol.ActionIDSundayFruitActive:
			w := protocol.NewWriter()
			w.WriteUint8(protocol.SundayFruitResultYes)
			w.WriteInt32(0)
			w.WriteInt32(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDSundayFruitInfo:
			w := protocol.NewWriter()
			w.WriteInt32(2)
			w.WriteInt32(0)
			w.WriteInt32(0)
			w.WriteInt32(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDSundayFruitDraw:
			n := drawCalls.Add(1)
			code := protocol.SundayFruitResultSuccess
			if n > 1 {
				code = protocol.SundayFruitResultTimeUsed
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{code}))
		}
	})
	defer sess.Close()

	if err := NewWeekendEventRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("weekend: %v", err)
	}
	if got := giftCalls.Load(); got != 1 {
		t.Fatalf("周末礼应按面板 can_receive 领取 1 个日期, 实际 %d 次", got)
	}
	if got := drawCalls.Load(); got != 2 {
		t.Fatalf("水果机应抽 2 次 (成功+次数用尽), 实际 %d 次", got)
	}
}

func TestWeekendEventRoutine_SkipsClosedFruit(t *testing.T) {
	var drawCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDWeekendGiftPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		case protocol.ActionIDWeekendGiftAward:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.WeekendGiftResultNotOpen}))
		case protocol.ActionIDSundayFruitActive:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.SundayFruitResultNo}))
		case protocol.ActionIDSundayFruitDraw:
			drawCalls.Add(1)
		}
	})
	defer sess.Close()

	err := NewWeekendEventRoutine().Execute(context.Background(), sess, nil)
	var cdErr *scheduler.CooldownError
	if !errors.As(err, &cdErr) {
		t.Fatalf("水果机未开启应返回 CooldownError, 实际: %v", err)
	}
	if got := drawCalls.Load(); got != 0 {
		t.Fatalf("水果机未开启时不应抽奖, 实际 %d 次", got)
	}
}

func TestWeekendEventRoutine_SkipsNonReceivableDays(t *testing.T) {
	var giftCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDWeekendGiftPanel:
			w := protocol.NewWriter()
			w.WriteInt32(0).WriteUint16(2)
			// 第 1 天已领过；第 2 天未到可领日期。
			w.WriteInt32(1).WriteInt32(1).WriteInt32(0).WriteInt32(1)
			w.WriteInt32(2).WriteInt32(0).WriteInt32(0).WriteInt32(0)
			w.WriteInt32(2)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDWeekendGiftAward:
			giftCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.WeekendGiftResultSuccess}))
		case protocol.ActionIDSundayFruitActive:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.SundayFruitResultNo}))
		}
	})
	defer sess.Close()

	err := NewWeekendEventRoutine().Execute(context.Background(), sess, nil)
	var cdErr *scheduler.CooldownError
	if !errors.As(err, &cdErr) {
		t.Fatalf("水果机未开启应返回 CooldownError, 实际: %v", err)
	}
	if got := giftCalls.Load(); got != 0 {
		t.Fatalf("can_receive=0 的日期不应领取, 实际 %d 次", got)
	}
}
