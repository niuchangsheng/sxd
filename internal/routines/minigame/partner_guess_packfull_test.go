package minigame

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// buildPackListPayload 构造 2/41 回包：容量 60，按 itemCount 填充占位物品。
func buildPackListPayload(t *testing.T, itemCount int) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	buf.Write([]byte{0x00, 0x3C})             // PackCapacity = 60
	buf.Write([]byte{0x00, 0x00, 0x00, 0x00}) // PackEmptyGrids（线上恒为 0）
	count := uint16(itemCount)
	buf.Write([]byte{byte(count >> 8), byte(count)})
	for i := 0; i < itemCount; i++ {
		if err := protocol.WriteItemRecord(buf, protocol.ItemRecord{
			PlayerItemID: int32(10000 + i),
			ItemID:       1488,
			GridID:       int32(i + 1),
			ItemType:     20000,
		}); err != nil {
			t.Fatalf("write item record: %v", err)
		}
	}
	buf.Write([]byte{0x00, 0x00}) // WarehouseCapacity
	buf.Write([]byte{0x00, 0x00}) // WarehouseItems count
	return buf.Bytes()
}

// TestPartnerGuessRoutine_StartPackFullWithFreeSlotsCools 验证服务端在背包仍有空格时
// 返回 PACK_FULL（本服已知语义偏差）不再触发 10 分钟背包熔断空转，而是进入 2 小时语义冷却。
func TestPartnerGuessRoutine_StartPackFullWithFreeSlotsCools(t *testing.T) {
	var packQueries atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGuessPartnerInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGuessPartnerInfoResultPacket(5))
		case protocol.ActionIDGuessPartnerStart:
			_ = protocol.WritePacket(conn, protocol.BuildGuessPartnerStartResultPacket(protocol.GuessPartnerRound{
				Result: protocol.GuessPartnerResultPackFull,
			}))
		case protocol.ActionIDItemGetPackList:
			packQueries.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDItemGetPackList, buildPackListPayload(t, 10)))
		}
	})
	defer sess.Close()

	err := NewPartnerGuessRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("背包仍有空格时应返回 CooldownError, got %v", err)
	}
	if packQueries.Load() != 1 {
		t.Fatalf("应拉取一次背包快照用于诊断, calls = %d", packQueries.Load())
	}
}

// TestPartnerGuessRoutine_StartPackFullWithFullBagReturnsBagFull 验证背包真的没有空格时
// 仍按背包熔断处理（ErrBagFull），保证背包装满时能立刻让清理任务介入。
func TestPartnerGuessRoutine_StartPackFullWithFullBagReturnsBagFull(t *testing.T) {
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGuessPartnerInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGuessPartnerInfoResultPacket(5))
		case protocol.ActionIDGuessPartnerStart:
			_ = protocol.WritePacket(conn, protocol.BuildGuessPartnerStartResultPacket(protocol.GuessPartnerRound{
				Result: protocol.GuessPartnerResultPackFull,
			}))
		case protocol.ActionIDItemGetPackList:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDItemGetPackList, buildPackListPayload(t, 60)))
		}
	})
	defer sess.Close()

	err := NewPartnerGuessRoutine().Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrBagFull) {
		t.Fatalf("背包无空格时应返回 ErrBagFull, got %v", err)
	}
}

// TestPartnerGuessRoutine_ScrapeFailureStops 验证刮图回包返回失败码时立即停止刮图，
// 不再按剩余次数空转，并继续进入作答环节。
func TestPartnerGuessRoutine_ScrapeFailureStops(t *testing.T) {
	var scrapeCalls, guessCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGuessPartnerInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGuessPartnerInfoResultPacket(5))
		case protocol.ActionIDGuessPartnerStart:
			_ = protocol.WritePacket(conn, protocol.BuildGuessPartnerStartResultPacket(protocol.GuessPartnerRound{
				Result:            protocol.GuessPartnerResultSuccess,
				RemainScrapeTimes: 3,
				RemainGuessTimes:  1,
				RoleIDs:           []int32{101, 102},
			}))
		case protocol.ActionIDGuessPartnerScrape:
			scrapeCalls.Add(1)
			// 失败码：2=ERROR，服务端未继续消耗刮图次数。
			_ = protocol.WritePacket(conn, protocol.BuildGuessPartnerScrapeResultPacket(protocol.GuessPartnerResultError, 3))
		case protocol.ActionIDGuessPartnerGuess:
			guessCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildGuessPartnerGuessResultPacket(protocol.GuessPartnerResultWrong, 0, 0, 0))
		}
	})

	if err := NewPartnerGuessRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("partner guess: %v", err)
	}
	if got := scrapeCalls.Load(); got != 1 {
		t.Fatalf("刮图失败后应立即停止, scrape calls = %d", got)
	}
	if got := guessCalls.Load(); got != 1 {
		t.Fatalf("刮图失败后仍应进入作答, guess calls = %d", got)
	}
}
