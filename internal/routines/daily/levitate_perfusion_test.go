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

func TestLevitatePerfusionRoutine_PerfusesUntilUsedUp(t *testing.T) {
	var perfCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDLevitateInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0}))
		case protocol.ActionIDLevitatePerfusion:
			n := perfCalls.Add(1)
			result := uint8(protocol.LevitateSuccess)
			if n >= 3 {
				result = protocol.LevitateUsedUp
			}
			_ = protocol.WritePacket(conn, protocol.BuildLevitatePerfusionResultPacket(result, nil))
		}
	})
	defer sess.Close()

	if err := NewLevitatePerfusionRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("levitate: %v", err)
	}
	if perfCalls.Load() != 3 {
		t.Fatalf("应在第 3 次 USED_UP 时停止, calls = %d", perfCalls.Load())
	}
}

func TestLevitatePerfusionRoutine_NotOpenCooldown(t *testing.T) {
	var perfCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDLevitateInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0}))
		case protocol.ActionIDLevitatePerfusion:
			perfCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildLevitatePerfusionResultPacket(protocol.LevitateFuncNoOpen, nil))
		}
	})
	defer sess.Close()

	err := NewLevitatePerfusionRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("功能未开放应返回 CooldownError, got %v", err)
	}
	if perfCalls.Load() != 1 {
		t.Fatalf("功能未开放后应停止, calls = %d", perfCalls.Load())
	}
}

func TestLevitatePerfusionRoutine_InfoNoResponse(t *testing.T) {
	var perfCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		if pkt.ActionID == protocol.ActionIDLevitatePerfusion {
			perfCalls.Add(1)
		}
	})
	defer sess.Close()

	err := NewLevitatePerfusionRoutine().Execute(context.Background(), sess, nil)
	if err == nil {
		t.Fatal("信息无回包应进入冷却")
	}
	if perfCalls.Load() != 0 {
		t.Fatalf("信息无回包时不得灌注, calls = %d", perfCalls.Load())
	}
}
