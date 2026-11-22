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
