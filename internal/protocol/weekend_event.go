// Package protocol 提供周末狂欢礼 (Mod_WeekendRevelryGiftPack_Base, 637) 与
// 周末水果机 (Mod_SundayFruit_Base, 242) 协议。
package protocol

import "fmt"

const (
	// ActionIDWeekendGiftPanel 周末狂欢礼面板 (637/0 -> 0x027D0000)
	ActionIDWeekendGiftPanel uint32 = 0x027D0000
	// ActionIDWeekendGiftAward 领取周末狂欢礼 (637/1 -> 0x027D0001)
	ActionIDWeekendGiftAward uint32 = 0x027D0001
	// ActionIDWeekendGiftBack 补领周末狂欢礼 (637/2 -> 0x027D0002)
	ActionIDWeekendGiftBack uint32 = 0x027D0002

	// ActionIDSundayFruitActive 水果机开放状态 (242/0 -> 0x00F20000)
	ActionIDSundayFruitActive uint32 = 0x00F20000
	// ActionIDSundayFruitInfo 水果机面板 (242/1 -> 0x00F20001)
	ActionIDSundayFruitInfo uint32 = 0x00F20001
	// ActionIDSundayFruitDraw 水果机抽奖 (242/2 -> 0x00F20002)
	ActionIDSundayFruitDraw uint32 = 0x00F20002
)

// 周末狂欢礼结果码。
const (
	WeekendGiftResultSuccess        uint8 = 0
	WeekendGiftResultFailed         uint8 = 1
	WeekendGiftResultHasGet         uint8 = 2
	WeekendGiftResultCannotGet      uint8 = 3
	WeekendGiftResultDayError       uint8 = 4
	WeekendGiftResultPackFull       uint8 = 6
	WeekendGiftResultNotOpen        uint8 = 13
	WeekendGiftResultNotEnoughIngot uint8 = 14
)

// 水果机结果码。
const (
	SundayFruitResultYes           uint8 = 0
	SundayFruitResultNo            uint8 = 1
	SundayFruitResultSuccess       uint8 = 2
	SundayFruitResultTimeUsed      uint8 = 3
	SundayFruitResultPackFull      uint8 = 4
	SundayFruitResultActiveNotOpen uint8 = 7
)

// BuildWeekendGiftPanelPacket 构造周末狂欢礼面板封包。
func BuildWeekendGiftPanelPacket() *Packet {
	pkt := NewPacket(ActionIDWeekendGiftPanel, []byte{0, 0, 0, 0})
	pkt.HasPrevAct = true
	return pkt
}

// BuildWeekendGiftAwardPacket 构造领取周末狂欢礼封包。
//
// 官方 request=[Int index]；线上实测追加 PrevAct 会被静默丢弃，
// 因此发送精确的 4 字节载荷。
func BuildWeekendGiftAwardPacket(index int32) *Packet {
	w := NewWriter()
	w.WriteInt32(index)
	return NewPacket(ActionIDWeekendGiftAward, w.Bytes())
}

// BuildSundayFruitActivePacket 构造水果机开放状态封包。
func BuildSundayFruitActivePacket() *Packet {
	pkt := NewPacket(ActionIDSundayFruitActive, []byte{0, 0, 0, 0})
	pkt.HasPrevAct = true
	return pkt
}

// BuildSundayFruitInfoPacket 构造水果机面板封包。
func BuildSundayFruitInfoPacket() *Packet {
	pkt := NewPacket(ActionIDSundayFruitInfo, []byte{0, 0, 0, 0})
	pkt.HasPrevAct = true
	return pkt
}

// BuildSundayFruitDrawPacket 构造水果机抽奖封包。
func BuildSundayFruitDrawPacket() *Packet {
	pkt := NewPacket(ActionIDSundayFruitDraw, []byte{0, 0, 0, 0})
	pkt.HasPrevAct = true
	return pkt
}

// ParseFirstByteResult 读取单字节结果码。
func ParseFirstByteResult(payload []byte, scene string) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%s 结果码为空", scene)
	}
	return payload[0], nil
}
