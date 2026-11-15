// Package protocol 提供纪元秘藏·轮回图录 (Mod_EraLunHuiAlbum_Base, Module 882 / 0x372) 协议。
//
// 抓包基准: /tmp/lunhuitulu.pcapng（2026-11-15，qq s0）：
//
//	0 panel_info            面板
//	1 get_task_info [Int tab] → 任务列表 [Short count] + count×[Int id, Int state, Int claimed] + Int
//	2 get_task [Int id]     领取任务奖励
//	3 get_karma_award [Int id] 领取轮回奖励
//	5 get_karma_cycle_award 轮回周期奖励/状态
package protocol

import "fmt"

const (
	// ActionIDEraLunHuiPanel 面板 (882/0)
	ActionIDEraLunHuiPanel uint32 = 0x03720000
	// ActionIDEraLunHuiTaskInfo 任务列表 (882/1)
	ActionIDEraLunHuiTaskInfo uint32 = 0x03720001
	// ActionIDEraLunHuiGetTask 领取任务奖励 (882/2)
	ActionIDEraLunHuiGetTask uint32 = 0x03720002
	// ActionIDEraLunHuiGetKarmaAward 领取轮回奖励 (882/3)
	ActionIDEraLunHuiGetKarmaAward uint32 = 0x03720003
	// ActionIDEraLunHuiKarmaCycleAward 轮回周期奖励 (882/5)
	ActionIDEraLunHuiKarmaCycleAward uint32 = 0x03720005
)

// EraLunHuiTask 表示轮回图录任务列表中的一项。
type EraLunHuiTask struct {
	ID      int32
	State   int32
	Claimed int32
}

func eraLunHuiPrevPacket(actionID uint32, prefix []byte) *Packet {
	payload := make([]byte, 0, len(prefix)+4)
	payload = append(payload, prefix...)
	payload = append(payload, 0, 0, 0, 0)
	pkt := NewPacket(actionID, payload)
	pkt.HasPrevAct = true
	return pkt
}

// BuildEraLunHuiPanelPacket 构造面板查询 (882/0)。
func BuildEraLunHuiPanelPacket() *Packet {
	return eraLunHuiPrevPacket(ActionIDEraLunHuiPanel, nil)
}

// BuildEraLunHuiTaskInfoPacket 构造任务列表查询 (882/1)。
func BuildEraLunHuiTaskInfoPacket(tab int32) *Packet {
	w := NewWriter()
	w.WriteInt32(tab)
	return eraLunHuiPrevPacket(ActionIDEraLunHuiTaskInfo, w.Bytes())
}

// BuildEraLunHuiGetTaskPacket 构造领取任务奖励 (882/2)。
func BuildEraLunHuiGetTaskPacket(id int32) *Packet {
	w := NewWriter()
	w.WriteInt32(id)
	return eraLunHuiPrevPacket(ActionIDEraLunHuiGetTask, w.Bytes())
}

// BuildEraLunHuiGetKarmaAwardPacket 构造领取轮回奖励 (882/3)。
func BuildEraLunHuiGetKarmaAwardPacket(id int32) *Packet {
	w := NewWriter()
	w.WriteInt32(id)
	return eraLunHuiPrevPacket(ActionIDEraLunHuiGetKarmaAward, w.Bytes())
}

// BuildEraLunHuiKarmaCycleAwardPacket 构造轮回周期奖励/状态 (882/5)。
func BuildEraLunHuiKarmaCycleAwardPacket() *Packet {
	return eraLunHuiPrevPacket(ActionIDEraLunHuiKarmaCycleAward, nil)
}

// ParseEraLunHuiResult 解析单字节结果码（0=成功）。
func ParseEraLunHuiResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty era lunhui result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// ParseEraLunHuiTasks 解析 882/1 任务列表。
//
// 线格式: [Short count] + count × [Int id, Int state, Int claimed] + Int tail。
func ParseEraLunHuiTasks(payload []byte) ([]EraLunHuiTask, error) {
	p := DecompressPayloadIfNeeded(payload)
	r := NewReader(p)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: era lunhui task count: %v", ErrInvalidPayload, err)
	}
	tasks := make([]EraLunHuiTask, 0, count)
	for i := 0; i < int(count); i++ {
		var t EraLunHuiTask
		if t.ID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: era lunhui task[%d].id: %v", ErrInvalidPayload, i, err)
		}
		if t.State, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: era lunhui task[%d].state: %v", ErrInvalidPayload, i, err)
		}
		if t.Claimed, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: era lunhui task[%d].claimed: %v", ErrInvalidPayload, i, err)
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

// BuildEraLunHuiTasksResultPacket 构造 882/1 回包，供离线 mock 与单测使用。
func BuildEraLunHuiTasksResultPacket(tasks []EraLunHuiTask, tail int32) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(tasks)))
	for _, t := range tasks {
		w.WriteInt32(t.ID)
		w.WriteInt32(t.State)
		w.WriteInt32(t.Claimed)
	}
	w.WriteInt32(tail)
	return NewPacket(ActionIDEraLunHuiTaskInfo, w.Bytes())
}