// Package protocol 提供道缘商店/精品杂货 VIP 商店协议 (Mod_StDaoyuanShop_Base, Module 132)。
//
//	daoyuan_vip_shop_item_list 132/3 request=[UByte vip_level]
//	  response=[[Byte index, Int item_id, Short price, Short limit]]
//	buy_daoyuan_shop_item 132/1 request=[Byte index, Short count]
//	  response=[UByte result, Short]
package protocol

import "fmt"

const (
	// ActionIDDaoyuanVipShopList 查询 VIP 商店列表 (132/3 -> 0x00840003)
	ActionIDDaoyuanVipShopList uint32 = 0x00840003
	// ActionIDDaoyuanShopBuy 购买道缘商店商品 (132/1 -> 0x00840001)
	ActionIDDaoyuanShopBuy uint32 = 0x00840001
)

// 道缘商店结果码。
const (
	DaoyuanShopResultSuccess      uint8 = 0
	DaoyuanShopResultItemNotExist uint8 = 1
	DaoyuanShopResultNoDaoYuan    uint8 = 2
	DaoyuanShopResultNoIngot      uint8 = 3
	DaoyuanShopResultPackFull     uint8 = 8
	DaoyuanShopResultVipTwo       uint8 = 9
	DaoyuanShopResultVipFive      uint8 = 10
	DaoyuanShopResultVipEight     uint8 = 11
)

// DaoyuanShopItem 表示 VIP 商店中的一个条目。
type DaoyuanShopItem struct {
	Index  int8
	ItemID int32
	// Price 价格；具体货币由服务端商店配置决定，高位价格按元宝档处理。
	Price int16
	// Limit 剩余限购数量。
	Limit int16
}

// BuildDaoyuanVipShopListPacket 构造 VIP 商店列表查询封包。
//
// 官方 request=[UByte vip_level]；会话层在末尾补 4 字节真实 PrevAct。
func BuildDaoyuanVipShopListPacket(vipLevel uint8) *Packet {
	w := NewWriter()
	w.WriteUint8(vipLevel)
	w.WriteUint32(0)
	pkt := NewPacket(ActionIDDaoyuanVipShopList, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// BuildDaoyuanShopBuyPacket 构造购买封包。
func BuildDaoyuanShopBuyPacket(index int8, count int16) *Packet {
	w := NewWriter()
	w.WriteInt8(index)
	w.WriteInt16(count)
	return NewPacket(ActionIDDaoyuanShopBuy, w.Bytes())
}

// ParseDaoyuanVipShopList 解析 VIP 商店列表。
func ParseDaoyuanVipShopList(payload []byte) ([]DaoyuanShopItem, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("道缘商店条目数量读取失败: %w", err)
	}
	items := make([]DaoyuanShopItem, 0, count)
	for i := 0; i < int(count); i++ {
		var it DaoyuanShopItem
		if it.Index, err = r.ReadInt8(); err != nil {
			return nil, fmt.Errorf("道缘商店条目[%d].index 读取失败: %w", i, err)
		}
		if it.ItemID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("道缘商店条目[%d].item_id 读取失败: %w", i, err)
		}
		if it.Price, err = r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("道缘商店条目[%d].price 读取失败: %w", i, err)
		}
		if it.Limit, err = r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("道缘商店条目[%d].limit 读取失败: %w", i, err)
		}
		items = append(items, it)
	}
	return items, nil
}

// ParseDaoyuanShopBuyResult 解析购买结果。
func ParseDaoyuanShopBuyResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("道缘商店购买结果码为空")
	}
	return payload[0], nil
}

// DaoyuanShopResultText 返回结果码文本。
func DaoyuanShopResultText(code uint8) string {
	switch code {
	case DaoyuanShopResultSuccess:
		return "成功"
	case DaoyuanShopResultItemNotExist:
		return "商品不存在"
	case DaoyuanShopResultNoDaoYuan:
		return "道缘不足"
	case DaoyuanShopResultNoIngot:
		return "元宝不足"
	case DaoyuanShopResultPackFull:
		return "背包已满"
	case DaoyuanShopResultVipTwo:
		return "VIP2 不足"
	case DaoyuanShopResultVipFive:
		return "VIP5 不足"
	case DaoyuanShopResultVipEight:
		return "VIP8 不足"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}
