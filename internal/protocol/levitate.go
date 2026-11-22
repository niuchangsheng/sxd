// Package protocol 提供飞升 (Mod_Levitate_Base, Module 357 / 0x165) 协议。
//
// 抓包基准: /tmp/feisheng.pcapng（2026-11-15，qq s0）：
//
//	0 get_levitate_info [UByte type]
//	1 perfusion         [UByte type] → [UByte result, [Int,Int]]
//
// type: 0=主角(MAINROLE)、1=伙伴(ROLE)。
//
// 结果码：2 成功、3 已灌注完(USED_UP)、4 功能未开放、5 已满、7 材料不足、
// 8 飞升上限、9 灵智不足、10 上限、12/13 阶段上限。
package protocol

import "fmt"

const (
	// ActionIDLevitateInfo 飞升信息 (357/0)
	ActionIDLevitateInfo uint32 = 0x01650000
	// ActionIDLevitatePerfusion 灌注经验 (357/1)
	ActionIDLevitatePerfusion uint32 = 0x01650001
	// ActionIDLevitate 飞升 (357/2)
	//
	// 请求 [UByte type, Int is_use_shen_lian_zhu]：useShenLianZhu=1 表示材料不足时
	// 勾选使用神炼石替代（客户端字符串 isUseShenLianZhu）。回包 [UByte result, Int]。
	ActionIDLevitate uint32 = 0x01650002
)

// 灌注对象类型。
const (
	LevitateTypeMainRole uint8 = 0
	LevitateTypeRole     uint8 = 1
)

// 357 结果码。
const (
	LevitateSuccess          uint8 = 2
	LevitateUsedUp           uint8 = 3
	LevitateFuncNoOpen       uint8 = 4
	LevitateFull             uint8 = 5
	LevitateStepLimit        uint8 = 6
	LevitateNoEnoughMaterial uint8 = 7
	LevitateLevitateLimit    uint8 = 8
	LevitateNoEnoughLingZhi  uint8 = 9
	LevitateLimit            uint8 = 10
	LevitateAvailableLingZhi uint8 = 11
	LevitateMaxStepLimit     uint8 = 12
	LevitateStepLimit2       uint8 = 13
)

// BuildLevitateInfoPacket 构造飞升信息查询 (357/0)。
func BuildLevitateInfoPacket(typ uint8) *Packet {
	return levitatePrevPacket(ActionIDLevitateInfo, typ)
}

// BuildLevitatePerfusionPacket 构造灌注经验 (357/1)。
func BuildLevitatePerfusionPacket(typ uint8) *Packet {
	return levitatePrevPacket(ActionIDLevitatePerfusion, typ)
}

func levitatePrevPacket(actionID uint32, typ uint8) *Packet {
	w := NewWriter()
	w.WriteUint8(typ)
	w.WriteUint32(0) // PrevAct 占位，由会话层覆盖
	pkt := NewPacket(actionID, w.Bytes())
	pkt.PrevActFromLastSent = true
	return pkt
}

// LevitateMaterial 表示飞升阶段材料需求中的一项（item 3425 为主角经验，
// 1747 为铜钱，其余为飞升材料）。
type LevitateMaterial struct {
	ItemID    int32
	Available int64
	Need      int64
}

// LevitateExpItem 表示灌注经验丹数量（5644 普通 / 5645 高级）。
type LevitateExpItem struct {
	ItemID int32
	Num    int32
}

// BuildLevitatePacket 构造飞升 (357/2)，useShenLianZhu=1 表示勾选神炼石替代材料。
func BuildLevitatePacket(typ uint8, useShenLianZhu int32) *Packet {
	w := NewWriter()
	w.WriteUint8(typ)
	w.WriteInt32(useShenLianZhu)
	w.WriteUint32(0) // PrevAct 占位，由会话层覆盖
	pkt := NewPacket(ActionIDLevitate, w.Bytes())
	pkt.PrevActFromLastSent = true
	return pkt
}

// LevitateActionResult 表示 357/2 飞升回包 [UByte result, Int]。
type LevitateActionResult struct {
	Result uint8
	Value  int32
}

// ParseLevitateActionResult 解析 357/2 回包。
func ParseLevitateActionResult(payload []byte) (*LevitateActionResult, error) {
	p := DecompressPayloadIfNeeded(payload)
	r := NewReader(p)
	out := &LevitateActionResult{}
	var err error
	if out.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: levitate result: %v", ErrInvalidPayload, err)
	}
	// 部分失败码只有 1 字节结果码，Value 缺失不视为解析失败。
	if out.Value, err = r.ReadInt32(); err != nil {
		return out, nil
	}
	return out, nil
}

// BuildLevitateActionResultPacket 构造 357/2 回包，供离线 mock 与单测使用。
func BuildLevitateActionResultPacket(result uint8, value int32) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	w.WriteInt32(value)
	return NewPacket(ActionIDLevitate, w.Bytes())
}

// LevitateInfo 表示 357/0 get_levitate_info 的关键字段。
//
// 线格式（Mod_Levitate_Base.get_levitate_info）：
//
//	Short step, Short avaliable_ling_zhi, Short max_ling_zhi,
//	[UInt16 n, n×(Int item_id, Long available, Long need)],
//	[UInt16 n, n×(Short,Short)],
//	Long level_limit_exp, Long perfusionable_exp,
//	[UInt16 n, n×(Int,Int)], Int shen_lian_shi_num
type LevitateInfo struct {
	Step             int16
	AvailableLingZhi int16
	MaxLingZhi       int16
	Materials        []LevitateMaterial
	LevelLimitExp    int64
	PerfusionableExp int64
	ExpItems         []LevitateExpItem
	ShenLianShiNum   int32
}

// 飞升阶段材料中的特殊 item id。
const (
	LevitateMaterialExpItemID  int32 = 3425 // 主角经验
	LevitateMaterialCoinItemID int32 = 1747 // 铜钱
	// LevitateMaxStep 飞升阶段上限；到 5 即已结束，等待飞升动作。
	LevitateMaxStep int16 = 5
)

// ParseLevitateInfo 解析 357/0 飞升信息。
func ParseLevitateInfo(payload []byte) (*LevitateInfo, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	out := &LevitateInfo{}
	var err error
	if out.Step, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: levitate step: %v", ErrInvalidPayload, err)
	}
	if out.AvailableLingZhi, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: levitate avaliable_ling_zhi: %v", ErrInvalidPayload, err)
	}
	if out.MaxLingZhi, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: levitate max_ling_zhi: %v", ErrInvalidPayload, err)
	}
	mcount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: levitate material count: %v", ErrInvalidPayload, err)
	}
	for i := 0; i < int(mcount); i++ {
		var m LevitateMaterial
		if m.ItemID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: levitate material[%d].item_id: %v", ErrInvalidPayload, i, err)
		}
		if m.Available, err = r.ReadInt64(); err != nil {
			return nil, fmt.Errorf("%w: levitate material[%d].available: %v", ErrInvalidPayload, i, err)
		}
		if m.Need, err = r.ReadInt64(); err != nil {
			return nil, fmt.Errorf("%w: levitate material[%d].need: %v", ErrInvalidPayload, i, err)
		}
		out.Materials = append(out.Materials, m)
	}
	acount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: levitate attr plus count: %v", ErrInvalidPayload, err)
	}
	for i := 0; i < int(acount); i++ {
		if _, err = r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("%w: levitate attr plus[%d].type: %v", ErrInvalidPayload, i, err)
		}
		if _, err = r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("%w: levitate attr plus[%d].count: %v", ErrInvalidPayload, i, err)
		}
	}
	if out.LevelLimitExp, err = r.ReadInt64(); err != nil {
		return nil, fmt.Errorf("%w: levitate level_limit_exp: %v", ErrInvalidPayload, err)
	}
	if out.PerfusionableExp, err = r.ReadInt64(); err != nil {
		return nil, fmt.Errorf("%w: levitate perfusionable_exp: %v", ErrInvalidPayload, err)
	}
	ecount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: levitate exp item count: %v", ErrInvalidPayload, err)
	}
	for i := 0; i < int(ecount); i++ {
		var it LevitateExpItem
		if it.ItemID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: levitate exp item[%d].item_id: %v", ErrInvalidPayload, i, err)
		}
		if it.Num, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: levitate exp item[%d].num: %v", ErrInvalidPayload, i, err)
		}
		out.ExpItems = append(out.ExpItems, it)
	}
	if out.ShenLianShiNum, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: levitate shen_lian_shi_num: %v", ErrInvalidPayload, err)
	}
	return out, nil
}

// LevitatePerfusionResult 表示灌注回包。
type LevitatePerfusionResult struct {
	Result uint8
	// Gains 每项 [Int,Int]（抓包为空列表）。
	Gains [][2]int32
}

// ParseLevitatePerfusionResult 解析 357/1：[UByte result, [Int,Int]]。
func ParseLevitatePerfusionResult(payload []byte) (*LevitatePerfusionResult, error) {
	p := DecompressPayloadIfNeeded(payload)
	if len(p) == 0 {
		return nil, fmt.Errorf("%w: empty levitate result", ErrInvalidPayload)
	}
	out := &LevitatePerfusionResult{Result: p[0]}
	r := NewReader(p[1:])
	count, err := r.ReadUint16()
	if err != nil {
		return out, nil
	}
	for i := 0; i < int(count); i++ {
		a, err := r.ReadInt32()
		if err != nil {
			break
		}
		b, err := r.ReadInt32()
		if err != nil {
			break
		}
		out.Gains = append(out.Gains, [2]int32{a, b})
	}
	return out, nil
}

// LevitateResultText 返回 357 结果码文本。
func LevitateResultText(code uint8) string {
	switch code {
	case LevitateSuccess:
		return "成功"
	case LevitateUsedUp:
		return "经验已灌注完"
	case LevitateFuncNoOpen:
		return "功能未开放"
	case LevitateFull:
		return "已满"
	case LevitateStepLimit:
		return "阶段上限"
	case LevitateNoEnoughMaterial:
		return "材料不足"
	case LevitateLevitateLimit:
		return "飞升次数上限"
	case LevitateNoEnoughLingZhi:
		return "灵智不足"
	case LevitateLimit:
		return "已达上限"
	case LevitateAvailableLingZhi:
		return "仍有可用灵智"
	case LevitateMaxStepLimit:
		return "最大阶段上限"
	case LevitateStepLimit2:
		return "阶段上限"
	default:
		return "未知结果码"
	}
}

// BuildLevitateInfoResultPacket 构造 357/0 回包，供离线 mock 与单测使用。
func BuildLevitateInfoResultPacket(info *LevitateInfo) *Packet {
	w := NewWriter()
	w.WriteInt16(info.Step)
	w.WriteInt16(info.AvailableLingZhi)
	w.WriteInt16(info.MaxLingZhi)
	w.WriteUint16(uint16(len(info.Materials)))
	for _, m := range info.Materials {
		w.WriteInt32(m.ItemID)
		w.WriteInt64(m.Available)
		w.WriteInt64(m.Need)
	}
	w.WriteUint16(0) // attr plus list
	w.WriteInt64(info.LevelLimitExp)
	w.WriteInt64(info.PerfusionableExp)
	w.WriteUint16(uint16(len(info.ExpItems)))
	for _, it := range info.ExpItems {
		w.WriteInt32(it.ItemID)
		w.WriteInt32(it.Num)
	}
	w.WriteInt32(info.ShenLianShiNum)
	return NewPacket(ActionIDLevitateInfo, w.Bytes())
}

// BuildLevitatePerfusionResultPacket 构造灌注回包，供离线 mock 与单测使用。
func BuildLevitatePerfusionResultPacket(result uint8, values []int32) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	w.WriteUint16(uint16((len(values) + 1) / 2))
	for i := 0; i+1 < len(values); i += 2 {
		w.WriteInt32(values[i])
		w.WriteInt32(values[i+1])
	}
	return NewPacket(ActionIDLevitatePerfusion, w.Bytes())
}
