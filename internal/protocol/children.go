// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现孩子玩法 (Module 312 / Mod_Children_Base, Module 315 / Mod_ChildrenTask_Base)
// 中与每日活跃度相关的只读查询与奖励领取协议：
//
//	get_info          (312/0,  0x01380000) request=[] response=[UByte,[孩子记录],UByte,UByte,UByte]
//	get_school_info   (312/26, 0x0138001A) request=[Int child_id] response=[[Byte,UByte]]
//	attend_school     (312/27, 0x0138001B) request=[Int child_id, Byte school_id] response=[UByte]
//	get_children_task (315/0,  0x013B0000) request=[Int child_id] response=[[Int,Int,Int],Int,UByte]
//	get_award         (315/1,  0x013B0001) request=[Int child_id, Int task_id] response=[UByte]
//
// 孩子记录只解析首个字段 role_id 与总数，完整记录结构较长且与每日任务无关。
package protocol

import "fmt"

const (
	// ActionIDChildrenInfo 查询孩子主面板 (Module 312, Action 0 -> 0x01380000)
	ActionIDChildrenInfo uint32 = 0x01380000
	// ActionIDChildrenSchoolInfo 查询孩子学堂信息 (Module 312, Action 26 -> 0x0138001A)
	ActionIDChildrenSchoolInfo uint32 = 0x0138001A
	// ActionIDChildrenAttendSchool 孩子上学 (Module 312, Action 27 -> 0x0138001B)
	ActionIDChildrenAttendSchool uint32 = 0x0138001B
	// ActionIDChildrenTaskInfo 查询孩子任务 (Module 315, Action 0 -> 0x013B0000)
	ActionIDChildrenTaskInfo uint32 = 0x013B0000
	// ActionIDChildrenTaskAward 领取孩子任务奖励 (Module 315, Action 1 -> 0x013B0001)
	ActionIDChildrenTaskAward uint32 = 0x013B0001
)

// 孩子玩法结果码 (Mod_Children_Base)。
const (
	// ChildrenResultSuccess 成功
	ChildrenResultSuccess uint8 = 7
	// ChildrenResultAlreadyDone 已完成
	ChildrenResultAlreadyDone uint8 = 13
	// ChildrenResultLackOfIngot 元宝不足
	ChildrenResultLackOfIngot uint8 = 16
	// ChildrenResultLackOfGrowthGold 培养金不足
	ChildrenResultLackOfGrowthGold uint8 = 17
	// ChildrenResultUndo 未完成 (学堂可上学状态)
	ChildrenResultUndo uint8 = 18
	// ChildrenResultDone 已完成 (学堂今日已上学)
	ChildrenResultDone uint8 = 19
	// ChildrenResultOnSchool 已在学堂
	ChildrenResultOnSchool uint8 = 32
	// ChildrenResultTimesLimit 次数上限
	ChildrenResultTimesLimit uint8 = 34
)

// 孩子任务结果码 (Mod_ChildrenTask_Base)。
const (
	// ChildrenTaskResultSuccess 成功
	ChildrenTaskResultSuccess uint8 = 2
	// ChildrenTaskResultNotComplete 任务未完成
	ChildrenTaskResultNotComplete uint8 = 3
	// ChildrenTaskResultHadGet 已领取
	ChildrenTaskResultHadGet uint8 = 4
	// ChildrenTaskResultFailure 失败
	ChildrenTaskResultFailure uint8 = 5
	// ChildrenTaskResultNoTimes 次数不足
	ChildrenTaskResultNoTimes uint8 = 6
	// ChildrenTaskResultHadComplete 已完成
	ChildrenTaskResultHadComplete uint8 = 7
)

// ChildrenPanel 表示孩子主面板中的最小可用信息。
type ChildrenPanel struct {
	// Result 结果码 (SUCCESS=7)
	Result uint8
	// ChildIDs 孩子角色 ID 列表
	ChildIDs []int32
}

// ChildrenSchool 表示孩子学堂中的一个学堂状态。
type ChildrenSchool struct {
	// SchoolID 学堂编号
	SchoolID uint8
	// State 状态 (UNDO=18 可上学 / DONE=19 已上学)
	State uint8
}

// ChildrenTask 表示孩子面板中的一条每日任务。
type ChildrenTask struct {
	// TaskID 任务编号
	TaskID int32
	// Cur 当前进度
	Cur int32
	// Max 目标进度
	Max int32
}

// BuildChildrenInfoPacket 构造孩子主面板查询封包。
//
// 官方 request 为空，但线上要求尾部携带 PrevAct，因此载荷固定 4 字节。
func BuildChildrenInfoPacket() *Packet {
	pkt := NewPacket(ActionIDChildrenInfo, []byte{0, 0, 0, 0})
	pkt.HasPrevAct = true
	return pkt
}

// BuildChildrenSchoolInfoPacket 构造孩子学堂信息查询封包。
func BuildChildrenSchoolInfoPacket(childID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(childID)
	w.WriteUint32(0)
	pkt := NewPacket(ActionIDChildrenSchoolInfo, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// BuildChildrenAttendSchoolPacket 构造孩子上学封包。
func BuildChildrenAttendSchoolPacket(childID int32, schoolID uint8) *Packet {
	w := NewWriter()
	w.WriteInt32(childID)
	w.WriteUint8(schoolID)
	return NewPacket(ActionIDChildrenAttendSchool, w.Bytes())
}

// BuildChildrenTaskInfoPacket 构造孩子任务查询封包。
func BuildChildrenTaskInfoPacket(childID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(childID)
	w.WriteUint32(0)
	pkt := NewPacket(ActionIDChildrenTaskInfo, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// BuildChildrenTaskAwardPacket 构造孩子任务奖励领取封包。
func BuildChildrenTaskAwardPacket(childID, taskID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(childID)
	w.WriteInt32(taskID)
	return NewPacket(ActionIDChildrenTaskAward, w.Bytes())
}

// ParseChildrenPanel 解析孩子主面板，仅提取结果码与每个孩子的 role_id。
//
// 完整记录结构:
//
//	[Int role_id, Int parent_id, UByte age, UByte sex, String name, String parent_name,
//	 String other_name, [UByte,Int,Int], Int, Short, [UByte,Int,Short], [UByte], [UByte],
//	 UByte, Int, UByte, UByte, UByte, Short, Short, Short, Int]
func ParseChildrenPanel(payload []byte) (*ChildrenPanel, error) {
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("孩子面板结果码读取失败: %w", err)
	}
	panel := &ChildrenPanel{Result: result}
	if result != ChildrenResultSuccess {
		return panel, nil
	}
	if _, err = r.ReadInt32(); err != nil { // 面板公共 Int 字段
		return nil, fmt.Errorf("孩子面板公共字段读取失败: %w", err)
	}
	for i := 0; i < 2; i++ { // 两个 UByte 公共字段
		if _, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("孩子面板公共字段[%d]读取失败: %w", i, err)
		}
	}
	if _, err = r.ReadInt16(); err != nil { // Short 公共字段
		return nil, fmt.Errorf("孩子面板公共 Short 字段读取失败: %w", err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("孩子数量读取失败: %w", err)
	}
	panel.ChildIDs = make([]int32, 0, count)
	for i := 0; i < int(count); i++ {
		roleID, readErr := r.ReadInt32()
		if readErr != nil {
			return nil, fmt.Errorf("孩子[%d] role_id 读取失败: %w", i, readErr)
		}
		panel.ChildIDs = append(panel.ChildIDs, roleID)
		if err := skipChildrenRecord(r); err != nil {
			return nil, fmt.Errorf("孩子[%d] 记录解析失败: %w", i, err)
		}
	}
	return panel, nil
}

// skipChildrenRecord 跳过孩子记录中除 role_id 之外的剩余字段。
func skipChildrenRecord(r *Reader) error {
	if _, err := r.ReadInt32(); err != nil { // parent_id
		return err
	}
	for i := 0; i < 2; i++ { // age, sex
		if _, err := r.ReadUint8(); err != nil {
			return err
		}
	}
	for i := 0; i < 3; i++ { // name, parent_name, other_name
		if _, err := r.ReadString(); err != nil {
			return err
		}
	}
	if err := skipChildrenArray(r, func() error {
		if _, err := r.ReadUint8(); err != nil {
			return err
		}
		for i := 0; i < 2; i++ {
			if _, err := r.ReadInt32(); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if _, err := r.ReadInt32(); err != nil {
		return err
	}
	if _, err := r.ReadInt16(); err != nil {
		return err
	}
	if err := skipChildrenArray(r, func() error {
		if _, err := r.ReadUint8(); err != nil {
			return err
		}
		if _, err := r.ReadInt32(); err != nil {
			return err
		}
		_, err := r.ReadInt16()
		return err
	}); err != nil {
		return err
	}
	for i := 0; i < 2; i++ { // 两个 [UByte] 数组
		if err := skipChildrenArray(r, func() error {
			_, err := r.ReadUint8()
			return err
		}); err != nil {
			return err
		}
	}
	// UByte, Int, UByte, UByte, UByte (线上实测 5 个字段，共 8 字节)
	if _, err := r.ReadUint8(); err != nil {
		return err
	}
	if _, err := r.ReadInt32(); err != nil {
		return err
	}
	for i := 0; i < 3; i++ {
		if _, err := r.ReadUint8(); err != nil {
			return err
		}
	}
	for i := 0; i < 3; i++ { // Short x3
		if _, err := r.ReadInt16(); err != nil {
			return err
		}
	}
	_, err := r.ReadInt32()
	return err
}

func skipChildrenArray(r *Reader, readElem func() error) error {
	count, err := r.ReadUint16()
	if err != nil {
		return err
	}
	for i := 0; i < int(count); i++ {
		if err := readElem(); err != nil {
			return err
		}
	}
	return nil
}

// ParseChildrenSchoolInfo 解析孩子学堂列表。
func ParseChildrenSchoolInfo(payload []byte) ([]ChildrenSchool, error) {
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("学堂数量读取失败: %w", err)
	}
	schools := make([]ChildrenSchool, 0, count)
	for i := 0; i < int(count); i++ {
		schoolID, readErr := r.ReadUint8()
		if readErr != nil {
			return nil, fmt.Errorf("学堂[%d]编号读取失败: %w", i, readErr)
		}
		state, readErr := r.ReadUint8()
		if readErr != nil {
			return nil, fmt.Errorf("学堂[%d]状态读取失败: %w", i, readErr)
		}
		schools = append(schools, ChildrenSchool{SchoolID: schoolID, State: state})
	}
	return schools, nil
}

// ParseChildrenTaskInfo 解析孩子任务列表。
func ParseChildrenTaskInfo(payload []byte) ([]ChildrenTask, error) {
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("孩子任务数量读取失败: %w", err)
	}
	tasks := make([]ChildrenTask, 0, count)
	for i := 0; i < int(count); i++ {
		taskID, readErr := r.ReadInt32()
		if readErr != nil {
			return nil, fmt.Errorf("孩子任务[%d]编号读取失败: %w", i, readErr)
		}
		cur, readErr := r.ReadInt32()
		if readErr != nil {
			return nil, fmt.Errorf("孩子任务[%d]进度读取失败: %w", i, readErr)
		}
		max, readErr := r.ReadInt32()
		if readErr != nil {
			return nil, fmt.Errorf("孩子任务[%d]目标读取失败: %w", i, readErr)
		}
		tasks = append(tasks, ChildrenTask{TaskID: taskID, Cur: cur, Max: max})
	}
	return tasks, nil
}

// ParseChildrenResultCode 解析只返回一个结果码的孩子协议回包。
func ParseChildrenResultCode(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty children result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// ChildrenResultText 返回孩子玩法结果码的可读文本。
func ChildrenResultText(code uint8) string {
	switch code {
	case ChildrenResultSuccess:
		return "成功"
	case ChildrenResultAlreadyDone:
		return "已完成"
	case ChildrenResultLackOfIngot:
		return "元宝不足"
	case ChildrenResultLackOfGrowthGold:
		return "培养金不足"
	case ChildrenResultUndo:
		return "未完成"
	case ChildrenResultDone:
		return "已完成"
	case ChildrenResultOnSchool:
		return "已在学堂"
	case ChildrenResultTimesLimit:
		return "次数已达上限"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// ChildrenTaskResultText 返回孩子任务结果码的可读文本。
func ChildrenTaskResultText(code uint8) string {
	switch code {
	case ChildrenTaskResultSuccess:
		return "成功"
	case ChildrenTaskResultNotComplete:
		return "任务未完成"
	case ChildrenTaskResultHadGet:
		return "已领取"
	case ChildrenTaskResultFailure:
		return "失败"
	case ChildrenTaskResultNoTimes:
		return "次数不足"
	case ChildrenTaskResultHadComplete:
		return "已完成"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// BuildChildrenPanelResultPacket 构造孩子主面板最小回包，供离线 mock 与单测使用。
func BuildChildrenPanelResultPacket(result uint8, childIDs []int32) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	w.WriteInt32(0)
	w.WriteUint8(0)
	w.WriteUint8(0)
	w.WriteInt16(0)
	w.WriteUint16(uint16(len(childIDs)))
	for _, id := range childIDs {
		w.WriteInt32(id)
		w.WriteInt32(0) // parent_id
		w.WriteUint8(0) // age
		w.WriteUint8(0) // sex
		for i := 0; i < 3; i++ {
			w.WriteString("")
		}
		w.WriteUint16(0) // [UByte,Int,Int]
		w.WriteInt32(0)
		w.WriteInt16(0)
		w.WriteUint16(0) // [UByte,Int,Short]
		w.WriteUint16(0) // [UByte]
		w.WriteUint16(0) // [UByte]
		w.WriteUint8(0)
		w.WriteInt32(0)
		w.WriteUint8(0)
		w.WriteUint8(0)
		w.WriteUint8(0)
		w.WriteInt16(0)
		w.WriteInt16(0)
		w.WriteInt16(0)
		w.WriteInt32(0)
	}
	w.WriteUint8(0)
	w.WriteUint8(0)
	w.WriteUint8(0)
	return NewPacket(ActionIDChildrenInfo, w.Bytes())
}

// BuildChildrenSchoolInfoResultPacket 构造学堂信息回包，供离线 mock 与单测使用。
func BuildChildrenSchoolInfoResultPacket(schools []ChildrenSchool) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(schools)))
	for _, s := range schools {
		w.WriteUint8(s.SchoolID)
		w.WriteUint8(s.State)
	}
	return NewPacket(ActionIDChildrenSchoolInfo, w.Bytes())
}

// BuildChildrenTaskInfoResultPacket 构造孩子任务回包，供离线 mock 与单测使用。
func BuildChildrenTaskInfoResultPacket(tasks []ChildrenTask) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(tasks)))
	for _, t := range tasks {
		w.WriteInt32(t.TaskID)
		w.WriteInt32(t.Cur)
		w.WriteInt32(t.Max)
	}
	w.WriteInt32(0)
	w.WriteUint8(0)
	return NewPacket(ActionIDChildrenTaskInfo, w.Bytes())
}
