// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// @author Ateng
// @since 2026-10-22
package protocol_test

import (
	"bytes"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestItemProtocol_PackListCodec(t *testing.T) {
	// 构造测试数据
	reqPkt := protocol.BuildGetPackItemListPacket()
	if reqPkt.ActionID != protocol.ActionIDItemGetPackList {
		t.Fatalf("expected action ID 0x%08X, got 0x%08X", protocol.ActionIDItemGetPackList, reqPkt.ActionID)
	}

	buf := new(bytes.Buffer)
	// PackCapacity: 30
	buf.Write([]byte{0x00, 0x1E})
	// PackEmptyGrids: 28
	buf.Write([]byte{0x00, 0x00, 0x00, 0x1C})
	// PackItems count: 2
	buf.Write([]byte{0x00, 0x02})

	// Item 1: 包子 (ItemID 1488, GridID 1)
	item1 := protocol.ItemRecord{
		PlayerItemID: 10001,
		ItemID:       protocol.ItemIDSteamedBun,
		GridID:       1,
		ItemType:     20000,
	}
	if err := protocol.WriteItemRecord(buf, item1); err != nil {
		t.Fatalf("write item1 failed: %v", err)
	}

	// Item 2: 粽子 (ItemID 1444, GridID 2)
	item2 := protocol.ItemRecord{
		PlayerItemID: 10002,
		ItemID:       protocol.ItemIDRiceDumpling,
		GridID:       2,
		ItemType:     20000,
	}
	if err := protocol.WriteItemRecord(buf, item2); err != nil {
		t.Fatalf("write item2 failed: %v", err)
	}

	// WarehouseCapacity: 10
	buf.Write([]byte{0x00, 0x0A})
	// WarehouseItems count: 0
	buf.Write([]byte{0x00, 0x00})

	resp, err := protocol.ParsePackItemListResponse(buf.Bytes())
	if err != nil {
		t.Fatalf("parse pack item list failed: %v", err)
	}

	if resp.PackCapacity != 30 {
		t.Errorf("expected pack capacity 30, got %d", resp.PackCapacity)
	}
	if resp.PackEmptyGrids != 28 {
		t.Errorf("expected empty grids 28, got %d", resp.PackEmptyGrids)
	}
	if len(resp.PackItems) != 2 {
		t.Fatalf("expected 2 pack items, got %d", len(resp.PackItems))
	}
	if resp.PackItems[0].ItemID != protocol.ItemIDSteamedBun || resp.PackItems[0].GridID != 1 {
		t.Errorf("unexpected item 1: %+v", resp.PackItems[0])
	}
	if resp.PackItems[1].ItemID != protocol.ItemIDRiceDumpling || resp.PackItems[1].GridID != 2 {
		t.Errorf("unexpected item 2: %+v", resp.PackItems[1])
	}
}

func TestItemProtocol_UseGridItem(t *testing.T) {
	pkt, err := protocol.BuildUseGridItemPacket(5)
	if err != nil {
		t.Fatalf("build use grid packet failed: %v", err)
	}
	if pkt.ActionID != protocol.ActionIDItemUseGrid {
		t.Errorf("expected action ID 0x%08X, got 0x%08X", protocol.ActionIDItemUseGrid, pkt.ActionID)
	}
	if len(pkt.Payload) != 2 || pkt.Payload[0] != 0 || pkt.Payload[1] != 5 {
		t.Errorf("unexpected payload: %v", pkt.Payload)
	}

	// 验证成功响应 (20)
	ok, code, err := protocol.ParseUseGridItemResponse([]byte{20})
	if err != nil || !ok || code != 20 {
		t.Errorf("expected success with 20, got ok=%v, code=%d, err=%v", ok, code, err)
	}

	// 验证成功响应 (0)
	ok, code, err = protocol.ParseUseGridItemResponse([]byte{0})
	if err != nil || !ok || code != 0 {
		t.Errorf("expected success with 0, got ok=%v, code=%d, err=%v", ok, code, err)
	}

	// 验证失败响应 (143)
	ok, code, err = protocol.ParseUseGridItemResponse([]byte{143})
	if err != nil || ok || code != 143 {
		t.Errorf("expected failure with 143, got ok=%v, code=%d, err=%v", ok, code, err)
	}

	// 验证超短响应
	_, _, err = protocol.ParseUseGridItemResponse([]byte{})
	if err == nil {
		t.Error("expected error for empty payload")
	}
}
