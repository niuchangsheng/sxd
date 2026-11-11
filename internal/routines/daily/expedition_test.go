package daily

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

func expeditionInfoPayload(missionIDs ...int32) []byte {
	w := protocol.NewWriter()
	w.WriteInt64(25).WriteInt32(780).WriteUint16(uint16(len(missionIDs)))
	for _, id := range missionIDs {
		w.WriteInt32(id).WriteInt32(0).WriteInt32(0).WriteInt32(0).WriteUint16(1)
		w.WriteInt32(6192).WriteInt32(10)
	}
	w.WriteInt32(81).WriteUint16(0)
	return w.Bytes()
}

func TestExpeditionRoutine_StartsTasks(t *testing.T) {
	var startCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDExpeditionInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, expeditionInfoPayload(3, 4, 5)))
		case protocol.ActionIDExpeditionStart:
			startCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.ExpeditionSuccess}))
		}
	})
	defer sess.Close()

	if err := NewExpeditionRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("expedition: %v", err)
	}
	if startCalls.Load() != 3 {
		t.Fatalf("start calls = %d, want 3", startCalls.Load())
	}
}

func TestExpeditionRoutine_AlreadyStartNotCounted(t *testing.T) {
	var calls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDExpeditionInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, expeditionInfoPayload(9)))
		case protocol.ActionIDExpeditionStart:
			calls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.ExpeditionAlreadyStart}))
		}
	})
	defer sess.Close()

	if err := NewExpeditionRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("expedition: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("start calls = %d, want 1", calls.Load())
	}
}

func TestExpeditionRoutine_PanelSilentDropCooldown(t *testing.T) {
	sess := newDailyMockSession(t, func(net.Conn, *protocol.Packet) {})
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := NewExpeditionRoutine().Execute(ctx, sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("远征面板无回包应返回 CooldownError, 实际: %v", err)
	}
}
