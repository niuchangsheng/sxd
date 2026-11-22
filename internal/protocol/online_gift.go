// Package protocol 提供在线礼包 (Mod_OnlineGift_Base) 与系统领奖台 (Mod_GetAward_Base) 协议编解码。
package protocol

import (
	"fmt"
)

// OnlineGiftItem 表示在线成长礼包中的一项奖励。
type OnlineGiftItem struct {
	Type   int32 // 奖励类型
	ItemID int32 // 道具/资源编号
	Value  int32 // 数量
}

// OnlineGiftCurrent 表示在线成长礼包当前状态。
type OnlineGiftCurrent struct {
	TimeStamp int32            // 0 表示暂无待领取礼包; 非 0 时客户端展示下一份礼包预览
	Gifts     []OnlineGiftItem // 待领取奖励列表
}

// BuildOnlineGiftCurrentPacket 构造查询在线成长礼包状态封包 (Module 0, Action 31)。
// 依据原版客户端 Mod_Player_Base.cinit: request 描述为空, 请求体为空。
func BuildOnlineGiftCurrentPacket() *Packet {
	return NewPacket(ActionIDPlayerGetCurrentOnlineGift, nil)
}

// ParseOnlineGiftCurrentResult 解析查询回包:
// int32 time_stamp + uint16 count + count*(int32 type,int32 item_id,int32 value)。
func ParseOnlineGiftCurrentResult(payload []byte) (*OnlineGiftCurrent, error) {
	decomp := DecompressPayloadIfNeeded(payload)
	if len(decomp) < 6 {
		return nil, fmt.Errorf("%w: payload too short for online gift state", ErrInvalidPayload)
	}
	r := NewReader(decomp)
	ts, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read time_stamp: %v", ErrInvalidPayload, err)
	}
	gifts, err := readOnlineGiftItems(r)
	if err != nil {
		return nil, err
	}
	return &OnlineGiftCurrent{TimeStamp: ts, Gifts: gifts}, nil
}

// OnlineGiftClaimResult 表示领取在线成长礼包回包。
type OnlineGiftClaimResult struct {
	Result        uint8            // 业务结果码: 97 成功 / 98 已领取 / 100 时间未到 / 101 背包已满
	Gifts         []OnlineGiftItem // 本次领取到的奖励列表
	NextTimeStamp int32            // 距离下一次礼包可领取的剩余秒数 (0 表示无后续礼包)
}

// BuildOnlineGiftClaimPacket 构造领取在线成长礼包封包 (Module 0, Action 30)。
// 依据原版客户端 Mod_Player_Base.cinit: request 描述为空, 请求体为空。
func BuildOnlineGiftClaimPacket() *Packet {
	return NewPacket(ActionIDPlayerGetOnlineGift, nil)
}

// ParseOnlineGiftClaimResult 解析领取回包:
// uint8 result + uint16 count + count*(int32 type,int32 item_id,int32 value) + int32 next_time_stamp。
func ParseOnlineGiftClaimResult(payload []byte) (*OnlineGiftClaimResult, error) {
	decomp := DecompressPayloadIfNeeded(payload)
	if len(decomp) < 3 {
		return nil, fmt.Errorf("%w: payload too short for online gift claim", ErrInvalidPayload)
	}
	r := NewReader(decomp)
	result, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read claim result: %v", ErrInvalidPayload, err)
	}
	gifts, err := readOnlineGiftItems(r)
	if err != nil {
		return nil, err
	}
	claim := &OnlineGiftClaimResult{Result: result, Gifts: gifts}
	// next_time_stamp 可能因服务端版本差异缺失, 缺失时保持 0
	if nextTS, err := r.ReadInt32(); err == nil {
		claim.NextTimeStamp = nextTS
	}
	return claim, nil
}

// readOnlineGiftItems 读取 uint16 count 及其后的 count 条 (type,item_id,value) 记录。
func readOnlineGiftItems(r *Reader) ([]OnlineGiftItem, error) {
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read gift count: %v", ErrInvalidPayload, err)
	}
	var gifts []OnlineGiftItem
	for i := 0; i < int(count); i++ {
		typ, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: truncated gift type at %d: %v", ErrInvalidPayload, i, err)
		}
		itemID, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: truncated gift item_id at %d: %v", ErrInvalidPayload, i, err)
		}
		value, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: truncated gift value at %d: %v", ErrInvalidPayload, i, err)
		}
		gifts = append(gifts, OnlineGiftItem{Type: typ, ItemID: itemID, Value: value})
	}
	return gifts, nil
}

// --------------------------------------------------------------------------------
// 1.1 序列号礼包领取 (haha.pcapng 实采协议, 非在线成长礼包)
// --------------------------------------------------------------------------------

// BuildClaimAwardPacket 构造领取指定序列号礼包封包 (Module 2, Action 29)。
//
// 官方抓包: [Int award_id][Uint32 PrevAct]；PrevAct 链由会话层在发包瞬间
// 用最近一次动作覆盖，因此这里标记 HasPrevAct。
func BuildClaimAwardPacket(awardSeqID uint32, prevAct uint32) *Packet {
	// 2026-11-14 实机：带 PrevAct 的 8 字节 2/29 请求对系统奖励被静默丢弃；
	// 与 35/0、25/0 一致，优先使用裸 [Int award_id] 载荷。prevAct 参数保留兼容。
	_ = prevAct
	w := NewWriter()
	w.WriteUint32(awardSeqID)
	return NewPacket(ActionIDItemClaimAward, w.Bytes())
}

// BuildSystemAwardListPacket 构造查询系统/活动奖励列表封包 (Module 2, Action 6)。
//
// 官方抓包请求为空载荷 + PrevAct 尾部。
func BuildSystemAwardListPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDSystemAwardList, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// SystemAward 表示一条待领取的系统/活动奖励。
type SystemAward struct {
	ID     uint32 // 奖励 ID，用于 2/29 领取
	Kind   int32  // 奖励类型/状态字段
	Text   string // 奖励描述 (UTF-8)
	Count  int32  // 奖励条目数
	Detail []SystemAwardItem
}

// SystemAwardItem 表示奖励明细中的一项。
type SystemAwardItem struct {
	Type  uint8 // 奖励类型
	Num   int32 // 数量
	Param int32 // 附加参数 (道具 ID 等)
}

// ParseSystemAwardListResult 解析 2/6 系统/活动奖励列表。
//
// 线格式: [Uint16 count] + count ×
// [Int32 id][Int32 kind][Uint16 text_len][text bytes]
// [Int32 item_count][item_count × (Uint8 type, Int32 num, Int32 param)]。
func ParseSystemAwardListResult(payload []byte) ([]SystemAward, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read system award count: %v", ErrInvalidPayload, err)
	}
	awards := make([]SystemAward, 0, count)
	for i := 0; i < int(count); i++ {
		var a SystemAward
		if a.ID, err = r.ReadUint32(); err != nil {
			return awards, nil
		}
		if a.Kind, err = r.ReadInt32(); err != nil {
			return awards, nil
		}
		textLen, err := r.ReadUint16()
		if err != nil {
			return awards, nil
		}
		text, err := r.ReadBytes(int(textLen))
		if err != nil {
			return awards, nil
		}
		a.Text = string(text)
		if a.Count, err = r.ReadInt32(); err != nil {
			return awards, nil
		}
		if a.Count < 0 || a.Count > 1000 {
			return awards, nil
		}
		for j := 0; j < int(a.Count); j++ {
			var it SystemAwardItem
			if it.Type, err = r.ReadUint8(); err != nil {
				return awards, nil
			}
			if it.Num, err = r.ReadInt32(); err != nil {
				return awards, nil
			}
			if it.Param, err = r.ReadInt32(); err != nil {
				return awards, nil
			}
			a.Detail = append(a.Detail, it)
		}
		awards = append(awards, a)
	}
	return awards, nil
}

// ParseClaimAwardResult 解析礼包领取结果。
func ParseClaimAwardResult(payload []byte) (bool, error) {
	if len(payload) == 0 {
		return true, nil
	}
	r := NewReader(payload)
	code, err := r.ReadUint8()
	if err != nil {
		return false, fmt.Errorf("%w: failed to read claim code: %v", ErrInvalidPayload, err)
	}
	// 结果码 0x14 (20) 或 0x00 表示成功
	return code == ItemActionSuccess || code == ItemSuccess, nil
}

// --------------------------------------------------------------------------------
// 2. 结缘宝箱：除了传说，其他都开
// --------------------------------------------------------------------------------
