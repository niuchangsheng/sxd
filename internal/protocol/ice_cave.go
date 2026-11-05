// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方一键冰窟探险协议 (Mod_IceCaveTreasure_Base, Module 393 / 0x189)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_IceCaveTreasure_Base 与
// com.datas.IceCaveTreasureData：按冰面状态逐块敲击，锤子不足时停止；
// 自动化不调用 buy_hammer (消耗元宝)。
package protocol

import "fmt"

const (
	// ActionIDIceCaveInfo 查询冰窟活动状态 (Module 393, Action 0 -> 0x01890000)
	ActionIDIceCaveInfo uint32 = 0x01890000
	// ActionIDIceCaveHit 敲击冰面 (Module 393, Action 1 -> 0x01890001)
	ActionIDIceCaveHit uint32 = 0x01890001
	// ActionIDIceCaveBuyHammer 购买锤子 (Module 393, Action 2；消耗元宝，自动化不调用)
	ActionIDIceCaveBuyHammer uint32 = 0x01890002
	// ActionIDIceCaveGetGift 兑换冰窟奖励 (Module 393, Action 3 -> 0x01890003)
	ActionIDIceCaveGetGift uint32 = 0x01890003
)

// Mod_IceCaveTreasure_Base 结果码。
const (
	// IceCaveResultUnOpen 未开放
	IceCaveResultUnOpen uint8 = 0
	// IceCaveResultIsOpen 已开放
	IceCaveResultIsOpen uint8 = 1
	// IceCaveResultWaitOpen 等待开放
	IceCaveResultWaitOpen uint8 = 2
	// IceCaveResultSuccess 成功
	IceCaveResultSuccess uint8 = 3
	// IceCaveResultCompleted 已完成
	IceCaveResultCompleted uint8 = 4
	// IceCaveResultFuncNotOpen 功能未开放
	IceCaveResultFuncNotOpen uint8 = 5
	// IceCaveResultNonActivityTime 非活动时间
	IceCaveResultNonActivityTime uint8 = 6
	// IceCaveResultItemPackFull 背包已满
	IceCaveResultItemPackFull uint8 = 7
	// IceCaveResultAlreadyOpen 已开启
	IceCaveResultAlreadyOpen uint8 = 8
	// IceCaveResultLocationForbid 位置禁止
	IceCaveResultLocationForbid uint8 = 9
	// IceCaveResultErrorLocation 位置错误
	IceCaveResultErrorLocation uint8 = 10
	// IceCaveResultNotEnoughActive 活跃度不足
	IceCaveResultNotEnoughActive uint8 = 11
	// IceCaveResultFailure 失败
	IceCaveResultFailure uint8 = 12
	// IceCaveResultNotActivityTime 非活动时间 (另一码位)
	IceCaveResultNotActivityTime uint8 = 13
	// IceCaveResultNotEnoughIngot 元宝不足
	IceCaveResultNotEnoughIngot uint8 = 14
	// IceCaveResultHammerLimit 锤子已达上限
	IceCaveResultHammerLimit uint8 = 15
	// IceCaveResultAlreadyGet 已领取
	IceCaveResultAlreadyGet uint8 = 16
	// IceCaveResultGiftLimit 兑换次数上限
	IceCaveResultGiftLimit uint8 = 17
	// IceCaveResultNotEnoughGold 冰晶不足
	IceCaveResultNotEnoughGold uint8 = 18
)

// IceCaveSlot 表示一块冰面。
type IceCaveSlot struct {
	// Ice 冰面编号
	Ice int32
	// State 状态 (0=未敲开)
	State uint8
	// IsIron 是否为铁冰
	IsIron int32
}

// IceCaveInfo 表示冰窟活动面板。
type IceCaveInfo struct {
	// Slots 冰面列表
	Slots []IceCaveSlot
	// Location 当前所在位置
	Location int32
	// ActiveNum 当前活跃度
	ActiveNum int32
	// ActiveLimit 敲击所需活跃度
	ActiveLimit int32
	// HammerNum 剩余锤子
	HammerNum int32
	// HammerToday 今日已用锤子
	HammerToday int32
	// HammerLimit 锤子上限
	HammerLimit int32
	// HammerIngot 购买锤子所需元宝
	HammerIngot int32
	// GoldNum 冰晶数量
	GoldNum int32
	// GiftItem 兑换奖励道具
	GiftItem int32
	// GiftGold 兑换所需冰晶
	GiftGold int32
	// GiftCount 已兑换次数
	GiftCount int32
	// GiftLimit 兑换次数上限
	GiftLimit int32
}

// IceCaveHitResult 表示敲击结果。
type IceCaveHitResult struct {
	// Result 结果码
	Result uint8
	// AwardItems 本次奖励 [item_id, item_num]
	AwardItems [][2]int32
	// CompletedItems 通关奖励 [item_id, item_num]
	CompletedItems [][2]int32
}

// BuildIceCaveInfoPacket 构造查询冰窟状态封包 (0x01890000)。
func BuildIceCaveInfoPacket() *Packet {
	return NewPacket(ActionIDIceCaveInfo, nil)
}

// BuildIceCaveHitPacket 构造敲击冰面封包 (0x01890001)。
func BuildIceCaveHitPacket(ice, isIron int32) *Packet {
	w := NewWriter()
	w.WriteInt32(ice)
	w.WriteInt32(isIron)
	return NewPacket(ActionIDIceCaveHit, w.Bytes())
}

// BuildIceCaveGetGiftPacket 构造兑换奖励封包 (0x01890003)。
func BuildIceCaveGetGiftPacket() *Packet {
	return NewPacket(ActionIDIceCaveGetGift, nil)
}

// ParseIceCaveInfoResult 解析冰窟面板回包。
func ParseIceCaveInfoResult(payload []byte) (*IceCaveInfo, error) {
	r := NewReader(payload)
	res := &IceCaveInfo{}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read ice cave count: %v", ErrInvalidPayload, err)
	}
	res.Slots = make([]IceCaveSlot, 0, count)
	for i := 0; i < int(count); i++ {
		var s IceCaveSlot
		if s.Ice, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read ice cave[%d].ice: %v", ErrInvalidPayload, i, err)
		}
		if s.State, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read ice cave[%d].state: %v", ErrInvalidPayload, i, err)
		}
		if s.IsIron, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read ice cave[%d].is_iron: %v", ErrInvalidPayload, i, err)
		}
		res.Slots = append(res.Slots, s)
	}
	fields := []*int32{
		&res.Location, &res.ActiveNum, &res.ActiveLimit, &res.HammerNum,
		&res.HammerToday, &res.HammerLimit, &res.HammerIngot, &res.GoldNum,
		&res.GiftItem, &res.GiftGold, &res.GiftCount, &res.GiftLimit,
	}
	names := []string{
		"location", "active_num", "active_limit", "hammer_num",
		"hammer_today", "hammer_limit", "hammer_ingot", "gold_num",
		"gift_item", "gift_gold", "gift_count", "gift_limit",
	}
	for i, dst := range fields {
		if *dst, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read ice cave %s: %v", ErrInvalidPayload, names[i], err)
		}
	}
	return res, nil
}

// ParseIceCaveHitResult 解析敲击回包。
func ParseIceCaveHitResult(payload []byte) (*IceCaveHitResult, error) {
	r := NewReader(payload)
	res := &IceCaveHitResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read ice cave hit result: %v", ErrInvalidPayload, err)
	}
	if res.AwardItems, err = readIceCaveItems(r); err != nil {
		return nil, fmt.Errorf("%w: failed to read ice cave award list: %v", ErrInvalidPayload, err)
	}
	if res.CompletedItems, err = readIceCaveItems(r); err != nil {
		return nil, fmt.Errorf("%w: failed to read ice cave completed list: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// ParseIceCaveGiftResult 解析兑换结果 (单字节结果码)。
func ParseIceCaveGiftResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty ice cave gift result", ErrInvalidPayload)
	}
	return payload[0], nil
}

func readIceCaveItems(r *Reader) ([][2]int32, error) {
	count, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	out := make([][2]int32, 0, count)
	for i := 0; i < int(count); i++ {
		id, err := r.ReadInt32()
		if err != nil {
			return nil, err
		}
		num, err := r.ReadInt32()
		if err != nil {
			return nil, err
		}
		out = append(out, [2]int32{id, num})
	}
	return out, nil
}

// BuildIceCaveInfoResultPacket 构造冰窟面板回包，供离线 mock 与单测使用。
func BuildIceCaveInfoResultPacket(info IceCaveInfo) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(info.Slots)))
	for _, s := range info.Slots {
		w.WriteInt32(s.Ice)
		w.WriteUint8(s.State)
		w.WriteInt32(s.IsIron)
	}
	for _, v := range []int32{
		info.Location, info.ActiveNum, info.ActiveLimit, info.HammerNum,
		info.HammerToday, info.HammerLimit, info.HammerIngot, info.GoldNum,
		info.GiftItem, info.GiftGold, info.GiftCount, info.GiftLimit,
	} {
		w.WriteInt32(v)
	}
	return NewPacket(ActionIDIceCaveInfo, w.Bytes())
}

// BuildIceCaveHitResultPacket 构造敲击回包，供离线 mock 与单测使用。
func BuildIceCaveHitResultPacket(result uint8) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	w.WriteUint16(0)
	w.WriteUint16(0)
	return NewPacket(ActionIDIceCaveHit, w.Bytes())
}

// BuildIceCaveGiftResultPacket 构造兑换回包，供离线 mock 与单测使用。
func BuildIceCaveGiftResultPacket(result uint8) *Packet {
	return NewPacket(ActionIDIceCaveGetGift, []byte{result})
}
