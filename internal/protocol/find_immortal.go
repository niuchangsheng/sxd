// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现喜从天降 (Mod_FindImmortal_Base, Module 68) 的龙鱼召唤/吹鱼/领奖
// 与五福祈福协议编解码。协议描述符对齐 2026 客户端 Main.swf 的 AS3 声明:
//   - open_find_immortal:           action 1,  response [Short,Short,Int,Short,Short,Short,Byte,Byte,Int,Int]
//   - start_find_immortal:          action 2,  response [UByte,Short,Int,Short,Short,Short,Byte,Byte,Short,Int]
//   - goad:                         action 3,  response [UByte,Short,Int,Short,Short,Short,Byte,Byte,Int]
//   - pickup_award:                 action 4,  response [UByte,Short,Int,Short,Short,Short,Int]
//   - get_xun_xian_ling_number:     action 5,  response [Short]
//   - exchange_xian_ling:           action 6,  request [Int], response [UByte]
//   - open_five_blessings:          action 10, response [Short,Byte,[Byte,Int,Short,Short,Short],Byte]
//   - start_bless/continue_bless:   action 11/12, response [UByte,Byte,[Byte,Int,Short,Short,Short]]
//   - end_bless:                    action 13, response [UByte,[Byte,Int,Short,Short,Short]]
//   - is_five_blessings_open:       action 14, response [UByte]
//   - five_blessings_change_lucky:  action 15, response [UByte,[Byte,Int,Short,Short,Short]]
//
// 客户端 DataBase.netCall 会在请求参数末尾追加前序 Module/Action 四字节，
// 因此所有空参数请求的载荷均为 4 字节前序协议号。
//
// 线上抓包 (2026-11-03) 确认服务端会在上述声明布局之后额外追加 12 字节扩展
// 字段 (panel 36 vs 24、start 33 vs 21、pickup 29 vs 17)，累计仙令等面板字段
// 位于该扩展字段内。本解析器只消费客户端声明的前缀，扩展字段暂不解析。
package protocol

import "fmt"

// FindImmortalAward 表示龙鱼界面当前可领取的奖励数值。
type FindImmortalAward struct {
	// Fame 声望
	Fame int16
	// Skill 阅历
	Skill int32
	// Cultivation 仙令
	Cultivation int16
	// StatePoint 境界点
	StatePoint int16
	// YuPai 玉牌
	YuPai int16
}

// FindImmortalPanel 表示喜从天降面板信息 (Mod 68, Act 1 回包)。
type FindImmortalPanel struct {
	// CanBuyNumber 当前可召唤龙鱼次数 (龙鱼令/购买次数)
	CanBuyNumber int16
	// Award 当前龙鱼可领取奖励
	Award FindImmortalAward
	// State 当前龙鱼品阶 (0 表示未召唤, 1~5 表示已吹至对应阶数)
	State int8
	// GodID 当前龙鱼形象编号
	GodID int8
	// Cultivation 累计仙令。注意: 线上抓包显示该偏移读到的并非累计仙令，
	// 真实值位于回包末尾 12 字节扩展字段内，当前字段仅供诊断，不可作为业务依据。
	Cultivation int32
	// TotalCount 累计领取次数
	TotalCount int32
}

// FindImmortalStartResult 表示召唤龙鱼结算 (Mod 68, Act 2 回包)。
type FindImmortalStartResult struct {
	// Result 业务结果码 (FindImmortalResult*)
	Result uint8
	// Award 新召唤龙鱼的可领取奖励
	Award FindImmortalAward
	// State 新龙鱼品阶
	State int8
	// GodID 新龙鱼形象编号
	GodID int8
	// RemainNumber 剩余吹鱼次数 (随龙鱼随机)，并非龙鱼令余量。
	// 龙鱼令余量必须通过 Action 5 get_xun_xian_ling_number 查询。
	RemainNumber int16
	// TotalCount 累计领取次数
	TotalCount int32
}

// FindImmortalGoadResult 表示一次吹龙鱼结算 (Mod 68, Act 3 回包)。
type FindImmortalGoadResult struct {
	// Result 业务结果码 (FindImmortalResult*)
	Result uint8
	// Award 吹龙鱼后的可领取奖励
	Award FindImmortalAward
	// State 吹龙鱼后的品阶
	State int8
	// GodID 龙鱼形象编号
	GodID int8
	// TotalCount 累计领取次数
	TotalCount int32
}

// FindImmortalPickupResult 表示收取龙鱼奖励结算 (Mod 68, Act 4 回包)。
type FindImmortalPickupResult struct {
	// Result 业务结果码 (FindImmortalResult*)
	Result uint8
	// Award 本次收取的奖励
	Award FindImmortalAward
	// Cultivation 本次收取获得的仙令 (线上抓包与累计仙令的增量一致)。
	// 累计仙令位于回包末尾 12 字节扩展字段中，当前解析器未消费该扩展字段。
	Cultivation int32
}

// FiveBlessingsAward 表示五福祈福翻出的一张奖励卡。
type FiveBlessingsAward struct {
	// ImageID 卡面/奖励形象编号
	ImageID int8
	// Skill 阅历
	Skill int32
	// XianLing 仙令
	XianLing int16
	// StarSoul 星魂
	StarSoul int16
	// JewelScrap 珠宝碎片
	JewelScrap int16
}

// FiveBlessingsInfo 表示五福祈福面板信息 (Mod 68, Act 10 回包)。
type FiveBlessingsInfo struct {
	// BlessNumber 剩余祈福次数
	BlessNumber int16
	// Stage 当前祈福阶段 (已翻出的奖励卡数量)
	Stage int8
	// Awards 当前已翻出的奖励卡
	Awards []FiveBlessingsAward
	// TotalCount 累计领取次数
	TotalCount int8
}

// FiveBlessingsActionResult 表示开始/继续祈福结算 (Mod 68, Act 11/12 回包)。
type FiveBlessingsActionResult struct {
	// Result 业务结果码 (FindImmortalResult*)
	Result uint8
	// Stage 当前祈福阶段
	Stage int8
	// Awards 当前已翻出的奖励卡
	Awards []FiveBlessingsAward
}

// FiveBlessingsEndResult 表示结束祈福并领取奖励结算 (Mod 68, Act 13/15 回包)。
type FiveBlessingsEndResult struct {
	// Result 业务结果码 (FindImmortalResult*)
	Result uint8
	// Awards 本次领取的奖励卡
	Awards []FiveBlessingsAward
}

// BuildFindImmortalOpenPacket 构造查询喜从天降面板封包 (Mod 68, Act 1)。
func BuildFindImmortalOpenPacket(prevAct uint32) *Packet {
	return newPrevActPacket(ActionIDFindImmortalOpen, prevAct)
}

// BuildFindImmortalStartPacket 构造召唤龙鱼封包 (Mod 68, Act 2)。
func BuildFindImmortalStartPacket(prevAct uint32) *Packet {
	return newPrevActPacket(ActionIDFindImmortalStart, prevAct)
}

// BuildFindImmortalGoadPacket 构造吹龙鱼封包 (Mod 68, Act 3)。
func BuildFindImmortalGoadPacket(prevAct uint32) *Packet {
	return newPrevActPacket(ActionIDFindImmortalGoad, prevAct)
}

// BuildFindImmortalPickupAwardPacket 构造收取龙鱼奖励封包 (Mod 68, Act 4)。
func BuildFindImmortalPickupAwardPacket(prevAct uint32) *Packet {
	return newPrevActPacket(ActionIDFindImmortalPickupAward, prevAct)
}

// BuildFindImmortalGetXunXianLingPacket 构造查询龙鱼令数量封包 (Mod 68, Act 5)。
func BuildFindImmortalGetXunXianLingPacket(prevAct uint32) *Packet {
	return newPrevActPacket(ActionIDFindImmortalGetXunXianLing, prevAct)
}

// BuildFindImmortalExchangeXianLingPacket 构造消耗声望兑换龙鱼令封包 (Mod 68, Act 6)。
// 请求载荷 = int32 兑换数量 + uint32 前序协议号。
func BuildFindImmortalExchangeXianLingPacket(count int32, prevAct uint32) *Packet {
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w := NewWriter()
	w.WriteInt32(count)
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDFindImmortalExchangeXianLing, w.Bytes())
}

// BuildFindImmortalOpenFiveBlessingsPacket 构造查询五福祈福面板封包 (Mod 68, Act 10)。
func BuildFindImmortalOpenFiveBlessingsPacket(prevAct uint32) *Packet {
	return newPrevActPacket(ActionIDFindImmortalOpenFiveBlessings, prevAct)
}

// BuildFindImmortalStartBlessPacket 构造开始五福祈福封包 (Mod 68, Act 11)。
func BuildFindImmortalStartBlessPacket(prevAct uint32) *Packet {
	return newPrevActPacket(ActionIDFindImmortalStartBless, prevAct)
}

// BuildFindImmortalContinueBlessPacket 构造继续五福祈福封包 (Mod 68, Act 12)。
func BuildFindImmortalContinueBlessPacket(prevAct uint32) *Packet {
	return newPrevActPacket(ActionIDFindImmortalContinueBless, prevAct)
}

// BuildFindImmortalEndBlessPacket 构造结束祈福并领奖封包 (Mod 68, Act 13)。
func BuildFindImmortalEndBlessPacket(prevAct uint32) *Packet {
	return newPrevActPacket(ActionIDFindImmortalEndBless, prevAct)
}

// BuildFindImmortalIsFiveBlessingsOpenPacket 构造查询五福祈福是否开启封包 (Mod 68, Act 14)。
func BuildFindImmortalIsFiveBlessingsOpenPacket(prevAct uint32) *Packet {
	return newPrevActPacket(ActionIDFindImmortalIsFiveBlessingsOpen, prevAct)
}

// BuildFindImmortalChangeLuckyPacket 构造一键祈福奖励变金封包 (Mod 68, Act 15)。
func BuildFindImmortalChangeLuckyPacket(prevAct uint32) *Packet {
	return newPrevActPacket(ActionIDFindImmortalChangeLucky, prevAct)
}

// ParseFindImmortalPanelResult 解析喜从天降面板回包:
// int16 can_buy + award(5 字段) + int8 state + int8 god_id + int32 cultivation + int32 total_count。
func ParseFindImmortalPanelResult(payload []byte) (*FindImmortalPanel, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))

	canBuy, err := r.ReadInt16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read can_buy_number: %v", ErrInvalidPayload, err)
	}
	award, err := readFindImmortalAward(r)
	if err != nil {
		return nil, err
	}
	state, err := r.ReadInt8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read state: %v", ErrInvalidPayload, err)
	}
	godID, err := r.ReadInt8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read god_id: %v", ErrInvalidPayload, err)
	}
	cultivation, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read cultivation: %v", ErrInvalidPayload, err)
	}
	totalCount, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read total_count: %v", ErrInvalidPayload, err)
	}

	return &FindImmortalPanel{
		CanBuyNumber: canBuy,
		Award:        award,
		State:        state,
		GodID:        godID,
		Cultivation:  cultivation,
		TotalCount:   totalCount,
	}, nil
}

// ParseFindImmortalStartResult 解析召唤龙鱼回包:
// uint8 result + award(5 字段) + int8 state + int8 god_id + int16 remain_number + int32 total_count。
func ParseFindImmortalStartResult(payload []byte) (*FindImmortalStartResult, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))

	result, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read result: %v", ErrInvalidPayload, err)
	}
	award, err := readFindImmortalAward(r)
	if err != nil {
		return nil, err
	}
	state, err := r.ReadInt8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read state: %v", ErrInvalidPayload, err)
	}
	godID, err := r.ReadInt8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read god_id: %v", ErrInvalidPayload, err)
	}
	remain, err := r.ReadInt16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read remain_number: %v", ErrInvalidPayload, err)
	}
	totalCount, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read total_count: %v", ErrInvalidPayload, err)
	}

	return &FindImmortalStartResult{
		Result:       result,
		Award:        award,
		State:        state,
		GodID:        godID,
		RemainNumber: remain,
		TotalCount:   totalCount,
	}, nil
}

// ParseFindImmortalGoadResult 解析吹龙鱼回包:
// uint8 result + award(5 字段) + int8 state + int8 god_id + int32 total_count。
func ParseFindImmortalGoadResult(payload []byte) (*FindImmortalGoadResult, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))

	result, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read result: %v", ErrInvalidPayload, err)
	}
	award, err := readFindImmortalAward(r)
	if err != nil {
		return nil, err
	}
	state, err := r.ReadInt8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read state: %v", ErrInvalidPayload, err)
	}
	godID, err := r.ReadInt8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read god_id: %v", ErrInvalidPayload, err)
	}
	totalCount, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read total_count: %v", ErrInvalidPayload, err)
	}

	return &FindImmortalGoadResult{
		Result:     result,
		Award:      award,
		State:      state,
		GodID:      godID,
		TotalCount: totalCount,
	}, nil
}

// ParseFindImmortalPickupResult 解析收取龙鱼奖励回包:
// uint8 result + award(5 字段) + int32 cultivation。
func ParseFindImmortalPickupResult(payload []byte) (*FindImmortalPickupResult, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))

	result, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read result: %v", ErrInvalidPayload, err)
	}
	award, err := readFindImmortalAward(r)
	if err != nil {
		return nil, err
	}
	cultivation, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read cultivation: %v", ErrInvalidPayload, err)
	}

	return &FindImmortalPickupResult{
		Result:      result,
		Award:       award,
		Cultivation: cultivation,
	}, nil
}

// ParseFindImmortalXunXianLingResult 解析龙鱼令数量回包: int16 number。
func ParseFindImmortalXunXianLingResult(payload []byte) (int16, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	number, err := r.ReadInt16()
	if err != nil {
		return 0, fmt.Errorf("%w: failed to read xun_xian_ling number: %v", ErrInvalidPayload, err)
	}
	return number, nil
}

// ParseFindImmortalExchangeResult 解析兑换龙鱼令回包: uint8 result。
func ParseFindImmortalExchangeResult(payload []byte) (uint8, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	result, err := r.ReadUint8()
	if err != nil {
		return 0, fmt.Errorf("%w: failed to read exchange result: %v", ErrInvalidPayload, err)
	}
	return result, nil
}

// ParseFiveBlessingsOpenFlagResult 解析五福祈福活动开启状态回包: uint8 result (YES/NO)。
func ParseFiveBlessingsOpenFlagResult(payload []byte) (bool, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	result, err := r.ReadUint8()
	if err != nil {
		return false, fmt.Errorf("%w: failed to read five blessings open flag: %v", ErrInvalidPayload, err)
	}
	return result == FindImmortalResultYes, nil
}

// ParseFiveBlessingsInfoResult 解析五福祈福面板回包:
// int16 bless_number + int8 stage + uint16 count + count*award + int8 total_count。
func ParseFiveBlessingsInfoResult(payload []byte) (*FiveBlessingsInfo, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))

	blessNumber, err := r.ReadInt16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read bless_number: %v", ErrInvalidPayload, err)
	}
	stage, err := r.ReadInt8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read stage: %v", ErrInvalidPayload, err)
	}
	awards, err := readFiveBlessingsAwards(r)
	if err != nil {
		return nil, err
	}
	totalCount, err := r.ReadInt8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read total_count: %v", ErrInvalidPayload, err)
	}

	return &FiveBlessingsInfo{
		BlessNumber: blessNumber,
		Stage:       stage,
		Awards:      awards,
		TotalCount:  totalCount,
	}, nil
}

// ParseFiveBlessingsActionResult 解析开始/继续祈福回包:
// uint8 result + int8 stage + uint16 count + count*award。
func ParseFiveBlessingsActionResult(payload []byte) (*FiveBlessingsActionResult, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))

	result, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read result: %v", ErrInvalidPayload, err)
	}
	stage, err := r.ReadInt8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read stage: %v", ErrInvalidPayload, err)
	}
	awards, err := readFiveBlessingsAwards(r)
	if err != nil {
		return nil, err
	}

	return &FiveBlessingsActionResult{
		Result: result,
		Stage:  stage,
		Awards: awards,
	}, nil
}

// ParseFiveBlessingsEndResult 解析结束祈福/一键变金回包:
// uint8 result + uint16 count + count*award。
func ParseFiveBlessingsEndResult(payload []byte) (*FiveBlessingsEndResult, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))

	result, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read result: %v", ErrInvalidPayload, err)
	}
	awards, err := readFiveBlessingsAwards(r)
	if err != nil {
		return nil, err
	}

	return &FiveBlessingsEndResult{
		Result: result,
		Awards: awards,
	}, nil
}

// readFindImmortalAward 读取龙鱼奖励五元组:
// int16 fame + int32 skill + int16 cultivation + int16 state_point + int16 yu_pai。
func readFindImmortalAward(r *Reader) (FindImmortalAward, error) {
	fame, err := r.ReadInt16()
	if err != nil {
		return FindImmortalAward{}, fmt.Errorf("%w: failed to read award fame: %v", ErrInvalidPayload, err)
	}
	skill, err := r.ReadInt32()
	if err != nil {
		return FindImmortalAward{}, fmt.Errorf("%w: failed to read award skill: %v", ErrInvalidPayload, err)
	}
	cultivation, err := r.ReadInt16()
	if err != nil {
		return FindImmortalAward{}, fmt.Errorf("%w: failed to read award cultivation: %v", ErrInvalidPayload, err)
	}
	statePoint, err := r.ReadInt16()
	if err != nil {
		return FindImmortalAward{}, fmt.Errorf("%w: failed to read award state_point: %v", ErrInvalidPayload, err)
	}
	yuPai, err := r.ReadInt16()
	if err != nil {
		return FindImmortalAward{}, fmt.Errorf("%w: failed to read award yu_pai: %v", ErrInvalidPayload, err)
	}
	return FindImmortalAward{
		Fame:        fame,
		Skill:       skill,
		Cultivation: cultivation,
		StatePoint:  statePoint,
		YuPai:       yuPai,
	}, nil
}

// readFiveBlessingsAwards 读取 uint16 长度前缀的五福奖励卡数组。
func readFiveBlessingsAwards(r *Reader) ([]FiveBlessingsAward, error) {
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read five blessings award count: %v", ErrInvalidPayload, err)
	}
	awards := make([]FiveBlessingsAward, 0, count)
	for i := 0; i < int(count); i++ {
		imageID, err := r.ReadInt8()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read award[%d] image_id: %v", ErrInvalidPayload, i, err)
		}
		skill, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read award[%d] skill: %v", ErrInvalidPayload, i, err)
		}
		xianLing, err := r.ReadInt16()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read award[%d] xian_ling: %v", ErrInvalidPayload, i, err)
		}
		starSoul, err := r.ReadInt16()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read award[%d] star_soul: %v", ErrInvalidPayload, i, err)
		}
		jewelScrap, err := r.ReadInt16()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read award[%d] jewel_scrap: %v", ErrInvalidPayload, i, err)
		}
		awards = append(awards, FiveBlessingsAward{
			ImageID:    imageID,
			Skill:      skill,
			XianLing:   xianLing,
			StarSoul:   starSoul,
			JewelScrap: jewelScrap,
		})
	}
	return awards, nil
}
