package social

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func firstInt32(payload []byte) int32 {
	if len(payload) < 4 {
		return 0
	}
	return int32(binary.BigEndian.Uint32(payload[:4]))
}

// TestWuliangJieHaiRoutine_FightsLevelsUntilEnded 验证从面板 mission_level 的
// 下一关开始连续挑战，直到服务端返回「已结束」。
func TestWuliangJieHaiRoutine_FightsLevelsUntilEnded(t *testing.T) {
	var fightCalls atomic.Int32
	var mu sync.Mutex
	var levels []int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEraInfiniteSeason:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 4}))
		case protocol.ActionIDEraInfinitePanel:
			_ = protocol.WritePacket(conn, protocol.BuildEraInfinitePanelResultPacket(110, 2, 0, 0, 0, 1))
		case protocol.ActionIDEraInfiniteChapter:
			_ = protocol.WritePacket(conn, protocol.BuildEraInfiniteChapterResultPacket(0, 3, 1))
		case protocol.ActionIDEraInfiniteFight:
			n := fightCalls.Add(1)
			mu.Lock()
			levels = append(levels, firstInt32(pkt.Payload))
			mu.Unlock()
			if n < 3 {
				_ = protocol.WritePacket(conn, protocol.BuildEraInfiniteFightResultPacket(0, true))
			} else {
				_ = protocol.WritePacket(conn, protocol.BuildEraInfiniteFightResultPacket(
					protocol.EraInfiniteEnded, false))
			}
		}
	})
	defer sess.Close()

	if err := NewWuliangJieHaiRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("wuliang: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []int32{3, 4, 5}
	if len(levels) != len(want) {
		t.Fatalf("挑战关卡 = %v, want %v", levels, want)
	}
	for i := range want {
		if levels[i] != want[i] {
			t.Fatalf("挑战关卡 = %v, want %v", levels, want)
		}
	}
}

// TestWuliangJieHaiRoutine_EnterSequenceMatchesCapture 验证挑战前的
// fire-and-forget 顺序与章节换算：mission_level=10 → 第 2 章、挑战 11 关。
func TestWuliangJieHaiRoutine_EnterSequenceMatchesCapture(t *testing.T) {
	var mu sync.Mutex
	var seq []string
	var chapterID int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		if pkt.ActionID != protocol.ActionIDEraInfiniteSeason {
			mu.Lock()
			seq = append(seq, actionLabel(pkt.ActionID))
			mu.Unlock()
		}
		switch pkt.ActionID {
		case protocol.ActionIDEraInfiniteSeason:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 4}))
		case protocol.ActionIDEraInfinitePanel:
			_ = protocol.WritePacket(conn, protocol.BuildEraInfinitePanelResultPacket(140, 10, 0, 0, 0, 1))
		case protocol.ActionIDEraInfiniteDeployTypeSeq:
			_ = protocol.WritePacket(conn, protocol.BuildEraInfiniteDeployTypeSeqResultPacket(
				[][2]int32{{1, 90001}, {2, 90002}, {3, 90003}}))
		case protocol.ActionIDEraInfiniteChapter:
			chapterID = firstInt32(pkt.Payload)
			_ = protocol.WritePacket(conn, protocol.BuildEraInfiniteChapterResultPacket(0, 11, chapterID))
		case protocol.ActionIDEraInfiniteSuperDeployList, protocol.ActionIDEraInfiniteMonsterTeam:
			// 同样无需回包。
		case protocol.ActionIDEraInfiniteFight:
			_ = protocol.WritePacket(conn, protocol.BuildEraInfiniteFightResultPacket(
				protocol.EraInfiniteEnded, false))
		}
	})
	defer sess.Close()

	if err := NewWuliangJieHaiRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("wuliang: %v", err)
	}
	if chapterID != 2 {
		t.Fatalf("mission_level=10 应进入第 2 章, got %d", chapterID)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"853/1", "853/2", "854/39", "854/1", "853/3", "853/4"}
	if len(seq) < len(want) {
		t.Fatalf("请求序列 = %v, want 前缀 %v", seq, want)
	}
	for i := range want {
		if seq[i] != want[i] {
			t.Fatalf("请求序列 = %v, want 前缀 %v", seq, want)
		}
	}
}

// actionLabel 将 action id 格式化为 "模块/动作"。
func actionLabel(id uint32) string {
	return fmt.Sprintf("%d/%d", id>>16, id&0xFFFF)
}

// TestWuliangJieHaiRoutine_FightDroppedBacksOff 验证 853/4 被静默丢弃时连续 3 轮
// 后挂起，且面板成功不会清零挑战丢包计数。
func TestWuliangJieHaiRoutine_FightDroppedBacksOff(t *testing.T) {
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEraInfiniteSeason:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 4}))
		case protocol.ActionIDEraInfinitePanel:
			_ = protocol.WritePacket(conn, protocol.BuildEraInfinitePanelResultPacket(110, 2, 0, 0, 0, 1))
		case protocol.ActionIDEraInfiniteChapter:
			_ = protocol.WritePacket(conn, protocol.BuildEraInfiniteChapterResultPacket(0, 3, 1))
		case protocol.ActionIDEraInfiniteDeployTypeSeq:
			_ = protocol.WritePacket(conn, protocol.BuildEraInfiniteDeployTypeSeqResultPacket(
				[][2]int32{{1, 90001}}))
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
		t.Fatalf("面板成功不应清零挑战丢包计数, got %d", n)
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

// TestWuliangJieHaiRoutine_LossCooldown 验证多队伍战斗整体失败时重试，
// 连续 3 次失败后退出并进入冷却。
func TestWuliangJieHaiRoutine_LossCooldown(t *testing.T) {
	var fightCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEraInfiniteSeason:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 4}))
		case protocol.ActionIDEraInfinitePanel:
			_ = protocol.WritePacket(conn, protocol.BuildEraInfinitePanelResultPacket(110, 12, 0, 0, 0, 1))
		case protocol.ActionIDEraInfiniteChapter:
			_ = protocol.WritePacket(conn, protocol.BuildEraInfiniteChapterResultPacket(0, 13, 2))
		case protocol.ActionIDEraInfiniteDeployTypeSeq:
			_ = protocol.WritePacket(conn, protocol.BuildEraInfiniteDeployTypeSeqResultPacket(
				[][2]int32{{1, 90001}}))
		case protocol.ActionIDEraInfiniteFight:
			fightCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildEraInfiniteFightResultPacket(0, false))
		}
	})
	defer sess.Close()

	err := NewWuliangJieHaiRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("连续挑战失败应进入冷却, got %v", err)
	}
	if got := fightCalls.Load(); got != maxWuliangConsecutiveLosses {
		t.Fatalf("失败重试次数 = %d, want %d", got, maxWuliangConsecutiveLosses)
	}
}

// TestWuliangChapterForLevel 验证章节边界。
func TestWuliangChapterForLevel(t *testing.T) {
	cases := map[int32]int32{1: 1, 10: 1, 11: 2, 30: 2, 31: 3, 60: 3, 61: 4, 300: 4}
	for level, want := range cases {
		if got := wuliangChapterForLevel(level); got != want {
			t.Fatalf("level %d → chapter %d, want %d", level, got, want)
		}
	}
}

// TestWuliangJieHaiRoutine_ChapterPanelOverridesAssumedLevel 验证章节面板
// 返回的 now_level 优先于 mission_level+1 推断。
func TestWuliangJieHaiRoutine_ChapterPanelOverridesAssumedLevel(t *testing.T) {
	var mu sync.Mutex
	var fightLevels []int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEraInfiniteSeason:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 4}))
		case protocol.ActionIDEraInfinitePanel:
			// 推断关卡 = mission_level+1 = 3
			_ = protocol.WritePacket(conn, protocol.BuildEraInfinitePanelResultPacket(110, 2, 0, 0, 0, 1))
		case protocol.ActionIDEraInfiniteChapter:
			// 章节面板给出真实关卡 7
			_ = protocol.WritePacket(conn, protocol.BuildEraInfiniteChapterResultPacket(0, 7, 1))
		case protocol.ActionIDEraInfiniteDeployTypeSeq:
			_ = protocol.WritePacket(conn, protocol.BuildEraInfiniteDeployTypeSeqResultPacket(
				[][2]int32{{1, 90001}}))
		case protocol.ActionIDEraInfiniteFight:
			mu.Lock()
			fightLevels = append(fightLevels, firstInt32(pkt.Payload))
			mu.Unlock()
			_ = protocol.WritePacket(conn, protocol.BuildEraInfiniteFightResultPacket(
				protocol.EraInfiniteEnded, false))
		}
	})
	defer sess.Close()

	if err := NewWuliangJieHaiRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("wuliang: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(fightLevels) != 1 || fightLevels[0] != 7 {
		t.Fatalf("应以章节面板 now_level=7 发起挑战, got %v", fightLevels)
	}
}
