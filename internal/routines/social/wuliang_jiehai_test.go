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

// TestWuliangJieHaiRoutine_ChapterOKFightDroppedBacksOff 验证章节面板 853/2 正常回包、
// 但挑战 853/5 被静默丢弃时，丢包计数不会被章节成功清零，连续 3 轮后挂起到每日重置。
func TestWuliangJieHaiRoutine_ChapterOKFightDroppedBacksOff(t *testing.T) {
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEraInfiniteSeason, protocol.ActionIDEraInfinitePanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 4}))
		case protocol.ActionIDEraInfiniteChapter:
			_ = protocol.WritePacket(conn, protocol.BuildEraInfiniteChapterResultPacket(0, 3, 1))
		case protocol.ActionIDEraInfiniteMonsterTeam:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 0}))
		case protocol.ActionIDEraInfiniteFight:
			// 静默丢弃：不回包。
		}
	})
	defer sess.Close()
	scheduler.ResetSilentDrops(sess, "wuliang_jiehai")

	routine := NewWuliangJieHaiRoutine()
	var lastErr error
	for i := 0; i < 3; i++ {
		lastErr = routine.Execute(context.Background(), sess, nil)
		if lastErr == nil {
			t.Fatalf("第 %d 轮挑战无回包应返回冷却错误", i+1)
		}
	}
	if n := scheduler.SilentDropCount(sess, "wuliang_jiehai"); n < 3 {
		t.Fatalf("章节面板成功不应清零挑战丢包计数, got %d", n)
	}
	var cd *scheduler.CooldownError
	if !errors.As(lastErr, &cd) {
		t.Fatalf("达到阈值后应返回 CooldownError, got %v", lastErr)
	}
	// HandleSilentDrop 到阈值后挂起至 min(次日 00:05, 6 小时)。
	exp := scheduler.NextDailyReset(time.Now())
	if cap := time.Now().Add(6 * time.Hour); exp.After(cap) {
		exp = cap
	}
	if d := cd.Until.Sub(exp); d > time.Minute || d < -time.Minute {
		t.Fatalf("熔断唤醒时间 = %s, want %s", cd.Until, exp)
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
