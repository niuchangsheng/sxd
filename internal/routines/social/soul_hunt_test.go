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

func soulHuntPanelPacket(result uint8, step int32) *protocol.Packet {
	var values [8]int32
	values[1] = step
	return protocol.BuildSoulHuntMainPanelResultPacket(result, values)
}

func TestSoulHuntRoutine_FightsUntilNoTimes(t *testing.T) {
	var panelCalls, fightCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDSoulHuntMainPanel:
			panelCalls.Add(1)
			_ = protocol.WritePacket(conn, soulHuntPanelPacket(protocol.SoulHuntSuccess, 3))
		case protocol.ActionIDSoulHuntChallengePanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.SoulHuntSuccess}))
		case protocol.ActionIDSoulHuntStartChallenge:
			n := fightCalls.Add(1)
			result := protocol.SoulHuntSuccess
			if n >= 3 {
				result = protocol.SoulHuntNoChallengeTime
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{result}))
		}
	})
	defer sess.Close()

	if err := NewSoulHuntRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("soul hunt: %v", err)
	}
	if fightCalls.Load() != 3 {
		t.Fatalf("应在第 3 次返回次数不足时停止, fight calls = %d", fightCalls.Load())
	}
	if panelCalls.Load() < 3 {
		t.Fatalf("每次胜利后应刷新主面板, panel calls = %d", panelCalls.Load())
	}
}

// TestSoulHuntRoutine_ChallengePanelOKFightDroppedBacksOff 验证关卡面板 669/3 正常回包、
// 但挑战 669/5 被静默丢弃时，连续丢包计数不会被面板成功清零，
// 连续 3 轮后挂起到每日重置，而不是每 10 分钟无限重试。
func TestSoulHuntRoutine_ChallengePanelOKFightDroppedBacksOff(t *testing.T) {
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDSoulHuntMainPanel:
			_ = protocol.WritePacket(conn, soulHuntPanelPacket(protocol.SoulHuntSuccess, 3))
		case protocol.ActionIDSoulHuntChallengePanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.SoulHuntSuccess}))
		case protocol.ActionIDSoulHuntStartChallenge:
			// 静默丢弃：不回包。
		}
	})
	defer sess.Close()
	scheduler.ResetSilentDrops(sess, "soul_hunt")

	routine := NewSoulHuntRoutine()
	var lastErr error
	for i := 0; i < 3; i++ {
		lastErr = routine.Execute(context.Background(), sess, nil)
		if lastErr == nil {
			t.Fatalf("第 %d 轮挑战无回包应返回冷却错误", i+1)
		}
	}
	if n := scheduler.SilentDropCount(sess, "soul_hunt"); n < 3 {
		t.Fatalf("关卡面板成功不应清零挑战丢包计数, got %d", n)
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
}

func TestSoulHuntRoutine_NotOpenCooldown(t *testing.T) {
	var fightCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDSoulHuntMainPanel:
			_ = protocol.WritePacket(conn, soulHuntPanelPacket(protocol.SoulHuntFuncNotOpen, 0))
		case protocol.ActionIDSoulHuntChallengePanel, protocol.ActionIDSoulHuntStartChallenge:
			fightCalls.Add(1)
		}
	})
	defer sess.Close()

	err := NewSoulHuntRoutine().Execute(context.Background(), sess, nil)
	if err == nil {
		t.Fatal("功能未开放时应返回 CooldownError")
	}
	if fightCalls.Load() != 0 {
		t.Fatalf("功能未开放时不得发起挑战, calls = %d", fightCalls.Load())
	}
}

func TestSoulHuntRoutine_CantChallengeStops(t *testing.T) {
	var fightCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDSoulHuntMainPanel:
			_ = protocol.WritePacket(conn, soulHuntPanelPacket(protocol.SoulHuntSuccess, 3))
		case protocol.ActionIDSoulHuntChallengePanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.SoulHuntCantChallenge}))
		case protocol.ActionIDSoulHuntStartChallenge:
			fightCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewSoulHuntRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("soul hunt: %v", err)
	}
	if fightCalls.Load() != 0 {
		t.Fatalf("关卡不可挑战时不得发起战斗, calls = %d", fightCalls.Load())
	}
}
