// Package protocol 提供仙幽猎境 (Mod_MysticCapture_Base, Module 762) 协议编解码。
//
// @author Ateng
// @since 2026-11-07
package protocol

import (
	"fmt"
)

// 仙幽猎境动作编号 (Mod_MysticCapture_Base)。
const (
	// ActionIDMysticHuntClickEnter 点击进入猎境 (Action 0 -> 0x02FA0000)
	ActionIDMysticHuntClickEnter uint32 = 0x02FA0000
	// ActionIDMysticHuntEnter 进入猎境 (Action 1 -> 0x02FA0001)
	ActionIDMysticHuntEnter uint32 = 0x02FA0001
	// ActionIDMysticHuntCaptureSoul 捕捉魂 (Action 2 -> 0x02FA0002)
	ActionIDMysticHuntCaptureSoul uint32 = 0x02FA0002
	// ActionIDMysticHuntExit 退出猎境 (Action 3 -> 0x02FA0003)
	ActionIDMysticHuntExit uint32 = 0x02FA0003
	// ActionIDMysticHuntEntranceOpen 猎境入口开放状态 (Action 5 -> 0x02FA0005)
	ActionIDMysticHuntEntranceOpen uint32 = 0x02FA0005
)

// 仙幽猎境结果码 (Mod_MysticCapture_Base)。
const (
	// MysticHuntResultSuccess 成功
	MysticHuntResultSuccess uint8 = 0
	// MysticHuntResultMaxSouls 魂已达上限
	MysticHuntResultMaxSouls uint8 = 1
	// MysticHuntResultNoTimes 次数不足
	MysticHuntResultNoTimes uint8 = 2
	// MysticHuntResultFailed 失败
	MysticHuntResultFailed uint8 = 3
	// MysticHuntResultNotEnoughItem 道具不足
	MysticHuntResultNotEnoughItem uint8 = 4
	// MysticHuntResultWrongItemID 道具编号错误
	MysticHuntResultWrongItemID uint8 = 5
	// MysticHuntResultWrongHuntSoulID 魂编号错误
	MysticHuntResultWrongHuntSoulID uint8 = 6
	// MysticHuntResultAlreadyExit 已退出/不在猎境中
	MysticHuntResultAlreadyExit uint8 = 7
)

// BuildMysticHuntEntrancePacket 构造查询猎境入口开放状态封包。
//
// 线上实测该请求不需要 PrevAct，空载荷即可。
func BuildMysticHuntEntrancePacket() *Packet {
	return NewPacket(ActionIDMysticHuntEntranceOpen, nil)
}

// BuildMysticHuntEnterPacket 构造进入猎境封包 (空载荷，不带 PrevAct)。
func BuildMysticHuntEnterPacket() *Packet {
	return NewPacket(ActionIDMysticHuntEnter, nil)
}

// ParseMysticHuntEntranceResult 解析入口开放状态 (Int, 1=开放)。
func ParseMysticHuntEntranceResult(payload []byte) (int32, error) {
	r := NewReader(payload)
	open, err := r.ReadInt32()
	if err != nil {
		return 0, fmt.Errorf("%w: failed to read mystic hunt entrance: %v", ErrInvalidPayload, err)
	}
	return open, nil
}

// BuildMysticHuntEntranceResultPacket 构造入口状态回包 (测试与 Mock 使用)。
func BuildMysticHuntEntranceResultPacket(open int32) *Packet {
	w := NewWriter()
	w.WriteInt32(open)
	return NewPacket(ActionIDMysticHuntEntranceOpen, w.Bytes())
}

// ParseMysticHuntEnterResult 解析进入猎境结果码。
func ParseMysticHuntEnterResult(payload []byte) (uint8, error) {
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return 0, fmt.Errorf("%w: failed to read mystic hunt enter result: %v", ErrInvalidPayload, err)
	}
	return result, nil
}

// BuildMysticHuntEnterResultPacket 构造进入猎境回包 (测试与 Mock 使用)。
func BuildMysticHuntEnterResultPacket(result uint8) *Packet {
	return NewPacket(ActionIDMysticHuntEnter, []byte{result})
}