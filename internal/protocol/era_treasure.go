// Package protocol 提供纪元秘藏/轮回图录协议 (Mod_EraTreasureOrder_Base, Module 848)。
//
//	get_panel_info    848/0 request=[]
//	get_award         848/1 request=[Int]
//	one_click_get_award 848/2 request=[]
//	task_info         848/5 request=[]
//	get_task          848/6 request=[Int]
package protocol

import "fmt"

const (
	ActionIDEraTreasurePanel    uint32 = 0x03500000
	ActionIDEraTreasureAward    uint32 = 0x03500001
	ActionIDEraTreasureOneKey   uint32 = 0x03500002
	ActionIDEraTreasureTaskInfo uint32 = 0x03500005
	ActionIDEraTreasureGetTask  uint32 = 0x03500006
)

// BuildEraTreasurePanelPacket 构造秘藏面板查询封包。
func BuildEraTreasurePanelPacket() *Packet {
	return NewPacket(ActionIDEraTreasurePanel, nil)
}

// BuildEraTreasureOneKeyPacket 构造一键领取封包。
func BuildEraTreasureOneKeyPacket() *Packet {
	return NewPacket(ActionIDEraTreasureOneKey, nil)
}

// BuildEraTreasureTaskInfoPacket 构造任务列表查询封包。
func BuildEraTreasureTaskInfoPacket() *Packet {
	return NewPacket(ActionIDEraTreasureTaskInfo, nil)
}

// ParseEraTreasureResult 解析单字节结果码。
func ParseEraTreasureResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("纪元秘藏结果码为空")
	}
	return payload[0], nil
}
