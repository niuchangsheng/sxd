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

func TestSaWorldTowerRoutine_NotOpenSkips(t *testing.T) {
	var panelCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDSaWorldTowerOpen:
			w := protocol.NewWriter().WriteInt32(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDSaWorldTowerPanel:
			panelCalls.Add(1)
		}
	})
	defer sess.Close()

	err := NewSaWorldTowerRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if err != nil && !errors.As(err, &cd) {
		t.Fatalf("sa world tower: %v", err)
	}
	if panelCalls.Load() != 0 {
		t.Fatalf("未开放时不应查询面板, calls = %d", panelCalls.Load())
	}
}

func TestSaWorldTowerRoutine_FightsWhenOpen(t *testing.T) {
	var fightCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDSaWorldTowerOpen:
			w := protocol.NewWriter().WriteInt32(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDSaWorldTowerPanel:
			w := protocol.NewWriter().WriteInt32(3).WriteUint16(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDSaWorldTowerFight:
			fightCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.SaWorldTowerSuccess, 0, 0, 0, 0}))
		}
	})
	defer sess.Close()

	if err := NewSaWorldTowerRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("sa world tower: %v", err)
	}
	if fightCalls.Load() == 0 {
		t.Fatal("开放时应发起挑战")
	}
}

func TestSaWorldTowerRoutine_UsesPanelTowerIDs(t *testing.T) {
	var firstID atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDSaWorldTowerOpen:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 1}))
		case protocol.ActionIDSaWorldTowerPanel:
			w := protocol.NewWriter().WriteInt32(3).WriteUint16(1)
			w.WriteInt32(22).WriteInt32(0).WriteInt32(0).WriteInt32(0).WriteInt32(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDSaWorldTowerFight:
			r := protocol.NewReader(pkt.Payload)
			id, _ := r.ReadInt32()
			firstID.Store(id)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.SaWorldTowerFuncNoOpen}))
		}
	})
	defer sess.Close()

	err := NewSaWorldTowerRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("FuncNoOpen 应返回 CooldownError, got %v", err)
	}
	if firstID.Load() != 22 {
		t.Fatalf("fight tower id = %d, want 22", firstID.Load())
	}
}

func TestSaWorldTowerRoutine_NoTimeStops(t *testing.T) {
	var fightCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDSaWorldTowerOpen:
			w := protocol.NewWriter().WriteInt32(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDSaWorldTowerPanel:
			w := protocol.NewWriter().WriteInt32(1).WriteUint16(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDSaWorldTowerFight:
			fightCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.SaWorldTowerNoTime}))
		}
	})
	defer sess.Close()

	err := NewSaWorldTowerRoutine().Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrAttemptsExhausted) {
		t.Fatalf("次数不足应返回 ErrAttemptsExhausted, got %v", err)
	}
	if fightCalls.Load() != 1 {
		t.Fatalf("次数不足后应停止, fight calls = %d", fightCalls.Load())
	}
}

// TestSaWorldTowerRoutine_FightSilentDropBacksOff 验证挑战无回包时按连续丢包退避。
func TestSaWorldTowerRoutine_FightSilentDropBacksOff(t *testing.T) {
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDSaWorldTowerOpen:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 1}))
		case protocol.ActionIDSaWorldTowerPanel:
			w := protocol.NewWriter().WriteInt32(1).WriteUint16(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDSaWorldTowerFight:
			// 静默丢弃：不回包。
		}
	})
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := NewSaWorldTowerRoutine().Execute(ctx, sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("挑战无回包应返回 CooldownError, got %v", err)
	}
}

// TestSaWorldTowerRoutine_RetriesAfterFailure 验证单次战斗失败后继续挑战下一座，
// 连续 N 次失败才结束本轮。
func TestSaWorldTowerRoutine_RetriesAfterFailure(t *testing.T) {
	var fightCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDSaWorldTowerOpen:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 1}))
		case protocol.ActionIDSaWorldTowerPanel:
			w := protocol.NewWriter().WriteInt32(3).WriteUint16(3)
			for _, id := range []int32{1, 2, 3} {
				w.WriteInt32(id).WriteInt32(0).WriteInt32(0).WriteInt32(0).WriteInt32(0)
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDSaWorldTowerFight:
			n := fightCalls.Add(1)
			code := protocol.SaWorldTowerFailure
			if n == 3 {
				code = protocol.SaWorldTowerSuccess
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{code}))
		}
	})
	defer sess.Close()

	if err := NewSaWorldTowerRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("sa world tower: %v", err)
	}
	if got := fightCalls.Load(); got != 3 {
		t.Fatalf("前两次失败后应继续，第三次成功，期望 3 次挑战，实际 %d", got)
	}
}

// TestSaWorldTowerRoutine_StopsAfterNConsecutiveFailures 验证连续 N 次失败后停止。
func TestSaWorldTowerRoutine_StopsAfterNConsecutiveFailures(t *testing.T) {
	var fightCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDSaWorldTowerOpen:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 1}))
		case protocol.ActionIDSaWorldTowerPanel:
			w := protocol.NewWriter().WriteInt32(3).WriteUint16(3)
			for _, id := range []int32{1, 2, 3} {
				w.WriteInt32(id).WriteInt32(0).WriteInt32(0).WriteInt32(0).WriteInt32(0)
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDSaWorldTowerFight:
			fightCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.SaWorldTowerFailure}))
		}
	})
	defer sess.Close()

	if err := NewSaWorldTowerRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("sa world tower: %v", err)
	}
	if got, want := fightCalls.Load(), int32(saWorldTowerFailureThreshold); got != want {
		t.Fatalf("连续失败达到阈值 %d 后应停止，实际挑战 %d 次", want, got)
	}
}
