// Package protocol 提供百炼乾坤一/二 (Mod_BaiLianQianKun_Base / Two, Module 272/320) 协议编解码。
package protocol

import (
	"fmt"
)

// 百炼乾坤动作 ID。
const (
	ActionIDBaiLianOneInfo  uint32 = 0x01100000
	ActionIDBaiLianOneFight uint32 = 0x01100001
	ActionIDBaiLianOneKey   uint32 = 0x01100002
	ActionIDBaiLianTwoInfo  uint32 = 0x01400000
	ActionIDBaiLianTwoFight uint32 = 0x01400001
	ActionIDBaiLianTwoKey   uint32 = 0x01400002
)

// 百炼乾坤一结果码 (Module 272)。
const (
	BaiLianOneHasGet      uint8 = 0
	BaiLianOneUnGet       uint8 = 1
	BaiLianOneCompleted   uint8 = 2
	BaiLianOneUncompleted uint8 = 3
	BaiLianOneActivate    uint8 = 4
	BaiLianOneNonActivate uint8 = 5
	BaiLianOneWin         uint8 = 6
	BaiLianOneLose        uint8 = 7
	BaiLianOneTimeCD      uint8 = 8
	BaiLianOneFailed      uint8 = 9
	BaiLianOneSuccess     uint8 = 10
	BaiLianOneFighting    uint8 = 11
	BaiLianOneLock        uint8 = 12
	BaiLianOnePass        uint8 = 13
)

// 百炼乾坤二结果码 (Module 320)。
const (
	BaiLianTwoHasGet     uint8 = 0
	BaiLianTwoUnGet      uint8 = 1
	BaiLianTwoCompleted  uint8 = 2
	BaiLianTwoUncomplete uint8 = 3
	BaiLianTwoActivate   uint8 = 4
	BaiLianTwoNonActive  uint8 = 5
	BaiLianTwoSuccess    uint8 = 6
	BaiLianTwoFuncUnopen uint8 = 7
	BaiLianTwoWin        uint8 = 8
	BaiLianTwoLose       uint8 = 9
	BaiLianTwoTimeCD     uint8 = 10
	BaiLianTwoFailed     uint8 = 11
	BaiLianTwoFighting   uint8 = 12
	BaiLianTwoLock       uint8 = 13
	BaiLianTwoPass       uint8 = 14
)

// BuildBaiLianInfoPacket 构造百炼乾坤面板查询封包。
func BuildBaiLianInfoPacket(two bool) *Packet {
	if two {
		return NewPacket(ActionIDBaiLianTwoInfo, nil)
	}
	return NewPacket(ActionIDBaiLianOneInfo, nil)
}

// BuildBaiLianChallengePacket 构造百炼乾坤单次挑战封包。
func BuildBaiLianChallengePacket(two bool) *Packet {
	if two {
		return NewPacket(ActionIDBaiLianTwoFight, nil)
	}
	return NewPacket(ActionIDBaiLianOneFight, nil)
}

// BuildBaiLianOneKeyPacket 构造百炼乾坤一键挑战/扫荡封包。
func BuildBaiLianOneKeyPacket(two bool) *Packet {
	if two {
		return NewPacket(ActionIDBaiLianTwoKey, nil)
	}
	return NewPacket(ActionIDBaiLianOneKey, nil)
}

// BaiLianInfo 表示百炼乾坤面板的可读字段。
type BaiLianInfo struct {
	// Result 320 首字节状态；272 无独立结果码，保留面板首字节。
	Result uint8
	// Level 当前关卡。
	Level int32
	// Fields 其余状态字段（按协议顺序）。
	Fields []int32
	// Aux1 面板最后一个 Int 前的第一个 Short 字段（语义未确认，仅用于日志）。
	Aux1 int32
}

// ParseBaiLianInfo 解析百炼乾坤面板。two=true 时按 320 布局（首字节为状态）。
func ParseBaiLianInfo(payload []byte, two bool) (*BaiLianInfo, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	info := &BaiLianInfo{}
	if two {
		res, err := r.ReadUint8()
		if err != nil {
			return nil, fmt.Errorf("百炼乾坤二状态读取失败: %w", err)
		}
		info.Result = res
		level, err := r.ReadInt8()
		if err != nil {
			return nil, fmt.Errorf("百炼乾坤二关卡读取失败: %w", err)
		}
		info.Level = int32(level)
	} else {
		level, err := r.ReadInt8()
		if err != nil {
			return nil, fmt.Errorf("百炼乾坤一关卡读取失败: %w", err)
		}
		info.Level = int32(level)
	}
	for i := 0; i < 3; i++ {
		v, err := r.ReadUint8()
		if err != nil {
			return nil, fmt.Errorf("百炼乾坤状态字段[%d]读取失败: %w", i, err)
		}
		info.Fields = append(info.Fields, int32(v))
	}
	score, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("百炼乾坤积分读取失败: %w", err)
	}
	info.Fields = append(info.Fields, score)
	short1, err := r.ReadInt16()
	if err != nil {
		return nil, fmt.Errorf("百炼乾坤字段读取失败: %w", err)
	}
	short2, err := r.ReadInt16()
	if err != nil {
		return nil, fmt.Errorf("百炼乾坤次数读取失败: %w", err)
	}
	info.Fields = append(info.Fields, int32(short1), int32(short2))
	info.Aux1 = int32(short1)
	return info, nil
}
