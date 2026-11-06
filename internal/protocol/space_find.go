// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方「寻找异兽」协议 (Mod_SpaceFind_Base, Module 123 / 0x007B)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_SpaceFind_Base、
// com.controllers.SpaceFindController 与 com.views.xian_yuan.SpaceFindView：
//   - open_space_find  Act 0  -> 0x007B0000  查询剩余寻找次数与免费状态
//   - do_space_find    Act 1  -> 0x007B0001  [UByte use_type] 0=普通(免费)/1=元宝
//   - get_space_find   Act 2  -> 0x007B0002  领取本次寻找到的道具
//
// 自动化只使用 use_type=0 的免费次数，绝不调用元宝寻宝，也不调用 150 元宝的
// call_golden_chaos_monster。
package protocol

import "fmt"

const (
	// ModuleSpaceFind 寻找异兽模块编号 (0x007B)
	ModuleSpaceFind uint16 = 123
)

// 寻找异兽动作编号 (Mod_SpaceFind_Base)。
const (
	// ActionIDSpaceFindOpen 打开寻找异兽面板 (Action 0 -> 0x007B0000)
	ActionIDSpaceFindOpen uint32 = 0x007B0000
	// ActionIDSpaceFindDo 执行一次寻找 (Action 1 -> 0x007B0001)
	ActionIDSpaceFindDo uint32 = 0x007B0001
	// ActionIDSpaceFindGet 领取本次寻找到的道具 (Action 2 -> 0x007B0002)
	ActionIDSpaceFindGet uint32 = 0x007B0002
)

// 寻找异兽消费类型 (Mod_SpaceFind_Base)。
const (
	// SpaceFindUseNormal 普通寻找 (免费次数)
	SpaceFindUseNormal uint8 = 0
	// SpaceFindUseIngot 元宝寻找 (自动化永不使用)
	SpaceFindUseIngot uint8 = 1
)

// 寻找异兽结果码 (Mod_SpaceFind_Base)。
const (
	// SpaceFindResultSuccess 成功
	SpaceFindResultSuccess uint8 = 2
	// SpaceFindResultFailed 失败
	SpaceFindResultFailed uint8 = 3
	// SpaceFindResultNoEnoughTime 时间不足
	SpaceFindResultNoEnoughTime uint8 = 4
	// SpaceFindResultNoEnoughIngot 元宝不足
	SpaceFindResultNoEnoughIngot uint8 = 5
	// SpaceFindResultAlreadyFind 已经寻找到
	SpaceFindResultAlreadyFind uint8 = 6
	// SpaceFindResultNoEnoughVIP VIP 不足
	SpaceFindResultNoEnoughVIP uint8 = 7
	// SpaceFindResultNoCount 寻找次数不足
	SpaceFindResultNoCount uint8 = 8
	// SpaceFindResultPackFull 背包已满
	SpaceFindResultPackFull uint8 = 9
	// SpaceFindResultFull 次数已满
	SpaceFindResultFull uint8 = 10
	// SpaceFindResultNotHaveFind 没有可领取的寻宝结果
	SpaceFindResultNotHaveFind uint8 = 13
	// SpaceFindResultHaveMonster 已有异兽等待处理
	SpaceFindResultHaveMonster uint8 = 16
)

// SpaceFindOpenInfo 表示寻找异兽面板状态。
type SpaceFindOpenInfo struct {
	// Count 剩余免费寻找次数
	Count int16
	// ItemID 当前已寻找到的道具 ID (0 表示没有)
	ItemID int32
	// Golden 金色异兽标记
	Golden int16
	// BeAppNickname 好友申请来源昵称
	BeAppNickname string
	// CanBuyCount 可购买次数
	CanBuyCount int16
	// Integral 寻宝积分
	Integral int32
	// UseType 当前消费类型
	UseType uint8
	// CallGoldenIngot 召唤金色异兽所需元宝
	CallGoldenIngot int32
	// RemainCatchCount 剩余抓捕次数
	RemainCatchCount int8
}

// SpaceFindDoResult 表示一次寻找的结果。
type SpaceFindDoResult struct {
	// Result 结果码
	Result uint8
	// ItemID 寻找到的道具 ID
	ItemID int32
	// Golden 金色异兽标记
	Golden int16
	// Integral 寻宝积分
	Integral int32
	// UseType 消费类型
	UseType uint8
}

// SpaceFindGoods 表示领取到的单件寻宝道具。
type SpaceFindGoods struct {
	// ItemID 道具 ID
	ItemID int32
	// Num 数量
	Num int16
}

// SpaceFindGetResult 表示领取寻宝道具的结果。
type SpaceFindGetResult struct {
	// Result 结果码
	Result uint8
	// Goods 领取到的道具列表
	Goods []SpaceFindGoods
	// Count 剩余免费寻找次数
	Count int16
	// CanBuyCount 可购买次数
	CanBuyCount int16
}

// BuildSpaceFindOpenPacket 构造打开寻找异兽面板封包 (0x007B0000)。
func BuildSpaceFindOpenPacket() *Packet {
	return NewPacket(ActionIDSpaceFindOpen, nil)
}

// BuildSpaceFindDoPacket 构造执行寻找封包 (0x007B0001)。
func BuildSpaceFindDoPacket(useType uint8) *Packet {
	return NewPacket(ActionIDSpaceFindDo, []byte{useType})
}

// BuildSpaceFindGetPacket 构造领取寻宝结果封包 (0x007B0002)。
func BuildSpaceFindGetPacket() *Packet {
	return NewPacket(ActionIDSpaceFindGet, nil)
}

// ParseSpaceFindOpenResult 解析寻找异兽面板。
func ParseSpaceFindOpenResult(payload []byte) (*SpaceFindOpenInfo, error) {
	r := NewReader(payload)
	info := &SpaceFindOpenInfo{}
	var err error
	if info.Count, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read space find count: %v", ErrInvalidPayload, err)
	}
	if info.ItemID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read space find item_id: %v", ErrInvalidPayload, err)
	}
	if info.Golden, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read space find golden: %v", ErrInvalidPayload, err)
	}
	if info.BeAppNickname, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: failed to read space find nickname: %v", ErrInvalidPayload, err)
	}
	if info.CanBuyCount, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read space find can_buy_count: %v", ErrInvalidPayload, err)
	}
	if info.Integral, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read space find integral: %v", ErrInvalidPayload, err)
	}
	if info.UseType, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read space find use_type: %v", ErrInvalidPayload, err)
	}
	if info.CallGoldenIngot, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read space find golden ingot: %v", ErrInvalidPayload, err)
	}
	if info.RemainCatchCount, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read space find remain catch: %v", ErrInvalidPayload, err)
	}
	return info, nil
}

// ParseSpaceFindDoResult 解析一次寻找结果。
func ParseSpaceFindDoResult(payload []byte) (*SpaceFindDoResult, error) {
	r := NewReader(payload)
	res := &SpaceFindDoResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read space find result: %v", ErrInvalidPayload, err)
	}
	if res.Result != SpaceFindResultSuccess {
		return res, nil
	}
	if res.ItemID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read space find found item: %v", ErrInvalidPayload, err)
	}
	if res.Golden, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read space find found golden: %v", ErrInvalidPayload, err)
	}
	if res.Integral, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read space find found integral: %v", ErrInvalidPayload, err)
	}
	if res.UseType, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read space find found use_type: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// ParseSpaceFindGetResult 解析领取寻宝道具结果。
func ParseSpaceFindGetResult(payload []byte) (*SpaceFindGetResult, error) {
	r := NewReader(payload)
	res := &SpaceFindGetResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read space find get result: %v", ErrInvalidPayload, err)
	}
	if res.Result != SpaceFindResultSuccess {
		return res, nil
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read space find goods count: %v", ErrInvalidPayload, err)
	}
	res.Goods = make([]SpaceFindGoods, 0, count)
	for i := 0; i < int(count); i++ {
		var g SpaceFindGoods
		if g.ItemID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read goods[%d].item_id: %v", ErrInvalidPayload, i, err)
		}
		if g.Num, err = r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("%w: failed to read goods[%d].num: %v", ErrInvalidPayload, i, err)
		}
		res.Goods = append(res.Goods, g)
	}
	if res.Count, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read space find remain count: %v", ErrInvalidPayload, err)
	}
	if res.CanBuyCount, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read space find can buy count: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// BuildSpaceFindOpenResultPacket 构造面板回包，供离线 mock 与单测使用。
func BuildSpaceFindOpenResultPacket(info SpaceFindOpenInfo) *Packet {
	w := NewWriter()
	w.WriteInt16(info.Count)
	w.WriteInt32(info.ItemID)
	w.WriteInt16(info.Golden)
	w.WriteString(info.BeAppNickname)
	w.WriteInt16(info.CanBuyCount)
	w.WriteInt32(info.Integral)
	w.WriteUint8(info.UseType)
	w.WriteInt32(info.CallGoldenIngot)
	w.WriteInt8(info.RemainCatchCount)
	return NewPacket(ActionIDSpaceFindOpen, w.Bytes())
}

// BuildSpaceFindDoResultPacket 构造寻找回包，供离线 mock 与单测使用。
func BuildSpaceFindDoResultPacket(res SpaceFindDoResult) *Packet {
	w := NewWriter()
	w.WriteUint8(res.Result)
	if res.Result == SpaceFindResultSuccess {
		w.WriteInt32(res.ItemID)
		w.WriteInt16(res.Golden)
		w.WriteInt32(res.Integral)
		w.WriteUint8(res.UseType)
	}
	return NewPacket(ActionIDSpaceFindDo, w.Bytes())
}

// BuildSpaceFindGetResultPacket 构造领取回包，供离线 mock 与单测使用。
func BuildSpaceFindGetResultPacket(res SpaceFindGetResult) *Packet {
	w := NewWriter()
	w.WriteUint8(res.Result)
	if res.Result == SpaceFindResultSuccess {
		w.WriteUint16(uint16(len(res.Goods)))
		for _, g := range res.Goods {
			w.WriteInt32(g.ItemID)
			w.WriteInt16(g.Num)
		}
		w.WriteInt16(res.Count)
		w.WriteInt16(res.CanBuyCount)
	}
	return NewPacket(ActionIDSpaceFindGet, w.Bytes())
}
