package minigame

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func TestCultivationCardRoutine_ClaimsTaskBuysAndOpens(t *testing.T) {
	var awardCalls, buyCalls, openCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDCultivationTaskInfo:
			w := protocol.NewWriter().WriteInt32(4)
			if string(pkt.Payload) == string([]byte{0, 0, 0, 1}) {
				w.WriteUint16(1).WriteInt32(1).WriteInt32(6).WriteUint8(1)
			} else {
				w.WriteUint16(0)
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDCultivationTaskAward:
			awardCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.CultivationCardSuccess}))
		case protocol.ActionIDCultivationShopInfo:
			// shop 1 = 铜钱 1,000,000；left_number=1 表示仍可购买。
			w := protocol.NewWriter()
			w.WriteUint16(1).WriteInt32(1).WriteInt32(1).WriteInt32(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDCultivationBuyShop:
			buyCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.CultivationCardSuccess}))
		case protocol.ActionIDCultivationBagInfo:
			w := protocol.NewWriter()
			w.WriteUint16(1).WriteInt32(100).WriteInt32(2)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDCultivationOpenBag:
			openCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.CultivationCardSuccess, 0, 0, 0, 0}))
		}
	})
	defer sess.Close()

	if err := NewCultivationCardRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("cultivation card: %v", err)
	}
	if awardCalls.Load() != 1 || buyCalls.Load() != 1 || openCalls.Load() != 2 {
		t.Fatalf("calls award=%d buy=%d open=%d, want 1/1/2",
			awardCalls.Load(), buyCalls.Load(), openCalls.Load())
	}
}

func TestCultivationCardRoutine_BuysCheapestAffordableCoinGood(t *testing.T) {
	var buySlot atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDCultivationTaskInfo:
			w := protocol.NewWriter().WriteInt32(4).WriteUint16(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDCultivationShopInfo:
			// shop 4 = 元宝 20；shop 3 = 铜钱 5,000,000；shop 1 = 铜钱 1,000,000。
			// Mock 铜钱余额 1,000,000，只能买 shop 1，绝不能碰元宝档。
			w := protocol.NewWriter()
			w.WriteUint16(3)
			w.WriteInt32(4).WriteInt32(5)
			w.WriteInt32(3).WriteInt32(1)
			w.WriteInt32(1).WriteInt32(1)
			w.WriteInt32(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDCultivationBuyShop:
			r := protocol.NewReader(pkt.Payload)
			id, _ := r.ReadInt32()
			buySlot.Store(id)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.CultivationCardSuccess}))
		case protocol.ActionIDCultivationBagInfo:
			w := protocol.NewWriter().WriteUint16(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		}
	})
	defer sess.Close()

	if err := NewCultivationCardRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("cultivation card: %v", err)
	}
	if buySlot.Load() != 1 {
		t.Fatalf("应购买余额可负担的铜钱档位 1, 实际 %d", buySlot.Load())
	}
}

func TestCultivationCardRoutine_NeverBuysIngotGood(t *testing.T) {
	var buyCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDCultivationTaskInfo:
			w := protocol.NewWriter().WriteInt32(4).WriteUint16(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDCultivationShopInfo:
			// 仅有元宝档 (shop 4/5) 与买不起的铜钱档 (shop 3, 500 万)。
			w := protocol.NewWriter()
			w.WriteUint16(3)
			w.WriteInt32(4).WriteInt32(5)
			w.WriteInt32(5).WriteInt32(2)
			w.WriteInt32(3).WriteInt32(1)
			w.WriteInt32(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDCultivationBuyShop:
			buyCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.CultivationCardSuccess}))
		case protocol.ActionIDCultivationBagInfo:
			w := protocol.NewWriter().WriteUint16(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		}
	})
	defer sess.Close()

	if err := NewCultivationCardRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("cultivation card: %v", err)
	}
	if got := buyCalls.Load(); got != 0 {
		t.Fatalf("元宝/不可负担档位不应购买, buy calls = %d", got)
	}
}

func TestCultivationCardRoutine_BagFull(t *testing.T) {
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDCultivationTaskInfo:
			w := protocol.NewWriter().WriteInt32(4).WriteUint16(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDCultivationShopInfo:
			w := protocol.NewWriter().WriteUint16(0).WriteInt32(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDCultivationBagInfo:
			w := protocol.NewWriter().WriteUint16(1).WriteInt32(100).WriteInt32(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDCultivationOpenBag:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.CultivationCardItemPackFull}))
		}
	})
	defer sess.Close()

	err := NewCultivationCardRoutine().Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrBagFull) {
		t.Fatalf("err = %v, want ErrBagFull", err)
	}
}
