// Package protocol 提供灵霄宝殿炼宝炉协议 (Mod_RefinePot_Base, Module 654)。
//
//	from_item_list   654/2 request=[] response=[UByte result, [Int item_id, Int count]]
//	select_from_item 654/3 request=[Int item_id, Int count] response=[UByte, Int, Int, [Int×4]]
//	refine           654/4 request=[] response=[UByte result, UByte, [Int×7], [Int×5]]
//	get_refine_award 654/6 request=[] response=[UByte, [Int,Int]]
package protocol

import "fmt"

const (
	ActionIDRefinePotFromItems uint32 = 0x028E0002
	ActionIDRefinePotSelect    uint32 = 0x028E0003
	ActionIDRefinePotRefine    uint32 = 0x028E0004
	ActionIDRefinePotAward     uint32 = 0x028E0006
)

// 炼宝炉结果码。
const (
	RefinePotResultSuccess      uint8 = 0
	RefinePotResultFailed       uint8 = 1
	RefinePotResultNoFromItem   uint8 = 2
	RefinePotResultNoRefineItem uint8 = 3
	RefinePotResultPackFull     uint8 = 10
)

// RefinePotMaterial 表示一种炼制材料。
type RefinePotMaterial struct {
	ItemID int32
	Count  int32
}

// BuildRefinePotFromItemsPacket 构造材料列表查询封包。
func BuildRefinePotFromItemsPacket() *Packet {
	pkt := NewPacket(ActionIDRefinePotFromItems, []byte{0, 0, 0, 0})
	pkt.HasPrevAct = true
	return pkt
}

// BuildRefinePotSelectPacket 构造选择材料封包。
func BuildRefinePotSelectPacket(itemID, count int32) *Packet {
	w := NewWriter()
	w.WriteInt32(itemID)
	w.WriteInt32(count)
	return NewPacket(ActionIDRefinePotSelect, w.Bytes())
}

// BuildRefinePotRefinePacket 构造炼制封包。
func BuildRefinePotRefinePacket() *Packet {
	return NewPacket(ActionIDRefinePotRefine, nil)
}

// BuildRefinePotAwardPacket 构造领取炼制奖励封包。
func BuildRefinePotAwardPacket() *Packet {
	pkt := NewPacket(ActionIDRefinePotAward, []byte{0, 0, 0, 0})
	pkt.HasPrevAct = true
	return pkt
}

// ParseRefinePotMaterials 解析材料列表。
func ParseRefinePotMaterials(payload []byte) (uint8, []RefinePotMaterial, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	result, err := r.ReadUint8()
	if err != nil {
		return 0, nil, fmt.Errorf("炼宝炉材料结果码读取失败: %w", err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return result, nil, fmt.Errorf("炼宝炉材料数量读取失败: %w", err)
	}
	materials := make([]RefinePotMaterial, 0, count)
	for i := 0; i < int(count); i++ {
		var m RefinePotMaterial
		if m.ItemID, err = r.ReadInt32(); err != nil {
			return result, nil, fmt.Errorf("炼宝炉材料[%d].item_id 读取失败: %w", i, err)
		}
		if m.Count, err = r.ReadInt32(); err != nil {
			return result, nil, fmt.Errorf("炼宝炉材料[%d].count 读取失败: %w", i, err)
		}
		materials = append(materials, m)
	}
	return result, materials, nil
}

// ParseRefinePotResult 解析单字节结果码。
func ParseRefinePotResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("炼宝炉结果码为空")
	}
	return payload[0], nil
}
