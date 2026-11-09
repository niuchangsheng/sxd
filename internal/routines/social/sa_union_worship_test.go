package social

import (
	"context"
	"errors"
	"net"
	"testing"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func TestSaUnionWorshipRoutine_FailureCooldown(t *testing.T) {
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDSaUnionWorshipPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(
				protocol.ActionIDSaUnionWorshipPanel, make([]byte, 44)))
		case protocol.ActionIDSaUnionWorship:
			_ = protocol.WritePacket(conn, protocol.NewPacket(
				protocol.ActionIDSaUnionWorship, []byte{protocol.SaUnionWorshipResultFailed, 0, 0}))
		}
	})
	defer sess.Close()

	err := NewSaUnionWorshipRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("膜拜失败应返回 CooldownError, 实际: %v", err)
	}
}

func TestSaUnionWorshipRoutine_Success(t *testing.T) {
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDSaUnionWorshipPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(
				protocol.ActionIDSaUnionWorshipPanel, make([]byte, 44)))
		case protocol.ActionIDSaUnionWorship:
			_ = protocol.WritePacket(conn, protocol.NewPacket(
				protocol.ActionIDSaUnionWorship, []byte{protocol.SaUnionWorshipResultSuccess, 0, 0}))
		}
	})
	defer sess.Close()

	if err := NewSaUnionWorshipRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("膜拜成功不应报错: %v", err)
	}
}