// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现每日活跃度面板解析 (Mod_Player_Base.get_game_assistant_info, Module 0 Act 42)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_Player_Base：
//
//	"module":0, "action":42, "request":[],
//	"response":[IntUtil,LongUtil,IntUtil,LongUtil,IntUtil x29,
//	            [IntUtil,IntUtil,IntUtil,IntUtil],
//	            [IntUtil,IntUtil,IntUtil,IntUtil]]
//
// 其中前 33 个标量里有 2 个 Long (下标 1、3)，随后是两个四元组数组：
// back_times_list 与 active_degree_list。active_degree_list 每行为
// [task_id, cur_times, max_times, per_active_add]。
package protocol

import "fmt"

const (
	// ActionIDGameAssistantInfo 查询每日活跃度面板 (Module 0, Action 42 -> 0x0000002A)
	ActionIDGameAssistantInfo uint32 = 0x0000002A

	// DailyActivityTaskWeeklyThreshold 每日任务与周常任务的 task_id 分界。
	// task_id <= 83 计入每日活跃度，>= 84 为周常任务。
	DailyActivityTaskWeeklyThreshold int32 = 83

	// GameAssistantScalarCount 面板回包中活跃度列表之前的标量字段个数。
	GameAssistantScalarCount = 33
)

// DailyActivityTask 表示 active_degree_list 中的一行任务进度。
type DailyActivityTask struct {
	// TaskID 小助手任务编号 (<= 83 为每日任务)
	TaskID int32
	// CurTimes 今日已完成次数
	CurTimes int32
	// MaxTimes 今日可计次上限
	MaxTimes int32
	// PerActiveAdd 每次完成提供的活跃度
	PerActiveAdd int32
}

// Earned 返回该任务已贡献的活跃度。
func (t DailyActivityTask) Earned() int {
	if t.PerActiveAdd <= 0 || t.CurTimes <= 0 {
		return 0
	}
	return int(t.CurTimes * t.PerActiveAdd)
}

// RemainingTimes 返回该任务还可完成的次数。
func (t DailyActivityTask) RemainingTimes() int {
	remain := t.MaxTimes - t.CurTimes
	if remain <= 0 {
		return 0
	}
	return int(remain)
}

// RemainingActive 返回该任务还能提供的活跃度。
func (t DailyActivityTask) RemainingActive() int {
	if t.PerActiveAdd <= 0 {
		return 0
	}
	return t.RemainingTimes() * int(t.PerActiveAdd)
}

// DailyActivityPanel 表示每日活跃度面板。
type DailyActivityPanel struct {
	// Tasks 全部任务进度 (含周常)
	Tasks []DailyActivityTask
}

// FindTask 按任务编号查找进度。
func (p *DailyActivityPanel) FindTask(taskID int32) (DailyActivityTask, bool) {
	if p == nil {
		return DailyActivityTask{}, false
	}
	for _, task := range p.Tasks {
		if task.TaskID == taskID {
			return task, true
		}
	}
	return DailyActivityTask{}, false
}

// DailyTotal 返回每日任务已获得活跃度合计。
func (p *DailyActivityPanel) DailyTotal() int {
	if p == nil {
		return 0
	}
	total := 0
	for _, task := range p.Tasks {
		if task.TaskID <= DailyActivityTaskWeeklyThreshold {
			total += task.Earned()
		}
	}
	return total
}

// RemainingTimesFor 返回指定任务剩余次数；任务不存在时返回 0。
func (p *DailyActivityPanel) RemainingTimesFor(taskID int32) int {
	task, ok := p.FindTask(taskID)
	if !ok {
		return 0
	}
	return task.RemainingTimes()
}

// ParseGameAssistantInfo 解析每日活跃度面板回包。
func ParseGameAssistantInfo(payload []byte) (*DailyActivityPanel, error) {
	r := NewReader(payload)
	for i := 0; i < GameAssistantScalarCount; i++ {
		var err error
		switch i {
		case 1, 3:
			_, err = r.ReadInt64()
		default:
			_, err = r.ReadInt32()
		}
		if err != nil {
			return nil, fmt.Errorf("活跃度面板标量[%d]解析失败: %w", i, err)
		}
	}

	readRows := func(name string) ([]DailyActivityTask, error) {
		n, err := r.ReadUint16()
		if err != nil {
			return nil, fmt.Errorf("活跃度面板 %s 数量读取失败: %w", name, err)
		}
		rows := make([]DailyActivityTask, 0, n)
		for i := 0; i < int(n); i++ {
			var row [4]int32
			for j := 0; j < 4; j++ {
				v, readErr := r.ReadInt32()
				if readErr != nil {
					return nil, fmt.Errorf("活跃度面板 %s[%d] 字段[%d]解析失败: %w", name, i, j, readErr)
				}
				row[j] = v
			}
			rows = append(rows, DailyActivityTask{
				TaskID:       row[0],
				CurTimes:     row[1],
				MaxTimes:     row[2],
				PerActiveAdd: row[3],
			})
		}
		return rows, nil
	}

	if _, err := readRows("back_times_list"); err != nil {
		return nil, err
	}
	tasks, err := readRows("active_degree_list")
	if err != nil {
		return nil, err
	}
	return &DailyActivityPanel{Tasks: tasks}, nil
}

// BuildGameAssistantInfoPacket 构造每日活跃度面板查询封包 (0x0000002A)。
func BuildGameAssistantInfoPacket() *Packet {
	return NewPacket(ActionIDGameAssistantInfo, nil)
}

// BuildGameAssistantInfoResultPacket 构造活跃度面板回包，供离线 mock 与单测使用。
func BuildGameAssistantInfoResultPacket(tasks []DailyActivityTask) *Packet {
	w := NewWriter()
	for i := 0; i < GameAssistantScalarCount; i++ {
		switch i {
		case 1, 3:
			w.WriteInt64(0)
		default:
			w.WriteInt32(0)
		}
	}
	w.WriteUint16(0)
	w.WriteUint16(uint16(len(tasks)))
	for _, task := range tasks {
		w.WriteInt32(task.TaskID)
		w.WriteInt32(task.CurTimes)
		w.WriteInt32(task.MaxTimes)
		w.WriteInt32(task.PerActiveAdd)
	}
	return NewPacket(ActionIDGameAssistantInfo, w.Bytes())
}
