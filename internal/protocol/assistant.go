// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方小助手日活跃奖励协议 (Mod_Assistant_Base, Module 52 / 0x0034)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_Assistant_Base：
//   - info              Act 0 -> 0x00340000
//   - get_award         Act 1 -> 0x00340001  [Int box_id]
//   - complete_number   Act 2 -> 0x00340002
//   - one_key_get_award Act 3 -> 0x00340003  [Int type]
package protocol

import "fmt"

const (
	// ModuleAssistant 小助手模块编号 (0x0034)
	ModuleAssistant uint16 = 52
)

// 小助手动作编号 (Mod_Assistant_Base)。
const (
	// ActionIDAssistantInfo 查询小助手信息 (Action 0 -> 0x00340000)
	ActionIDAssistantInfo uint32 = 0x00340000
	// ActionIDAssistantGetAward 领取单个活跃宝箱 (Action 1 -> 0x00340001)
	ActionIDAssistantGetAward uint32 = 0x00340001
	// ActionIDAssistantCompleteNumber 查询已完成活跃度 (Action 2 -> 0x00340002)
	ActionIDAssistantCompleteNumber uint32 = 0x00340002
	// ActionIDAssistantOneKeyGetAward 一键领取 (Action 3 -> 0x00340003)
	ActionIDAssistantOneKeyGetAward uint32 = 0x00340003
)

// 小助手结果码 (Mod_Assistant_Base)。
const (
	// AssistantResultSuccess 成功
	AssistantResultSuccess uint8 = 0
	// AssistantResultNotEnoughVIP VIP 不足
	AssistantResultNotEnoughVIP uint8 = 1
	// AssistantResultNotEnoughNum 活跃度不足
	AssistantResultNotEnoughNum uint8 = 2
	// AssistantResultPackFull 背包已满
	AssistantResultPackFull uint8 = 3
	// AssistantResultHaveDone 已领取
	AssistantResultHaveDone uint8 = 4
	// AssistantResultFailed 失败
	AssistantResultFailed uint8 = 5
	// AssistantResultFishhookLimit 鱼钩上限
	AssistantResultFishhookLimit uint8 = 6
	// AssistantResultFindImmortalLimit 喜从天降上限
	AssistantResultFindImmortalLimit uint8 = 7
)

// AssistantAward 表示一键领取获得的道具。
type AssistantAward struct {
	// ItemID 道具 ID
	ItemID int32
	// ItemNum 数量
	ItemNum int32
}

// BuildAssistantInfoPacket 构造查询小助手信息封包 (0x00340000)。
func BuildAssistantInfoPacket() *Packet {
	return NewPacket(ActionIDAssistantInfo, nil)
}

// BuildAssistantOneKeyGetAwardPacket 构造一键领取封包 (0x00340003)。
func BuildAssistantOneKeyGetAwardPacket(typeID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(typeID)
	return NewPacket(ActionIDAssistantOneKeyGetAward, w.Bytes())
}

// ParseAssistantOneKeyResult 解析一键领取回包: UByte result, [Int item_id, Int item_num]。
func ParseAssistantOneKeyResult(payload []byte) (uint8, []AssistantAward, error) {
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return 0, nil, fmt.Errorf("%w: failed to read assistant result: %v", ErrInvalidPayload, err)
	}
	if result != AssistantResultSuccess {
		return result, nil, nil
	}
	count, err := r.ReadUint16()
	if err != nil {
		return 0, nil, fmt.Errorf("%w: failed to read assistant award count: %v", ErrInvalidPayload, err)
	}
	awards := make([]AssistantAward, 0, count)
	for i := 0; i < int(count); i++ {
		a := AssistantAward{}
		if a.ItemID, err = r.ReadInt32(); err != nil {
			return 0, nil, fmt.Errorf("%w: failed to read assistant award[%d].item_id: %v", ErrInvalidPayload, i, err)
		}
		if a.ItemNum, err = r.ReadInt32(); err != nil {
			return 0, nil, fmt.Errorf("%w: failed to read assistant award[%d].item_num: %v", ErrInvalidPayload, i, err)
		}
		awards = append(awards, a)
	}
	return result, awards, nil
}

// BuildAssistantOneKeyResultPacket 构造一键领取回包，供离线 mock 与单测使用。
func BuildAssistantOneKeyResultPacket(result uint8, awards []AssistantAward) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	if result == AssistantResultSuccess {
		w.WriteUint16(uint16(len(awards)))
		for _, a := range awards {
			w.WriteInt32(a.ItemID)
			w.WriteInt32(a.ItemNum)
		}
	}
	return NewPacket(ActionIDAssistantOneKeyGetAward, w.Bytes())
}
