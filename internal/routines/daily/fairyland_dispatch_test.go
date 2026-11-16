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

func TestFairylandDispatchRoutine_NotOpenSkips(t *testing.T) {
	var taskCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDFairylandMainPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.FairylandFailed, 0}))
		case protocol.ActionIDFairylandTaskPanel:
			taskCalls.Add(1)
		}
	})
	defer sess.Close()

	err := NewFairylandDispatchRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if err != nil && !errors.As(err, &cd) {
		t.Fatalf("fairyland: %v", err)
	}
	if taskCalls.Load() != 0 {
		t.Fatalf("未开放时不应查询任务, calls = %d", taskCalls.Load())
	}
}

func TestFairylandDispatchRoutine_Dispatches(t *testing.T) {
	var dispatchCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDFairylandMainPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.FairylandSuccess, 0}))
		case protocol.ActionIDFairylandTaskPanel:
			w := protocol.NewWriter().WriteUint8(protocol.FairylandSuccess).WriteUint16(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDFairylandDispatch:
			dispatchCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.FairylandSuccess}))
		}
	})
	defer sess.Close()

	if err := NewFairylandDispatchRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("fairyland: %v", err)
	}
	if dispatchCalls.Load() == 0 {
		t.Fatal("开放时应派遣游历")
	}
}

func TestFairylandDispatchRoutine_OrderCompletedStops(t *testing.T) {
	var dispatchCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDFairylandMainPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.FairylandSuccess, 0}))
		case protocol.ActionIDFairylandTaskPanel:
			w := protocol.NewWriter().WriteUint8(protocol.FairylandSuccess).WriteUint16(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDFairylandDispatch:
			dispatchCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.FairylandOrderCompleted}))
		}
	})
	defer sess.Close()

	if err := NewFairylandDispatchRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("fairyland: %v", err)
	}
	if dispatchCalls.Load() != 1 {
		t.Fatalf("订单完成后应停止, dispatch calls = %d", dispatchCalls.Load())
	}
}

// TestFairylandDispatchRoutine_PrevActFallback 验证裸载荷被网关静默丢弃后，
// 使用服务端最后受理动作号作为显式锚点重试一次。
func TestFairylandDispatchRoutine_PrevActFallback(t *testing.T) {
	var prevActCalls, dispatchCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDFairylandMainPanel:
			if len(pkt.Payload) == 0 {
				return // 模拟网关静默丢弃裸载荷
			}
			prevActCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.FairylandSuccess, 0}))
		case protocol.ActionIDFairylandTaskPanel:
			w := protocol.NewWriter().WriteUint8(protocol.FairylandSuccess).WriteUint16(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDFairylandDispatch:
			dispatchCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.FairylandOrderCompleted}))
		}
	})
	defer sess.Close()

	if err := NewFairylandDispatchRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("fairyland: %v", err)
	}
	if prevActCalls.Load() != 1 {
		t.Fatalf("裸载荷丢弃后应使用 PrevAct 重试一次, 实际 %d", prevActCalls.Load())
	}
	if dispatchCalls.Load() != 1 {
		t.Fatalf("面板恢复后应进入游历派遣, dispatch calls = %d", dispatchCalls.Load())
	}
}

func TestNextFairylandWeeklyReset(t *testing.T) {
	loc := time.Local
	// 上限后按每天 05:05 低频探测（2026-11-16 00:26 实测周重置不是周一 00:00）。
	sun := time.Date(2026, 11, 15, 10, 0, 0, 0, loc)
	if got, want := nextFairylandWeeklyReset(sun), time.Date(2026, 11, 16, 5, 5, 0, 0, loc); !got.Equal(want) {
		t.Fatalf("周日应等到次日 05:05, got %v want %v", got, want)
	}
	monEarly := time.Date(2026, 11, 16, 0, 26, 0, 0, loc)
	if got, want := nextFairylandWeeklyReset(monEarly), time.Date(2026, 11, 16, 5, 5, 0, 0, loc); !got.Equal(want) {
		t.Fatalf("周一 00:26 应等到当天 05:05, got %v want %v", got, want)
	}
	monAfter := time.Date(2026, 11, 16, 6, 0, 0, 0, loc)
	if got, want := nextFairylandWeeklyReset(monAfter), time.Date(2026, 11, 17, 5, 5, 0, 0, loc); !got.Equal(want) {
		t.Fatalf("05:05 后应等到次日 05:05, got %v want %v", got, want)
	}
}
