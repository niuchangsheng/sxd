// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方 VIP 每日礼包协议 (Mod_Vip_Base, Module 18 / 0x12)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_Vip_Base 与 com.datas.VipData：
// 免费礼包 get_send_gift 每天可领取一次，按 VIP 等级逐档领取；
// buy_send_gift 需要消耗元宝，自动化永不调用。
package protocol

import "fmt"

const (
	// ActionIDVIPInfo 查询 VIP 等级与礼包领取状态 (Module 18, Action 0 -> 0x00120000)
	ActionIDVIPInfo uint32 = 0x00120000
	// ActionIDVIPSendGift 领取免费 VIP 每日礼包 (Module 18, Action 1 -> 0x00120001)
	ActionIDVIPSendGift uint32 = 0x00120001
	// ActionIDVIPBuyGift 购买付费 VIP 礼包 (Module 18, Action 2 -> 0x00120002；消耗元宝，自动化不调用)
	ActionIDVIPBuyGift uint32 = 0x00120002
)

// Mod_Vip_Base 结果码。
const (
	// VIPResultSuccess 领取成功
	VIPResultSuccess uint8 = 0
	// VIPResultNotEnoughLevel VIP 等级不足
	VIPResultNotEnoughLevel uint8 = 1
	// VIPResultFailed 领取失败
	VIPResultFailed uint8 = 2
	// VIPResultPackFull 背包已满
	VIPResultPackFull uint8 = 3
	// VIPResultNoIngot 元宝不足 (仅付费礼包)
	VIPResultNoIngot uint8 = 4
)

// VIPGiftState 表示单个 VIP 等级礼包的领取状态。
type VIPGiftState struct {
	// Level VIP 等级
	Level uint8
	// SendGiftGot 免费礼包是否已领取 (0=可领取, 1=已领取)
	SendGiftGot uint8
	// BuyGiftGot 付费礼包是否已购买 (0=未购买, 1=已购买)
	BuyGiftGot uint8
}

// SendGiftClaimable 判断该档免费礼包是否可领取。
func (s VIPGiftState) SendGiftClaimable() bool { return s.SendGiftGot == 0 }

// VIPInfo 表示 get_player_vip_info 回包。
//
// 线格式: Int player_vip_level, Int money_to_next_level,
// [Byte vip_level, Byte send_gift_is_get, Byte buy_gift_is_get] gift_list。
type VIPInfo struct {
	// VIPLevel 当前 VIP 等级
	VIPLevel int32
	// MoneyToNextLevel 距离下一等级所需充值金额
	MoneyToNextLevel int32
	// Gifts 各等级礼包领取状态
	Gifts []VIPGiftState
}

// BuildVIPInfoPacket 构造查询 VIP 信息封包 (0x00120000)。
func BuildVIPInfoPacket() *Packet {
	return NewPacket(ActionIDVIPInfo, nil)
}

// BuildVIPSendGiftPacket 构造领取指定等级免费 VIP 礼包封包 (0x00120001)。
func BuildVIPSendGiftPacket(level uint8) *Packet {
	return NewPacket(ActionIDVIPSendGift, []byte{level})
}

// ParseVIPInfoResult 解析 VIP 信息回包。
func ParseVIPInfoResult(payload []byte) (*VIPInfo, error) {
	r := NewReader(payload)
	res := &VIPInfo{}
	var err error
	if res.VIPLevel, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read vip_level: %v", ErrInvalidPayload, err)
	}
	if res.MoneyToNextLevel, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read money_to_next_level: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read vip gift count: %v", ErrInvalidPayload, err)
	}
	res.Gifts = make([]VIPGiftState, 0, count)
	for i := 0; i < int(count); i++ {
		var g VIPGiftState
		if g.Level, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read vip gift[%d].level: %v", ErrInvalidPayload, i, err)
		}
		if g.SendGiftGot, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read vip gift[%d].send_gift_is_get: %v", ErrInvalidPayload, i, err)
		}
		if g.BuyGiftGot, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read vip gift[%d].buy_gift_is_get: %v", ErrInvalidPayload, i, err)
		}
		res.Gifts = append(res.Gifts, g)
	}
	return res, nil
}

// ParseVIPGiftResult 解析 VIP 礼包领取结果 (单字节结果码)。
func ParseVIPGiftResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty vip gift result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// BuildVIPInfoResultPacket 构造 VIP 信息回包，供离线 mock 与单测使用。
func BuildVIPInfoResultPacket(info VIPInfo) *Packet {
	w := NewWriter()
	w.WriteInt32(info.VIPLevel)
	w.WriteInt32(info.MoneyToNextLevel)
	w.WriteUint16(uint16(len(info.Gifts)))
	for _, g := range info.Gifts {
		w.WriteUint8(g.Level)
		w.WriteUint8(g.SendGiftGot)
		w.WriteUint8(g.BuyGiftGot)
	}
	return NewPacket(ActionIDVIPInfo, w.Bytes())
}

// BuildVIPGiftResultPacket 构造 VIP 礼包领取回包，供离线 mock 与单测使用。
func BuildVIPGiftResultPacket(result uint8) *Packet {
	return NewPacket(ActionIDVIPSendGift, []byte{result})
}
