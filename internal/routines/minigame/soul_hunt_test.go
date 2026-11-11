package minigame

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
				v := int32(0)
				if i == 1 {
					v = 1 // 当前猎妖阶段
				}
				w.WriteInt32(v)
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
				v := int32(0)
				if i == 1 {
					v = 1 // 当前猎妖阶段
				}
				w.WriteInt32(v)
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

func TestSoulHuntRoutine_StateSilentDropCooldown(t *testing.T) {
	sess := newMinigameMockSession(t, func(net.Conn, *protocol.Packet) {})
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := NewSoulHuntRoutine().Execute(ctx, sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("灵域猎妖状态无回包应返回 CooldownError, 实际: %v", err)
	}
}

func TestSoulHuntRoutine_NotChallengeablePanelCooldown(t *testing.T) {
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
			// challenge_panel 返回非成功码时直接收尾，不再调用 start_challenge。
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.SoulHuntCantChallenge, 0, 0, 0, 0}))
		case protocol.ActionIDSoulHuntStartChallenge:
			startCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewSoulHuntRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("例程不应报错: %v", err)
	}
	if startCalls.Load() != 0 {
		t.Fatalf("关卡面板不可挑战时不得调用 start_challenge, 实际 %d 次", startCalls.Load())
	}
}
