// Package minigame 提供叶公好龙普通培养自动化测试。
//
// @author Ateng
// @since 2026-11-07
package minigame

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

func TestPetAnimalRoutine_CoinFeedsUntilExhausted(t *testing.T) {
	var feedCalls atomic.Int32
	var usedIngot atomic.Bool
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPetAnimalInfo:
			_ = protocol.WritePacket(conn, protocol.BuildPetAnimalInfoResultPacket(protocol.PetAnimalInfo{
				Lv: 2, Star: 9, Exp: 180228, FeedNum: 2,
			}))
		case protocol.ActionIDPetAnimalFeed:
			feedCalls.Add(1)
			// 除结果码外，请求第一字节是 feed_type；自动化必须始终为 0 (铜钱)。
			if len(pkt.Payload) == 0 || pkt.Payload[0] != protocol.PetAnimalFeedTypeCoin {
				usedIngot.Store(true)
			}
			left := int32(2) - feedCalls.Load()
			_ = protocol.WritePacket(conn, protocol.BuildPetAnimalFeedResultPacket(protocol.PetAnimalFeedResult{
				Result: protocol.PetAnimalResultSuccess, Lv: 2, Star: 9, Exp: 180228 + int32(feedCalls.Load())*100,
				ExpAdd: 100, FeedType: protocol.PetAnimalFeedTypeCoin, FeedNum: left, Hit: 0,
			}))
		}
	})
	defer sess.Close()

	if err := NewPetAnimalRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("pet animal: %v", err)
	}
	if got := feedCalls.Load(); got != 2 {
		t.Fatalf("feed_num=2 时应培养 2 次, 实际 %d 次", got)
	}
	if usedIngot.Load() {
		t.Fatal("自动化不得使用元宝培养 feed_type=1")
	}
}

func TestPetAnimalRoutine_FeedNumNotDecreaseStaleCircuitBreaker(t *testing.T) {
	var feedCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPetAnimalInfo:
			_ = protocol.WritePacket(conn, protocol.BuildPetAnimalInfoResultPacket(protocol.PetAnimalInfo{
				Lv: 2, Star: 9, Exp: 180228, FeedNum: 3,
			}))
		case protocol.ActionIDPetAnimalFeed:
			feedCalls.Add(1)
			// 服务端异常: 培养后剩余次数不递减。
			_ = protocol.WritePacket(conn, protocol.BuildPetAnimalFeedResultPacket(protocol.PetAnimalFeedResult{
				Result: protocol.PetAnimalResultSuccess, Lv: 2, Star: 9, Exp: 180328,
				ExpAdd: 100, FeedType: protocol.PetAnimalFeedTypeCoin, FeedNum: 3, Hit: 0,
			}))
		}
	})
	defer sess.Close()

	if err := NewPetAnimalRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("pet animal: %v", err)
	}
	// 连续不递减达到 maxPetAnimalStaleFeeds 才熔断，不能因暴击或回包滞后只跑一次。
	if got := feedCalls.Load(); got != maxPetAnimalStaleFeeds {
		t.Fatalf("次数持续未递减时应按 stale 上限熔断, 期望 %d 次, 实际 %d 次", maxPetAnimalStaleFeeds, got)
	}
}

func TestPetAnimalRoutine_NoFeedNumSkips(t *testing.T) {
	var feedCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPetAnimalInfo:
			_ = protocol.WritePacket(conn, protocol.BuildPetAnimalInfoResultPacket(protocol.PetAnimalInfo{
				Lv: 2, Star: 9, Exp: 180228, FeedNum: 0,
			}))
		case protocol.ActionIDPetAnimalFeed:
			feedCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewPetAnimalRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("pet animal: %v", err)
	}
	if got := feedCalls.Load(); got != 0 {
		t.Fatalf("feed_num=0 时不得培养, 实际 %d 次", got)
	}
}

func TestPetAnimalRoutine_NoEnoughCoinStops(t *testing.T) {
	var feedCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPetAnimalInfo:
			_ = protocol.WritePacket(conn, protocol.BuildPetAnimalInfoResultPacket(protocol.PetAnimalInfo{
				Lv: 2, Star: 9, Exp: 180228, FeedNum: 20,
			}))
		case protocol.ActionIDPetAnimalFeed:
			feedCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildPetAnimalFeedResultPacket(protocol.PetAnimalFeedResult{
				Result: protocol.PetAnimalResultNoEnoughCoin,
			}))
		}
	})
	defer sess.Close()

	if err := NewPetAnimalRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("pet animal: %v", err)
	}
	if got := feedCalls.Load(); got != 1 {
		t.Fatalf("铜钱不足后应立即停止, 实际 %d 次", got)
	}
}

func TestPetAnimalRoutine_TooMuchExpStops(t *testing.T) {
	var feedCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPetAnimalInfo:
			_ = protocol.WritePacket(conn, protocol.BuildPetAnimalInfoResultPacket(protocol.PetAnimalInfo{
				Lv: 2, Star: 10, Exp: 200000, FeedNum: 20,
			}))
		case protocol.ActionIDPetAnimalFeed:
			feedCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildPetAnimalFeedResultPacket(protocol.PetAnimalFeedResult{
				Result: protocol.PetAnimalResultTooMuchExp,
			}))
		}
	})
	defer sess.Close()

	if err := NewPetAnimalRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("pet animal: %v", err)
	}
	if got := feedCalls.Load(); got != 1 {
		t.Fatalf("经验已满后应立即停止 (等待升阶), 实际 %d 次", got)
	}
}

func TestPetAnimalRoutine_MaxLevelSkips(t *testing.T) {
	var feedCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPetAnimalInfo:
			_ = protocol.WritePacket(conn, protocol.BuildPetAnimalInfoResultPacket(protocol.PetAnimalInfo{
				Lv: 10, Star: 10, Exp: 0, FeedNum: 20,
			}))
		case protocol.ActionIDPetAnimalFeed:
			feedCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewPetAnimalRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("pet animal: %v", err)
	}
	if got := feedCalls.Load(); got != 0 {
		t.Fatalf("满级后不得培养, 实际 %d 次", got)
	}
}

// TestPetAnimalRoutine_FeedDropRetriesWithResync 验证培养请求被连续丢弃时，
// 会先用功能开放查询重置动作链再重试，而不是把剩余次数留到下一轮。
func TestPetAnimalRoutine_FeedDropRetriesWithResync(t *testing.T) {
	var feedCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPetAnimalInfo:
			_ = protocol.WritePacket(conn, protocol.BuildPetAnimalInfoResultPacket(protocol.PetAnimalInfo{
				Lv: 2, Star: 9, Exp: 180228, FeedNum: 1,
			}))
		case protocol.ActionIDPlayerCheckFunctionOpen:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.PlayerFunctionOpenTrue}))
		case protocol.ActionIDPetAnimalFeed:
			n := feedCalls.Add(1)
			if n <= 2 {
				// 前两次（首次 + PrevAct 重同步）均静默丢弃。
				return
			}
			_ = protocol.WritePacket(conn, protocol.BuildPetAnimalFeedResultPacket(protocol.PetAnimalFeedResult{
				Result: protocol.PetAnimalResultSuccess, Lv: 2, Star: 9, Exp: 180328,
				ExpAdd: 100, FeedType: protocol.PetAnimalFeedTypeCoin, FeedNum: 0, Hit: 0,
			}))
		}
	})
	defer sess.Close()

	if err := NewPetAnimalRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("pet animal: %v", err)
	}
	if got := feedCalls.Load(); got != 3 {
		t.Fatalf("功能查询重置链后应再试一次, 期望 3 次请求, 实际 %d 次", got)
	}
}

// TestPetAnimalRoutine_InfoOKFeedDroppedBacksOff 验证面板 48/0 正常回包、培养 48/1
// 被静默丢弃时，丢包计数不会被面板成功清零：连续 3 轮后挂起到每日重置。
func TestPetAnimalRoutine_InfoOKFeedDroppedBacksOff(t *testing.T) {
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPetAnimalInfo:
			_ = protocol.WritePacket(conn, protocol.BuildPetAnimalInfoResultPacket(protocol.PetAnimalInfo{
				Lv: 2, Star: 9, Exp: 180228, FeedNum: 3,
			}))
		case protocol.ActionIDPetAnimalFeed:
			// 静默丢弃：不回包。
		}
	})

	routine := NewPetAnimalRoutine()
	var lastErr error
	for i := 0; i < 3; i++ {
		lastErr = routine.Execute(context.Background(), sess, nil)
		if lastErr == nil {
			t.Fatalf("第 %d 轮培养无回包应返回冷却错误", i+1)
		}
	}
	if n := scheduler.SilentDropCount(sess, "pet_animal"); n < 3 {
		t.Fatalf("面板成功不应清零培养丢包计数, got %d", n)
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
