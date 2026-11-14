package social

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

// 缩短步骤间隔，避免单测等待 7 秒。
func shortenTowerRoomGaps(t *testing.T) {
	t.Helper()
	oldStep, oldEnter, oldNow := towerRoomStepGap, towerRoomEnterGap, towerRoomNow
	towerRoomStepGap, towerRoomEnterGap = 0, 0
	// 固定为开放日（2026-11-12 周四）避免测试受当天星期影响。
	towerRoomNow = func() time.Time { return time.Date(2026, 11, 12, 20, 0, 0, 0, time.Local) }
	t.Cleanup(func() {
		towerRoomStepGap, towerRoomEnterGap = oldStep, oldEnter
		towerRoomNow = oldNow
	})
}

// 正常路径：创建房间 -> 解除封印 -> 进入战斗，三步都应以结果码 4 通过。
func TestTowerRoomRoutine_HappyPath(t *testing.T) {
	shortenTowerRoomGaps(t)

	var createCalls, unsealCalls, enterCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDTowerRoomOpen:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{4}))
		case protocol.ActionIDTowerRoomCreate:
			createCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{4, 0x00, 0x00, 0x07, 0xd1}))
		case protocol.ActionIDTowerRoomUnseal:
			unsealCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{4}))
		case protocol.ActionIDTowerRoomNext:
			enterCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{4}))
		}
	})
	defer sess.Close()

	if err := NewTowerRoomRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("tower room routine: %v", err)
	}
	if createCalls.Load() != 1 || unsealCalls.Load() != 1 || enterCalls.Load() != 1 {
		t.Fatalf("调用次数 create=%d unseal=%d enter=%d, 期望各 1 次",
			createCalls.Load(), unsealCalls.Load(), enterCalls.Load())
	}
}

// 创建房间失败（结果码非 4）时不得继续解除封印。
func TestTowerRoomRoutine_CreateFailureStops(t *testing.T) {
	shortenTowerRoomGaps(t)

	var unsealCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDTowerRoomOpen:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{4}))
		case protocol.ActionIDTowerRoomCreate:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{0, 0x00, 0x00, 0x07, 0xd1}))
		case protocol.ActionIDTowerRoomUnseal:
			unsealCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{4}))
		}
	})
	defer sess.Close()

	if err := NewTowerRoomRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("tower room routine: %v", err)
	}
	if unsealCalls.Load() != 0 {
		t.Fatalf("创建失败后不应解除封印, unseal=%d", unsealCalls.Load())
	}
}

// 解除封印失败时不得进入战斗。
func TestTowerRoomRoutine_UnsealFailureStops(t *testing.T) {
	shortenTowerRoomGaps(t)

	var enterCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDTowerRoomOpen:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{4}))
		case protocol.ActionIDTowerRoomCreate:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{4, 0x00, 0x00, 0x07, 0xd1}))
		case protocol.ActionIDTowerRoomUnseal:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{1}))
		case protocol.ActionIDTowerRoomNext:
			enterCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{4}))
		}
	})
	defer sess.Close()

	if err := NewTowerRoomRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("tower room routine: %v", err)
	}
	if enterCalls.Load() != 0 {
		t.Fatalf("解除封印失败后不应进入战斗, enter=%d", enterCalls.Load())
	}
}

// 非活动时间（341/1 与 341/4 均返回 5）应进入 1 小时冷却并停止后续动作。
func TestTowerRoomRoutine_NotOpenCooldown(t *testing.T) {
	shortenTowerRoomGaps(t)

	var unsealCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDTowerRoomOpen:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.TowerRoomResultNotOpen}))
		case protocol.ActionIDTowerRoomCreate:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.TowerRoomResultNotOpen, 0, 0, 0, 0}))
		case protocol.ActionIDTowerRoomUnseal:
			unsealCalls.Add(1)
		}
	})
	defer sess.Close()

	err := NewTowerRoomRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("未开放应返回冷却错误, got %v", err)
	}
	if unsealCalls.Load() != 0 {
		t.Fatalf("未开放时不应解除封印, unseal=%d", unsealCalls.Load())
	}
}

// 非开放日（周三）应直接进入冷却，不发送任何 341 请求。
func TestTowerRoomRoutine_ClosedWeekdaySkips(t *testing.T) {
	oldNow := towerRoomNow
	towerRoomNow = func() time.Time { return time.Date(2026, 11, 11, 20, 0, 0, 0, time.Local) }
	t.Cleanup(func() { towerRoomNow = oldNow })

	var calls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		calls.Add(1)
	})
	defer sess.Close()

	err := NewTowerRoomRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("非开放日应返回冷却错误, got %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("非开放日不应发送请求, calls=%d", calls.Load())
	}
}

// TestTowerRoom_OpenWindow 验证开放时段：周二/四/六 14:00–23:59。
func TestTowerRoom_OpenWindow(t *testing.T) {
	satBefore := time.Date(2026, 11, 14, 13, 59, 0, 0, time.Local)
	satOpen := time.Date(2026, 11, 14, 14, 0, 0, 0, time.Local)
	satLate := time.Date(2026, 11, 14, 23, 59, 0, 0, time.Local)
	fri := time.Date(2026, 11, 13, 15, 0, 0, 0, time.Local)

	if towerRoomOpenNow(satBefore) {
		t.Fatal("周六 13:59 不应视为开放")
	}
	if !towerRoomOpenNow(satOpen) || !towerRoomOpenNow(satLate) {
		t.Fatal("周六 14:00 与 23:59 应视为开放")
	}
	if towerRoomOpenNow(fri) {
		t.Fatal("周五不应视为开放")
	}

	if d := towerRoomNextOpenDelay(satBefore); d < 59*time.Second || d > 61*time.Second {
		t.Fatalf("周六 13:59 距离开放应约 1 分钟（60s）, got %v", d)
	}
	want := time.Date(2026, 11, 14, 14, 0, 0, 0, time.Local).Sub(fri)
	if d := towerRoomNextOpenDelay(fri); d != want {
		t.Fatalf("周五 15:00 应等待到周六 14:00, got %v want %v", d, want)
	}
}
