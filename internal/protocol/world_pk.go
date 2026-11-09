// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方跨服 PK 排行协议 (Mod_WorldPkRanking_Base, Module 349 / 0x15D)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_WorldPkRanking_Base：
// 先查询活动状态，仅在 ON 时加入 PK；OFF/超级城镇关闭时跳过。
package protocol

import "fmt"

const (
	// ActionIDWorldPkStatus 查询跨服 PK 状态 (Module 349, Action 0 -> 0x015D0000)
	ActionIDWorldPkStatus uint32 = 0x015D0000
	// ActionIDWorldPkJoin 加入跨服 PK (Module 349, Action 3 -> 0x015D0003)
	ActionIDWorldPkJoin uint32 = 0x015D0003
)

// Mod_WorldPkRanking_Base 状态/结果码。
const (
	// WorldPkStatusOn 活动开启
	WorldPkStatusOn uint8 = 0
	// WorldPkStatusOff 活动关闭
	WorldPkStatusOff uint8 = 1
	// WorldPkStatusSuperTownClose 超级城镇关闭
	WorldPkStatusSuperTownClose uint8 = 2
	// WorldPkResultSuccess 加入成功
	WorldPkResultSuccess uint8 = 3
	// WorldPkResultActivityNotOpen 活动未开放
	WorldPkResultActivityNotOpen uint8 = 4
	// WorldPkResultFailed 失败
	WorldPkResultFailed uint8 = 5
	// WorldPkResultTimeCD 冷却中
	WorldPkResultTimeCD uint8 = 6
	// WorldPkResultTimesLimit 次数上限
	WorldPkResultTimesLimit uint8 = 12
	// WorldPkResultLowLevel 等级不足
	WorldPkResultLowLevel uint8 = 23
)

// WorldPkStatus 表示跨服 PK 状态。
//
// 线格式: UByte status, Short, Int, Int, Int (后四项内容不消费)。
type WorldPkStatus struct {
	// Status 状态码
	Status uint8
}

// WorldPkJoinResult 表示加入 PK 的结果。
type WorldPkJoinResult struct {
	// Result 结果码
	Result uint8
}

// BuildWorldPkStatusPacket 构造查询跨服 PK 状态封包 (0x015D0000)。
func BuildWorldPkStatusPacket() *Packet {
	return NewPacket(ActionIDWorldPkStatus, nil)
}

// BuildWorldPkJoinPacket 构造加入跨服 PK 封包 (0x015D0003)。
func BuildWorldPkJoinPacket() *Packet {
	return NewPacket(ActionIDWorldPkJoin, nil)
}

// ParseWorldPkStatusResult 解析状态回包 (只消费首字节)。
func ParseWorldPkStatusResult(payload []byte) (*WorldPkStatus, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("%w: empty world pk status", ErrInvalidPayload)
	}
	return &WorldPkStatus{Status: payload[0]}, nil
}

// ParseWorldPkJoinResult 解析加入结果 (只消费首字节)。
func ParseWorldPkJoinResult(payload []byte) (*WorldPkJoinResult, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("%w: empty world pk join result", ErrInvalidPayload)
	}
	return &WorldPkJoinResult{Result: payload[0]}, nil
}

// BuildWorldPkStatusResultPacket 构造状态回包，供离线 mock 与单测使用。
func BuildWorldPkStatusResultPacket(status uint8) *Packet {
	w := NewWriter()
	w.WriteUint8(status)
	w.WriteInt16(0)
	w.WriteInt32(0)
	w.WriteInt32(0)
	w.WriteInt32(0)
	return NewPacket(ActionIDWorldPkStatus, w.Bytes())
}

// BuildWorldPkJoinResultPacket 构造加入回包，供离线 mock 与单测使用。
func BuildWorldPkJoinResultPacket(result uint8) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	w.WriteInt32(0)
	w.WriteInt32(0)
	return NewPacket(ActionIDWorldPkJoin, w.Bytes())
}

// 跨服 PK 奖励/任务动作 ID (Module 349)。
const (
	ActionIDWorldPkReceiveSpecial uint32 = 0x015D000E
	ActionIDWorldPkSetAutoMatch   uint32 = 0x015D0010
	ActionIDWorldPkAwardPanel     uint32 = 0x015D0019
	ActionIDWorldPkOpenTaskPanel  uint32 = 0x015D001A
	ActionIDWorldPkTaskAward      uint32 = 0x015D001B
)

// 跨服 PK 奖励结果码。
const (
	WorldPkRewardSuccess      uint8 = 3
	WorldPkRewardFailed       uint8 = 5
	WorldPkRewardTimesLimit   uint8 = 12
	WorldPkRewardPackFull     uint8 = 13
	WorldPkRewardNotCompleted uint8 = 22
)

// BuildWorldPkSetAutoMatchPacket 构造自动匹配开关封包 [Byte]。
func BuildWorldPkSetAutoMatchPacket(on bool) *Packet {
	v := uint8(0)
	if on {
		v = 1
	}
	return NewPacket(ActionIDWorldPkSetAutoMatch, []byte{v})
}

// BuildWorldPkReceiveSpecialPacket 构造领取特殊奖励封包。
func BuildWorldPkReceiveSpecialPacket() *Packet {
	return NewPacket(ActionIDWorldPkReceiveSpecial, nil)
}

// BuildWorldPkOpenTaskPanelPacket 构造打开每日任务面板封包。
func BuildWorldPkOpenTaskPanelPacket() *Packet {
	return NewPacket(ActionIDWorldPkOpenTaskPanel, nil)
}

// BuildWorldPkTaskAwardPacket 构造领取每日任务奖励封包。
func BuildWorldPkTaskAwardPacket(taskID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(taskID)
	return NewPacket(ActionIDWorldPkTaskAward, w.Bytes())
}

// BuildWorldPkAwardPanelPacket 构造奖励面板查询封包。
func BuildWorldPkAwardPanelPacket() *Packet {
	return NewPacket(ActionIDWorldPkAwardPanel, nil)
}

// WorldPkTask 表示跨服 PK 每日/周常任务。
type WorldPkTask struct {
	ID       int32
	Progress int32
	// State 完成状态：实测 1=已完成/可领取。
	State int32
}

// ParseWorldPkTasks 解析 open_task_panel 前两组任务列表。
func ParseWorldPkTasks(payload []byte) ([]WorldPkTask, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	res, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: empty world pk task panel", ErrInvalidPayload)
	}
	if res != WorldPkRewardSuccess {
		return nil, nil
	}
	var tasks []WorldPkTask
	for list := 0; list < 2; list++ {
		count, err := r.ReadUint16()
		if err != nil {
			break
		}
		for i := 0; i < int(count); i++ {
			var t WorldPkTask
			if t.ID, err = r.ReadInt32(); err != nil {
				return tasks, nil
			}
			if t.Progress, err = r.ReadInt32(); err != nil {
				return tasks, nil
			}
			if t.State, err = r.ReadInt32(); err != nil {
				return tasks, nil
			}
			tasks = append(tasks, t)
		}
	}
	return tasks, nil
}

// ParseWorldPkTaskIDs 解析 open_task_panel 前三组任务列表中的任务 ID。
//
// 回包首字节为结果码，随后是 3 个 [Int,Int,Int] 列表。
func ParseWorldPkTaskIDs(payload []byte) ([]int32, error) {
	if len(payload) < 1 {
		return nil, fmt.Errorf("%w: empty world pk task panel", ErrInvalidPayload)
	}
	r := NewReader(payload)
	if _, err := r.ReadUint8(); err != nil {
		return nil, err
	}
	var ids []int32
	for list := 0; list < 3; list++ {
		if r.Remaining() < 2 {
			break
		}
		count, err := r.ReadUint16()
		if err != nil {
			break
		}
		for i := 0; i < int(count); i++ {
			if r.Remaining() < 12 {
				return ids, nil
			}
			id, _ := r.ReadInt32()
			_, _ = r.ReadInt32()
			_, _ = r.ReadInt32()
			ids = append(ids, id)
		}
	}
	return ids, nil
}
