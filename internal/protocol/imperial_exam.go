// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方金榜题名活动状态协议 (Mod_StJinBangTiMing_Base, Module 237 / 0xED)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_StJinBangTiMing_Base：
// 先查询活动时间与阶段，仅在活动进行中才允许开始作答。
// 题目内容由客户端资源 SWF 提供，自动化在缺少题库时只做状态检测、不提交答案。
package protocol

import "fmt"

const (
	// ActionIDImperialExamStatus 查询金榜题名活动状态 (Module 237, Action 0 -> 0x00ED0000)
	ActionIDImperialExamStatus uint32 = 0x00ED0000
)

// Mod_StJinBangTiMing_Base 活动阶段码。
const (
	// ImperialExamActivityStart 活动进行中
	ImperialExamActivityStart uint8 = 0
	// ImperialExamActivityEnd 活动已结束
	ImperialExamActivityEnd uint8 = 1
	// ImperialExamActivityClose 活动关闭
	ImperialExamActivityClose uint8 = 2
	// ImperialExamAnswerOver 答题已结束
	ImperialExamAnswerOver uint8 = 3
)

// ImperialExamStatus 表示活动时间与阶段。
type ImperialExamStatus struct {
	// StartTime 活动开始时间
	StartTime int32
	// EndTime 活动结束时间
	EndTime int32
	// Status 阶段码
	Status uint8
}

// BuildImperialExamStatusPacket 构造查询活动状态封包 (0x00ED0000)。
func BuildImperialExamStatusPacket() *Packet {
	return NewPacket(ActionIDImperialExamStatus, nil)
}

// ParseImperialExamStatusResult 解析活动状态: Int start_time, Int end_time, UByte status。
func ParseImperialExamStatusResult(payload []byte) (*ImperialExamStatus, error) {
	r := NewReader(payload)
	res := &ImperialExamStatus{}
	var err error
	if res.StartTime, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read imperial exam start_time: %v", ErrInvalidPayload, err)
	}
	if res.EndTime, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read imperial exam end_time: %v", ErrInvalidPayload, err)
	}
	if res.Status, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read imperial exam status: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// BuildImperialExamStatusResultPacket 构造活动状态回包，供离线 mock 与单测使用。
func BuildImperialExamStatusResultPacket(status ImperialExamStatus) *Packet {
	w := NewWriter()
	w.WriteInt32(status.StartTime)
	w.WriteInt32(status.EndTime)
	w.WriteUint8(status.Status)
	return NewPacket(ActionIDImperialExamStatus, w.Bytes())
}

const (
	// ActionIDImperialExamStart 开始答题 (Module 237, Action 2 -> 0x00ED0002)
	ActionIDImperialExamStart uint32 = 0x00ED0002
	// ActionIDImperialExamSubmit 提交答案 (Module 237, Action 3 -> 0x00ED0003)
	ActionIDImperialExamSubmit uint32 = 0x00ED0003
)

// 答题结果码 (Mod_StJinBangTiMing_Base)。
const (
	// ImperialExamResultSuccess 答题成功
	ImperialExamResultSuccess uint8 = 11
	// ImperialExamResultNonActivityTime 非活动时间
	ImperialExamResultNonActivityTime uint8 = 12
	// ImperialExamResultSubmitTimeOut 提交超时
	ImperialExamResultSubmitTimeOut uint8 = 13
)

// 答案序号常量 (SEQUENCE_A..D)。
const (
	// ImperialExamSequenceA 选项 A
	ImperialExamSequenceA uint8 = 7
)

// ImperialExamQuestion 表示 start_answer 回包。
type ImperialExamQuestion struct {
	// Result 结果码
	Result uint8
	// CurAnswerSeq 当前题号
	CurAnswerSeq uint8
	// TotalTopicNumber 总题数
	TotalTopicNumber uint8
	// QuestionID 题目编号
	QuestionID int32
	// StartTime 本题开始时间
	StartTime int32
	// DayRightNumber 今日答对数
	DayRightNumber int32
	// DayScore 今日得分
	DayScore int32
	// TodayUseTime 今日用时
	TodayUseTime int16
	// JinBangStep 金榜阶段
	JinBangStep uint8
	// Rank 排名
	Rank int32
	// TotalRightNumber 总答对数
	TotalRightNumber int32
	// TotalScore 总得分
	TotalScore int32
	// DecipherKey 题库解密密钥
	DecipherKey string
}

// ImperialExamSubmitResult 表示 submit_answer 回包。
type ImperialExamSubmitResult struct {
	// Result 结果码
	Result uint8
	// ContinueRightNum 连续答对数
	ContinueRightNum uint8
	// RightScore 本题得分
	RightScore uint8
	// RightSequence 正确答案序号 (7..10)
	RightSequence uint8
}

// BuildImperialExamStartPacket 构造开始答题封包 (0x00ED0002)。
func BuildImperialExamStartPacket() *Packet {
	return NewPacket(ActionIDImperialExamStart, nil)
}

// BuildImperialExamSubmitPacket 构造提交答案封包 (0x00ED0003)。
func BuildImperialExamSubmitPacket(curAnswerSeq, sequence uint8) *Packet {
	return NewPacket(ActionIDImperialExamSubmit, []byte{curAnswerSeq, sequence})
}

// ParseImperialExamQuestionResult 解析 start_answer 回包。
func ParseImperialExamQuestionResult(payload []byte) (*ImperialExamQuestion, error) {
	r := NewReader(payload)
	res := &ImperialExamQuestion{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read exam result: %v", ErrInvalidPayload, err)
	}
	if res.CurAnswerSeq, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read exam cur_answer_seq: %v", ErrInvalidPayload, err)
	}
	if res.TotalTopicNumber, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read exam total_topic_number: %v", ErrInvalidPayload, err)
	}
	if res.QuestionID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read exam question_id: %v", ErrInvalidPayload, err)
	}
	if res.StartTime, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read exam start_time: %v", ErrInvalidPayload, err)
	}
	if res.DayRightNumber, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read exam day_right_number: %v", ErrInvalidPayload, err)
	}
	if res.DayScore, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read exam day_score: %v", ErrInvalidPayload, err)
	}
	if res.TodayUseTime, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read exam today_use_time: %v", ErrInvalidPayload, err)
	}
	if res.JinBangStep, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read exam jin_bang_step: %v", ErrInvalidPayload, err)
	}
	if res.Rank, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read exam rank: %v", ErrInvalidPayload, err)
	}
	if res.TotalRightNumber, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read exam total_right_number: %v", ErrInvalidPayload, err)
	}
	if res.TotalScore, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read exam total_score: %v", ErrInvalidPayload, err)
	}
	if res.DecipherKey, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: failed to read exam decipher_key: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// ParseImperialExamSubmitResult 解析 submit_answer 回包。
func ParseImperialExamSubmitResult(payload []byte) (*ImperialExamSubmitResult, error) {
	r := NewReader(payload)
	res := &ImperialExamSubmitResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read exam submit result: %v", ErrInvalidPayload, err)
	}
	if res.ContinueRightNum, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read exam continue_right_num: %v", ErrInvalidPayload, err)
	}
	if res.RightScore, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read exam right_score: %v", ErrInvalidPayload, err)
	}
	if res.RightSequence, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read exam right_sequence: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// BuildImperialExamStartResultPacket 构造开始答题回包，供离线 mock 与单测使用。
func BuildImperialExamStartResultPacket(q ImperialExamQuestion) *Packet {
	w := NewWriter()
	w.WriteUint8(q.Result)
	w.WriteUint8(q.CurAnswerSeq)
	w.WriteUint8(q.TotalTopicNumber)
	w.WriteInt32(q.QuestionID)
	w.WriteInt32(q.StartTime)
	w.WriteInt32(q.DayRightNumber)
	w.WriteInt32(q.DayScore)
	w.WriteInt16(q.TodayUseTime)
	w.WriteUint8(q.JinBangStep)
	w.WriteInt32(q.Rank)
	w.WriteInt32(q.TotalRightNumber)
	w.WriteInt32(q.TotalScore)
	w.WriteString(q.DecipherKey)
	return NewPacket(ActionIDImperialExamStart, w.Bytes())
}

// BuildImperialExamSubmitResultPacket 构造提交回包，供离线 mock 与单测使用。
func BuildImperialExamSubmitResultPacket(res ImperialExamSubmitResult) *Packet {
	return NewPacket(ActionIDImperialExamSubmit, []byte{res.Result, res.ContinueRightNum, res.RightScore, res.RightSequence})
}
