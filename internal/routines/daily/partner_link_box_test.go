package daily

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func TestPartnerLinkBoxRoutine_BuysCoinBoxUntilLimit(t *testing.T) {
	var buyCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGameAssistantInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: DailyTaskPartnerLinkBox, CurTimes: 0, MaxTimes: 9, PerActiveAdd: 5},
			}))
		case protocol.ActionIDPartnerLinkShop:
			_ = protocol.WritePacket(conn, protocol.BuildPartnerLinkShopResultPacket([]protocol.PartnerLinkBox{
				{BoxType: 7, Times: 5, MoneyType: protocol.PartnerLinkMoneyIngot},
				{BoxType: 6, Times: 3, MoneyType: protocol.PartnerLinkMoneyDaoYuan, MoneyNum: 200},
			}))
		case protocol.ActionIDPartnerLinkBuy:
			buyCalls.Add(1)
			code := protocol.PartnerLinkResultSuccess
			if len(pkt.Payload) > 0 && pkt.Payload[0] == 7 {
				code = protocol.PartnerLinkResultNotBuyCount
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDPartnerLinkBuy, []byte{code}))
		}
	})
	defer sess.Close()

	if err := NewPartnerLinkBoxRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("partner link box: %v", err)
	}
	if got := buyCalls.Load(); got != 3 {
		t.Fatalf("buy calls = %d, want 3", got)
	}
}

func TestPartnerLinkBoxRoutine_SkipsIngotOnly(t *testing.T) {
	var buyCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGameAssistantInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: DailyTaskPartnerLinkBox, CurTimes: 0, MaxTimes: 9, PerActiveAdd: 5},
			}))
		case protocol.ActionIDPartnerLinkShop:
			_ = protocol.WritePacket(conn, protocol.BuildPartnerLinkShopResultPacket([]protocol.PartnerLinkBox{
				{BoxType: 7, Times: 5, MoneyType: protocol.PartnerLinkMoneyIngot},
			}))
		case protocol.ActionIDPartnerLinkBuy:
			buyCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewPartnerLinkBoxRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("partner link box: %v", err)
	}
	if got := buyCalls.Load(); got != 0 {
		t.Fatalf("buy calls = %d, want 0", got)
	}
}

func TestPartnerLinkBoxRoutine_SkipsWhenTaskComplete(t *testing.T) {
	var shopCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGameAssistantInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: DailyTaskPartnerLinkBox, CurTimes: 9, MaxTimes: 9, PerActiveAdd: 5},
			}))
		case protocol.ActionIDPartnerLinkShop:
			shopCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewPartnerLinkBoxRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("partner link box: %v", err)
	}
	if got := shopCalls.Load(); got != 0 {
		t.Fatalf("shop calls = %d, want 0", got)
	}
}

func TestPartnerLinkBoxRoutine_ShopSilentDropCooldown(t *testing.T) {
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		if pkt.ActionID == protocol.ActionIDGameAssistantInfo {
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: DailyTaskPartnerLinkBox, CurTimes: 0, MaxTimes: 9, PerActiveAdd: 5},
			}))
		}
	})
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := NewPartnerLinkBoxRoutine().Execute(ctx, sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("商店无回包应返回 CooldownError, 实际: %v", err)
	}
}
