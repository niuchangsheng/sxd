package dungeon_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"sync"
	"testing"

	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/dungeon"
)

// TestBagCleanup_SellsOnlyEquipmentCraftScrolls 验证背包空格不足时只出售
// 装备制作卷（TypeID=1002 且名称/描述匹配），其它物品一律不动。
func TestBagCleanup_SellsOnlyEquipmentCraftScrolls(t *testing.T) {
	dictRepo, err := dictionary.NewRepository("")
	if err != nil {
		t.Fatalf("load dictionary: %v", err)
	}
	defer dictRepo.Close()

	sess, srvConn := createTestSession(t, 0)
	defer sess.Close()
	defer srvConn.Close()

	var mu sync.Mutex
	var soldGrids []int16
	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionIDItemGetPackList:
			buf := new(bytes.Buffer)
			// 容量 6、已用 3 件 → 真实空格 3（<= 阈值 5，触发清理）。
			// empty 字段在线上不可靠（恒为 0），这里仍填入 2 以确认逻辑不依赖它。
			_ = binary.Write(buf, binary.BigEndian, int16(6))
			_ = binary.Write(buf, binary.BigEndian, int32(2))
			_ = binary.Write(buf, binary.BigEndian, uint16(3))
			// 940 = 逍遥戟制作卷（TypeID 1002，装备制作卷）
			_ = protocol.WriteItemRecord(buf, protocol.ItemRecord{
				PlayerItemID: 1001, ItemID: 940, GridID: 5, Field4: 100, Field5: 1,
			})
			// 包子：非制作卷
			_ = protocol.WriteItemRecord(buf, protocol.ItemRecord{
				PlayerItemID: 1002, ItemID: protocol.ItemIDSteamedBun, GridID: 6, Field4: 1, Field5: 9,
			})
			// 结缘宝箱：TypeID 36000，非制作卷
			_ = protocol.WriteItemRecord(buf, protocol.ItemRecord{
				PlayerItemID: 1003, ItemID: 3937, GridID: 7, Field4: 1, Field5: 35,
			})
			_ = binary.Write(buf, binary.BigEndian, int16(10))
			_ = binary.Write(buf, binary.BigEndian, uint16(0))
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(pkt.ActionID, buf.Bytes()))
			return true
		case protocol.ActionIDItemSell:
			grid := int16(binary.BigEndian.Uint16(pkt.Payload[:2]))
			mu.Lock()
			soldGrids = append(soldGrids, grid)
			mu.Unlock()
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.ItemActionSuccess}))
			return true
		}
		return false
	})

	routine := dungeon.NewBagCleanupRoutine(dictRepo)
	if err := routine.Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("bag cleanup: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(soldGrids) != 1 || soldGrids[0] != 5 {
		t.Fatalf("只应出售装备制作卷(格位5), 实际出售: %v", soldGrids)
	}
}

// TestBagCleanup_SkipsWhenSpaceEnough 验证空格充足时不发任何出售请求。
func TestBagCleanup_SkipsWhenSpaceEnough(t *testing.T) {
	dictRepo, err := dictionary.NewRepository("")
	if err != nil {
		t.Fatalf("load dictionary: %v", err)
	}
	defer dictRepo.Close()

	sess, srvConn := createTestSession(t, 0)
	defer sess.Close()
	defer srvConn.Close()

	var sellCalls int
	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionIDItemGetPackList:
			buf := new(bytes.Buffer)
			_ = binary.Write(buf, binary.BigEndian, int16(30))
			_ = binary.Write(buf, binary.BigEndian, int32(20))
			_ = binary.Write(buf, binary.BigEndian, uint16(0))
			_ = binary.Write(buf, binary.BigEndian, int16(10))
			_ = binary.Write(buf, binary.BigEndian, uint16(0))
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(pkt.ActionID, buf.Bytes()))
			return true
		case protocol.ActionIDItemSell:
			sellCalls++
			_ = protocol.WritePacket(srvConn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.ItemActionSuccess}))
			return true
		}
		return false
	})

	routine := dungeon.NewBagCleanupRoutine(dictRepo)
	if err := routine.Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("bag cleanup: %v", err)
	}
	if sellCalls != 0 {
		t.Fatalf("空格充足时不应出售, sell calls = %d", sellCalls)
	}
}
