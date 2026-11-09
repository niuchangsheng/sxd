package pvp

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func ladderTaskPanelPayload(taskID int32) []byte {
	w := protocol.NewWriter().WriteUint8(protocol.WorldPkRewardSuccess)
	w.WriteUint16(1).WriteInt32(taskID).WriteInt32(1).WriteInt32(0)
	w.WriteUint16(0)
	w.WriteUint16(0)
	w.WriteInt32(0).WriteInt32(0).WriteInt32(0).WriteInt32(0)
	w.WriteUint16(0)
	return w.Bytes()
}

func TestCrossServerLadderRoutine_ClaimsRewards(t *testing.T) {
	var awardCalls atomic.Int32
	sess := newPvPMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDWorldPkStatus:
			_ = protocol.WritePacket(conn, protocol.BuildWorldPkStatusResultPacket(protocol.WorldPkStatusOn))
		case protocol.ActionIDWorldPkJoin:
			_ = protocol.WritePacket(conn, protocol.BuildWorldPkJoinResultPacket(protocol.WorldPkResultSuccess))
		case protocol.ActionIDWorldPkSetAutoMatch:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.WorldPkRewardSuccess}))
		case protocol.ActionIDWorldPkReceiveSpecial:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.WorldPkRewardSuccess}))
		case protocol.ActionIDWorldPkOpenTaskPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, ladderTaskPanelPayload(7)))
		case protocol.ActionIDWorldPkTaskAward:
			awardCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.WorldPkRewardSuccess}))
		case protocol.ActionIDWorldPkAwardPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.WorldPkRewardSuccess}))
		}
	})
	defer sess.Close()

	if err := NewCrossServerLadderRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("ladder: %v", err)
	}
	if awardCalls.Load() != 1 {
		t.Fatalf("task award calls = %d, want 1", awardCalls.Load())
	}
}

func TestCrossServerLadderRoutine_PackFull(t *testing.T) {
	sess := newPvPMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDWorldPkStatus:
			_ = protocol.WritePacket(conn, protocol.BuildWorldPkStatusResultPacket(protocol.WorldPkStatusOn))
		case protocol.ActionIDWorldPkJoin:
			_ = protocol.WritePacket(conn, protocol.BuildWorldPkJoinResultPacket(protocol.WorldPkResultSuccess))
		case protocol.ActionIDWorldPkSetAutoMatch:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.WorldPkRewardSuccess}))
		case protocol.ActionIDWorldPkReceiveSpecial:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.WorldPkRewardPackFull}))
		}
	})
	defer sess.Close()

	err := NewCrossServerLadderRoutine().Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrBagFull) {
		t.Fatalf("err = %v, want ErrBagFull", err)
	}
}

func TestCrossServerLadderRoutine_OffSkipsRewards(t *testing.T) {
	var claimCalls atomic.Int32
	sess := newPvPMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDWorldPkStatus:
			_ = protocol.WritePacket(conn, protocol.BuildWorldPkStatusResultPacket(protocol.WorldPkStatusOff))
		case protocol.ActionIDWorldPkReceiveSpecial, protocol.ActionIDWorldPkOpenTaskPanel:
			claimCalls.Add(1)
		}
	})
	defer sess.Close()

	err := NewCrossServerLadderRoutine().Execute(context.Background(), sess, nil)
	var cdErr *scheduler.CooldownError
	if !errors.As(err, &cdErr) {
		t.Fatalf("OFF 时应返回 CooldownError, 实际: %v", err)
	}
	if claimCalls.Load() != 0 {
		t.Fatalf("OFF 时不应领取奖励, calls = %d", claimCalls.Load())
	}
}
