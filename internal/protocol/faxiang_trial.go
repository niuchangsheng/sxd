// Package protocol 提供法相页面「造物佛手 / 守护魔兽」试炼协议
// (Mod_ExtremeTrial_Base, Module 721)。
//
//	main_panel_info      721/1  request=[]  response=[UByte result, Int,Int,Int,[Int],[Int,Int]]
//	task_panel_info      721/2  request=[]  response=[UByte result,[Int task,Int progress,Int state]]
//	get_task_award       721/3  request=[Int task_id] response=[UByte]
//	score_award_panel    721/4  request=[]  response=[UByte result,Int,[Int award_id]]
//	get_score_award      721/5  request=[Int award_id] response=[UByte]
//	monster_config_info  721/6  request=[]  response=[UByte,...]
//	start_challenge      721/16 request=[]  response=[UByte result, 战报...]
package protocol

import "fmt"

const (
	// ActionIDFaxiangTrialPanel 法相试炼主面板 (721/1)
	ActionIDFaxiangTrialPanel uint32 = 0x02D10001
	// ActionIDFaxiangTrialTaskPanel 任务面板 (721/2)
	ActionIDFaxiangTrialTaskPanel uint32 = 0x02D10002
	// ActionIDFaxiangTrialTaskAward 领取任务奖励 (721/3)
	ActionIDFaxiangTrialTaskAward uint32 = 0x02D10003
	// ActionIDFaxiangTrialScorePanel 积分奖励面板 (721/4)
	ActionIDFaxiangTrialScorePanel uint32 = 0x02D10004
	// ActionIDFaxiangTrialScoreAward 领取积分奖励 (721/5)
	ActionIDFaxiangTrialScoreAward uint32 = 0x02D10005
	// ActionIDFaxiangTrialMonsterConfig 守护魔兽配置 (721/6)
	ActionIDFaxiangTrialMonsterConfig uint32 = 0x02D10006
	// ActionIDFaxiangTrialStartChallenge 发起试炼挑战 (721/16)
	ActionIDFaxiangTrialStartChallenge uint32 = 0x02D10010
)

// 法相试炼结果码。
const (
	FaxiangTrialSuccess       uint8 = 0
	FaxiangTrialFailed        uint8 = 1
	FaxiangTrialNoTimes       uint8 = 2
	FaxiangTrialTimesLimited  uint8 = 3
	FaxiangTrialPackFull      uint8 = 4
	FaxiangTrialLowScore      uint8 = 5
	FaxiangTrialIsGot         uint8 = 6
	FaxiangTrialNoEnoughScore uint8 = 36
)

// FaxiangTrialTask 表示法相试炼任务条目。
type FaxiangTrialTask struct {
	TaskID   int32
	Progress int32
	State    int32
}

// BuildFaxiangTrialPanelPacket 构造法相试炼主面板封包。
func BuildFaxiangTrialPanelPacket() *Packet {
	return NewPacket(ActionIDFaxiangTrialPanel, nil)
}

// BuildFaxiangTrialTaskPanelPacket 构造任务面板封包。
func BuildFaxiangTrialTaskPanelPacket() *Packet {
	return NewPacket(ActionIDFaxiangTrialTaskPanel, nil)
}

// BuildFaxiangTrialTaskAwardPacket 构造任务奖励领取封包。
func BuildFaxiangTrialTaskAwardPacket(taskID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(taskID)
	return NewPacket(ActionIDFaxiangTrialTaskAward, w.Bytes())
}

// BuildFaxiangTrialScorePanelPacket 构造积分奖励面板封包。
func BuildFaxiangTrialScorePanelPacket() *Packet {
	return NewPacket(ActionIDFaxiangTrialScorePanel, nil)
}

// BuildFaxiangTrialScoreAwardPacket 构造积分奖励领取封包。
func BuildFaxiangTrialScoreAwardPacket(awardID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(awardID)
	return NewPacket(ActionIDFaxiangTrialScoreAward, w.Bytes())
}

// BuildFaxiangTrialMonsterConfigPacket 构造守护魔兽配置查询封包。
func BuildFaxiangTrialMonsterConfigPacket() *Packet {
	return NewPacket(ActionIDFaxiangTrialMonsterConfig, nil)
}

// BuildFaxiangTrialStartChallengePacket 构造试炼挑战封包。
func BuildFaxiangTrialStartChallengePacket() *Packet {
	return NewPacket(ActionIDFaxiangTrialStartChallenge, nil)
}

// ParseFaxiangTrialTasks 解析 [UByte result, [Int task, Int progress, Int state]]。
func ParseFaxiangTrialTasks(payload []byte) (uint8, []FaxiangTrialTask, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	res, err := r.ReadUint8()
	if err != nil {
		return 0, nil, fmt.Errorf("法相试炼任务结果码读取失败: %w", err)
	}
	if res != FaxiangTrialSuccess {
		return res, nil, nil
	}
	count, err := r.ReadUint16()
	if err != nil {
		return res, nil, nil
	}
	tasks := make([]FaxiangTrialTask, 0, count)
	for i := 0; i < int(count); i++ {
		var t FaxiangTrialTask
		if t.TaskID, err = r.ReadInt32(); err != nil {
			return res, tasks, fmt.Errorf("法相试炼任务[%d]读取失败: %w", i, err)
		}
		if t.Progress, err = r.ReadInt32(); err != nil {
			return res, tasks, fmt.Errorf("法相试炼任务[%d]进度读取失败: %w", i, err)
		}
		if t.State, err = r.ReadInt32(); err != nil {
			return res, tasks, fmt.Errorf("法相试炼任务[%d]状态读取失败: %w", i, err)
		}
		tasks = append(tasks, t)
	}
	return res, tasks, nil
}

// ParseFaxiangTrialScoreAwardIDs 解析 [UByte result, Int, [Int award_id]]。
func ParseFaxiangTrialScoreAwardIDs(payload []byte) ([]int32, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	res, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("法相试炼积分结果码读取失败: %w", err)
	}
	if res != FaxiangTrialSuccess {
		return nil, nil
	}
	if _, err := r.ReadInt32(); err != nil {
		return nil, nil
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, nil
	}
	ids := make([]int32, 0, count)
	for i := 0; i < int(count); i++ {
		id, err := r.ReadInt32()
		if err != nil {
			return ids, fmt.Errorf("法相试炼积分奖励[%d]读取失败: %w", i, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// FaxiangTrialResultText 返回法相试炼结果码文本。
func FaxiangTrialResultText(code uint8) string {
	switch code {
	case FaxiangTrialSuccess:
		return "成功"
	case FaxiangTrialFailed:
		return "失败"
	case FaxiangTrialNoTimes:
		return "次数不足"
	case FaxiangTrialTimesLimited:
		return "次数上限"
	case FaxiangTrialPackFull:
		return "背包已满"
	case FaxiangTrialLowScore:
		return "积分不足"
	case FaxiangTrialIsGot:
		return "已领取"
	case FaxiangTrialNoEnoughScore:
		return "积分不足"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}
