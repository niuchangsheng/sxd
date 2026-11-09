package minigame

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func TestFortuneBoxRoutine_NotOpenSkips(t *testing.T) {
	var panelCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDFortuneBoxOpen:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 0}))
		case protocol.ActionIDFortuneBoxPanel:
			panelCalls.Add(1)
		}
	})
	defer sess.Close()

	err := NewFortuneBoxRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if err != nil && !errors.As(err, &cd) {
		t.Fatalf("fortune box: %v", err)
	}
	if panelCalls.Load() != 0 {
		t.Fatalf("未开放时不应查询面板, calls = %d", panelCalls.Load())
	}
}

func TestFortuneBoxRoutine_OpensFreeBoxes(t *testing.T) {
	var openCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDFortuneBoxOpen:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 1}))
		case protocol.ActionIDFortuneBoxPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 0}))
		case protocol.ActionIDFortuneBoxOpenBox:
			openCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.FortuneBoxSuccess, 0, 0, 0, 0}))
		}
	})
	defer sess.Close()

	if err := NewFortuneBoxRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("fortune box: %v", err)
	}
	if openCalls.Load() != maxFortuneBoxOpen {
		t.Fatalf("open calls = %d, want %d", openCalls.Load(), maxFortuneBoxOpen)
	}
}

func TestFortuneBoxRoutine_PackFull(t *testing.T) {
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDFortuneBoxOpen:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 1}))
		case protocol.ActionIDFortuneBoxPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 0}))
		case protocol.ActionIDFortuneBoxOpenBox:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.FortuneBoxPackageFull}))
		}
	})
	defer sess.Close()

	err := NewFortuneBoxRoutine().Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrBagFull) {
		t.Fatalf("err = %v, want ErrBagFull", err)
	}
}

func TestFortuneBoxRoutine_NoScoreStops(t *testing.T) {
	var openCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDFortuneBoxOpen:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 1}))
		case protocol.ActionIDFortuneBoxPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 0}))
		case protocol.ActionIDFortuneBoxOpenBox:
			openCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.FortuneBoxNoScore}))
		}
	})
	defer sess.Close()

	if err := NewFortuneBoxRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("fortune box: %v", err)
	}
	if openCalls.Load() != 1 {
		t.Fatalf("积分不足后应停止, open calls = %d", openCalls.Load())
	}
}
