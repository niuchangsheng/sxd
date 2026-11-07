// Package protocol 提供宠物庭院/灵符召唤协议 (Mod_CourtyardPet_Base, Module 216)。
//
//	get_player_info 216/0 request=[] response=[Short talisman, Byte, Byte, Byte, Short, Byte, Byte]
//	call            216/1 request=[UByte type] response=[UByte result, Short, UByte]
//
// type: NORMAL=0 使用灵符（免费）；INGOT=1 使用元宝（自动化禁用）。
package protocol

import "fmt"

const (
	// ActionIDCourtyardPetInfo 查询宠物庭院玩家信息 (216/0 -> 0x00D80000)
	ActionIDCourtyardPetInfo uint32 = 0x00D80000
	// ActionIDCourtyardPetCall 灵符/元宝召唤宠物 (216/1 -> 0x00D80001)
	ActionIDCourtyardPetCall uint32 = 0x00D80001
)

// 宠物召唤消耗类型。
const (
	CourtyardPetUseNormal uint8 = 0
	CourtyardPetUseIngot  uint8 = 1
)

// 宠物庭院结果码。
const (
	CourtyardPetResultSuccess         uint8 = 2
	CourtyardPetResultFunctionNotOpen uint8 = 3
	CourtyardPetResultTimesUsedUp     uint8 = 4
	CourtyardPetResultNoEnoughIngot   uint8 = 5
	CourtyardPetResultNoEnoughPet     uint8 = 8
	CourtyardPetResultReachMax        uint8 = 9
	CourtyardPetResultNotEnoughCount  uint8 = 11
	CourtyardPetResultFailed          uint8 = 33
)

// CourtyardPetPlayerInfo 表示宠物庭院玩家信息。
type CourtyardPetPlayerInfo struct {
	// TalismanCount 灵符数量；仅该值 >0 时调用 call(NORMAL)。
	TalismanCount int16
}

// BuildCourtyardPetInfoPacket 构造查询宠物庭院信息封包。
func BuildCourtyardPetInfoPacket() *Packet {
	pkt := NewPacket(ActionIDCourtyardPetInfo, []byte{0, 0, 0, 0})
	pkt.HasPrevAct = true
	return pkt
}

// BuildCourtyardPetCallPacket 构造召唤宠物封包。
//
// 官方 request=[UByte type]；线上实测追加 4 字节 PrevAct 会被静默丢弃，
// 因此这里发送精确的 1 字节载荷。
func BuildCourtyardPetCallPacket(useType uint8) *Packet {
	return NewPacket(ActionIDCourtyardPetCall, []byte{useType})
}

// ParseCourtyardPetInfo 解析玩家信息回包，仅取灵符数量。
func ParseCourtyardPetInfo(payload []byte) (*CourtyardPetPlayerInfo, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	count, err := r.ReadInt16()
	if err != nil {
		return nil, fmt.Errorf("宠物庭院灵符数量读取失败: %w", err)
	}
	return &CourtyardPetPlayerInfo{TalismanCount: count}, nil
}

// ParseCourtyardPetResultCode 解析召唤回包首字节结果码。
func ParseCourtyardPetResultCode(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("宠物召唤结果码为空")
	}
	return payload[0], nil
}

// CourtyardPetResultText 返回结果码文本。
func CourtyardPetResultText(code uint8) string {
	switch code {
	case CourtyardPetResultSuccess:
		return "成功"
	case CourtyardPetResultFunctionNotOpen:
		return "功能未开放"
	case CourtyardPetResultTimesUsedUp:
		return "次数已用尽"
	case CourtyardPetResultNoEnoughIngot:
		return "元宝不足"
	case CourtyardPetResultNoEnoughPet:
		return "无可用宠物"
	case CourtyardPetResultReachMax:
		return "已达上限"
	case CourtyardPetResultNotEnoughCount:
		return "灵符不足"
	case CourtyardPetResultFailed:
		return "失败"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}
