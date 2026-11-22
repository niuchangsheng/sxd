// Package protocol 提供神仙卡牌收藏集 (Mod_CultivationCardGame_Base, Module 859) 协议编解码。
package protocol

import (
	"fmt"
)

// 神仙卡牌收藏集动作 ID (Mod_CultivationCardGame_Base, Module 859)。
const (
	ActionIDCultivationCardPanel  uint32 = 0x035B0000
	ActionIDCultivationGroupAward uint32 = 0x035B0002
	ActionIDCultivationBagInfo    uint32 = 0x035B0003
	ActionIDCultivationOpenBag    uint32 = 0x035B0004
	ActionIDCultivationTaskInfo   uint32 = 0x035B0005
	ActionIDCultivationTaskAward  uint32 = 0x035B0006
	ActionIDCultivationShopInfo   uint32 = 0x035B0007
	ActionIDCultivationBuyShop    uint32 = 0x035B0008
)

// 神仙卡牌结果码。
const (
	CultivationCardSuccess       uint8 = 0
	CultivationCardFailed        uint8 = 1
	CultivationCardActNotOpen    uint8 = 2
	CultivationCardCardLock      uint8 = 3
	CultivationCardNotComplete   uint8 = 9
	CultivationCardHadGet        uint8 = 10
	CultivationCardItemPackFull  uint8 = 12
	CultivationCardNotEnoughItem uint8 = 13
	CultivationCardNotEnoughRes  uint8 = 14
	CultivationCardBuyLimit      uint8 = 15
	CultivationCardCondError     uint8 = 30
)

// CultivationCardTask 表示觅寻仙踪任务条目。
type CultivationCardTask struct {
	TaskID   int32
	Progress int32
	Flag     uint8
}

// CultivationCardBag 表示卡包条目。
type CultivationCardBag struct {
	ItemID int32
	Count  int32
}

// CultivationCardShopItem 表示卡包商城条目 [shop_id, left_number]。
//
// 客户端 CultivationCardGameData.shop_info 将两个 Int 解为 shop_id 与
// left_number（剩余可购次数）；静态价格与货币在 CultivationCardGameTypeData.objShop。
type CultivationCardShopItem struct {
	ShopID     int32
	LeftNumber int32
}

// BuildCultivationCardPanelPacket 构造收藏集面板查询封包。
func BuildCultivationCardPanelPacket() *Packet {
	return NewPacket(ActionIDCultivationCardPanel, nil)
}

// BuildCultivationCardTaskInfoPacket 构造任务信息查询封包。
func BuildCultivationCardTaskInfoPacket(groupID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(groupID)
	return NewPacket(ActionIDCultivationTaskInfo, w.Bytes())
}

// BuildCultivationCardTaskAwardPacket 构造任务奖励领取封包。
func BuildCultivationCardTaskAwardPacket(taskID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(taskID)
	return NewPacket(ActionIDCultivationTaskAward, w.Bytes())
}

// BuildCultivationCardBagInfoPacket 构造卡包查询封包。
func BuildCultivationCardBagInfoPacket() *Packet {
	return NewPacket(ActionIDCultivationBagInfo, nil)
}

// BuildCultivationCardOpenBagPacket 构造开卡包封包。
func BuildCultivationCardOpenBagPacket(itemID, count int32) *Packet {
	w := NewWriter()
	w.WriteInt32(itemID)
	w.WriteInt32(count)
	return NewPacket(ActionIDCultivationOpenBag, w.Bytes())
}

// BuildCultivationCardShopInfoPacket 构造卡包商城查询封包。
func BuildCultivationCardShopInfoPacket() *Packet {
	return NewPacket(ActionIDCultivationShopInfo, nil)
}

// BuildCultivationCardBuyShopPacket 构造卡包商城购买封包。
func BuildCultivationCardBuyShopPacket(itemID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(itemID)
	return NewPacket(ActionIDCultivationBuyShop, w.Bytes())
}

// BuildCultivationCardGroupAwardPacket 构造套装奖励领取封包。
func BuildCultivationCardGroupAwardPacket(groupID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(groupID)
	return NewPacket(ActionIDCultivationGroupAward, w.Bytes())
}

// ParseCultivationCardTasks 解析任务列表 [Int group, [Int task, Int progress, Byte flag]]。
func ParseCultivationCardTasks(payload []byte) (int32, []CultivationCardTask, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	group, err := r.ReadInt32()
	if err != nil {
		return 0, nil, fmt.Errorf("卡牌任务组读取失败: %w", err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return group, nil, nil
	}
	tasks := make([]CultivationCardTask, 0, count)
	for i := 0; i < int(count); i++ {
		var task CultivationCardTask
		if task.TaskID, err = r.ReadInt32(); err != nil {
			return group, tasks, fmt.Errorf("卡牌任务[%d] id 读取失败: %w", i, err)
		}
		if task.Progress, err = r.ReadInt32(); err != nil {
			return group, tasks, fmt.Errorf("卡牌任务[%d] 进度读取失败: %w", i, err)
		}
		if task.Flag, err = r.ReadUint8(); err != nil {
			return group, tasks, fmt.Errorf("卡牌任务[%d] 状态读取失败: %w", i, err)
		}
		tasks = append(tasks, task)
	}
	return group, tasks, nil
}

// ParseCultivationCardBags 解析卡包列表 [[Int item, Int count]]。
func ParseCultivationCardBags(payload []byte) ([]CultivationCardBag, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	count, err := r.ReadUint16()
	if err != nil {
		return nil, nil
	}
	bags := make([]CultivationCardBag, 0, count)
	for i := 0; i < int(count); i++ {
		var bag CultivationCardBag
		if bag.ItemID, err = r.ReadInt32(); err != nil {
			return bags, fmt.Errorf("卡包[%d] id 读取失败: %w", i, err)
		}
		if bag.Count, err = r.ReadInt32(); err != nil {
			return bags, fmt.Errorf("卡包[%d] 数量读取失败: %w", i, err)
		}
		bags = append(bags, bag)
	}
	return bags, nil
}

// ParseCultivationCardShop 解析卡包商城 [[Int shop_id, Int left_number], Int extra]。
//
// 尾随 Int 是玩家当前活动积分（不同角色不同，客户端 COST_ITEM[10442]）。
// 条目价格与货币需查静态表 CultivationCardShopGoods，不能把 left_number 当价格。
func ParseCultivationCardShop(payload []byte) ([]CultivationCardShopItem, int32, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	count, err := r.ReadUint16()
	if err != nil {
		return nil, 0, fmt.Errorf("卡包商城数量读取失败: %w", err)
	}
	items := make([]CultivationCardShopItem, 0, count)
	for i := 0; i < int(count); i++ {
		var item CultivationCardShopItem
		if item.ShopID, err = r.ReadInt32(); err != nil {
			return items, 0, fmt.Errorf("卡包商城[%d] shop_id 读取失败: %w", i, err)
		}
		if item.LeftNumber, err = r.ReadInt32(); err != nil {
			return items, 0, fmt.Errorf("卡包商城[%d] left_number 读取失败: %w", i, err)
		}
		items = append(items, item)
	}
	extra := int32(0)
	if r.Remaining() >= 4 {
		extra, _ = r.ReadInt32()
	}
	return items, extra, nil
}

// CultivationCardResultText 返回卡牌结果码文本。
func CultivationCardResultText(code uint8) string {
	switch code {
	case CultivationCardSuccess:
		return "成功"
	case CultivationCardFailed:
		return "失败"
	case CultivationCardActNotOpen:
		return "活动未开放"
	case CultivationCardCardLock:
		return "卡牌锁定"
	case CultivationCardNotComplete:
		return "未完成"
	case CultivationCardHadGet:
		return "已领取"
	case CultivationCardItemPackFull:
		return "背包已满"
	case CultivationCardNotEnoughItem:
		return "道具不足"
	case CultivationCardNotEnoughRes:
		return "资源不足"
	case CultivationCardBuyLimit:
		return "购买上限"
	case CultivationCardCondError:
		return "条件不满足"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}
