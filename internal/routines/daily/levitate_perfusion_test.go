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
		t.Fatal("灌注也无回包时应进入冷却")
	}
	// 面板 357/0 无回包不再阻断链路：仍要尝试一次 357/1 以结果码为准。
	if perfCalls.Load() != 1 {
		t.Fatalf("面板无回包时仍应尝试一次灌注, calls = %d", perfCalls.Load())
	}
}

// TestLevitatePerfusionRoutine_InfoFallsBackToMainRole 验证伙伴页签 (type=1) 的
// 357/0 被服务端静默丢弃时，改用主角页签 (type=0) 探测后仍会继续灌注。
func TestLevitatePerfusionRoutine_InfoFallsBackToMainRole(t *testing.T) {
	var perfCalls, mainInfoCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDLevitateInfo:
			if len(pkt.Payload) > 0 && pkt.Payload[0] == protocol.LevitateTypeMainRole {
				mainInfoCalls.Add(1)
				_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0}))
			}
			// type=1 伙伴页签：静默丢弃，不回包。
		case protocol.ActionIDLevitatePerfusion:
			perfCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildLevitatePerfusionResultPacket(
				protocol.LevitateUsedUp, nil))
		}
	})
	defer sess.Close()

	if err := NewLevitatePerfusionRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("levitate: %v", err)
	}
	if mainInfoCalls.Load() != 1 {
		t.Fatalf("应回退到主角页签探测, calls = %d", mainInfoCalls.Load())
	}
	if perfCalls.Load() != 1 {
		t.Fatalf("主角页签探活后应继续灌注, calls = %d", perfCalls.Load())
	}
}
