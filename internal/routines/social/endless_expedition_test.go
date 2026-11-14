package social

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func endlessPanelPacket(result uint8, layer int32) *protocol.Packet {
	var values [11]int32
	values[1] = layer
	return protocol.BuildEndlessPanelResultPacket(result, values)
}

func TestEndlessExpeditionRoutine_ChallengesAndAdvances(t *testing.T) {
	var panelCalls, fightCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEndlessActivityState:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{0, 0, 0, 1, 0xf5}))
		case protocol.ActionIDEndlessPanel:
			n := panelCalls.Add(1)
			layer := int32(23)
			if n >= 2 {
				layer = 24
			}
			_ = protocol.WritePacket(conn, endlessPanelPacket(protocol.EndlessSuccess, layer))
		case protocol.ActionIDEndlessBanList, protocol.ActionIDEndlessMonsterTeamInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.EndlessSuccess, 0}))
		case protocol.ActionIDEndlessChallenge:
			fightCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.EndlessSuccess}))
		}
	})
	defer sess.Close()

	if err := NewEndlessExpeditionRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("endless expedition: %v", err)
	}
	// 23 层胜利后推进到 24；24 层持续未推进时继续挑战，直到单轮上限。
	if fightCalls.Load() != maxEndlessChallenges {
		t.Fatalf("应在同一层继续挑战至上限, fight calls = %d", fightCalls.Load())
	}
}

func TestEndlessExpeditionRoutine_NoPowerStops(t *testing.T) {
	var fightCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEndlessActivityState:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 1, 0xf5}))
		case protocol.ActionIDEndlessPanel:
			_ = protocol.WritePacket(conn, endlessPanelPacket(protocol.EndlessSuccess, 23))
		case protocol.ActionIDEndlessBanList, protocol.ActionIDEndlessMonsterTeamInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.EndlessSuccess, 0}))
		case protocol.ActionIDEndlessChallenge:
			fightCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.EndlessNoPower}))
		}
	})
	defer sess.Close()

	if err := NewEndlessExpeditionRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("endless expedition: %v", err)
	}
	if fightCalls.Load() != 1 {
		t.Fatalf("体力不足后应停止, fight calls = %d", fightCalls.Load())
	}
}

func TestEndlessExpeditionRoutine_NotOpenCooldown(t *testing.T) {
	var fightCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEndlessActivityState:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 0}))
		case protocol.ActionIDEndlessPanel:
			_ = protocol.WritePacket(conn, endlessPanelPacket(protocol.EndlessNotOpen, 0))
		case protocol.ActionIDEndlessChallenge:
			fightCalls.Add(1)
		}
	})
	defer sess.Close()

	err := NewEndlessExpeditionRoutine().Execute(context.Background(), sess, nil)
	if err == nil {
		t.Fatal("未开放时应返回 CooldownError")
	}
	if fightCalls.Load() != 0 {
		t.Fatalf("未开放时不得挑战, calls = %d", fightCalls.Load())
	}
}
