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

// TestLandContractRoutine_RetriesAfterFailure 验证单场战斗失败后继续挑战，
// 连续 N 次失败才结束本轮。
func TestLandContractRoutine_RetriesAfterFailure(t *testing.T) {
	var fightCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDLandContractPanel:
			_ = protocol.WritePacket(conn, protocol.BuildLandContractPanelResultPacket([7]int32{0, 5, 0, 0, 0, 0, 0}))
		case protocol.ActionIDLandContractFight:
			n := fightCalls.Add(1)
			result := uint8(protocol.LandContractFailed)
			if n == 3 {
				result = protocol.LandContractSuccess
			} else if n == 4 {
				result = protocol.LandContractNoFightCount
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{result, 0, 0}))
		}
	})
	defer sess.Close()

	if err := NewLandContractRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("land contract: %v", err)
	}
	if got := fightCalls.Load(); got != 4 {
		t.Fatalf("两次失败后第三次成功、第四次次数不足，期望 4 次挑战，实际 %d", got)
	}
}

// TestLandContractRoutine_StopsAfterNConsecutiveFailures 验证连续 N 次失败后停止。
func TestLandContractRoutine_StopsAfterNConsecutiveFailures(t *testing.T) {
	var fightCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDLandContractPanel:
			_ = protocol.WritePacket(conn, protocol.BuildLandContractPanelResultPacket([7]int32{0, 5, 0, 0, 0, 0, 0}))
		case protocol.ActionIDLandContractFight:
			fightCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.LandContractFailed, 0, 0}))
		}
	})
	defer sess.Close()

	if err := NewLandContractRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("land contract: %v", err)
	}
	if got, want := fightCalls.Load(), int32(landContractFailureThreshold); got != want {
		t.Fatalf("连续失败达到阈值 %d 后应停止，实际挑战 %d 次", want, got)
	}
}
