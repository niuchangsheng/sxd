// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现功能终结礼包 (Mod_FunctionEnd, Module 127) 协议。
//
// 协议基准 (原版 Main.swf com.protocols.Mod_FunctionEnd_Base):
//   - is_game_function_end         Module 127, Action 0, request []
//   - game_function_end_gift       Module 127, Action 1, request []
//   - get_game_function_end_gift   Module 127, Action 2, request [Short gift_id]
//   - random_award                 Module 127, Action 3, request [Short gift_id]
//   - notify_function_end          Module 127, Action 4, request [] (服务端主动推送)
//
// 玩法背景: 吉星高照、喜从天降 (龙鱼仙令/境界点)、灵脉、拜关公、聚灵等玩法
// 达到等级上限终结后，会转为每日可领取的「功能终结礼包」，统一由本模块下发。
package protocol

import "fmt"

const (
	// ModuleFunctionEnd 功能终结礼包模块 (0x007F)
	ModuleFunctionEnd uint16 = 127

	// ActionIDIsGameFunctionEnd 查询已终结功能列表 (Module 127, Action 0 -> 0x007F0000)
	ActionIDIsGameFunctionEnd uint32 = 0x007F0000
	// ActionIDGameFunctionEndGift 查询待领取的功能终结礼包列表 (Module 127, Action 1 -> 0x007F0001)
	ActionIDGameFunctionEndGift uint32 = 0x007F0001
	// ActionIDGetGameFunctionEndGift 领取指定功能终结礼包 (Module 127, Action 2 -> 0x007F0002)
	ActionIDGetGameFunctionEndGift uint32 = 0x007F0002
	// ActionIDFunctionEndRandomAward 预览指定功能终结礼包奖励 (Module 127, Action 3 -> 0x007F0003)
	ActionIDFunctionEndRandomAward uint32 = 0x007F0003
	// ActionIDNotifyFunctionEnd 服务端主动推送功能终结通知 (Module 127, Action 4 -> 0x007F0004)
	ActionIDNotifyFunctionEnd uint32 = 0x007F0004

	// 功能终结礼包业务结果码 (对齐 Mod_FunctionEnd_Base)
	FunctionEndSuccess      uint8 = 0 // SUCCESS 领取成功
	FunctionEndNoIngot      uint8 = 1 // NO_INGOT 元宝不足
	FunctionEndNoVIPLevel   uint8 = 2 // NO_VIP_LEVEL VIP 等级不足
	FunctionEndItemPackFull uint8 = 3 // ITEM_PACK_FULL 背包已满
)

// FunctionEndStatus 表示一个已终结功能及其状态。
type FunctionEndStatus struct {
	FunctionID int16 // 功能编号 (客户端 FunctionType.sign 映射)
	Ended      bool  // true 表示该功能已终结
}

// FunctionEndGift 表示一个待领取的功能终结礼包。
//
// 字段顺序对齐客户端 FunctionEndData.endGiftList:
// giftId, state_point, skill, xian_ling, fame, nimbus, coin_buff, exp_buff,
// ingot, bei, juling。
//
// 其中 IngotCost 是领取该礼包需要消耗的元宝 (0 表示免费)；
// 其余奖励字段仅作为诊断信息，实际奖励最终以领取回包或属性推送为准。
type FunctionEndGift struct {
	GiftID     int16 // 礼包编号
	StatePoint int32 // 境界点
	Skill      int32 // 技能点
	XianLing   int32 // 仙令
	Fame       int32 // 声望
	Nimbus     int32 // 灵脉
	CoinBuff   int32 // 铜钱加成
	ExpBuff    int32 // 经验加成
	IngotCost  int32 // 领取消耗元宝 (0 表示免费)
	Bei        uint8 // 倍数
	JuLing     int32 // 聚灵/灵尘
}

// FunctionEndClaimResult 表示领取功能终结礼包回包。
type FunctionEndClaimResult struct {
	Result     uint8 // 0 成功 / 1 元宝不足 / 2 VIP 等级不足 / 3 背包已满
	StatePoint int32 // 境界点
	Skill      int32 // 技能点
	XianLing   int32 // 仙令
	Fame       int32 // 声望
	Nimbus     int32 // 灵脉
	CoinBuff   int32 // 铜钱加成
	ExpBuff    int32 // 经验加成
	IsHaveNext bool  // 是否还有后续礼包可继续领取
	JuLing     int32 // 聚灵/灵尘
	Payload    []byte
}

// FunctionEndRandomAward 表示功能终结礼包奖励预览回包。
type FunctionEndRandomAward struct {
	StatePoint int32 // 境界点
	Skill      int32 // 技能点
	XianLing   int32 // 仙令
	Fame       int32 // 声望
	Nimbus     int32 // 灵脉
	ExpBuff    int32 // 经验加成
	Ingot      int32 // 元宝
	JuLing     int32 // 聚灵/灵尘
}

// BuildIsGameFunctionEndPacket 构造查询已终结功能列表封包 (Module 127, Action 0)。
func BuildIsGameFunctionEndPacket() *Packet {
	return NewPacket(ActionIDIsGameFunctionEnd, nil)
}

// BuildFunctionEndGiftListPacket 构造查询待领取终结礼包列表封包 (Module 127, Action 1)。
func BuildFunctionEndGiftListPacket() *Packet {
	return NewPacket(ActionIDGameFunctionEndGift, nil)
}

// BuildGetFunctionEndGiftPacket 构造领取指定终结礼包封包 (Module 127, Action 2)。
func BuildGetFunctionEndGiftPacket(giftID int16) *Packet {
	w := NewWriter()
	w.WriteInt16(giftID)
	return NewPacket(ActionIDGetGameFunctionEndGift, w.Bytes())
}

// BuildFunctionEndRandomAwardPacket 构造预览指定终结礼包奖励封包 (Module 127, Action 3)。
func BuildFunctionEndRandomAwardPacket(giftID int16) *Packet {
	w := NewWriter()
	w.WriteInt16(giftID)
	return NewPacket(ActionIDFunctionEndRandomAward, w.Bytes())
}

// ParseIsGameFunctionEndResult 解析已终结功能列表回包:
// uint16 count + count × (int16 function_id, uint8 ended)。
func ParseIsGameFunctionEndResult(payload []byte) ([]FunctionEndStatus, error) {
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: function end count: %v", ErrInvalidPayload, err)
	}
	statuses := make([]FunctionEndStatus, 0, count)
	for i := 0; i < int(count); i++ {
		functionID, err := r.ReadInt16()
		if err != nil {
			return nil, fmt.Errorf("%w: function id at %d: %v", ErrInvalidPayload, i, err)
		}
		ended, err := r.ReadUint8()
		if err != nil {
			return nil, fmt.Errorf("%w: function ended at %d: %v", ErrInvalidPayload, i, err)
		}
		statuses = append(statuses, FunctionEndStatus{FunctionID: functionID, Ended: ended == 1})
	}
	return statuses, nil
}

// ParseFunctionEndGiftListResult 解析待领取终结礼包列表回包:
// uint16 count + count × (int16 gift_id, int16 state_point, int32 skill, int32 xian_ling,
// int32 fame, int32 nimbus, int16 coin_buff, int16 exp_buff, int32 ingot, uint8 bei, int32 juling)。
func ParseFunctionEndGiftListResult(payload []byte) ([]FunctionEndGift, error) {
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: function end gift count: %v", ErrInvalidPayload, err)
	}
	gifts := make([]FunctionEndGift, 0, count)
	for i := 0; i < int(count); i++ {
		var g FunctionEndGift
		if g.GiftID, err = r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("%w: gift id at %d: %v", ErrInvalidPayload, i, err)
		}
		statePoint, err := r.ReadInt16()
		if err != nil {
			return nil, fmt.Errorf("%w: state point at %d: %v", ErrInvalidPayload, i, err)
		}
		g.StatePoint = int32(statePoint)
		if g.Skill, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: skill at %d: %v", ErrInvalidPayload, i, err)
		}
		if g.XianLing, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: xian ling at %d: %v", ErrInvalidPayload, i, err)
		}
		if g.Fame, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: fame at %d: %v", ErrInvalidPayload, i, err)
		}
		if g.Nimbus, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: nimbus at %d: %v", ErrInvalidPayload, i, err)
		}
		coinBuff, err := r.ReadInt16()
		if err != nil {
			return nil, fmt.Errorf("%w: coin buff at %d: %v", ErrInvalidPayload, i, err)
		}
		g.CoinBuff = int32(coinBuff)
		expBuff, err := r.ReadInt16()
		if err != nil {
			return nil, fmt.Errorf("%w: exp buff at %d: %v", ErrInvalidPayload, i, err)
		}
		g.ExpBuff = int32(expBuff)
		if g.IngotCost, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: ingot cost at %d: %v", ErrInvalidPayload, i, err)
		}
		if g.Bei, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: bei at %d: %v", ErrInvalidPayload, i, err)
		}
		if g.JuLing, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: juling at %d: %v", ErrInvalidPayload, i, err)
		}
		gifts = append(gifts, g)
	}
	return gifts, nil
}

// ParseGetGameFunctionEndGiftResult 解析领取终结礼包回包:
// uint8 result + int16 + int32 × 4 + int16 × 2 + uint8 is_have_next + int32。
func ParseGetGameFunctionEndGiftResult(payload []byte) (*FunctionEndClaimResult, error) {
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: function end claim result: %v", ErrInvalidPayload, err)
	}
	statePoint, err := r.ReadInt16()
	if err != nil {
		return nil, fmt.Errorf("%w: function end claim state point: %v", ErrInvalidPayload, err)
	}
	reward := FunctionEndClaimResult{Result: result, StatePoint: int32(statePoint)}
	if reward.Skill, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: function end claim skill: %v", ErrInvalidPayload, err)
	}
	if reward.XianLing, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: function end claim xian ling: %v", ErrInvalidPayload, err)
	}
	if reward.Fame, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: function end claim fame: %v", ErrInvalidPayload, err)
	}
	if reward.Nimbus, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: function end claim nimbus: %v", ErrInvalidPayload, err)
	}
	coinBuff, err := r.ReadInt16()
	if err != nil {
		return nil, fmt.Errorf("%w: function end claim coin buff: %v", ErrInvalidPayload, err)
	}
	reward.CoinBuff = int32(coinBuff)
	expBuff, err := r.ReadInt16()
	if err != nil {
		return nil, fmt.Errorf("%w: function end claim exp buff: %v", ErrInvalidPayload, err)
	}
	reward.ExpBuff = int32(expBuff)
	isHaveNext, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: function end claim is_have_next: %v", ErrInvalidPayload, err)
	}
	reward.IsHaveNext = isHaveNext == 1
	if reward.JuLing, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: function end claim juling: %v", ErrInvalidPayload, err)
	}
	reward.Payload = append([]byte(nil), payload...)
	return &reward, nil
}

// ParseFunctionEndRandomAwardResult 解析终结礼包奖励预览回包:
// int16 state_point + int32 × 7 (skill, xian_ling, fame, nimbus, exp_buff, ingot, juling)。
func ParseFunctionEndRandomAwardResult(payload []byte) (*FunctionEndRandomAward, error) {
	r := NewReader(payload)
	statePoint, err := r.ReadInt16()
	if err != nil {
		return nil, fmt.Errorf("%w: random award state point: %v", ErrInvalidPayload, err)
	}
	values := make([]int32, 7)
	for i := range values {
		if values[i], err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: random award field %d: %v", ErrInvalidPayload, i, err)
		}
	}
	return &FunctionEndRandomAward{
		StatePoint: int32(statePoint),
		Skill:      values[0],
		XianLing:   values[1],
		Fame:       values[2],
		Nimbus:     values[3],
		ExpBuff:    values[4],
		Ingot:      values[5],
		JuLing:     values[6],
	}, nil
}

// FunctionEndClaimResultText 返回领取结果码的中文语义。
func FunctionEndClaimResultText(code uint8) string {
	switch code {
	case FunctionEndSuccess:
		return "领取成功"
	case FunctionEndNoIngot:
		return "元宝不足"
	case FunctionEndNoVIPLevel:
		return "VIP 等级不足"
	case FunctionEndItemPackFull:
		return "背包已满"
	default:
		return "未知结果码"
	}
}
