package minigame

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestSoulHuntRoutine_ChallengesFreeLevel(t *testing.T) {
	var startCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDSoulHuntState:
			w := protocol.NewWriter().WriteInt32(111)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDSoulHuntMainPanel:
			w := protocol.NewWriter()
			w.WriteUint8(protocol.SoulHuntSuccess)
			for i := 0; i < 8; i++ {
				w.WriteInt32(int32(i))
			}
			w.WriteUint16(0)
			w.WriteInt32(1)
			w.WriteInt32(1)
			w.WriteInt32(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDSoulHuntChallengePanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.SoulHuntSuccess, 0, 0, 0, 0}))
		case protocol.ActionIDSoulHuntStartChallenge:
			startCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.SoulHuntSuccess}))
		}
	})
	defer sess.Close()

	if err := NewSoulHuntRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("soul hunt: %v", err)
	}
	if startCalls.Load() == 0 {
		t.Fatal("start_challenge 未被调用")
	}
}

func TestSoulHuntRoutine_CantChallengeStops(t *testing.T) {
	var startCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDSoulHuntState:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 1}))
		case protocol.ActionIDSoulHuntMainPanel:
			w := protocol.NewWriter()
			w.WriteUint8(protocol.SoulHuntSuccess)
			for i := 0; i < 8; i++ {
				w.WriteInt32(0)
			}
			w.WriteUint16(0)
			w.WriteInt32(0)
			w.WriteInt32(0)
			w.WriteInt32(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDSoulHuntChallengePanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.SoulHuntSuccess, 0, 0, 0, 0}))
		case protocol.ActionIDSoulHuntStartChallenge:
			startCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.SoulHuntCantChallenge}))
		}
	})
	defer sess.Close()

	if err := NewSoulHuntRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("soul hunt: %v", err)
	}
	if startCalls.Load() != 1 {
		t.Fatalf("CANT_CHALLENGE 后应停止，start calls = %d", startCalls.Load())
	}
}

func TestSoulHuntRoutine_NoIngotStops(t *testing.T) {
	var startCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDSoulHuntState:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 0}))
		case protocol.ActionIDSoulHuntMainPanel:
			w := protocol.NewWriter()
			w.WriteUint8(protocol.SoulHuntSuccess)
			for i := 0; i < 8; i++ {
				w.WriteInt32(0)
			}
			w.WriteUint16(0)
			w.WriteInt32(0)
			w.WriteInt32(0)
			w.WriteInt32(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDSoulHuntChallengePanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.SoulHuntSuccess, 0, 0, 0, 0}))
		case protocol.ActionIDSoulHuntStartChallenge:
			startCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.SoulHuntNoIngot}))
		}
	})
	defer sess.Close()

	if err := NewSoulHuntRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("soul hunt: %v", err)
	}
	if startCalls.Load() != 1 {
		t.Fatalf("NO_INGOT 后应停止，start calls = %d", startCalls.Load())
	}
}