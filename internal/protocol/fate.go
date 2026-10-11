// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// @author Ateng
// @since 2026-10-09
package protocol

import (
	"fmt"
	"strings"
)

// FateOption 代表仙履奇缘题目的单个分支选项。
type FateOption struct {
	// ChoiceID 选项唯一数字编号 (如 0x5A, 0x5B)
	ChoiceID int32
	// Key 选项字母按键 (如 "A", "B")
	Key string
	// Text 选项文字描述
	Text string
}

// FateGetInfoRequest 代表查询仙履奇缘状态请求。
type FateGetInfoRequest struct {
	// PrevAct 前序协议编号
	PrevAct uint32
}

// FateGetInfoResult 代表仙履奇缘当前状态信息。
type FateGetInfoResult struct {
	// RemainingTimes 今日剩余答题次数
	RemainingTimes int32
	// MaxTimes 每日最大允许答题次数
	MaxTimes int32
}

// FateQuestionRequest 代表获取仙履奇缘当前故事题目请求。
type FateQuestionRequest struct {
	// PrevAct 前序协议编号
	PrevAct uint32
}

// FateQuestionResult 代表仙履奇缘故事题目与分支选项明细。
type FateQuestionResult struct {
	// QuestionID 故事事件题号
	QuestionID int32
	// Story 故事情节叙述文本
	Story string
	// Options 可供选择的分支列表
	Options []FateOption
}

// FateAnswerRequest 代表提交仙履奇缘选项答案请求。
type FateAnswerRequest struct {
	// QuestionID 对应题目的事件编号
	QuestionID int32
	// AnswerID 选中的选项唯一编号
	AnswerID int32
	// PrevAct 前序协议编号
	PrevAct uint32
}

// FateAnswerResult 代表仙履奇缘答题结算收益。
type FateAnswerResult struct {
	// Message 结算文字提示 (如 "你获得50阅历,5体力！")
	Message string
	// RewardExp 获得的经验值
	RewardExp int64
	// RewardCoins 获得的铜钱数
	RewardCoins int64
	// RewardPower 获得的体力点数
	RewardPower int32
	// RewardFame 获得的名望声望
	RewardFame int32
}

// BuildFateGetInfoPacket 构造查询仙履奇缘状态请求二进制封包。
func BuildFateGetInfoPacket(req FateGetInfoRequest) (*Packet, error) {
	w := NewWriter()
	prevAct := req.PrevAct
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDFateGetInfo, w.Bytes()), nil
}

// ParseFateGetInfoRequest 从载荷中反序列化查询仙履奇缘状态请求。
func ParseFateGetInfoRequest(payload []byte) (*FateGetInfoRequest, error) {
	r := NewReader(payload)
	prevAct, err := r.ReadUint32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read prev_act: %v", ErrInvalidPayload, err)
	}
	return &FateGetInfoRequest{PrevAct: prevAct}, nil
}

// BuildFateQuestionPacket 构造获取仙履奇缘题目请求二进制封包。
func BuildFateQuestionPacket(req FateQuestionRequest) (*Packet, error) {
	w := NewWriter()
	prevAct := req.PrevAct
	if prevAct == 0 {
		prevAct = ActionIDFateGetInfo
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDFateGetQuestion, w.Bytes()), nil
}

// ParseFateQuestionRequest 从载荷中反序列化获取题目请求。
func ParseFateQuestionRequest(payload []byte) (*FateQuestionRequest, error) {
	r := NewReader(payload)
	prevAct, err := r.ReadUint32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read prev_act: %v", ErrInvalidPayload, err)
	}
	return &FateQuestionRequest{PrevAct: prevAct}, nil
}

// BuildFateAnswerPacket 构造提交仙履奇缘选项请求二进制封包。
func BuildFateAnswerPacket(req FateAnswerRequest) (*Packet, error) {
	w := NewWriter()
	// 1. 写入 QuestionID (4B 大端序有符号整型)
	w.WriteInt32(req.QuestionID)
	// 2. 写入 AnswerID (4B 大端序有符号整型)
	w.WriteInt32(req.AnswerID)
	// 3. 写入 PrevAct (4B 大端序协议号)
	prevAct := req.PrevAct
	if prevAct == 0 {
		prevAct = ActionIDFateGetQuestion
	}
	w.WriteUint32(prevAct)

	return NewPacket(ActionIDFateAnswer, w.Bytes()), nil
}

// ParseFateAnswerRequest 从载荷中反序列化提交选项请求。
func ParseFateAnswerRequest(payload []byte) (*FateAnswerRequest, error) {
	r := NewReader(payload)
	qid, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read question_id: %v", ErrInvalidPayload, err)
	}
	aid, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read answer_id: %v", ErrInvalidPayload, err)
	}
	prevAct, err := r.ReadUint32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read prev_act: %v", ErrInvalidPayload, err)
	}

	return &FateAnswerRequest{
		QuestionID: qid,
		AnswerID:   aid,
		PrevAct:    prevAct,
	}, nil
}

// ParseFateAnswerResult 从服务端回包中解析结算奖励文本与数值。
func ParseFateAnswerResult(payload []byte) (*FateAnswerResult, error) {
	if len(payload) == 0 {
		return &FateAnswerResult{}, nil
	}

	// 尝试作为带长度前缀的 UTF-8 字符串读取
	r := NewReader(payload)
	msg, err := r.ReadString()
	if err != nil {
		// 容错降级：直接按原始字节转字符串
		msg = string(payload)
	}

	res := &FateAnswerResult{
		Message: strings.TrimSpace(msg),
	}

	return res, nil
}

// BuildFateGetInfoResultPacket 构造仙履奇缘状态回包。
func BuildFateGetInfoResultPacket(res FateGetInfoResult) *Packet {
	w := NewWriter()
	w.WriteInt32(res.RemainingTimes)
	w.WriteInt32(res.MaxTimes)
	return NewPacket(ActionIDFateGetInfo, w.Bytes())
}

// ParseFateGetInfoResult 从载荷中反序列化奇缘状态。
func ParseFateGetInfoResult(payload []byte) (*FateGetInfoResult, error) {
	if len(payload) < 4 {
		return &FateGetInfoResult{RemainingTimes: 1, MaxTimes: 3}, nil
	}
	r := NewReader(payload)
	rem, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read remaining_times: %v", ErrInvalidPayload, err)
	}
	maxT, _ := r.ReadInt32()
	return &FateGetInfoResult{
		RemainingTimes: rem,
		MaxTimes:       maxT,
	}, nil
}

// BuildFateQuestionResultPacket 构造仙履奇缘故事题目回包。
func BuildFateQuestionResultPacket(res FateQuestionResult) *Packet {
	w := NewWriter()
	w.WriteInt32(res.QuestionID)
	w.WriteString(res.Story)
	w.WriteUint16(uint16(len(res.Options)))
	for _, opt := range res.Options {
		w.WriteInt32(opt.ChoiceID)
		w.WriteString(opt.Key)
		w.WriteString(opt.Text)
	}
	return NewPacket(ActionIDFateGetQuestion, w.Bytes())
}

// ParseFateQuestionResult 从载荷中反序列化题目与选项。
func ParseFateQuestionResult(payload []byte) (*FateQuestionResult, error) {
	if len(payload) < 6 {
		return nil, fmt.Errorf("%w: payload too short for fate question", ErrInvalidPayload)
	}
	r := NewReader(payload)
	qid, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read question_id: %v", ErrInvalidPayload, err)
	}
	story, err := r.ReadString()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read story: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return &FateQuestionResult{QuestionID: qid, Story: story}, nil
	}
	opts := make([]FateOption, 0, count)
	for i := 0; i < int(count); i++ {
		if r.Remaining() < 6 {
			break
		}
		cid, err := r.ReadInt32()
		if err != nil {
			break
		}
		key, _ := r.ReadString()
		txt, _ := r.ReadString()
		opts = append(opts, FateOption{
			ChoiceID: cid,
			Key:      key,
			Text:     txt,
		})
	}
	return &FateQuestionResult{
		QuestionID: qid,
		Story:      story,
		Options:    opts,
	}, nil
}
