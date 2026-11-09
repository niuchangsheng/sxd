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

// 灵霄宝殿炼宝炉动作 ID (Mod_RefineFurnace_Base, Module 586)。
//
//	open_panel   586/1 request=[] response=[UByte result, Int exp, Int level, Int selected,
//	             [Int item,Int count], [Int product,Int num,Int time], [Int...], Byte, Int, Int]
//	select_type  586/2 request=[Int type]
//	select_air   586/3 request=[Int air_id]
//	start_refine 586/4 request=[Int batches]（每批消耗 50 元气）
//	stop_refine  586/5 request=[]
//	get_award    586/6 request=[]
const (
	ActionIDRefineFurnacePanel uint32 = 0x024A0001
	ActionIDRefineFurnaceType  uint32 = 0x024A0002
	ActionIDRefineFurnaceAir   uint32 = 0x024A0003
	ActionIDRefineFurnaceStart uint32 = 0x024A0004
	ActionIDRefineFurnaceStop  uint32 = 0x024A0005
	ActionIDRefineFurnaceAward uint32 = 0x024A0006
)

// 炼宝炉结果码。
const (
	RefineFurnaceSuccess     uint8 = 0
	RefineFurnaceFailed      uint8 = 1
	RefineFurnaceNotComplete uint8 = 2
	RefineFurnaceLessAir     uint8 = 3
	RefineFurnaceLessCoin    uint8 = 4
	RefineFurnaceNotGetAward uint8 = 5
	RefineFurnacePackFull    uint8 = 6
)

// RefineFurnaceMaterial 表示一种元气材料及持有数量。
type RefineFurnaceMaterial struct {
	ItemID int32
	Count  int32
}

// RefineFurnaceProduct 表示一次炼制产出 (产出数量、批数、完成时间)。
type RefineFurnaceProduct struct {
	Num     int32
	Batch   int32
	Created int32
}

// RefineFurnacePanel 表示炼宝炉面板。
type RefineFurnacePanel struct {
	Result    uint8
	Exp       int32
	Level     int32
	Selected  int32
	Materials []RefineFurnaceMaterial
	Products  []RefineFurnaceProduct
}

// BuildRefineFurnacePanelPacket 构造炼宝炉面板查询封包。
func BuildRefineFurnacePanelPacket() *Packet {
	return NewPacket(ActionIDRefineFurnacePanel, nil)
}

// BuildRefineFurnaceTypePacket 构造选择元气类型封包。
func BuildRefineFurnaceTypePacket(typeID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(typeID)
	return NewPacket(ActionIDRefineFurnaceType, w.Bytes())
}

// BuildRefineFurnaceStartPacket 构造炼制封包 (batches 为批数，每批 50 元气)。
func BuildRefineFurnaceStartPacket(batches int32) *Packet {
	w := NewWriter()
	w.WriteInt32(batches)
	return NewPacket(ActionIDRefineFurnaceStart, w.Bytes())
}

// BuildRefineFurnaceAwardPacket 构造领取炼制产物封包。
func BuildRefineFurnaceAwardPacket() *Packet {
	return NewPacket(ActionIDRefineFurnaceAward, nil)
}

// ParseRefineFurnacePanel 解析炼宝炉面板。
func ParseRefineFurnacePanel(payload []byte) (*RefineFurnacePanel, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	p := &RefineFurnacePanel{}
	var err error
	if p.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("炼宝炉结果码读取失败: %w", err)
	}
	if p.Exp, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("炼宝炉经验读取失败: %w", err)
	}
	if p.Level, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("炼宝炉等级读取失败: %w", err)
	}
	if p.Selected, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("炼宝炉选中材料读取失败: %w", err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return p, nil
	}
	for i := 0; i < int(count); i++ {
		var m RefineFurnaceMaterial
		if m.ItemID, err = r.ReadInt32(); err != nil {
			return p, fmt.Errorf("炼宝炉材料[%d]读取失败: %w", i, err)
		}
		if m.Count, err = r.ReadInt32(); err != nil {
			return p, fmt.Errorf("炼宝炉材料[%d]数量读取失败: %w", i, err)
		}
		p.Materials = append(p.Materials, m)
	}
	count, err = r.ReadUint16()
	if err != nil {
		return p, nil
	}
	for i := 0; i < int(count); i++ {
		var prod RefineFurnaceProduct
		if prod.Num, err = r.ReadInt32(); err != nil {
			return p, nil
		}
		if prod.Batch, err = r.ReadInt32(); err != nil {
			return p, nil
		}
		if prod.Created, err = r.ReadInt32(); err != nil {
			return p, nil
		}
		p.Products = append(p.Products, prod)
	}
	return p, nil
}
