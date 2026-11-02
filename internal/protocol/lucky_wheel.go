// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现帮派转盘 (Mod_LuckyWheel_Base) 的请求封包构造与回包解析。
// 协议描述符对齐 2026 客户端 Main.swf 中 Mod_LuckyWheel_Base 的 AS3 声明:
//   - open_lucky_wheel:  module 69, action 1, request [], response [UByte, Int, Int]
//   - start_lucky_wheel: module 69, action 2, request [], response [UByte, Byte, Int, Int]
package protocol

import "fmt"

// LuckyWheelOpenInfo 表示打开帮派转盘时返回的状态 (Mod 69, Act 1 回包)。
type LuckyWheelOpenInfo struct {
	// State 转盘状态 (LuckyWheelResultOK 表示可以转动)
	State uint8
	// FactionTodayContribution 玩家今日帮派贡献 (客户端字段 faction_today_con)
	FactionTodayContribution int32
	// RosefinchNumber 剩余转盘凭证 (青龙令) 数量，沿用客户端字段名 rosefinch_number
	RosefinchNumber int32
}

// LuckyWheelRunResult 表示一次帮派转盘抽奖结算 (Mod 69, Act 2 回包)。
type LuckyWheelRunResult struct {
	// RunState 抽奖结果 (LuckyWheelResultSuccess 表示成功)
	RunState uint8
	// Type 奖励类型编号
	Type int8
	// RosefinchNumber 抽奖后剩余青龙令数量
	RosefinchNumber int32
	// ItemID 奖励道具编号 (奖励类型为道具时有效)
	ItemID int32
}

// BuildLuckyWheelOpenPacket 构造打开帮派转盘封包 (Mod 69, Act 1)。
func BuildLuckyWheelOpenPacket(prevAct uint32) *Packet {
	return newPrevActPacket(ActionIDLuckyWheelOpen, prevAct)
}

// ParseLuckyWheelOpenResult 解析打开转盘回包:
// uint8 state + int32 faction_today_con + int32 rosefinch_number。
func ParseLuckyWheelOpenResult(payload []byte) (*LuckyWheelOpenInfo, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))

	state, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read lucky wheel state: %v", ErrInvalidPayload, err)
	}
	factionTodayCon, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read faction_today_con: %v", ErrInvalidPayload, err)
	}
	rosefinch, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read rosefinch_number: %v", ErrInvalidPayload, err)
	}

	return &LuckyWheelOpenInfo{
		State:                    state,
		FactionTodayContribution: factionTodayCon,
		RosefinchNumber:          rosefinch,
	}, nil
}

// BuildLuckyWheelStartPacket 构造转动一次帮派转盘封包 (Mod 69, Act 2)。
func BuildLuckyWheelStartPacket(prevAct uint32) *Packet {
	return newPrevActPacket(ActionIDLuckyWheelStart, prevAct)
}

// ParseLuckyWheelRunResult 解析转盘抽奖回包:
// uint8 run_state + int8 type + int32 rosefinch_number + int32 item_id。
func ParseLuckyWheelRunResult(payload []byte) (*LuckyWheelRunResult, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))

	runState, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read lucky wheel run_state: %v", ErrInvalidPayload, err)
	}
	typ, err := r.ReadInt8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read lucky wheel type: %v", ErrInvalidPayload, err)
	}
	rosefinch, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read lucky wheel rosefinch_number: %v", ErrInvalidPayload, err)
	}
	itemID, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read lucky wheel item_id: %v", ErrInvalidPayload, err)
	}

	return &LuckyWheelRunResult{
		RunState:        runState,
		Type:            typ,
		RosefinchNumber: rosefinch,
		ItemID:          itemID,
	}, nil
}
