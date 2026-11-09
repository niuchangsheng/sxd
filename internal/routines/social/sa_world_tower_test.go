package social

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
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

	if err := NewSaWorldTowerRoutine().Execute(context.Background(), sess, nil); err != nil {
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

	if err := NewSaWorldTowerRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("sa world tower: %v", err)
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

	if err := NewSaWorldTowerRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("sa world tower: %v", err)
	}
	if fightCalls.Load() != 1 {
		t.Fatalf("次数不足后应停止, fight calls = %d", fightCalls.Load())
	}
}
