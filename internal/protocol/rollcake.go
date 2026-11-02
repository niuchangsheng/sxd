// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现个人吉星高照 (Mod_RollCake_Base) 的请求封包与回包解析，
// 协议描述符对齐 2026 客户端 Main.swf 中 Mod_RollCake_Base 的 AS3 声明:
//   - roll:      module 38, action 0, response [UByteUtil, ByteUtil, IntUtil, ByteUtil x6, IntUtil]
//   - get_count: module 38, action 2, response [ShortUtil, ByteUtil, ByteUtil, IntUtil]
//   - reroll:    module 38, action 3, response [UByteUtil, ByteUtil, ByteUtil, IntUtil, ByteUtil x6, IntUtil]
//   - get_award: module 38, action 4, response [UByteUtil]
//   - get_state: module 38, action 5, response [UByteUtil, ByteUtil, IntUtil, ByteUtil x6]
//   - is_auto:   module 38, action 6, response [ByteUtil]
//   - set_auto:  module 38, action 7, request [ByteUtil], response [UByteUtil]
//
// 该模块与帮派吉星高照 (Mod_Faction_Base, module 10) 是两套独立玩法:
// 个人吉星高照每次投掷 6 颗骰子，可消耗免费次数或元宝逆天改运，
// 最后领取本次投掷奖励；帮派吉星高照则统计帮派成员成绩。
package protocol

import "fmt"

// rollCakeDiceCount 个人吉星高照每次投掷的骰子数量。
const rollCakeDiceCount = 6

// RollCakeCount 表示个人吉星高照次数面板 (Mod 38, Act 2 回包)。
type RollCakeCount struct {
	// Count 今日剩余投掷次数
	Count int16
	// FreeRerollTimes 今日剩余免费逆天改运次数
	FreeRerollTimes int8
	// MaxBuyRerollTimes 今日剩余可购买改运次数
	MaxBuyRerollTimes int8
	// RollCount 今日累计投掷次数 (对应熟练度进度)
	RollCount int32
}

// RollCakeRollResult 表示一次投掷结算 (Mod 38, Act 0 回包)。
type RollCakeRollResult struct {
	// Msg 业务结果码 (RollCakeCodeSuccess 等)
	Msg uint8
	// Type 本次投掷奖励类型
	Type int8
	// RerollIngot 本次投掷后逆天改运所需元宝
	RerollIngot int32
	// Dice 本次投掷的 6 颗骰子点数
	Dice []int8
	// RollCount 今日累计投掷次数
	RollCount int32
}

// RollCakeRerollResult 表示一次逆天改运结算 (Mod 38, Act 3 回包)。
type RollCakeRerollResult struct {
	// Msg 业务结果码 (RollCakeCodeSuccess 等)
	Msg uint8
	// Type 改运后的奖励类型
	Type int8
	// FreeRerollTimes 剩余免费改运次数
	FreeRerollTimes int8
	// RerollIngot 下次逆天改运所需元宝
	RerollIngot int32
	// Dice 改运后的 6 颗骰子点数
	Dice []int8
	// RollCount 今日累计投掷次数
	RollCount int32
}

// RollCakeState 表示未领取的投掷记录 (Mod 38, Act 5 回包)。
type RollCakeState struct {
	// Msg 业务结果码 (RollCakeCodeHaveRecord / RollCakeCodeNoRecord)
	Msg uint8
	// Type 奖励类型
	Type int8
	// RerollIngot 逆天改运所需元宝
	RerollIngot int32
	// Dice 当前记录的 6 颗骰子点数
	Dice []int8
}

// RollCakeAwardResult 表示领取投掷奖励结果 (Mod 38, Act 4 回包)。
type RollCakeAwardResult struct {
	// Msg 业务结果码 (RollCakeCodeSuccess 等)
	Msg uint8
}

// BuildRollCakeRollPacket 构造投掷一次个人吉星高照骰子封包 (Mod 38, Act 0)。
func BuildRollCakeRollPacket(prevAct uint32) *Packet {
	return newPrevActPacket(ActionIDRollCakeRoll, prevAct)
}

// BuildRollCakeGetCountPacket 构造查询个人吉星高照剩余次数封包 (Mod 38, Act 2)。
func BuildRollCakeGetCountPacket(prevAct uint32) *Packet {
	return newPrevActPacket(ActionIDRollCakeGetCount, prevAct)
}

// BuildRollCakeRerollPacket 构造逆天改运封包 (Mod 38, Act 3)。
func BuildRollCakeRerollPacket(prevAct uint32) *Packet {
	return newPrevActPacket(ActionIDRollCakeReroll, prevAct)
}

// BuildRollCakeGetAwardPacket 构造领取投掷奖励封包 (Mod 38, Act 4)。
func BuildRollCakeGetAwardPacket(prevAct uint32) *Packet {
	return newPrevActPacket(ActionIDRollCakeGetAward, prevAct)
}

// BuildRollCakeGetStatePacket 构造查询未领取投掷记录封包 (Mod 38, Act 5)。
func BuildRollCakeGetStatePacket(prevAct uint32) *Packet {
	return newPrevActPacket(ActionIDRollCakeGetState, prevAct)
}

// BuildRollCakeIsAutoPacket 构造查询自动逆天改运开关封包 (Mod 38, Act 6)。
func BuildRollCakeIsAutoPacket(prevAct uint32) *Packet {
	return newPrevActPacket(ActionIDRollCakeIsAuto, prevAct)
}

// BuildRollCakeSetAutoPacket 构造设置自动逆天改运开关封包 (Mod 38, Act 7)。
func BuildRollCakeSetAutoPacket(auto int8, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt8(auto)
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDRollCakeSetAuto, w.Bytes())
}

// ParseRollCakeCountResult 解析次数面板回包:
// int16 count + int8 free_reroll_times + int8 max_buy_reroll_times + int32 roll_count。
func ParseRollCakeCountResult(payload []byte) (*RollCakeCount, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))

	count, err := r.ReadInt16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read roll cake count: %v", ErrInvalidPayload, err)
	}
	freeRerolls, err := r.ReadInt8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read roll cake free_reroll_times: %v", ErrInvalidPayload, err)
	}
	maxBuyRerolls, err := r.ReadInt8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read roll cake max_buy_reroll_times: %v", ErrInvalidPayload, err)
	}
	rollCount, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read roll cake roll_count: %v", ErrInvalidPayload, err)
	}

	return &RollCakeCount{
		Count:             count,
		FreeRerollTimes:   freeRerolls,
		MaxBuyRerollTimes: maxBuyRerolls,
		RollCount:         rollCount,
	}, nil
}

// ParseRollCakeRollResult 解析投掷回包:
// uint8 msg + int8 type + int32 reroll_ingot + 6*int8 dice + int32 roll_count。
func ParseRollCakeRollResult(payload []byte) (*RollCakeRollResult, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))

	msg, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read roll cake msg: %v", ErrInvalidPayload, err)
	}
	typ, err := r.ReadInt8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read roll cake type: %v", ErrInvalidPayload, err)
	}
	rerollIngot, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read roll cake reroll_ingot: %v", ErrInvalidPayload, err)
	}
	dice, err := readRollCakeDice(r)
	if err != nil {
		return nil, err
	}
	rollCount, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read roll cake roll_count: %v", ErrInvalidPayload, err)
	}

	return &RollCakeRollResult{
		Msg:         msg,
		Type:        typ,
		RerollIngot: rerollIngot,
		Dice:        dice,
		RollCount:   rollCount,
	}, nil
}

// ParseRollCakeRerollResult 解析逆天改运回包:
// uint8 msg + int8 type + int8 free_reroll_times + int32 reroll_ingot + 6*int8 dice + int32 roll_count。
func ParseRollCakeRerollResult(payload []byte) (*RollCakeRerollResult, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))

	msg, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read roll cake msg: %v", ErrInvalidPayload, err)
	}
	typ, err := r.ReadInt8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read roll cake type: %v", ErrInvalidPayload, err)
	}
	freeRerolls, err := r.ReadInt8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read roll cake free_reroll_times: %v", ErrInvalidPayload, err)
	}
	rerollIngot, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read roll cake reroll_ingot: %v", ErrInvalidPayload, err)
	}
	dice, err := readRollCakeDice(r)
	if err != nil {
		return nil, err
	}
	rollCount, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read roll cake roll_count: %v", ErrInvalidPayload, err)
	}

	return &RollCakeRerollResult{
		Msg:             msg,
		Type:            typ,
		FreeRerollTimes: freeRerolls,
		RerollIngot:     rerollIngot,
		Dice:            dice,
		RollCount:       rollCount,
	}, nil
}

// ParseRollCakeStateResult 解析未领取记录回包:
// uint8 msg + int8 type + int32 reroll_ingot + 6*int8 dice。
func ParseRollCakeStateResult(payload []byte) (*RollCakeState, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))

	msg, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read roll cake state msg: %v", ErrInvalidPayload, err)
	}
	typ, err := r.ReadInt8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read roll cake state type: %v", ErrInvalidPayload, err)
	}
	rerollIngot, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read roll cake state reroll_ingot: %v", ErrInvalidPayload, err)
	}
	dice, err := readRollCakeDice(r)
	if err != nil {
		return nil, err
	}

	return &RollCakeState{
		Msg:         msg,
		Type:        typ,
		RerollIngot: rerollIngot,
		Dice:        dice,
	}, nil
}

// ParseRollCakeAwardResult 解析领取奖励回包: uint8 msg。
func ParseRollCakeAwardResult(payload []byte) (*RollCakeAwardResult, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))

	msg, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read roll cake award msg: %v", ErrInvalidPayload, err)
	}
	return &RollCakeAwardResult{Msg: msg}, nil
}

// ParseRollCakeIsAutoResult 解析自动逆天改运开关回包: int8 auto。
func ParseRollCakeIsAutoResult(payload []byte) (int8, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	auto, err := r.ReadInt8()
	if err != nil {
		return 0, fmt.Errorf("%w: failed to read roll cake is_auto: %v", ErrInvalidPayload, err)
	}
	return auto, nil
}

// ParseRollCakeSetAutoResult 解析设置自动逆天改运开关回包: uint8 msg。
func ParseRollCakeSetAutoResult(payload []byte) (uint8, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	msg, err := r.ReadUint8()
	if err != nil {
		return 0, fmt.Errorf("%w: failed to read roll cake set_auto msg: %v", ErrInvalidPayload, err)
	}
	return msg, nil
}

// readRollCakeDice 读取固定 6 颗骰子点数。
func readRollCakeDice(r *Reader) ([]int8, error) {
	dice := make([]int8, rollCakeDiceCount)
	for i := 0; i < rollCakeDiceCount; i++ {
		num, err := r.ReadInt8()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read roll cake dice[%d]: %v", ErrInvalidPayload, i, err)
		}
		dice[i] = num
	}
	return dice, nil
}
