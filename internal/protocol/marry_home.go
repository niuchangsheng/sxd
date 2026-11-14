// Package protocol 提供家园每日奖励 (Mod_MarryHome_Base, Module 210) 协议编解码。
//
// @author Ateng
// @since 2026-11-07
package protocol

import (
	"fmt"
)

// ActionIDMarryHomeGetCake 领取家园小蛋糕奖励 (Module 210, Action 19 -> 0x00D20013)。
const ActionIDMarryHomeGetCake uint32 = 0x00D20013

// 家园业务结果码 (对齐 2026 官方微端 Mod_MarryHome_Base)。
const (
	// MarryHomeResultSuccess 领取成功
	MarryHomeResultSuccess uint8 = 0
	// MarryHomeResultNonMarryStatus 未结婚/无家园
	MarryHomeResultNonMarryStatus uint8 = 3
	// MarryHomeResultAlreadyGet 今日已领取
	MarryHomeResultAlreadyGet uint8 = 12
	// MarryHomeResultNonActivityTime 非家园活动时间
	MarryHomeResultNonActivityTime uint8 = 18
	// MarryHomeResultTodayLimit 今日次数已达上限
	MarryHomeResultTodayLimit uint8 = 20
	// MarryHomeResultItemPackFull 背包已满
	MarryHomeResultItemPackFull uint8 = 21
	// MarryHomeResultFunctionNoOpen 功能未开放
	MarryHomeResultFunctionNoOpen uint8 = 22
	// MarryHomeResultFailure 失败
	MarryHomeResultFailure uint8 = 23
)

// MarryHomeCakeAward 代表家园小蛋糕奖励中的单件道具。
type MarryHomeCakeAward struct {
	// ItemID 道具编号
	ItemID int32
	// ItemNum 道具数量
	ItemNum int32
}

// MarryHomeCakeResult 代表领取家园小蛋糕的结果。
//
// 线格式 (Mod_MarryHome_Base.get_cake):
// UByte result, [Int32 item_id, Int32 item_num] award_list。
type MarryHomeCakeResult struct {
	// Result 业务结果码 (MarryHomeResultSuccess 等)
	Result uint8
	// Awards 奖励道具明细
	Awards []MarryHomeCakeAward
}

// BuildMarryHomeGetCakePacket 构造领取家园小蛋糕封包。
//
// 官方 Main.swf 中 get_cake(210/19) 请求与 get_self_home_info(210/0) 一样，
// 仅包含 [Int player_id]，没有 PrevAct 尾部。
// 线上实测 (Issue #37): 追加 Uint32 PrevAct 的 8 字节请求会被服务端静默丢弃，
// 客户端一直等到超时；改为 4 字节后与 210/0 行为一致。
func BuildMarryHomeGetCakePacket(playerID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(playerID)
	return NewPacket(ActionIDMarryHomeGetCake, w.Bytes())
}

// ParseMarryHomeCakeResult 解析领取家园小蛋糕回包。
func ParseMarryHomeCakeResult(payload []byte) (*MarryHomeCakeResult, error) {
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read marry home result: %v", ErrInvalidPayload, err)
	}
	res := &MarryHomeCakeResult{Result: result}
	count, err := r.ReadUint16()
	if err != nil {
		// 失败码回包可能没有奖励列表。
		return res, nil
	}
	for i := 0; i < int(count); i++ {
		itemID, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read cake award item_id #%d: %v", ErrInvalidPayload, i, err)
		}
		num, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read cake award item_num #%d: %v", ErrInvalidPayload, i, err)
		}
		res.Awards = append(res.Awards, MarryHomeCakeAward{ItemID: itemID, ItemNum: num})
	}
	return res, nil
}

// BuildMarryHomeCakeResultPacket 构造领取家园小蛋糕回包 (测试与 Mock 服务端使用)。
func BuildMarryHomeCakeResultPacket(res MarryHomeCakeResult) *Packet {
	w := NewWriter()
	w.WriteUint8(res.Result)
	w.WriteUint16(uint16(len(res.Awards)))
	for _, award := range res.Awards {
		w.WriteInt32(award.ItemID)
		w.WriteInt32(award.ItemNum)
	}
	return NewPacket(ActionIDMarryHomeGetCake, w.Bytes())
}

// ActionIDMarryHomePlayerInfo 查询玩家家园信息 (Module 210, Action 2 -> 0x00D20002)。
//
// 官方客户端进入家园时先 get_player_home_info 建立家园场景上下文，
// 再调用 batch_get_furniture_effect；缺少该前置时 210/18 会被静默丢弃。
const ActionIDMarryHomePlayerInfo uint32 = 0x00D20002

// BuildMarryHomePlayerInfoPacket 构造玩家家园信息查询封包，仅 4 字节 player_id。
func BuildMarryHomePlayerInfoPacket(playerID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(playerID)
	return NewPacket(ActionIDMarryHomePlayerInfo, w.Bytes())
}

// ActionIDMarryHomeBatchFurnitureEffect 批量领取家具效果/家园奖励
// (Module 210, Action 18 -> 0x00D20012)。
const ActionIDMarryHomeBatchFurnitureEffect uint32 = 0x00D20012

// MarryHomeFurnitureEffect 表示批量领取结果中的一件家具效果。
type MarryHomeFurnitureEffect struct {
	// ItemID 家具/道具编号
	ItemID int32
	// Effect 效果值
	Effect int32
}

// BuildMarryHomeBatchFurnitureEffectPacket 构造批量家具效果领取封包。
//
// 2026-11-14 抓包 (/tmp/jiayuan.pcapng)：该动作实际在 8219 跨服节点上发送，
// 请求载荷为 4 字节 PrevAct（抓包中为 0x01FB0000）。只读领取，不消耗元宝。
func BuildMarryHomeBatchFurnitureEffectPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDMarryHomeBatchFurnitureEffect
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDMarryHomeBatchFurnitureEffect, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// ParseMarryHomeBatchFurnitureEffectResult 解析批量家具效果回包。
//
// 线格式: [[Int item_id, Int effect]]。
func ParseMarryHomeBatchFurnitureEffectResult(payload []byte) ([]MarryHomeFurnitureEffect, error) {
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("家具效果数量读取失败: %w", err)
	}
	out := make([]MarryHomeFurnitureEffect, 0, count)
	for i := 0; i < int(count); i++ {
		var e MarryHomeFurnitureEffect
		if e.ItemID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("家具效果[%d].item_id 读取失败: %w", i, err)
		}
		if e.Effect, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("家具效果[%d].effect 读取失败: %w", i, err)
		}
		out = append(out, e)
	}
	return out, nil
}

// BuildMarryHomeBatchFurnitureEffectResultPacket 构造批量家具效果回包，供离线 mock/单测使用。
func BuildMarryHomeBatchFurnitureEffectResultPacket(effects []MarryHomeFurnitureEffect) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(effects)))
	for _, e := range effects {
		w.WriteInt32(e.ItemID)
		w.WriteInt32(e.Effect)
	}
	return NewPacket(ActionIDMarryHomeBatchFurnitureEffect, w.Bytes())
}
