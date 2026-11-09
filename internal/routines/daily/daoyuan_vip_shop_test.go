package daily

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func TestDaoyuanVipShopRoutine_ListSilentDropCooldown(t *testing.T) {
	sess := newDailyMockSession(t, func(net.Conn, *protocol.Packet) {
		// 列表查询模拟服务端静默丢包：不回包。
	})
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := NewDaoyuanVipShopRoutine().Execute(ctx, sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("列表无回包应返回 CooldownError, 实际: %v", err)
	}
}

func TestDaoyuanVipShopRoutine_PackFullStops(t *testing.T) {
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDDaoyuanVipShopList:
			if len(pkt.Payload) != 1 || pkt.Payload[0] != protocol.DaoyuanVipShopTabFive {
				t.Errorf("VIP5 请求载荷 = %v, want [%d] (不能附带 PrevAct)",
					pkt.Payload, protocol.DaoyuanVipShopTabFive)
			}
			w := protocol.NewWriter()
			w.WriteUint16(1)
			w.WriteInt8(1)
			w.WriteInt32(itemShenJiaJingShi)
			w.WriteInt16(100)
			w.WriteInt16(2)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDDaoyuanVipShopList, w.Bytes()))
		case protocol.ActionIDDaoyuanShopBuy:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDDaoyuanShopBuy,
				[]byte{protocol.DaoyuanShopResultPackFull, 0, 0}))
		}
	})
	defer sess.Close()

	err := NewDaoyuanVipShopRoutine().Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrBagFull) {
		t.Fatalf("背包已满应返回 ErrBagFull, 实际: %v", err)
	}
}
