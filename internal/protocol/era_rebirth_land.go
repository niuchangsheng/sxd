// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现轮回净地挂机奖励 (Mod_EraRebirthLandAuto_Base, Module 852 / 0x0354)，
// 对齐官方每日活跃度任务 70「每日轮回净地领取挂机奖励」。
//
//	get_panel_info            (Act 1, 0x03540001) request=[] response=[Int,[Int,Int],Int x16]
//	get_auto_profit           (Act 9, 0x03540009) request=[] response=[UByte,Int,[Int,Long]]
//	one_click_quick_free_back (Act 6, 0x03540006) request=[] response=[UByte,[Int,Long]]
//	one_click_quick_ingot_back(Act 7) 元宝档，自动化永不调用
package protocol

import "fmt"

const (
	// ActionIDEraRebirthLandPanel 轮回净地面板 (Module 852, Action 1 -> 0x03540001)
	ActionIDEraRebirthLandPanel uint32 = 0x03540001
	// ActionIDEraRebirthLandFreeBack 免费一键领取挂机奖励 (Module 852, Action 6 -> 0x03540006)
	ActionIDEraRebirthLandFreeBack uint32 = 0x03540006
	// ActionIDEraRebirthLandAutoProfit 查询挂机收益 (Module 852, Action 9 -> 0x03540009)
	ActionIDEraRebirthLandAutoProfit uint32 = 0x03540009
	// ActionIDEraRebirthLandPoolPanel 渡业台/渡业仙池面板 (Module 852, Action 12 -> 0x0354000C)
	ActionIDEraRebirthLandPoolPanel uint32 = 0x0354000C
	// ActionIDEraRebirthLandFreeAward 领取渡业仙池免费奖励 (Module 852, Action 13 -> 0x0354000D)
	ActionIDEraRebirthLandFreeAward uint32 = 0x0354000D
	// ActionIDEraRebirthLandTodayAward 领取渡业仙池今日奖励 (Module 852, Action 14 -> 0x0354000E)
	ActionIDEraRebirthLandTodayAward uint32 = 0x0354000E
)

// 轮回净地结果码 (Mod_EraRebirthLandAuto_Base)。
const (
	// EraRebirthResultSuccess 成功
	EraRebirthResultSuccess uint8 = 0
	// EraRebirthResultFuncNotOpen 功能未开放
	EraRebirthResultFuncNotOpen uint8 = 1
	// EraRebirthResultAlreadyStart 已在挂机
	EraRebirthResultAlreadyStart uint8 = 2
	// EraRebirthResultFailed 失败
	EraRebirthResultFailed uint8 = 3
	// EraRebirthResultNoCanGetGift 暂无可领取奖励
	EraRebirthResultNoCanGetGift uint8 = 4
	// EraRebirthResultPackageFull 背包已满
	EraRebirthResultPackageFull uint8 = 5
	// EraRebirthResultNoStorage 无储物
	EraRebirthResultNoStorage uint8 = 6
	// EraRebirthResultNotEnoughIngot 元宝不足
	EraRebirthResultNotEnoughIngot uint8 = 7
	// EraRebirthResultNoFreeStorage 无免费储物
	EraRebirthResultNoFreeStorage uint8 = 8
	// EraRebirthResultNoTimes 次数不足
	EraRebirthResultNoTimes uint8 = 9
	// EraRebirthResultNoProfit 暂无收益
	EraRebirthResultNoProfit uint8 = 10
	// EraRebirthResultNotEnoughItem 道具不足
	EraRebirthResultNotEnoughItem uint8 = 11
	// EraRebirthResultNotStart 未开始挂机
	EraRebirthResultNotStart uint8 = 12
	// EraRebirthResultNotMatchCon 条件不匹配
	EraRebirthResultNotMatchCon uint8 = 13
	// EraRebirthResultAlreadyGet 已领取
	EraRebirthResultAlreadyGet uint8 = 14
	// EraRebirthResultAlreadyBuy 已购买
	EraRebirthResultAlreadyBuy uint8 = 15
	// EraRebirthResultNotEnoughTodayIngot 今日元宝不足
	EraRebirthResultNotEnoughTodayIngot uint8 = 16
	// EraRebirthResultNotEnoughFu 渡业符不足
	EraRebirthResultNotEnoughFu uint8 = 17
)

// EraRebirthReward 表示轮回净地领取的一笔奖励。
type EraRebirthReward struct {
	// ItemID 道具编号
	ItemID int32
	// Num 数量
	Num int64
}

// EraRebirthFreeBackResult 表示免费一键领取结果。
type EraRebirthFreeBackResult struct {
	// Result 结果码
	Result uint8
	// Rewards 奖励列表
	Rewards []EraRebirthReward
}

// BuildEraRebirthLandPanelPacket 构造轮回净地面板查询封包。
func BuildEraRebirthLandPanelPacket() *Packet {
	pkt := NewPacket(ActionIDEraRebirthLandPanel, []byte{0, 0, 0, 0})
	pkt.HasPrevAct = true
	return pkt
}

// BuildEraRebirthLandAutoProfitPacket 构造挂机收益查询封包。
func BuildEraRebirthLandAutoProfitPacket() *Packet {
	pkt := NewPacket(ActionIDEraRebirthLandAutoProfit, []byte{0, 0, 0, 0})
	pkt.HasPrevAct = true
	return pkt
}

// BuildEraRebirthLandFreeBackPacket 构造免费一键领取挂机奖励封包。
func BuildEraRebirthLandFreeBackPacket() *Packet {
	pkt := NewPacket(ActionIDEraRebirthLandFreeBack, []byte{0, 0, 0, 0})
	pkt.HasPrevAct = true
	return pkt
}

// BuildEraRebirthLandPoolPanelPacket 构造渡业台/渡业仙池面板查询封包 (852/12)。
func BuildEraRebirthLandPoolPanelPacket() *Packet {
	pkt := NewPacket(ActionIDEraRebirthLandPoolPanel, []byte{0, 0, 0, 0})
	pkt.HasPrevAct = true
	return pkt
}

// BuildEraRebirthLandFreeAwardPacket 构造领取渡业仙池免费奖励封包 (852/13)。
func BuildEraRebirthLandFreeAwardPacket() *Packet {
	pkt := NewPacket(ActionIDEraRebirthLandFreeAward, []byte{0, 0, 0, 0})
	pkt.HasPrevAct = true
	return pkt
}

// BuildEraRebirthLandTodayAwardPacket 构造领取渡业仙池今日奖励封包 (852/14)。
func BuildEraRebirthLandTodayAwardPacket() *Packet {
	pkt := NewPacket(ActionIDEraRebirthLandTodayAward, []byte{0, 0, 0, 0})
	pkt.HasPrevAct = true
	return pkt
}

// ParseEraRebirthSimpleResult 解析仅含单字节结果码的 852 回包。
func ParseEraRebirthSimpleResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("轮回净地结果码为空")
	}
	return payload[0], nil
}

// ParseEraRebirthFreeBackResult 解析免费一键领取回包 [UByte result, [Int, Long]]。
func ParseEraRebirthFreeBackResult(payload []byte) (*EraRebirthFreeBackResult, error) {
	r := NewReader(payload)
	res := &EraRebirthFreeBackResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("轮回净地结果码读取失败: %w", err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("轮回净地奖励数量读取失败: %w", err)
	}
	res.Rewards = make([]EraRebirthReward, 0, count)
	for i := 0; i < int(count); i++ {
		var reward EraRebirthReward
		if reward.ItemID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("轮回净地奖励[%d]道具读取失败: %w", i, err)
		}
		if reward.Num, err = r.ReadInt64(); err != nil {
			return nil, fmt.Errorf("轮回净地奖励[%d]数量读取失败: %w", i, err)
		}
		res.Rewards = append(res.Rewards, reward)
	}
	return res, nil
}

// EraRebirthResultText 返回轮回净地结果码的可读文本。
func EraRebirthResultText(code uint8) string {
	switch code {
	case EraRebirthResultSuccess:
		return "成功"
	case EraRebirthResultFuncNotOpen:
		return "功能未开放"
	case EraRebirthResultAlreadyStart:
		return "已在挂机"
	case EraRebirthResultFailed:
		return "失败"
	case EraRebirthResultNoCanGetGift:
		return "暂无可领取奖励"
	case EraRebirthResultPackageFull:
		return "背包已满"
	case EraRebirthResultNoStorage:
		return "无储物"
	case EraRebirthResultNotEnoughIngot:
		return "元宝不足"
	case EraRebirthResultNoFreeStorage:
		return "无免费储物"
	case EraRebirthResultNoTimes:
		return "次数不足"
	case EraRebirthResultNoProfit:
		return "暂无收益"
	case EraRebirthResultNotEnoughItem:
		return "道具不足"
	case EraRebirthResultNotStart:
		return "未开始挂机"
	case EraRebirthResultNotMatchCon:
		return "条件不匹配"
	case EraRebirthResultAlreadyGet:
		return "已领取"
	case EraRebirthResultAlreadyBuy:
		return "已购买"
	case EraRebirthResultNotEnoughTodayIngot:
		return "今日元宝不足"
	case EraRebirthResultNotEnoughFu:
		return "渡业符不足"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// BuildEraRebirthFreeBackResultPacket 构造免费领取回包，供离线 mock 与单测使用。
func BuildEraRebirthFreeBackResultPacket(result uint8, rewards []EraRebirthReward) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	w.WriteUint16(uint16(len(rewards)))
	for _, reward := range rewards {
		w.WriteInt32(reward.ItemID)
		w.WriteInt64(reward.Num)
	}
	return NewPacket(ActionIDEraRebirthLandFreeBack, w.Bytes())
}
