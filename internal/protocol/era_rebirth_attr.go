// Package protocol 提供修真境界礼包 (Mod_EraRebirthLandAttr_Base, Module 851 / 0x353) 协议。
//
// 抓包基准: /tmp/henduo.pcapng（2026-11-14，qq s0）。
//
//	0 get_now_season         [Int]
//	1 get_panel_info         属性面板
//	4 resource_package       [[Int,Int] 时间道具列表, Int auto_level]
//	5 use_time_item          [Int item_id, Int item_num] → [UByte result,[Int,Int]]
//
// 所有请求尾部带 4 字节 PrevAct。
package protocol

import "fmt"

const (
	// ActionIDEraRebirthAttrSeason 当前赛季 (851/0)
	ActionIDEraRebirthAttrSeason uint32 = 0x03530000
	// ActionIDEraRebirthAttrPanel 属性面板 (851/1)
	ActionIDEraRebirthAttrPanel uint32 = 0x03530001
	// ActionIDEraRebirthAttrResourcePackage 资源礼包/时间道具 (851/4)
	ActionIDEraRebirthAttrResourcePackage uint32 = 0x03530004
	// ActionIDEraRebirthAttrUseTimeItem 使用时间道具 (851/5)
	ActionIDEraRebirthAttrUseTimeItem uint32 = 0x03530005
)

// 851 结果码。
const (
	EraRebirthAttrSuccess       uint8 = 0
	EraRebirthAttrFuncNotOpen   uint8 = 1
	EraRebirthAttrAlreadyStart  uint8 = 2
	EraRebirthAttrFailed        uint8 = 3
	EraRebirthAttrNotEnoughItem uint8 = 4
	EraRebirthAttrMissionLimit  uint8 = 5
)

// EraRebirthTimeItem 表示修真境界礼包中的一个时间道具。
type EraRebirthTimeItem struct {
	ItemID int32
	Num    int32
}

func eraRebirthAttrPrevPacket(actionID uint32, prefix []byte) *Packet {
	payload := make([]byte, 0, len(prefix)+4)
	payload = append(payload, prefix...)
	payload = append(payload, 0, 0, 0, 0)
	pkt := NewPacket(actionID, payload)
	pkt.HasPrevAct = true
	return pkt
}

// BuildEraRebirthAttrSeasonPacket 构造当前赛季查询 (851/0)。
func BuildEraRebirthAttrSeasonPacket() *Packet {
	return eraRebirthAttrPrevPacket(ActionIDEraRebirthAttrSeason, nil)
}

// BuildEraRebirthAttrPanelPacket 构造属性面板查询 (851/1)。
func BuildEraRebirthAttrPanelPacket() *Packet {
	return eraRebirthAttrPrevPacket(ActionIDEraRebirthAttrPanel, nil)
}

// BuildEraRebirthAttrResourcePackagePacket 构造资源礼包查询 (851/4)。
func BuildEraRebirthAttrResourcePackagePacket() *Packet {
	return eraRebirthAttrPrevPacket(ActionIDEraRebirthAttrResourcePackage, nil)
}

// BuildEraRebirthAttrUseTimeItemPacket 构造使用时间道具 (851/5)。
func BuildEraRebirthAttrUseTimeItemPacket(itemID, num int32) *Packet {
	w := NewWriter()
	w.WriteInt32(itemID)
	w.WriteInt32(num)
	return eraRebirthAttrPrevPacket(ActionIDEraRebirthAttrUseTimeItem, w.Bytes())
}

// ParseEraRebirthAttrTimeItems 解析 851/4 的时间道具列表。
func ParseEraRebirthAttrTimeItems(payload []byte) ([]EraRebirthTimeItem, int32, error) {
	p := DecompressPayloadIfNeeded(payload)
	r := NewReader(p)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, 0, fmt.Errorf("%w: era rebirth time item count: %v", ErrInvalidPayload, err)
	}
	items := make([]EraRebirthTimeItem, 0, count)
	for i := 0; i < int(count); i++ {
		var it EraRebirthTimeItem
		if it.ItemID, err = r.ReadInt32(); err != nil {
			return nil, 0, fmt.Errorf("%w: era rebirth time item[%d] id: %v", ErrInvalidPayload, i, err)
		}
		if it.Num, err = r.ReadInt32(); err != nil {
			return nil, 0, fmt.Errorf("%w: era rebirth time item[%d] num: %v", ErrInvalidPayload, i, err)
		}
		items = append(items, it)
	}
	autoLevel, err := r.ReadInt32()
	if err != nil {
		return nil, 0, fmt.Errorf("%w: era rebirth auto_level: %v", ErrInvalidPayload, err)
	}
	return items, autoLevel, nil
}

// ParseEraRebirthAttrUseResult 解析 851/5 回包首字节结果码。
func ParseEraRebirthAttrUseResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty era rebirth use result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// EraRebirthAttrResultText 返回 851 结果码文本。
func EraRebirthAttrResultText(code uint8) string {
	switch code {
	case EraRebirthAttrSuccess:
		return "成功"
	case EraRebirthAttrFuncNotOpen:
		return "功能未开放"
	case EraRebirthAttrAlreadyStart:
		return "已开始"
	case EraRebirthAttrFailed:
		return "失败"
	case EraRebirthAttrNotEnoughItem:
		return "道具不足"
	case EraRebirthAttrMissionLimit:
		return "任务次数上限"
	default:
		return "未知结果码"
	}
}

// BuildEraRebirthAttrTimeItemsResultPacket 构造 851/4 回包，供离线 mock 与单测使用。
func BuildEraRebirthAttrTimeItemsResultPacket(items []EraRebirthTimeItem, autoLevel int32) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(items)))
	for _, it := range items {
		w.WriteInt32(it.ItemID)
		w.WriteInt32(it.Num)
	}
	w.WriteInt32(autoLevel)
	return NewPacket(ActionIDEraRebirthAttrResourcePackage, w.Bytes())
}

// BuildEraRebirthAttrUseResultPacket 构造 851/5 回包，供离线 mock 与单测使用。
func BuildEraRebirthAttrUseResultPacket(result uint8, itemID, num int32) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	w.WriteInt32(itemID)
	w.WriteInt32(num)
	return NewPacket(ActionIDEraRebirthAttrUseTimeItem, w.Bytes())
}

// ParseEraRebirthAttrPanelAutoUse 解析 851/1 属性面板中的
// is_auto_use_time_item 开关（官方客户端只有该开关为 1 时才自动使用时间道具）。
//
// 线格式 (Mod_EraRebirthLandAttr_Base.get_attr_panel):
// Int is_start_func, Int last_rebirth_rate,
// [UInt16 n, n×(Int,Int)] rebirth_attr_list,
// Int level, Long season_exp, Long season_coin, Long season_dan,
// Int mission_level, [UInt16 n, n×(Int,Int)] attr_list,
// Int is_auto_use_time_item, Int player_level, Int attr_rate, Int next_attr_rate。
func ParseEraRebirthAttrPanelAutoUse(payload []byte) (int32, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	var err error
	if _, err = r.ReadInt32(); err != nil { // is_start_func
		return 0, fmt.Errorf("%w: era rebirth attr is_start_func: %v", ErrInvalidPayload, err)
	}
	if _, err = r.ReadInt32(); err != nil { // last_rebirth_rate
		return 0, fmt.Errorf("%w: era rebirth attr last_rate: %v", ErrInvalidPayload, err)
	}
	if err = skipEraRebirthAttrPairs(r); err != nil { // rebirth_attr_list
		return 0, err
	}
	if _, err = r.ReadInt32(); err != nil { // level
		return 0, fmt.Errorf("%w: era rebirth attr level: %v", ErrInvalidPayload, err)
	}
	for i := 0; i < 3; i++ { // season_exp / season_coin / season_dan
		if _, err = r.ReadInt64(); err != nil {
			return 0, fmt.Errorf("%w: era rebirth attr season value[%d]: %v", ErrInvalidPayload, i, err)
		}
	}
	if _, err = r.ReadInt32(); err != nil { // mission_level
		return 0, fmt.Errorf("%w: era rebirth attr mission_level: %v", ErrInvalidPayload, err)
	}
	if err = skipEraRebirthAttrPairs(r); err != nil { // attr_list
		return 0, err
	}
	flag, err := r.ReadInt32()
	if err != nil {
		return 0, fmt.Errorf("%w: era rebirth attr is_auto_use_time_item: %v", ErrInvalidPayload, err)
	}
	return flag, nil
}

func skipEraRebirthAttrPairs(r *Reader) error {
	n, err := r.ReadUint16()
	if err != nil {
		return fmt.Errorf("%w: era rebirth attr list count: %v", ErrInvalidPayload, err)
	}
	for i := 0; i < int(n); i++ {
		if _, err := r.ReadInt32(); err != nil {
			return fmt.Errorf("%w: era rebirth attr list[%d].a: %v", ErrInvalidPayload, i, err)
		}
		if _, err := r.ReadInt32(); err != nil {
			return fmt.Errorf("%w: era rebirth attr list[%d].b: %v", ErrInvalidPayload, i, err)
		}
	}
	return nil
}

// BuildEraRebirthAttrPanelResultPacket 构造 851/1 回包，供离线 mock 与单测使用。
func BuildEraRebirthAttrPanelResultPacket(isAutoUseTimeItem int32) *Packet {
	w := NewWriter()
	w.WriteInt32(0)  // is_start_func
	w.WriteInt32(0)  // last_rebirth_rate
	w.WriteUint16(0) // rebirth_attr_list
	w.WriteInt32(0)  // level
	w.WriteInt64(0)  // season_exp
	w.WriteInt64(0)  // season_coin
	w.WriteInt64(0)  // season_dan
	w.WriteInt32(0)  // mission_level
	w.WriteUint16(0) // attr_list
	w.WriteInt32(isAutoUseTimeItem)
	w.WriteInt32(0) // player_level
	w.WriteInt32(0) // attr_rate
	w.WriteInt32(0) // next_attr_rate
	return NewPacket(ActionIDEraRebirthAttrPanel, w.Bytes())
}
