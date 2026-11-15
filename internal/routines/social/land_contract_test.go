package social

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func TestLandContractRoutine_FightsUntilNoCount(t *testing.T) {
	var fightCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDLandContractPanel:
			_ = protocol.WritePacket(conn, protocol.BuildLandContractPanelResultPacket([7]int32{2, 5, 100, 0, 0, 0, 0}))
		case protocol.ActionIDLandContractFight:
			n := fightCalls.Add(1)
			result := uint8(protocol.LandContractSuccess)
			if n >= 3 {
				result = protocol.LandContractNoFightCount
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{result, 0, 0}))
		}
	})
	defer sess.Close()

	if err := NewLandContractRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("land contract: %v", err)
	}
	if fightCalls.Load() != 3 {
		t.Fatalf("应在次数不足时停止, calls = %d", fightCalls.Load())
	}
}

func TestLandContractRoutine_PackFull(t *testing.T) {
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDLandContractPanel:
			_ = protocol.WritePacket(conn, protocol.BuildLandContractPanelResultPacket([7]int32{0, 5, 0, 0, 0, 0, 0}))
		case protocol.ActionIDLandContractFight:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.LandContractPackFull}))
		}
	})
	defer sess.Close()

	err := NewLandContractRoutine().Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrBagFull) {
		t.Fatalf("背包满应返回 ErrBagFull, got %v", err)
	}
}

func TestLandContractRoutine_CDWaits(t *testing.T) {
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDLandContractPanel:
			_ = protocol.WritePacket(conn, protocol.BuildLandContractPanelResultPacket([7]int32{0, 5, 0, 0, 0, 0, 0}))
		case protocol.ActionIDLandContractFight:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.LandContractCDTime}))
		}
	})
	defer sess.Close()

	err := NewLandContractRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("冷却应返回 CooldownError, got %v", err)
	}
}
