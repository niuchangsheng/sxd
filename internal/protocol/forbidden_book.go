// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方禁地之书协议 (Mod_Awake_Base 中的 forbidden book 动作, Module 199 / 0xC7)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_Awake_Base 与 com.datas.AwakeData：
// 先查询各禁地区域剩余免费探索次数与铜钱/元宝价格，再探索或兑换次数。
package protocol

import "fmt"

const (
	// ActionIDForbiddenBookInfo 查询禁地之书信息 (Module 199, Action 11 -> 0x00C7000B)
	ActionIDForbiddenBookInfo uint32 = 0x00C7000B
	// ActionIDForbiddenBookExplore 探索禁地之书 (Module 199, Action 12 -> 0x00C7000C)
	ActionIDForbiddenBookExplore uint32 = 0x00C7000C
	// ActionIDForbiddenBookExchangeInfo 查询探索次数兑换信息 (Module 199, Action 13 -> 0x00C7000D)
	ActionIDForbiddenBookExchangeInfo uint32 = 0x00C7000D
	// ActionIDForbiddenBookExchange 兑换探索次数 (Module 199, Action 14 -> 0x00C7000E)
	ActionIDForbiddenBookExchange uint32 = 0x00C7000E
)

// 禁地类型与探索方式 (Mod_Awake_Base)。
const (
	// ForbiddenBookMortal 人间
	ForbiddenBookMortal uint8 = 18
	// ForbiddenBookRegulus 轩辕
	ForbiddenBookRegulus uint8 = 19
	// ForbiddenBookAncient 上古
	ForbiddenBookAncient uint8 = 20
	// ForbiddenBookOnce 单次探索
	ForbiddenBookOnce uint8 = 21
	// ForbiddenBookMulti 多次探索
	ForbiddenBookMulti uint8 = 22
)

// 结果码 (Mod_Awake_Base)。
const (
	// AwakeResultSuccess 成功
	AwakeResultSuccess uint8 = 5
	// AwakeResultNotOpen 功能未开放
	AwakeResultNotOpen uint8 = 6
	// AwakeResultNoEnoughCoin 铜钱不足
	AwakeResultNoEnoughCoin uint8 = 14
	// AwakeResultNoEnoughNumber 次数不足
	AwakeResultNoEnoughNumber uint8 = 23
	// AwakeResultNoEnoughIngot 元宝不足
	AwakeResultNoEnoughIngot uint8 = 24
	// AwakeResultPackFull 背包已满
	AwakeResultPackFull uint8 = 25
)

// ForbiddenBookArea 表示一个禁地区域的探索状态。
type ForbiddenBookArea struct {
	// ForbiddenType 禁地类型 (18 人间 / 19 轩辕 / 20 上古)
	ForbiddenType uint8
	// ExploreType 探索方式 (21 单次 / 22 多次)
	ExploreType uint8
	// CostCoin 兑换一次所需铜钱
	CostCoin int32
	// CostIngot 兑换一次所需元宝
	CostIngot int32
	// CurNumber 当前剩余探索次数
	CurNumber int16
	// TotalNumber 总探索次数上限
	TotalNumber int16
	// NextFreeTime 下次免费次数恢复时间
	NextFreeTime int32
}

// ForbiddenBookInfo 表示禁地之书面板。
type ForbiddenBookInfo struct {
	// Areas 各区域探索状态
	Areas []ForbiddenBookArea
	// ConsumScore 当前消耗积分
	ConsumScore int32
}

// BuildForbiddenBookInfoPacket 构造查询禁地之书封包 (0x00C7000B)。
func BuildForbiddenBookInfoPacket() *Packet {
	return NewPacket(ActionIDForbiddenBookInfo, nil)
}

// BuildForbiddenBookExplorePacket 构造探索封包 (0x00C7000C)。
func BuildForbiddenBookExplorePacket(forbiddenType, exploreType uint8) *Packet {
	return NewPacket(ActionIDForbiddenBookExplore, []byte{forbiddenType, exploreType})
}

// BuildForbiddenBookExchangePacket 构造兑换探索次数封包 (0x00C7000E)。
func BuildForbiddenBookExchangePacket(forbiddenType uint8) *Packet {
	return NewPacket(ActionIDForbiddenBookExchange, []byte{forbiddenType})
}

// ParseForbiddenBookInfoResult 解析禁地之书信息。
func ParseForbiddenBookInfoResult(payload []byte) (*ForbiddenBookInfo, error) {
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read forbidden book count: %v", ErrInvalidPayload, err)
	}
	res := &ForbiddenBookInfo{Areas: make([]ForbiddenBookArea, 0, count)}
	for i := 0; i < int(count); i++ {
		var a ForbiddenBookArea
		if a.ForbiddenType, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read area[%d].forbidden_type: %v", ErrInvalidPayload, i, err)
		}
		if a.ExploreType, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read area[%d].explore_type: %v", ErrInvalidPayload, i, err)
		}
		if a.CostCoin, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read area[%d].cost_coin: %v", ErrInvalidPayload, i, err)
		}
		if a.CostIngot, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read area[%d].cost_ingot: %v", ErrInvalidPayload, i, err)
		}
		if a.CurNumber, err = r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("%w: failed to read area[%d].cur_number: %v", ErrInvalidPayload, i, err)
		}
		if a.TotalNumber, err = r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("%w: failed to read area[%d].total_number: %v", ErrInvalidPayload, i, err)
		}
		if a.NextFreeTime, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read area[%d].next_free_time: %v", ErrInvalidPayload, i, err)
		}
		res.Areas = append(res.Areas, a)
	}
	if res.ConsumScore, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read consum_score: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// ParseForbiddenBookExploreResult 解析探索结果 (UByte result)。
func ParseForbiddenBookExploreResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty forbidden book explore result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// ParseForbiddenBookExchangeResult 解析兑换结果 (UByte result)。
func ParseForbiddenBookExchangeResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty forbidden book exchange result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// BuildForbiddenBookInfoResultPacket 构造禁地之书信息回包，供离线 mock 与单测使用。
func BuildForbiddenBookInfoResultPacket(info ForbiddenBookInfo) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(info.Areas)))
	for _, a := range info.Areas {
		w.WriteUint8(a.ForbiddenType)
		w.WriteUint8(a.ExploreType)
		w.WriteInt32(a.CostCoin)
		w.WriteInt32(a.CostIngot)
		w.WriteInt16(a.CurNumber)
		w.WriteInt16(a.TotalNumber)
		w.WriteInt32(a.NextFreeTime)
	}
	w.WriteInt32(info.ConsumScore)
	return NewPacket(ActionIDForbiddenBookInfo, w.Bytes())
}

// BuildForbiddenBookExploreResultPacket 构造探索回包，供离线 mock 与单测使用。
func BuildForbiddenBookExploreResultPacket(result uint8) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	w.WriteUint16(0)
	w.WriteInt32(0)
	return NewPacket(ActionIDForbiddenBookExplore, w.Bytes())
}

// BuildForbiddenBookExchangeResultPacket 构造兑换回包，供离线 mock 与单测使用。
func BuildForbiddenBookExchangeResultPacket(result uint8) *Packet {
	return NewPacket(ActionIDForbiddenBookExchange, []byte{result})
}
