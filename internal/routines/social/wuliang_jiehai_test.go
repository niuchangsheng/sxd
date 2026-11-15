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

func TestWuliangJieHaiRoutine_FightsLevelsThenNextChapter(t *testing.T) {
	var fightCalls atomic.Int32
	var chapterCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEraInfiniteSeason, protocol.ActionIDEraInfinitePanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 4}))
		case protocol.ActionIDEraInfiniteChapter:
			n := chapterCalls.Add(1)
			chapter := int32(uint32(pkt.Payload[0])<<24 | uint32(pkt.Payload[1])<<16 |
				uint32(pkt.Payload[2])<<8 | uint32(pkt.Payload[3]))
			if chapter == 1 {
				// 前两次 now_level=3/4，第三次不再推进；第 2/3 境返回 result=1。
				switch n {
				case 1:
					_ = protocol.WritePacket(conn, protocol.BuildEraInfiniteChapterResultPacket(0, 3, 1))
				case 2:
					_ = protocol.WritePacket(conn, protocol.BuildEraInfiniteChapterResultPacket(0, 4, 1))
				default:
					_ = protocol.WritePacket(conn, protocol.BuildEraInfiniteChapterResultPacket(0, 4, 1))
				}
			} else {
				_ = protocol.WritePacket(conn, protocol.BuildEraInfiniteChapterResultPacket(1, 0, chapter))
			}
		case protocol.ActionIDEraInfiniteMonsterTeam:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 0}))
		case protocol.ActionIDEraInfiniteFight:
			fightCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildEraInfiniteFightResultPacket(0, true))
		}
	})
	defer sess.Close()

	if err := NewWuliangJieHaiRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("wuliang: %v", err)
	}
	if fightCalls.Load() != 2 {
		t.Fatalf("应挑战 3、4 两关, calls = %d", fightCalls.Load())
	}
}

func TestWuliangJieHaiRoutine_LossCooldown(t *testing.T) {
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEraInfiniteSeason, protocol.ActionIDEraInfinitePanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 4}))
		case protocol.ActionIDEraInfiniteChapter:
			_ = protocol.WritePacket(conn, protocol.BuildEraInfiniteChapterResultPacket(0, 3, 1))
		case protocol.ActionIDEraInfiniteMonsterTeam:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 0}))
		case protocol.ActionIDEraInfiniteFight:
			_ = protocol.WritePacket(conn, protocol.BuildEraInfiniteFightResultPacket(0, false))
		}
	})
	defer sess.Close()

	err := NewWuliangJieHaiRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("挑战失败应进入冷却, got %v", err)
	}
}
