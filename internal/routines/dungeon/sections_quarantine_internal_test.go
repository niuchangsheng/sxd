package dungeon

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/protocol"
)

// TestFetchMissionSections_FailedMapQuarantined 验证同一地图 4/0 连续失败后，
// 静默期内不再重复探测（避免同轮多任务各等两轮 8 秒超时）。
func TestFetchMissionSections_FailedMapQuarantined(t *testing.T) {
	oldTimeout := missionSectionsCallTimeout
	missionSectionsCallTimeout = 50 * time.Millisecond
	t.Cleanup(func() { missionSectionsCallTimeout = oldTimeout })

	var sectionsCalls atomic.Int32
	sess := newTownResetTestSession(t, func(pkt *protocol.Packet, conn net.Conn) bool {
		switch pkt.ActionID {
		case protocol.ActionIDMissionGetSections:
			sectionsCalls.Add(1)
			return true // 静默丢弃
		case protocol.ActionIDPlayerGetInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDPlayerGetInfo, []byte{0x01}))
			return true
		case protocol.ActionIDTownEnter:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDTownEnter, []byte{0x01}))
			return true
		}
		return false
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := fetchMissionSections(ctx, sess, 21); err == nil {
		t.Fatal("4/0 静默丢弃应返回错误")
	}
	if got := sectionsCalls.Load(); got != 2 {
		t.Fatalf("首轮应探测 2 次, 实际 %d 次", got)
	}

	start := time.Now()
	if _, err := fetchMissionSections(ctx, sess, 21); err == nil {
		t.Fatal("静默期内应直接返回错误")
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("静默期内不应再等待网络, 实际耗时 %s", elapsed)
	}
	if got := sectionsCalls.Load(); got != 2 {
		t.Fatalf("静默期内不应再发 4/0, 实际累计 %d 次", got)
	}
}

// TestFetchMissionSections_QuarantineExpires 验证静默期结束后恢复探测。
func TestFetchMissionSections_QuarantineExpires(t *testing.T) {
	oldTimeout := missionSectionsCallTimeout
	oldTTL := questSectionsDownTTL
	missionSectionsCallTimeout = 30 * time.Millisecond
	questSectionsDownTTL = 50 * time.Millisecond
	t.Cleanup(func() {
		missionSectionsCallTimeout = oldTimeout
		questSectionsDownTTL = oldTTL
	})

	var sectionsCalls atomic.Int32
	sess := newTownResetTestSession(t, func(pkt *protocol.Packet, conn net.Conn) bool {
		switch pkt.ActionID {
		case protocol.ActionIDMissionGetSections:
			sectionsCalls.Add(1)
			return true
		case protocol.ActionIDPlayerGetInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDPlayerGetInfo, []byte{0x01}))
			return true
		case protocol.ActionIDTownEnter:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDTownEnter, []byte{0x01}))
			return true
		}
		return false
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := fetchMissionSections(ctx, sess, 21); err == nil {
		t.Fatal("首轮应失败")
	}
	time.Sleep(80 * time.Millisecond)
	if _, err := fetchMissionSections(ctx, sess, 21); err == nil {
		t.Fatal("静默期后探测仍丢包应返回错误")
	}
	if got := sectionsCalls.Load(); got != 4 {
		t.Fatalf("静默期结束后应重新探测 2 次 (累计 4), 实际累计 %d 次", got)
	}
}
