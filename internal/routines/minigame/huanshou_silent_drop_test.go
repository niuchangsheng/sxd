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

// TestHuanshouRoutine_SweepSilentDropBacksOff 验证山海幻兽入侵扫荡被服务端静默丢弃时
// 不会返回 nil（Cron 会等到次日才重试），而是按连续丢包退避：
// 前两次 10 分钟重试，第 3 次挂起至 min(次日 00:05, 6 小时)。
func TestHuanshouRoutine_SweepSilentDropBacksOff(t *testing.T) {
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDHuanshouPanel,
			protocol.ActionIDHuanshouInvadeChapter,
			protocol.ActionIDHuanshouInvadeChallenge:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		case protocol.ActionIDHuanshouInvadeSweep:
			// 静默丢弃：不回包，模拟当日次数已用完/资格不满足。
		default:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, nil))
		}
	})
	defer sess.Close()

	routine := NewHuanshouRoutine()
	var lastErr error
	for i := 0; i < 3; i++ {
		// 缩短父 ctx，避免等待 8 秒业务超时拖慢单测。
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		lastErr = routine.Execute(ctx, sess, nil)
		cancel()
		if lastErr == nil {
			t.Fatalf("第 %d 轮扫荡无回包应返回冷却错误", i+1)
		}
	}

	if got := scheduler.SilentDropCount(sess, "huanshou_invade"); got < 3 {
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
