// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方生肖挑战协议 (Mod_Zodiac_Base, Module 44 / 0x2C)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_Zodiac_Base 与
// com.datas.ZodiacData：先查询当前生肖关卡与等级要求，再发起挑战。
// 自动化在挑战前校验角色等级是否满足 require_level。
package protocol

import "fmt"

const (
	// ActionIDZodiacInfo 查询生肖挑战信息 (Module 44, Action 0 -> 0x002C0000)
	ActionIDZodiacInfo uint32 = 0x002C0000
	// ActionIDZodiacChallenge 发起生肖挑战 (Module 44, Action 1 -> 0x002C0001)
	ActionIDZodiacChallenge uint32 = 0x002C0001
	// ActionIDZodiacExchangeStone 兑换生肖石 (Module 44, Action 2 -> 0x002C0002)
	ActionIDZodiacExchangeStone uint32 = 0x002C0002
)

// Mod_Zodiac_Base 结果码。
const (
	// ZodiacResultSuccess 挑战成功
	ZodiacResultSuccess uint8 = 0
	// ZodiacResultFail 挑战失败
	ZodiacResultFail uint8 = 1
	// ZodiacResultTopHeaven 已通关最高天
	ZodiacResultTopHeaven uint8 = 2
	// ZodiacResultNotEnoughPlayerLevel 角色等级不足
	ZodiacResultNotEnoughPlayerLevel uint8 = 3
	// ZodiacResultBagFull 背包已满
	ZodiacResultBagFull uint8 = 4
	// ZodiacResultCDTime 冷却中
	ZodiacResultCDTime uint8 = 5
	// ZodiacResultNoEnoughStatePoint 境界点不足
	ZodiacResultNoEnoughStatePoint uint8 = 6
	// ZodiacResultLock 关卡未解锁
	ZodiacResultLock uint8 = 7
)

// ZodiacInfo 表示生肖挑战面板信息。
//
// 线格式: Byte now_zodiac_level, Byte now_barrier, Byte next_zodiac_level,
// Byte next_barrier, Int require_level, Int gold_oil_id, Byte heaven。
type ZodiacInfo struct {
	// NowZodiacLevel 当前生肖等级
	NowZodiacLevel uint8
	// NowBarrier 当前关卡
	NowBarrier uint8
	// NextZodiacLevel 下一生肖等级
	NextZodiacLevel uint8
	// NextBarrier 下一关卡
	NextBarrier uint8
	// RequireLevel 挑战所需角色等级
	RequireLevel int32
	// GoldOilID 金油道具编号
	GoldOilID int32
	// Heaven 当前天
	Heaven uint8
}

// ZodiacChallengeResult 表示挑战回包的首段结果。
type ZodiacChallengeResult struct {
	// Result 结果码
	Result uint8
	// ZodiacLevel 挑战后的生肖等级
	ZodiacLevel uint8
	// Barrier 挑战后的关卡
	Barrier uint8
	// GoldOilID 金油道具编号
	GoldOilID int32
}

// BuildZodiacInfoPacket 构造查询生肖信息封包 (0x002C0000)。
func BuildZodiacInfoPacket() *Packet {
	return NewPacket(ActionIDZodiacInfo, nil)
}

// BuildZodiacChallengePacket 构造发起生肖挑战封包 (0x002C0001)。
func BuildZodiacChallengePacket() *Packet {
	return NewPacket(ActionIDZodiacChallenge, nil)
}

// ParseZodiacInfoResult 解析生肖信息回包。
func ParseZodiacInfoResult(payload []byte) (*ZodiacInfo, error) {
	r := NewReader(payload)
	res := &ZodiacInfo{}
	var err error
	if res.NowZodiacLevel, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read now_zodiac_level: %v", ErrInvalidPayload, err)
	}
	if res.NowBarrier, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read now_barrier: %v", ErrInvalidPayload, err)
	}
	if res.NextZodiacLevel, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read next_zodiac_level: %v", ErrInvalidPayload, err)
	}
	if res.NextBarrier, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read next_barrier: %v", ErrInvalidPayload, err)
	}
	if res.RequireLevel, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read require_level: %v", ErrInvalidPayload, err)
	}
	if res.GoldOilID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read gold_oil_id: %v", ErrInvalidPayload, err)
	}
	if res.Heaven, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read heaven: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// ParseZodiacChallengeResult 解析挑战回包首段 (结果码/等级/关卡/金油)。
//
// 回包随后携带完整战报，自动化只消费首段字段。
func ParseZodiacChallengeResult(payload []byte) (*ZodiacChallengeResult, error) {
	r := NewReader(payload)
	res := &ZodiacChallengeResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read zodiac result: %v", ErrInvalidPayload, err)
	}
	if res.ZodiacLevel, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read zodiac_level: %v", ErrInvalidPayload, err)
	}
	if res.Barrier, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read zodiac barrier: %v", ErrInvalidPayload, err)
	}
	if res.GoldOilID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read zodiac gold_oil_id: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// BuildZodiacInfoResultPacket 构造生肖信息回包，供离线 mock 与单测使用。
func BuildZodiacInfoResultPacket(info ZodiacInfo) *Packet {
	w := NewWriter()
	w.WriteUint8(info.NowZodiacLevel)
	w.WriteUint8(info.NowBarrier)
	w.WriteUint8(info.NextZodiacLevel)
	w.WriteUint8(info.NextBarrier)
	w.WriteInt32(info.RequireLevel)
	w.WriteInt32(info.GoldOilID)
	w.WriteUint8(info.Heaven)
	return NewPacket(ActionIDZodiacInfo, w.Bytes())
}

// BuildZodiacChallengeResultPacket 构造挑战回包首段，供离线 mock 与单测使用。
func BuildZodiacChallengeResultPacket(result uint8, zodiacLevel, barrier uint8, goldOilID int32) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	w.WriteUint8(zodiacLevel)
	w.WriteUint8(barrier)
	w.WriteInt32(goldOilID)
	return NewPacket(ActionIDZodiacChallenge, w.Bytes())
}
