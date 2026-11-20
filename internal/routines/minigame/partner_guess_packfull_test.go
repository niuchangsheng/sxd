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

// buildEmptyPackListPayload 构造一份「背包容量 60、0 件物品」的 2/41 回包，
// 用于验证 PACK_FULL 分支会查询并记录真实背包空格。
func buildEmptyPackListPayload(t *testing.T) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	buf.Write([]byte{0x00, 0x3C})             // PackCapacity = 60
	buf.Write([]byte{0x00, 0x00, 0x00, 0x00}) // PackEmptyGrids (线上恒为 0)
	buf.Write([]byte{0x00, 0x00})             // PackItems count = 0
	buf.Write([]byte{0x00, 0x00})             // WarehouseCapacity = 0
	buf.Write([]byte{0x00, 0x00})             // WarehouseItems count = 0
	return buf.Bytes()
}

// TestPartnerGuessRoutine_StartPackFullReturnsBagFull 验证开局返回 PACK_FULL 时
// 例程按背包熔断处理（ErrBagFull），且会拉取 2/41 记录真实背包空格作为证据。
func TestPartnerGuessRoutine_StartPackFullReturnsBagFull(t *testing.T) {
	var packQueries atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGuessPartnerInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGuessPartnerInfoResultPacket(1))
		case protocol.ActionIDGuessPartnerStart:
			_ = protocol.WritePacket(conn, protocol.BuildGuessPartnerStartResultPacket(protocol.GuessPartnerRound{
				Result: protocol.GuessPartnerResultPackFull,
			}))
		case protocol.ActionIDItemGetPackList:
			packQueries.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDItemGetPackList, buildEmptyPackListPayload(t)))
		}
	})
	defer sess.Close()

	err := NewPartnerGuessRoutine().Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrBagFull) {
		t.Fatalf("开局 PACK_FULL 应返回 ErrBagFull, got %v", err)
	}
	if packQueries.Load() != 1 {
		t.Fatalf("应拉取一次背包快照用于诊断, calls = %d", packQueries.Load())
	}
}
