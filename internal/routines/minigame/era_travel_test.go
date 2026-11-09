package minigame

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func TestEraTravelRoutine_StateSilentDropCooldown(t *testing.T) {
	sess := newMinigameMockSession(t, func(net.Conn, *protocol.Packet) {
		// 状态查询模拟服务端静默丢包：不回包。
	})
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := NewEraTravelRoutine().Execute(ctx, sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("山河游历状态无回包应返回 CooldownError, 实际: %v", err)
	}
}

func TestEraTravelRoutine_StateNotOpenCooldown(t *testing.T) {
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		if pkt.ActionID == protocol.ActionIDEraTravelGetState {
			_ = protocol.WritePacket(conn, protocol.NewPacket(
				protocol.ActionIDEraTravelGetState, []byte{2, 0, 0, 0}))
		}
	})
	defer sess.Close()

	err := NewEraTravelRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("山河游历不可用应返回 CooldownError, 实际: %v", err)
	}
}