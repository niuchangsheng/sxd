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

// TestLevitatePerfusionRoutine_InfoOKButPerfusionDroppedBacksOff 验证面板 357/0 正常回包、
// 但灌注 357/1 被服务端静默丢弃时，连续丢包计数不会被面板成功清零：
// 达到阈值后必须挂起到每日重置，而不是每 30 分钟无限空转。
func TestLevitatePerfusionRoutine_InfoOKButPerfusionDroppedBacksOff(t *testing.T) {
	var perfCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDLevitateInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0}))
		case protocol.ActionIDLevitatePerfusion:
			perfCalls.Add(1)
			// 静默丢弃：不回包。
		}
	})
	defer sess.Close()

	routine := NewLevitatePerfusionRoutine()
	var lastErr error
	for i := 0; i < 3; i++ {
		lastErr = routine.Execute(context.Background(), sess, nil)
		if lastErr == nil {
			t.Fatalf("第 %d 轮灌注无回包应返回冷却错误", i+1)
		}
	}
	if got := scheduler.SilentDropCount(sess, "levitate_perfusion"); got < 3 {
		t.Fatalf("面板成功不应清零灌注丢包计数, got %d", got)
	}
	var cd *scheduler.CooldownError
	if !errors.As(lastErr, &cd) {
		t.Fatalf("达到阈值后应返回 CooldownError, got %v", lastErr)
	}
	// HandleSilentDrop 到阈值后挂起至 min(次日 00:05, 6 小时)。
	exp := scheduler.NextDailyReset(time.Now())
	if cap := time.Now().Add(6 * time.Hour); exp.After(cap) {
		exp = cap
	}
	if d := cd.Until.Sub(exp); d > time.Minute || d < -time.Minute {
		t.Fatalf("熔断唤醒时间 = %s, want %s", cd.Until, exp)
	}
	// 每轮 Execute 内部可能因 PrevAct 重同步重放一次 357/1，
	// 因此只校验没有出现额外轮次的请求（3 轮 × 最多 2 次）。
	if got := perfCalls.Load(); got < 3 || got > 6 {
		t.Fatalf("达到阈值后不应继续无限请求, perf calls = %d", got)
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
