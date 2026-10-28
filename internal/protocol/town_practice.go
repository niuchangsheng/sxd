// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// @author Ateng
// @since 2026-10-28
package protocol

import (
	"fmt"
)

// TownPracticeData 纳管玩家在普通城镇中的打坐练功实时数据 (Mod_Player_Base.get_practice_data)。
type TownPracticeData struct {
	// Param1 保留参数 1 (Short)
	Param1 int16
	// Param2 保留参数 2 (Short)
	Param2 int16
	// PracticeTime 已练功累计时长 (秒)
	PracticeTime int32
	// PracticeWar 战斗/保护状态标识
	PracticeWar int32
	// PracticeExp 当前已累积待领取的练功经验
	PracticeExp int64
	// PracticeSpeedNum 今日剩余可加速次数
	PracticeSpeedNum uint8
	// PracticeExpOne 单位时间基础经验收益
	PracticeExpOne int32
	// SuperTownPracticeExp 圣域额外练功经验加成
	SuperTownPracticeExp int32
}

// TownPracticeNotify 服务端推送的城镇打坐练功状态变更与经验跳动通知 (Mod_Player_Base.practice_notify)。
type TownPracticeNotify struct {
	// NotifyType 通知类型 (89: UPDATE, 90: START, 91: FINISHED, 92: CANCEL)
	NotifyType uint8
	// Exp 经验增长量或当前经验数值
	Exp int32
	// SuperExp 圣域额外经验加成数值
	SuperExp int32
}

// BuildPlayerStartPracticePacket 构造角色开始城镇打坐练功请求封包 (Module 0, Action 36 -> 0x00000024)。
func BuildPlayerStartPracticePacket(prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDPlayerStartPractice, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// BuildPlayerGetPracticeDataPacket 构造获取城镇打坐练功实时数据请求封包 (Module 0, Action 37 -> 0x00000025)。
func BuildPlayerGetPracticeDataPacket(prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDPlayerGetPracticeData, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// BuildPlayerSignFinishedPracticePacket 构造确认结束练功并结算累积经验请求封包 (Module 0, Action 38 -> 0x00000026)。
func BuildPlayerSignFinishedPracticePacket(prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDPlayerSignFinishedPractice, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// BuildPlayerCancelPracticePacket 构造取消/中断城镇打坐练功请求封包 (Module 0, Action 39 -> 0x00000027)。
func BuildPlayerCancelPracticePacket(prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDPlayerCancelPractice, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// ParseTownPracticeDataResponse 反序列化城镇打坐练功数据响应载荷。
func ParseTownPracticeDataResponse(payload []byte) (*TownPracticeData, error) {
	decompressed, err := DecompressIfNeeded(payload)
	if err != nil {
		return nil, fmt.Errorf("town practice: 解压失败: %w", err)
	}

	r := NewReader(decompressed)
	// 基础结构至少需要: Short(2) + Short(2) + Int(4) + Int(4) + Long(8) + Byte(1) + Int(4) = 25 字节
	if r.Remaining() < 25 {
		return nil, fmt.Errorf("town practice: 数据载荷不足 (剩余 %d 字节, 最少需要 25 字节)", r.Remaining())
	}

	data := &TownPracticeData{}
	data.Param1, _ = r.ReadInt16()
	data.Param2, _ = r.ReadInt16()
	data.PracticeTime, _ = r.ReadInt32()
	data.PracticeWar, _ = r.ReadInt32()
	data.PracticeExp, _ = r.ReadInt64()
	data.PracticeSpeedNum, _ = r.ReadUint8()
	data.PracticeExpOne, _ = r.ReadInt32()

	if r.Remaining() >= 4 {
		data.SuperTownPracticeExp, _ = r.ReadInt32()
	}

	return data, nil
}

// BuildTownPracticeDataResponsePacket 构造城镇打坐练功数据响应封包 (通常供单测或 Mock 网关使用)。
func BuildTownPracticeDataResponsePacket(data TownPracticeData) (*Packet, error) {
	w := NewWriter()
	w.WriteInt16(data.Param1)
	w.WriteInt16(data.Param2)
	w.WriteInt32(data.PracticeTime)
	w.WriteInt32(data.PracticeWar)
	w.WriteInt64(data.PracticeExp)
	w.WriteUint8(data.PracticeSpeedNum)
	w.WriteInt32(data.PracticeExpOne)
	w.WriteInt32(data.SuperTownPracticeExp)

	return NewPacket(ActionIDPlayerGetPracticeData, w.Bytes()), nil
}

// ParseTownPracticeNotify 反序列化城镇练功状态变更/经验跳动通知载荷。
func ParseTownPracticeNotify(payload []byte) (*TownPracticeNotify, error) {
	decompressed, err := DecompressIfNeeded(payload)
	if err != nil {
		return nil, fmt.Errorf("town practice notify: 解压失败: %w", err)
	}

	r := NewReader(decompressed)
	if r.Remaining() < 1 {
		return nil, fmt.Errorf("town practice notify: 载荷为空")
	}

	notify := &TownPracticeNotify{}
	notify.NotifyType, _ = r.ReadUint8()

	if r.Remaining() >= 4 {
		notify.Exp, _ = r.ReadInt32()
	}
	if r.Remaining() >= 4 {
		notify.SuperExp, _ = r.ReadInt32()
	}

	return notify, nil
}

// BuildTownPracticeNotifyPacket 构造城镇练功通知封包 (通常供单测或 Mock 网关使用)。
func BuildTownPracticeNotifyPacket(notify TownPracticeNotify) (*Packet, error) {
	w := NewWriter()
	w.WriteUint8(notify.NotifyType)
	w.WriteInt32(notify.Exp)
	w.WriteInt32(notify.SuperExp)

	return NewPacket(ActionIDPlayerPracticeNotify, w.Bytes()), nil
}
