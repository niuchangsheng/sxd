package social

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
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
