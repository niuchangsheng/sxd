// Package protocol 提供灵契之地 (Mod_LandContract_Base, Module 613 / 0x265) 协议。
//
// 抓包基准: /tmp/lingqizhidi.pcapng（2026-11-15，qq s0）：
//
//	1 get_panel_info  面板 [Int,Int,Long,Int,Int,Int,Int]
//	8 fight_boss      挑战（无参数）→ [UByte result, 战报]
//
// 结果码：3 成功、4 背包满、5 未完成、8 次数不足、9 非挑战时间、10 冷却、11 失败。
// 禁止调用 4 buy_fight_num（元宝购买次数）。
package protocol

import "fmt"

const (
	// ActionIDLandContractPanel 灵契之地面板 (613/1)
	ActionIDLandContractPanel uint32 = 0x02650001
	// ActionIDLandContractFight 挑战 Boss (613/8)
	ActionIDLandContractFight uint32 = 0x02650008
)

// 613 结果码。
const (
	LandContractNotCompleted uint8 = 0
	LandContractCanGain      uint8 = 1
	LandContractHasGain      uint8 = 2
	LandContractSuccess      uint8 = 3
	LandContractPackFull     uint8 = 4
	LandContractNotFinish    uint8 = 5
	LandContractFaild        uint8 = 6
	LandContractNoIngot      uint8 = 7
	LandContractNoFightCount uint8 = 8
	LandContractNotFightTime uint8 = 9
	LandContractCDTime       uint8 = 10
	LandContractFailed       uint8 = 11
)

// LandContractPanel 表示灵契之地面板关键字段。
type LandContractPanel struct {
	Values []int32
	Exp    int64
}

func landContractPrevPacket(actionID uint32) *Packet {
	pkt := NewPacket(actionID, []byte{0, 0, 0, 0})
	pkt.HasPrevAct = true
	return pkt
}

// BuildLandContractPanelPacket 构造面板查询 (613/1)。
func BuildLandContractPanelPacket() *Packet {
	return landContractPrevPacket(ActionIDLandContractPanel)
}

// BuildLandContractFightPacket 构造挑战请求 (613/8)。
func BuildLandContractFightPacket() *Packet {
	return landContractPrevPacket(ActionIDLandContractFight)
}

// ParseLandContractPanel 解析 613/1：[Int,Int,Long,Int,Int,Int,Int]。
func ParseLandContractPanel(payload []byte) (*LandContractPanel, error) {
	p := DecompressPayloadIfNeeded(payload)
	r := NewReader(p)
	out := &LandContractPanel{}
	var err error
	for i := 0; i < 2; i++ {
		v, e := r.ReadInt32()
		if e != nil {
			return nil, fmt.Errorf("%w: land contract int[%d]: %v", ErrInvalidPayload, i, e)
		}
		out.Values = append(out.Values, v)
	}
	if out.Exp, err = r.ReadInt64(); err != nil {
		return nil, fmt.Errorf("%w: land contract exp: %v", ErrInvalidPayload, err)
	}
	for i := 0; i < 4; i++ {
		v, e := r.ReadInt32()
		if e != nil {
			return nil, fmt.Errorf("%w: land contract int[%d]: %v", ErrInvalidPayload, i+2, e)
		}
		out.Values = append(out.Values, v)
	}
	return out, nil
}

// ParseLandContractResult 解析单字节结果码。
func ParseLandContractResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty land contract result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// LandContractResultText 返回 613 结果码文本。
func LandContractResultText(code uint8) string {
	switch code {
	case LandContractSuccess:
		return "成功"
	case LandContractPackFull:
		return "背包已满"
	case LandContractNotFinish:
		return "未完成"
	case LandContractNoFightCount:
		return "挑战次数不足"
	case LandContractNotFightTime:
		return "非挑战时间"
	case LandContractCDTime:
		return "冷却中"
	case LandContractFailed:
		return "失败"
	case LandContractFaild:
		return "失败"
	case LandContractNoIngot:
		return "元宝不足"
	default:
		return "未知结果码"
	}
}

// BuildLandContractPanelResultPacket 构造面板回包，供离线 mock 与单测使用。
func BuildLandContractPanelResultPacket(v [7]int32) *Packet {
	w := NewWriter()
	w.WriteInt32(v[0])
	w.WriteInt32(v[1])
	w.WriteInt64(int64(v[2]))
	for i := 3; i < 7; i++ {
		w.WriteInt32(v[i])
	}
	return NewPacket(ActionIDLandContractPanel, w.Bytes())
}
