// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现兵法圣水购买 (Mod_Tactics_Base, Module 299 / 0x012B)，
// 对齐官方每日活跃度任务 54「兵法」= 每次购买圣水 +5，每日上限 25 次。
//
//	get_tactics_info (Act 0, 0x012B0000) request=[] response=[Int,Int,[...],...]
//	buy_item         (Act 2, 0x012B0002) request=[UByte water_type] response=[UByte result]
//	  water_type: 湛蓝圣水=0 (铜钱), 黄金圣水=1 (元宝，自动化永不调用)
package protocol

import "fmt"

const (
	// ActionIDTacticsBuyItem 购买兵法圣水 (Module 299, Action 2 -> 0x012B0002)
	ActionIDTacticsBuyItem uint32 = 0x012B0002
)

// 兵法圣水类型 (Mod_Tactics_Base)。
const (
	// TacticsWaterXiao 湛蓝圣水 (铜钱购买)
	TacticsWaterXiao uint8 = 0
	// TacticsWaterDa 黄金圣水 (元宝购买，禁用)
	TacticsWaterDa uint8 = 1
	// TacticsWaterCoin 铜钱直接获取经验 (非圣水购买)
	TacticsWaterCoin uint8 = 2
	// TacticsWaterIngot 元宝直接获取经验 (禁用)
	TacticsWaterIngot uint8 = 3
)

// 兵法业务结果码 (Mod_Tactics_Base)。
const (
	// TacticsResultSucceed 成功 (购买/浇灌)
	TacticsResultSucceed uint8 = 7
	// TacticsResultNotOpenFun 功能未开启
	TacticsResultNotOpenFun uint8 = 8
	// TacticsResultNoEnoughItem 道具不足
	TacticsResultNoEnoughItem uint8 = 9
	// TacticsResultLevelLimit 等级不足
	TacticsResultLevelLimit uint8 = 10
	// TacticsResultFailed 失败
	TacticsResultFailed uint8 = 11
	// TacticsResultNoEnoughIngot 元宝不足
	TacticsResultNoEnoughIngot uint8 = 12
	// TacticsResultNoEnoughCoin 铜钱不足
	TacticsResultNoEnoughCoin uint8 = 13
	// TacticsResultBuyLimit 购买次数已达上限
	TacticsResultBuyLimit uint8 = 14
)

// BuildTacticsBuyItemPacket 构造购买兵法圣水封包。
//
// 官方 request=[UByte water_type]；线上实测购买只需 1 字节，无需 PrevAct。
func BuildTacticsBuyItemPacket(waterType uint8) *Packet {
	return NewPacket(ActionIDTacticsBuyItem, []byte{waterType})
}

// ParseTacticsResultCode 解析仅返回一个结果码的兵法回包。
func ParseTacticsResultCode(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty tactics result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// TacticsResultText 返回兵法结果码的可读文本。
func TacticsResultText(code uint8) string {
	switch code {
	case TacticsResultSucceed:
		return "成功"
	case TacticsResultNotOpenFun:
		return "功能未开启"
	case TacticsResultNoEnoughItem:
		return "道具不足"
	case TacticsResultLevelLimit:
		return "等级不足"
	case TacticsResultFailed:
		return "失败"
	case TacticsResultNoEnoughIngot:
		return "元宝不足"
	case TacticsResultNoEnoughCoin:
		return "铜钱不足"
	case TacticsResultBuyLimit:
		return "购买次数已达上限"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}
