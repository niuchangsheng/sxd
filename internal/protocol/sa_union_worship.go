// Package protocol 提供圣盟膜拜/万古神墟协议 (Mod_SaUnionWorship_Base, Module 419)。
//
//	open_panel 419/1 request=[] response=[Int×11]
//	worship    419/2 request=[UByte type] response=[UByte result, [Int,Int]]
//	type: COIN=0 铜钱/免费；INGOT=1 元宝（自动化禁用）。
package protocol

import "fmt"

const (
	// ActionIDSaUnionWorshipPanel 圣盟膜拜面板 (419/1 -> 0x01A30001)
	ActionIDSaUnionWorshipPanel uint32 = 0x01A30001
	// ActionIDSaUnionWorship 圣盟膜拜 (419/2 -> 0x01A30002)
	ActionIDSaUnionWorship uint32 = 0x01A30002
)

const (
	SaUnionWorshipUseCoin  uint8 = 0
	SaUnionWorshipUseIngot uint8 = 1
)

// 圣盟膜拜结果码。
const (
	SaUnionWorshipResultSuccess   uint8 = 2
	SaUnionWorshipResultNoCoin    uint8 = 3
	SaUnionWorshipResultNoIngot   uint8 = 4
	SaUnionWorshipResultNoTime    uint8 = 5
	SaUnionWorshipResultPackFull  uint8 = 6
	SaUnionWorshipResultJoinToday uint8 = 7
	SaUnionWorshipResultFailed    uint8 = 8
	SaUnionWorshipResultIsVoted   uint8 = 9
)

// BuildSaUnionWorshipPanelPacket 构造面板查询封包。
func BuildSaUnionWorshipPanelPacket() *Packet {
	pkt := NewPacket(ActionIDSaUnionWorshipPanel, []byte{0, 0, 0, 0})
	pkt.HasPrevAct = true
	return pkt
}

// BuildSaUnionWorshipPanelPacketWithPrev 构造带显式 PrevAct 的面板查询封包。
//
// 线上实测空载荷会被静默丢弃，必须携带 4 字节 PrevAct。
func BuildSaUnionWorshipPanelPacketWithPrev(prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDSaUnionWorshipPanel, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// BuildSaUnionWorshipPacket 构造膜拜封包。
func BuildSaUnionWorshipPacket(useType uint8) *Packet {
	return NewPacket(ActionIDSaUnionWorship, []byte{useType})
}

// ParseSaUnionWorshipResult 解析膜拜结果。
func ParseSaUnionWorshipResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("圣盟膜拜结果码为空")
	}
	return payload[0], nil
}

// SaUnionWorshipResultText 返回结果码文本。
func SaUnionWorshipResultText(code uint8) string {
	switch code {
	case SaUnionWorshipResultSuccess:
		return "成功"
	case SaUnionWorshipResultNoCoin:
		return "铜钱不足"
	case SaUnionWorshipResultNoIngot:
		return "元宝不足"
	case SaUnionWorshipResultNoTime:
		return "次数不足"
	case SaUnionWorshipResultPackFull:
		return "背包已满"
	case SaUnionWorshipResultJoinToday:
		return "今日已参与"
	case SaUnionWorshipResultFailed:
		return "失败"
	case SaUnionWorshipResultIsVoted:
		return "已膜拜"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}
