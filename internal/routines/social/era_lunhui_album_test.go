package social

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestEraLunHuiAlbumRoutine_ClaimsUnclaimed(t *testing.T) {
	var claimCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEraLunHuiPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		case protocol.ActionIDEraLunHuiTaskInfo:
			tab := int32(uint32(pkt.Payload[0])<<24 | uint32(pkt.Payload[1])<<16 |
				uint32(pkt.Payload[2])<<8 | uint32(pkt.Payload[3]))
			tasks := []protocol.EraLunHuiTask{}
			if tab == 1 {
				tasks = []protocol.EraLunHuiTask{
					{ID: 101, State: 10, Claimed: 0},
					{ID: 102, State: 10, Claimed: 1},
					{ID: 103, State: 10, Claimed: 0},
				}
			}
			_ = protocol.WritePacket(conn, protocol.BuildEraLunHuiTasksResultPacket(tasks, int32(tab)))
		case protocol.ActionIDEraLunHuiGetTask:
			n := claimCalls.Add(1)
			result := uint8(0)
			if n == 2 {
				result = 3 // 第二项条件不满足，应继续后续任务
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{result}))
		case protocol.ActionIDEraLunHuiKarmaCycleAward:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 5}))
		}
	})
	defer sess.Close()

	if err := NewEraLunHuiAlbumRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("era lunhui album: %v", err)
	}
	if claimCalls.Load() != 2 {
		t.Fatalf("应只对两个未领取任务发起领取, calls = %d", claimCalls.Load())
	}
}

func TestEraLunHuiAlbumRoutine_PanelFailedSkips(t *testing.T) {
	var claimCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEraLunHuiPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{3}))
		case protocol.ActionIDEraLunHuiTaskInfo, protocol.ActionIDEraLunHuiGetTask:
			claimCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewEraLunHuiAlbumRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("era lunhui album: %v", err)
	}
	if claimCalls.Load() != 0 {
		t.Fatalf("面板失败时不得领取, calls = %d", claimCalls.Load())
	}
}
