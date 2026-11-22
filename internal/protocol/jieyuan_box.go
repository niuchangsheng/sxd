// Package protocol 提供结缘宝箱 (Mod_NewTown_Base, Module 232) 协议编解码。
package protocol

import (
	"fmt"
)

// JieYuanBoxItem 表示玩家背包持有的结缘宝箱条目。
type JieYuanBoxItem struct {
	ItemID int32 // 宝箱道具 ID (3937..3942)
	Count  int32 // 持有数量
}

// BuildJieYuanBoxListPacket 构造查询结缘宝箱列表封包 (Module 232, Action 30)。
func BuildJieYuanBoxListPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDJieYuanBoxList, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// ParseJieYuanBoxListResult 解析结缘宝箱列表回包。
func ParseJieYuanBoxListResult(payload []byte) ([]JieYuanBoxItem, error) {
	if len(payload) < 2 {
		return nil, nil
	}
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read box count: %v", ErrInvalidPayload, err)
	}
	var items []JieYuanBoxItem
	for i := 0; i < int(count); i++ {
		itemID, err := r.ReadInt32()
		if err != nil {
			break
		}
		itemCount, err := r.ReadInt32()
		if err != nil {
			break
		}
		items = append(items, JieYuanBoxItem{ItemID: itemID, Count: itemCount})
	}
	return items, nil
}

// FilterOpenableJieYuanBoxes 按照规则“除了传说，其他都开”过滤出允许开启的宝箱。
// 传说结缘宝箱 (ItemJieYuanBoxLegendary = 3942) 被严格剔除。
func FilterOpenableJieYuanBoxes(boxes []JieYuanBoxItem) []JieYuanBoxItem {
	var openable []JieYuanBoxItem
	for _, b := range boxes {
		if b.Count <= 0 {
			continue
		}
		// 严正过滤：跳过传说结缘宝箱
		if b.ItemID == ItemJieYuanBoxLegendary {
			continue
		}
		openable = append(openable, b)
	}
	return openable
}

// JieYuanBoxAutoMergeQuality 结缘宝箱自动合成档位，与 528/1 set_setting
// [0x07 0x04] 保持一致（客户端 LinkFateBoxView.autoMergeQuality）。
const JieYuanBoxAutoMergeQuality int32 = 4

// BuildJieYuanBoxOpenContextPacket 构造结缘宝箱开箱前置设置包 (Module 528, Action 1)。
//
// 抓包 /tmp/jieyuan2.pcapng 中官方请求为 [0x07 0x04][PrevAct=上一条发送的请求]，
// 例如列表之后为 232/30。服务端把 528/1 当作 232/34 的上下文令牌，并校验这 4 字节
// 尾链；只要中间插入任何别的请求（如开箱前的背包查询 2/41），固定写 232/30 就会
// 错位，6 字节 528/1 与其后的 232/34 都会被静默丢弃（2026-11-21 实测）。裸 2 字节
// 虽能拿到 result=0，但不会建立有效上下文，随后开箱仍被丢弃。
//
// 因此这里只写入占位 PrevAct，置 PrevActFromLastSent，由会话层在发送瞬间填充
// 「上一条发送的动作号」，与官方 DataBase.netCall 的 tmpBuff 链一致。
func BuildJieYuanBoxOpenContextPacket() *Packet {
	w := NewWriter()
	w.WriteUint8(0x07)
	w.WriteUint8(0x04)
	w.WriteUint32(0) // 占位，发送时由会话层填充为上一条发送动作
	pkt := NewPacket(ActionIDJieYuanBoxOpenContext, w.Bytes())
	pkt.PrevActFromLastSent = true
	return pkt
}

// BuildJieYuanBoxOpenPacket 构造开启结缘宝箱请求封包 (Module 232, Action 34)。
//
// 官方客户端 LinkFateBoxView.tenOpen 传入的是 [box_item_id, autoMergeQuality]，
// 不是数量：抓包 /tmp/jieyuan2.pcapng 中恒为 [item, 4]，4 正是自动合成档位；
// 一次请求最多消耗 10 个该箱型（服务端按 10 连开），库存不足时按剩余数量结算。
// 末尾 PrevAct 同样由会话层填充为上一条发送动作（紧邻的 528/1）。
func BuildJieYuanBoxOpenPacket(itemID int32, autoMergeQuality int32) *Packet {
	w := NewWriter()
	w.WriteInt32(itemID)
	w.WriteInt32(autoMergeQuality)
	w.WriteUint32(0) // 占位，发送时由会话层填充为上一条发送动作
	pkt := NewPacket(ActionIDJieYuanBoxOpen, w.Bytes())
	pkt.PrevActFromLastSent = true
	return pkt
}

// ParseJieYuanOpenContextResult 解析 528/1 前置上下文握手回包。
// 官方抓包回包载荷仅 1 字节 result_code（0x00 表示握手成功）。
func ParseJieYuanOpenContextResult(payload []byte) (uint8, error) {
	if len(payload) < 1 {
		return 0, fmt.Errorf("%w: empty jieyuan context response", ErrInvalidPayload)
	}
	return payload[0], nil
}

// ParseJieYuanBoxOpenResult 解析 232/34 批量开箱回包首字节 result_code。
//
// 官方抓包回包为整包 zlib 压缩，解压后结构为:
//
//	[Uint32 ActionID][UByte result_code][Uint16 reward_count][reward_count * 10B 奖励条目]
//
// 成功码为 5。调用方必须依据结果码分流，禁止仅凭“收到回包”判定开箱成功。
func ParseJieYuanBoxOpenResult(payload []byte) (uint8, error) {
	if len(payload) < 1 {
		return 0, fmt.Errorf("%w: empty jieyuan open response", ErrInvalidPayload)
	}
	return payload[0], nil
}

// --------------------------------------------------------------------------------
// 3. 领取日活跃奖励 (协议见 assistant.go)
// --------------------------------------------------------------------------------

// --------------------------------------------------------------------------------
// 4. 奇门遁甲
// --------------------------------------------------------------------------------
