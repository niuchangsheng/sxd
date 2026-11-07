// Package minigame 提供叶公好龙普通培养自动化测试。
//
// @author Ateng
// @since 2026-11-07
package minigame

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
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
