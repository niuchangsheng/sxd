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

func TestEndlessBoxRoutine_ClosedWaits(t *testing.T) {
	var panelCalls, openCalls atomic.Int32
	past := int32(time.Now().Add(-48 * time.Hour).Unix())
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEndlessBoxActivity:
			_ = protocol.WritePacket(conn, protocol.BuildEndlessBoxActivityResultPacket(past, past+3600, 0))
		case protocol.ActionIDEndlessBoxPanel:
			panelCalls.Add(1)
		case protocol.ActionIDEndlessBoxOpen:
			openCalls.Add(1)
		}
	})
	defer sess.Close()

	err := NewEndlessBoxRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("活动外应返回 CooldownError, got %v", err)
	}
	if panelCalls.Load() != 0 || openCalls.Load() != 0 {
		t.Fatalf("活动外不得开箱, panel=%d open=%d", panelCalls.Load(), openCalls.Load())
	}
}

func TestEndlessBoxRoutine_OpensDuringEvent(t *testing.T) {
	now := int32(time.Now().Unix())
	var openPayloads [][]byte
	var gainCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEndlessBoxActivity:
			_ = protocol.WritePacket(conn, protocol.BuildEndlessBoxActivityResultPacket(now-600, now+600, 1))
		case protocol.ActionIDEndlessBoxPanel:
			_ = protocol.WritePacket(conn, protocol.BuildEndlessBoxPanelResultPacket(82, []protocol.EndlessBoxEntry{
				{Type: 1, Num: 5},
				{Type: 2, Num: 40},
			}, 5))
		case protocol.ActionIDEndlessBoxOpen:
			openPayloads = append(openPayloads, append([]byte(nil), pkt.Payload...))
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.EndlessBoxSuccess, 0, 0}))
		case protocol.ActionIDEndlessBoxGainAll:
			gainCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.EndlessBoxSuccess, 0, 0}))
		}
	})
	defer sess.Close()

	if err := NewEndlessBoxRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("endless box: %v", err)
	}
	if len(openPayloads) != 2 {
		t.Fatalf("应开两种箱子, opens=%d", len(openPayloads))
	}
	// 类型 1 num=5 → 单开(0)；类型 2 num=40 → 30 连(1)。
	if got := int32(openPayloads[0][7]); got != 0 {
		t.Fatalf("箱子1 open_type = %d, want 0", got)
	}
	if got := int32(openPayloads[1][7]); got != 1 {
		t.Fatalf("箱子2 open_type = %d, want 1", got)
	}
	if gainCalls.Load() != 1 {
		t.Fatalf("应领取全部奖励, calls=%d", gainCalls.Load())
	}
}

func TestEndlessBoxRoutine_PackFull(t *testing.T) {
	now := int32(time.Now().Unix())
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEndlessBoxActivity:
			_ = protocol.WritePacket(conn, protocol.BuildEndlessBoxActivityResultPacket(now-600, now+600, 1))
		case protocol.ActionIDEndlessBoxPanel:
			_ = protocol.WritePacket(conn, protocol.BuildEndlessBoxPanelResultPacket(0, []protocol.EndlessBoxEntry{{Type: 1, Num: 1}}, 0))
		case protocol.ActionIDEndlessBoxOpen:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.EndlessBoxPackFull}))
		}
	})
	defer sess.Close()

	err := NewEndlessBoxRoutine().Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrBagFull) {
		t.Fatalf("背包满应返回 ErrBagFull, got %v", err)
	}
}
