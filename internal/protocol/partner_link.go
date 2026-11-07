// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方「灵结」协议 (Mod_PartnerLink_Base, Module 290 / 0x0122)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_PartnerLink_Base 与
// com.datas.PartnerLinkData：
//   - get_info Act 0 -> 0x01220000  查询灵结等级、灵契与伙伴灵结状态
//   - activate Act 1 -> 0x01220001  [Byte link_type_id] 激活灵结
package protocol

import "fmt"

const (
	// ModulePartnerLink 灵结模块编号 (0x0122)
	ModulePartnerLink uint16 = 290
)

// 灵结动作编号 (Mod_PartnerLink_Base)。
const (
	// ActionIDPartnerLinkInfo 查询灵结信息 (Action 0 -> 0x01220000)
	ActionIDPartnerLinkInfo uint32 = 0x01220000
	// ActionIDPartnerLinkActivate 激活灵结 (Action 1 -> 0x01220001)
	ActionIDPartnerLinkActivate uint32 = 0x01220001
)

// 灵结结果码 (Mod_PartnerLink_Base)。
const (
	// PartnerLinkYes 是
	PartnerLinkYes uint8 = 0
	// PartnerLinkNo 否
	PartnerLinkNo uint8 = 1
	// PartnerLinkResultSuccess 成功
	PartnerLinkResultSuccess uint8 = 2
	// PartnerLinkResultLackOfLinkSoul 灵契不足
	PartnerLinkResultLackOfLinkSoul uint8 = 3
	// PartnerLinkResultFailed 失败
	PartnerLinkResultFailed uint8 = 4
	// PartnerLinkResultFunctionNotOpen 功能未开放
	PartnerLinkResultFunctionNotOpen uint8 = 12
	// PartnerLinkResultNotEnoughDaoYuan 道缘不足
	PartnerLinkResultNotEnoughDaoYuan uint8 = 14
	// PartnerLinkResultNotEnoughCoin 铜钱不足
	PartnerLinkResultNotEnoughCoin uint8 = 15
	// PartnerLinkResultNotEnoughIngot 元宝不足
	PartnerLinkResultNotEnoughIngot uint8 = 16
)

// PartnerLinkSoul 表示灵结所需的一个伙伴灵契。
type PartnerLinkSoul struct {
	// SoulTypeID 灵契类型
	SoulTypeID int16
	// IsEnhance 是否已强化
	IsEnhance uint8
	// NeedSoulNumber 需要数量
	NeedSoulNumber int16
}

// PartnerLinkEntry 表示一种灵结类型。
type PartnerLinkEntry struct {
	// LinkTypeID 灵结类型编号
	LinkTypeID uint8
	// IsActive 是否已激活 (0 是 / 1 否)
	IsActive uint8
	// Souls 所需灵契
	Souls []PartnerLinkSoul
	// IsAdvance 是否已进阶 (0 是 / 1 否)
	IsAdvance uint8
}

// PartnerLinkInfo 表示灵结面板。
type PartnerLinkInfo struct {
	// Level 灵结等级
	Level int16
	// LinkSoulList 灵契列表
	LinkSoulList [][2]int16
	// PartnerInfoList 伙伴信息
	PartnerInfoList []PartnerLinkPartner
	// Links 灵结类型列表
	Links []PartnerLinkEntry
}

// PartnerLinkPartner 表示一个已灵结伙伴。
type PartnerLinkPartner struct {
	// SoulTypeID 灵契类型
	SoulTypeID int16
	// RoleID 角色编号
	RoleID int16
	// Level 等级
	Level int16
	// StuntName 绝技名
	StuntName string
	// StuntInfo 绝技描述
	StuntInfo string
	// MaxHealth 最大生命
	MaxHealth int64
	// Strength 武力
	Strength int32
	// Agile 敏捷
	Agile int32
	// Intellect 绝技
	Intellect int32
	// IsEnhance 是否强化
	IsEnhance uint8
}

// BuildPartnerLinkInfoPacket 构造查询灵结信息封包 (0x01220000)。
func BuildPartnerLinkInfoPacket() *Packet {
	return NewPacket(ActionIDPartnerLinkInfo, nil)
}

// BuildPartnerLinkActivatePacket 构造激活灵结封包 (0x01220001)。
func BuildPartnerLinkActivatePacket(linkTypeID uint8) *Packet {
	return NewPacket(ActionIDPartnerLinkActivate, []byte{linkTypeID})
}

// ParsePartnerLinkInfoResult 解析灵结面板。
func ParsePartnerLinkInfoResult(payload []byte) (*PartnerLinkInfo, error) {
	r := NewReader(payload)
	info := &PartnerLinkInfo{}
	var err error
	if info.Level, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read partner link level: %v", ErrInvalidPayload, err)
	}
	soulCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read link soul count: %v", ErrInvalidPayload, err)
	}
	info.LinkSoulList = make([][2]int16, 0, soulCount)
	for i := 0; i < int(soulCount); i++ {
		a, err := r.ReadInt16()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read link soul[%d].a: %v", ErrInvalidPayload, i, err)
		}
		b, err := r.ReadInt16()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read link soul[%d].b: %v", ErrInvalidPayload, i, err)
		}
		info.LinkSoulList = append(info.LinkSoulList, [2]int16{a, b})
	}
	partnerCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read partner info count: %v", ErrInvalidPayload, err)
	}
	info.PartnerInfoList = make([]PartnerLinkPartner, 0, partnerCount)
	for i := 0; i < int(partnerCount); i++ {
		var p PartnerLinkPartner
		if p.SoulTypeID, err = r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("%w: failed to read partner[%d].soul_type: %v", ErrInvalidPayload, i, err)
		}
		if p.RoleID, err = r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("%w: failed to read partner[%d].role_id: %v", ErrInvalidPayload, i, err)
		}
		if p.Level, err = r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("%w: failed to read partner[%d].level: %v", ErrInvalidPayload, i, err)
		}
		if p.StuntName, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: failed to read partner[%d].stunt_name: %v", ErrInvalidPayload, i, err)
		}
		if p.StuntInfo, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: failed to read partner[%d].stunt_info: %v", ErrInvalidPayload, i, err)
		}
		if p.MaxHealth, err = r.ReadInt64(); err != nil {
			return nil, fmt.Errorf("%w: failed to read partner[%d].max_health: %v", ErrInvalidPayload, i, err)
		}
		for _, field := range []*int32{&p.Strength, &p.Agile, &p.Intellect} {
			if *field, err = r.ReadInt32(); err != nil {
				return nil, fmt.Errorf("%w: failed to read partner[%d] stats: %v", ErrInvalidPayload, i, err)
			}
		}
		if p.IsEnhance, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read partner[%d].is_enhance: %v", ErrInvalidPayload, i, err)
		}
		info.PartnerInfoList = append(info.PartnerInfoList, p)
	}
	linkCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read link count: %v", ErrInvalidPayload, err)
	}
	info.Links = make([]PartnerLinkEntry, 0, linkCount)
	for i := 0; i < int(linkCount); i++ {
		var link PartnerLinkEntry
		if link.LinkTypeID, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read link[%d].type: %v", ErrInvalidPayload, i, err)
		}
		if link.IsActive, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read link[%d].is_active: %v", ErrInvalidPayload, i, err)
		}
		soulN, err := r.ReadUint16()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read link[%d].soul count: %v", ErrInvalidPayload, i, err)
		}
		link.Souls = make([]PartnerLinkSoul, 0, soulN)
		for j := 0; j < int(soulN); j++ {
			var s PartnerLinkSoul
			if s.SoulTypeID, err = r.ReadInt16(); err != nil {
				return nil, fmt.Errorf("%w: failed to read link[%d].soul[%d].type: %v", ErrInvalidPayload, i, j, err)
			}
			if s.IsEnhance, err = r.ReadUint8(); err != nil {
				return nil, fmt.Errorf("%w: failed to read link[%d].soul[%d].is_enhance: %v", ErrInvalidPayload, i, j, err)
			}
			if s.NeedSoulNumber, err = r.ReadInt16(); err != nil {
				return nil, fmt.Errorf("%w: failed to read link[%d].soul[%d].need: %v", ErrInvalidPayload, i, j, err)
			}
			link.Souls = append(link.Souls, s)
		}
		if link.IsAdvance, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read link[%d].is_advance: %v", ErrInvalidPayload, i, err)
		}
		info.Links = append(info.Links, link)
	}
	return info, nil
}

// PartnerLinkActivateResult 表示激活灵结的结果。
type PartnerLinkActivateResult struct {
	// Result 结果码
	Result uint8
	// Level 激活后的灵结等级
	Level int16
	// Value 附加数值
	Value int16
}

// ParsePartnerLinkActivateResult 解析激活灵结回包。
func ParsePartnerLinkActivateResult(payload []byte) (*PartnerLinkActivateResult, error) {
	r := NewReader(payload)
	res := &PartnerLinkActivateResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read partner link activate result: %v", ErrInvalidPayload, err)
	}
	if res.Result != PartnerLinkResultSuccess {
		return res, nil
	}
	if res.Level, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read partner link level: %v", ErrInvalidPayload, err)
	}
	if res.Value, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read partner link value: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// BuildPartnerLinkInfoResultPacket 构造灵结面板回包，供离线 mock 与单测使用。
func BuildPartnerLinkInfoResultPacket(info PartnerLinkInfo) *Packet {
	w := NewWriter()
	w.WriteInt16(info.Level)
	w.WriteUint16(uint16(len(info.LinkSoulList)))
	for _, v := range info.LinkSoulList {
		w.WriteInt16(v[0])
		w.WriteInt16(v[1])
	}
	w.WriteUint16(uint16(len(info.PartnerInfoList)))
	for _, p := range info.PartnerInfoList {
		w.WriteInt16(p.SoulTypeID)
		w.WriteInt16(p.RoleID)
		w.WriteInt16(p.Level)
		w.WriteString(p.StuntName)
		w.WriteString(p.StuntInfo)
		w.WriteInt64(p.MaxHealth)
		w.WriteInt32(p.Strength)
		w.WriteInt32(p.Agile)
		w.WriteInt32(p.Intellect)
		w.WriteUint8(p.IsEnhance)
	}
	w.WriteUint16(uint16(len(info.Links)))
	for _, link := range info.Links {
		w.WriteUint8(link.LinkTypeID)
		w.WriteUint8(link.IsActive)
		w.WriteUint16(uint16(len(link.Souls)))
		for _, s := range link.Souls {
			w.WriteInt16(s.SoulTypeID)
			w.WriteUint8(s.IsEnhance)
			w.WriteInt16(s.NeedSoulNumber)
		}
		w.WriteUint8(link.IsAdvance)
	}
	return NewPacket(ActionIDPartnerLinkInfo, w.Bytes())
}

// BuildPartnerLinkActivateResultPacket 构造激活灵结回包，供离线 mock 与单测使用。
func BuildPartnerLinkActivateResultPacket(res PartnerLinkActivateResult) *Packet {
	w := NewWriter()
	w.WriteUint8(res.Result)
	if res.Result == PartnerLinkResultSuccess {
		w.WriteInt16(res.Level)
		w.WriteInt16(res.Value)
	}
	return NewPacket(ActionIDPartnerLinkActivate, w.Bytes())
}

// 天缘录缘魂宝箱商店 (Mod_PartnerLink_Base Act 2/3)。
const (
	// ActionIDPartnerLinkShop 查询缘魂宝箱商店 (Action 2 -> 0x01220002)
	ActionIDPartnerLinkShop uint32 = 0x01220002
	// ActionIDPartnerLinkBuy 购买缘魂宝箱 (Action 3 -> 0x01220003)
	ActionIDPartnerLinkBuy uint32 = 0x01220003
)

// 缘魂宝箱结算货币 (Mod_PartnerLink_Base)。
const (
	// PartnerLinkMoneyDaoYuan 道缘
	PartnerLinkMoneyDaoYuan uint8 = 8
	// PartnerLinkMoneyCoin 铜钱
	PartnerLinkMoneyCoin uint8 = 9
	// PartnerLinkMoneyIngot 元宝 (自动化永不购买)
	PartnerLinkMoneyIngot uint8 = 10
)

// PartnerLinkResultNotBuyCount 可购买次数不足 (线上实测)。
const PartnerLinkResultNotBuyCount uint8 = 13

// PartnerLinkBox 表示商店中的一个缘魂宝箱档位。
type PartnerLinkBox struct {
	// BoxType 宝箱类型
	BoxType uint8
	// OnceNum 单次开启数量
	OnceNum int32
	// Times 剩余可购买次数
	Times int32
	// TotalTimes 每日总次数
	TotalTimes int32
	// MoneyType 结算货币 (8 道缘 / 9 铜钱 / 10 元宝)
	MoneyType uint8
	// MoneyNum 单价
	MoneyNum int32
}

// BuildPartnerLinkShopPacket 构造缘魂宝箱商店查询封包。
func BuildPartnerLinkShopPacket() *Packet {
	pkt := NewPacket(ActionIDPartnerLinkShop, []byte{0, 0, 0, 0})
	pkt.HasPrevAct = true
	return pkt
}

// BuildPartnerLinkBuyPacket 构造购买缘魂宝箱封包。
func BuildPartnerLinkBuyPacket(boxType uint8) *Packet {
	return NewPacket(ActionIDPartnerLinkBuy, []byte{boxType})
}

// ParsePartnerLinkShopResult 解析缘魂宝箱商店回包。
func ParsePartnerLinkShopResult(payload []byte) ([]PartnerLinkBox, error) {
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("缘魂宝箱数量读取失败: %w", err)
	}
	boxes := make([]PartnerLinkBox, 0, count)
	for i := 0; i < int(count); i++ {
		var b PartnerLinkBox
		if b.BoxType, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("宝箱[%d]类型读取失败: %w", i, err)
		}
		if b.OnceNum, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("宝箱[%d]单次数量读取失败: %w", i, err)
		}
		if b.Times, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("宝箱[%d]剩余次数读取失败: %w", i, err)
		}
		if b.TotalTimes, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("宝箱[%d]总次数读取失败: %w", i, err)
		}
		if b.MoneyType, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("宝箱[%d]货币类型读取失败: %w", i, err)
		}
		if b.MoneyNum, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("宝箱[%d]单价读取失败: %w", i, err)
		}
		boxes = append(boxes, b)
	}
	return boxes, nil
}

// BuildPartnerLinkShopResultPacket 构造缘魂宝箱商店回包，供离线 mock 与单测使用。
func BuildPartnerLinkShopResultPacket(boxes []PartnerLinkBox) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(boxes)))
	for _, b := range boxes {
		w.WriteUint8(b.BoxType)
		w.WriteInt32(b.OnceNum)
		w.WriteInt32(b.Times)
		w.WriteInt32(b.TotalTimes)
		w.WriteUint8(b.MoneyType)
		w.WriteInt32(b.MoneyNum)
	}
	return NewPacket(ActionIDPartnerLinkShop, w.Bytes())
}
