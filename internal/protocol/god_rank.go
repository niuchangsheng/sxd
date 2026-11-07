// Package protocol 提供大神榜膜拜协议 (Mod_GodRankRank_Base, Module 500)。
//
//	get_rank_list 500/0 request=[UByte type]
//	worship      500/1 request=[] response=[UByte result]
package protocol

import "fmt"

const (
	// ActionIDGodRankWorship 大神榜膜拜 (500/1 -> 0x01F40001)
	ActionIDGodRankWorship uint32 = 0x01F40001
	// ActionIDGodRankBestPlayer 最佳玩家信息 (500/2 -> 0x01F40002)
	ActionIDGodRankBestPlayer uint32 = 0x01F40002
)

// 大神榜结果码。
const (
	GodRankResultSuccess      uint8 = 3
	GodRankResultHasWorshiped uint8 = 4
	GodRankResultFailed       uint8 = 5
	GodRankResultNoData       uint8 = 6
)

// BuildGodRankWorshipPacket 构造膜拜封包。
func BuildGodRankWorshipPacket() *Packet {
	return NewPacket(ActionIDGodRankWorship, nil)
}

// ParseGodRankResult 解析单字节结果码。
func ParseGodRankResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("大神榜结果码为空")
	}
	return payload[0], nil
}

// GodRankResultText 返回结果码文本。
func GodRankResultText(code uint8) string {
	switch code {
	case GodRankResultSuccess:
		return "成功"
	case GodRankResultHasWorshiped:
		return "今日已膜拜"
	case GodRankResultFailed:
		return "失败"
	case GodRankResultNoData:
		return "无数据"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}
