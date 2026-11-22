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

func TestEraRebirthAttrGiftRoutine_UsesItems(t *testing.T) {
	var useCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEraRebirthAttrSeason:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 1}))
		case protocol.ActionIDEraRebirthAttrPanel:
			_ = protocol.WritePacket(conn, protocol.BuildEraRebirthAttrPanelResultPacket(1))
		case protocol.ActionIDEraRebirthAttrResourcePackage:
			_ = protocol.WritePacket(conn, protocol.BuildEraRebirthAttrTimeItemsResultPacket(
				[]protocol.EraRebirthTimeItem{{ItemID: 10754, Num: 1}, {ItemID: 11000, Num: 9}}, 0))
		case protocol.ActionIDEraRebirthAttrUseTimeItem:
			useCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildEraRebirthAttrUseResultPacket(
				protocol.EraRebirthAttrSuccess, 0, 0))
		}
	})
	defer sess.Close()

	if err := NewEraRebirthAttrGiftRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("era rebirth attr: %v", err)
	}
	if useCalls.Load() != 2 {
		t.Fatalf("应使用两个时间道具, calls = %d", useCalls.Load())
	}
}

func TestEraRebirthAttrGiftRoutine_NotEnoughItemSkips(t *testing.T) {
	var useCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEraRebirthAttrSeason:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 1}))
		case protocol.ActionIDEraRebirthAttrPanel:
			_ = protocol.WritePacket(conn, protocol.BuildEraRebirthAttrPanelResultPacket(1))
		case protocol.ActionIDEraRebirthAttrResourcePackage:
			_ = protocol.WritePacket(conn, protocol.BuildEraRebirthAttrTimeItemsResultPacket(
				[]protocol.EraRebirthTimeItem{{ItemID: 10754, Num: 1}, {ItemID: 11000, Num: 9}}, 0))
		case protocol.ActionIDEraRebirthAttrUseTimeItem:
			n := useCalls.Add(1)
			result := protocol.EraRebirthAttrNotEnoughItem
			if n > 1 {
				result = protocol.EraRebirthAttrSuccess
			}
			_ = protocol.WritePacket(conn, protocol.BuildEraRebirthAttrUseResultPacket(result, 0, 0))
		}
	})
	defer sess.Close()

	if err := NewEraRebirthAttrGiftRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("era rebirth attr: %v", err)
	}
	if useCalls.Load() != 2 {
		t.Fatalf("道具不足应跳过并继续下一个, calls = %d", useCalls.Load())
	}
}

func TestEraRebirthAttrGiftRoutine_MissionLimitStops(t *testing.T) {
	var useCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEraRebirthAttrSeason:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 1}))
		case protocol.ActionIDEraRebirthAttrPanel:
			_ = protocol.WritePacket(conn, protocol.BuildEraRebirthAttrPanelResultPacket(1))
		case protocol.ActionIDEraRebirthAttrResourcePackage:
			_ = protocol.WritePacket(conn, protocol.BuildEraRebirthAttrTimeItemsResultPacket(
				[]protocol.EraRebirthTimeItem{{ItemID: 10754, Num: 1}, {ItemID: 11000, Num: 9}}, 0))
		case protocol.ActionIDEraRebirthAttrUseTimeItem:
			useCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildEraRebirthAttrUseResultPacket(
				protocol.EraRebirthAttrMissionLimit, 0, 0))
		}
	})
	defer sess.Close()

	if err := NewEraRebirthAttrGiftRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("era rebirth attr: %v", err)
	}
	if useCalls.Load() != 1 {
		t.Fatalf("次数上限后应停止, calls = %d", useCalls.Load())
	}
}

// TestEraRebirthAttrGiftRoutine_AutoUseDisabledSkips 验证 851/1 的
// is_auto_use_time_item=0 时不发送 851/5，而是按 2 小时冷却等待开关变化。
func TestEraRebirthAttrGiftRoutine_AutoUseDisabledSkips(t *testing.T) {
	var useCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEraRebirthAttrSeason:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 1}))
		case protocol.ActionIDEraRebirthAttrPanel:
			_ = protocol.WritePacket(conn, protocol.BuildEraRebirthAttrPanelResultPacket(0))
		case protocol.ActionIDEraRebirthAttrResourcePackage:
			_ = protocol.WritePacket(conn, protocol.BuildEraRebirthAttrTimeItemsResultPacket(
				[]protocol.EraRebirthTimeItem{{ItemID: 10754, Num: 1}}, 0))
		case protocol.ActionIDEraRebirthAttrUseTimeItem:
			useCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildEraRebirthAttrUseResultPacket(
				protocol.EraRebirthAttrSuccess, 0, 0))
		}
	})
	defer sess.Close()

	err := NewEraRebirthAttrGiftRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("自动使用关闭时应返回 CooldownError, got %v", err)
	}
	if useCalls.Load() != 0 {
		t.Fatalf("自动使用关闭时不得发送 851/5, calls = %d", useCalls.Load())
	}
}
