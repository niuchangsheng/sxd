package minigame

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// TestScratchCardRoutine_ShaveSilentDropBacksOff 验证刮卡中途被服务端静默丢弃时
// 不再 break + return nil（剩余免费次数被当成已完成），而是按连续丢包退避。
func TestScratchCardRoutine_ShaveSilentDropBacksOff(t *testing.T) {
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDScratchCardInfo:
			_ = protocol.WritePacket(conn, protocol.BuildScratchCardInfoResultPacket(protocol.ScratchCardInfo{
				InActivity: 1,
				CardTimes:  5,
			}))
		case protocol.ActionIDScratchCardShave:
			// 静默丢弃：不回包。
		default:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, nil))
		}
	})
	defer sess.Close()

	routine := NewScratchCardRoutine()
	var lastErr error
	for i := 0; i < 3; i++ {
		// 缩短父 ctx，避免等待 8 秒业务超时拖慢单测。
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		lastErr = routine.Execute(ctx, sess, nil)
		cancel()
		if lastErr == nil {
			t.Fatalf("第 %d 轮刮卡无回包应返回冷却错误", i+1)
		}
	}

	if got := scheduler.SilentDropCount(sess, "scratch_card_shave"); got < 3 {
		t.Fatalf("连续丢包计数应达到 3，实际 %d", got)
	}
	var cd *scheduler.CooldownError
	if !errors.As(lastErr, &cd) {
		t.Fatalf("达到阈值后应返回 CooldownError, got %v", lastErr)
	}
	exp := scheduler.NextDailyReset(time.Now())
	if cap := time.Now().Add(6 * time.Hour); exp.After(cap) {
		exp = cap
	}
	if d := cd.Until.Sub(exp); d > time.Minute || d < -time.Minute {
		t.Fatalf("熔断唤醒时间 = %s, want %s", cd.Until, exp)
	}
}

// TestScratchCardRoutine_NotOpenWakesHourly 验证活动未开放时不再 return nil，
// 而是 1 小时后重新探测（活动当天稍后开启仍能自动完成）。
func TestScratchCardRoutine_NotOpenWakesHourly(t *testing.T) {
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		if pkt.ActionID == protocol.ActionIDScratchCardInfo {
			_ = protocol.WritePacket(conn, protocol.BuildScratchCardInfoResultPacket(protocol.ScratchCardInfo{
				InActivity: 0,
				CardTimes:  0,
			}))
		}
	})
	defer sess.Close()

	err := NewScratchCardRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("活动未开放应返回 CooldownError, got %v", err)
	}
	if d := time.Until(cd.Until); d < 55*time.Minute || d > 65*time.Minute {
		t.Fatalf("活动未开放唤醒时间应约为 1 小时，实际 %s", d)
	}
}

// TestScratchCardRoutine_TimesExhaustedWaitsNextDay 验证活动已开放但免费次数为 0
// 时返回 ErrAttemptsExhausted（挂起至次日），而不是每小时空转。
func TestScratchCardRoutine_TimesExhaustedWaitsNextDay(t *testing.T) {
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		if pkt.ActionID == protocol.ActionIDScratchCardInfo {
			_ = protocol.WritePacket(conn, protocol.BuildScratchCardInfoResultPacket(protocol.ScratchCardInfo{
				InActivity: 1,
				CardTimes:  0,
			}))
		}
	})
	defer sess.Close()

	err := NewScratchCardRoutine().Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrAttemptsExhausted) {
		t.Fatalf("次数耗尽应返回 ErrAttemptsExhausted, got %v", err)
	}
}
