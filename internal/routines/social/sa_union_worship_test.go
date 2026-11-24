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

// TestSaUnionWorshipRoutine_BusinessFailureNotCounted 验证服务端返回明确结果码
// 时属于业务失败，不应计入静默丢包退避（改为 1 小时普通冷却）。
func TestSaUnionWorshipRoutine_BusinessFailureNotCounted(t *testing.T) {
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
	scheduler.ResetSilentDrops(sess, "sa_union_worship")
	for i := 0; i < 3; i++ {
		err := NewSaUnionWorshipRoutine().Execute(context.Background(), sess, nil)
		var cd *scheduler.CooldownError
		if !errors.As(err, &cd) {
			t.Fatalf("第 %d 轮业务失败应返回 CooldownError, got %v", i+1, err)
		}
	}
	if n := scheduler.SilentDropCount(sess, "sa_union_worship"); n != 0 {
		t.Fatalf("业务失败不应计入静默丢包, got %d", n)
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
