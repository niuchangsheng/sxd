// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// @author Ateng
// @since 2026-10-09
package protocol

import (
	"fmt"
)

// 仙旅奇缘业务结果码 (对齐 2026 官方微端 Mod_TravelEvent_Base)。
const (
	// FateResultCDTime 事件冷却中，需等待 cdTimer 或消耗元宝消除 CD
	FateResultCDTime uint8 = 0
	// FateResultReachMaxCount 今日事件次数已达上限
	FateResultReachMaxCount uint8 = 1
	// FateResultSuccess 操作成功
	FateResultSuccess uint8 = 2
	// FateResultNoIngot 元宝不足，无法消除 CD
	FateResultNoIngot uint8 = 8
)

// 仙旅奇缘奖励类型 (对齐 2026 官方微端 Mod_TravelEvent_Base)。
const (
	FateAwardCoin  uint8 = 3 // 铜钱
	FateAwardExp   uint8 = 4 // 经验
	FateAwardFame  uint8 = 5 // 声望
	FateAwardPower uint8 = 6 // 体力
	FateAwardSkill uint8 = 7 // 阅历
)

// FateOption 代表仙旅奇缘事件中的单个分支选项。
type FateOption struct {
	// AnswerID 选项唯一编号，提交答案时原样回传
	AnswerID int32
	// Sign 选项字母标记 (如 "A", "B")
	Sign string
	// Text 选项文字描述
	Text string
}

// FateGetInfoRequest 代表查询仙旅奇缘活动状态请求。
type FateGetInfoRequest struct {
	// PrevAct 前序协议编号
	PrevAct uint32
}

// FateGetInfoResult 代表仙旅奇缘活动状态。
//
// 线格式 (Mod_TravelEvent_Base.get_activity_info):
// Int32 joinCount, Int32 totalCount, Int32 cdTimer, Int32 cdIngot。
// 线上服务端可能在末尾追加扩展字段，客户端按官方协议仅读取前 4 个 Int32。
type FateGetInfoResult struct {
	// JoinCount 今日已参与次数
	JoinCount int32
	// TotalCount 今日总次数上限
	TotalCount int32
	// CdTimer 距下次可参与的冷却秒数 (<=0 表示无冷却)
	CdTimer int32
	// CdIngot 消除冷却所需元宝数
	CdIngot int32
}

// RemainingTimes 返回今日剩余可参与次数。
func (r *FateGetInfoResult) RemainingTimes() int32 {
	if r == nil {
		return 0
	}
	remain := r.TotalCount - r.JoinCount
	if remain < 0 {
		return 0
	}
	return remain
}

// FateQuestionRequest 代表获取仙旅奇缘当前事件与分支请求。
type FateQuestionRequest struct {
	// PrevAct 前序协议编号
	PrevAct uint32
}

// FateQuestionResult 代表仙旅奇缘事件题目与分支选项明细。
//
// 线格式 (Mod_TravelEvent_Base.get_event_and_answer):
// Int32 eventId, String event, [Int32 answerId, String answerSign, String eventAnswer] answerList,
// UByte msg, Int32 curAnswerTimes, Int32 tolAnswerTimes, Int32 joinCount。
//
// 当 CurAnswerTimes 达到 TolAnswerTimes 时说明本轮答题宝箱已可领取；
// 未开启宝箱前服务端会拒绝新的答题请求且不回包。
type FateQuestionResult struct {
	// EventID 事件编号，提交答案时原样回传
	EventID int32
	// Story 事件故事情节叙述文本
	Story string
	// Options 可供选择的分支列表
	Options []FateOption
	// Msg 业务结果码 (FateResultSuccess 等)
	Msg uint8
	// CurAnswerTimes 当前已答题次数 (回包尾部扩展字段, 缺省为 0)
	CurAnswerTimes int32
	// TolAnswerTimes 本轮可答题次数上限 (回包尾部扩展字段, 缺省为 0)
	TolAnswerTimes int32
	// JoinCount 今日已参与次数 (回包尾部扩展字段, 缺省为 0)
	JoinCount int32
}

// FateAnswerRequest 代表提交仙旅奇缘分支答案请求。
//
// 线上实测该 Action 仅接受 8 字节 [Int32 eventId, Int32 answerId]；
// 在载荷末尾追加 PrevAct 会被网关静默忽略且不回包。
type FateAnswerRequest struct {
	// EventID 对应事件编号
	EventID int32
	// AnswerID 选中的选项唯一编号
	AnswerID int32
}

// FateAward 代表仙旅奇缘单条结算奖励。
type FateAward struct {
	// Type 奖励类型 (FateAwardCoin / FateAwardExp / FateAwardFame / FateAwardPower / FateAwardSkill)
	Type uint8
	// Value 奖励数量，可能为负数 (如扣除铜钱)
	Value int32
}

// FateAnswerResult 代表仙旅奇缘答题结算收益。
//
// 线格式 (Mod_TravelEvent_Base.answer_travel_event):
// String message, [UByte type, Int32 value] awardList,
// Int32 joinCount, Int32 totalCount, Int32 cdTimer, Int32 cdIngot。
type FateAnswerResult struct {
	// Message 结算文字提示 (如 "你获得50声望,30阅历！")
	Message string
	// Awards 逐项奖励明细
	Awards []FateAward
	// Info 结算后的最新活动状态
	Info FateGetInfoResult
}

// BuildFateGetInfoPacket 构造查询仙旅奇缘状态请求二进制封包。
//
// 官方业务请求为空，但网关 PrevAct 原子链会在网络层追加前序 Action；
// 这里将 PrevAct 显式写入载荷，线上验证可正常回包。
func BuildFateGetInfoPacket(req FateGetInfoRequest) (*Packet, error) {
	w := NewWriter()
	prevAct := req.PrevAct
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDFateGetInfo, w.Bytes()), nil
}

// ParseFateGetInfoRequest 从载荷中反序列化查询仙旅奇缘状态请求。
func ParseFateGetInfoRequest(payload []byte) (*FateGetInfoRequest, error) {
	r := NewReader(payload)
	prevAct, err := r.ReadUint32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read prev_act: %v", ErrInvalidPayload, err)
	}
	return &FateGetInfoRequest{PrevAct: prevAct}, nil
}

// BuildFateQuestionPacket 构造获取仙旅奇缘事件题目请求二进制封包。
func BuildFateQuestionPacket(req FateQuestionRequest) (*Packet, error) {
	w := NewWriter()
	prevAct := req.PrevAct
	if prevAct == 0 {
		prevAct = ActionIDFateGetInfo
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDFateGetQuestion, w.Bytes()), nil
}

// ParseFateQuestionRequest 从载荷中反序列化获取事件题目请求。
func ParseFateQuestionRequest(payload []byte) (*FateQuestionRequest, error) {
	r := NewReader(payload)
	prevAct, err := r.ReadUint32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read prev_act: %v", ErrInvalidPayload, err)
	}
	return &FateQuestionRequest{PrevAct: prevAct}, nil
}

// BuildFateAnswerPacket 构造提交仙旅奇缘选项请求二进制封包。
func BuildFateAnswerPacket(req FateAnswerRequest) (*Packet, error) {
	w := NewWriter()
	// 1. 写入 EventID (4B 大端序有符号整型)
	w.WriteInt32(req.EventID)
	// 2. 写入 AnswerID (4B 大端序有符号整型)
	w.WriteInt32(req.AnswerID)

	return NewPacket(ActionIDFateAnswer, w.Bytes()), nil
}

// ParseFateAnswerRequest 从载荷中反序列化提交选项请求。
func ParseFateAnswerRequest(payload []byte) (*FateAnswerRequest, error) {
	r := NewReader(payload)
	eventID, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read event_id: %v", ErrInvalidPayload, err)
	}
	answerID, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read answer_id: %v", ErrInvalidPayload, err)
	}
	return &FateAnswerRequest{EventID: eventID, AnswerID: answerID}, nil
}

// FateDrawItem 代表仙旅奇缘答题宝箱中的单件道具奖励。
type FateDrawItem struct {
	// ItemID 道具编号
	ItemID int32
	// Num 道具数量
	Num int32
}

// FateLotteryDrawResult 代表仙旅奇缘开宝箱结算结果。
//
// 线格式 (Mod_TravelEvent_Base.lottery_draw):
// UByte msg, [Int32 itemId, Int32 itemNum] awardList。
type FateLotteryDrawResult struct {
	// Msg 业务结果码 (FateResultSuccess 等)
	Msg uint8
	// Awards 宝箱道具奖励明细
	Awards []FateDrawItem
}

// BuildFateLotteryDrawPacket 构造开启仙旅奇缘答题宝箱请求二进制封包。
//
// 官方请求为空，但网关 PrevAct 原子链需要在载荷中写入前序 Action。
func BuildFateLotteryDrawPacket(prevAct uint32) (*Packet, error) {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDFateGetQuestion
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDFateLotteryDraw, w.Bytes()), nil
}

// ParseFateLotteryDrawResult 从载荷中反序列化开宝箱结算结果。
func ParseFateLotteryDrawResult(payload []byte) (*FateLotteryDrawResult, error) {
	r := NewReader(payload)
	msg, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read lottery msg: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read lottery award count: %v", ErrInvalidPayload, err)
	}
	awards := make([]FateDrawItem, 0, count)
	for i := 0; i < int(count); i++ {
		itemID, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read lottery item_id #%d: %v", ErrInvalidPayload, i, err)
		}
		num, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read lottery item_num #%d: %v", ErrInvalidPayload, i, err)
		}
		awards = append(awards, FateDrawItem{ItemID: itemID, Num: num})
	}
	return &FateLotteryDrawResult{Msg: msg, Awards: awards}, nil
}

// ParseFateGetInfoResult 从载荷中反序列化仙旅奇缘活动状态。
func ParseFateGetInfoResult(payload []byte) (*FateGetInfoResult, error) {
	if len(payload) < 16 {
		return nil, fmt.Errorf("%w: payload too short for fate info (%dB)", ErrInvalidPayload, len(payload))
	}
	r := NewReader(payload)
	joinCount, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read join_count: %v", ErrInvalidPayload, err)
	}
	totalCount, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read total_count: %v", ErrInvalidPayload, err)
	}
	cdTimer, _ := r.ReadInt32()
	cdIngot, _ := r.ReadInt32()

	return &FateGetInfoResult{
		JoinCount:  joinCount,
		TotalCount: totalCount,
		CdTimer:    cdTimer,
		CdIngot:    cdIngot,
	}, nil
}

// ParseFateQuestionResult 从载荷中反序列化仙旅奇缘事件题目与选项。
func ParseFateQuestionResult(payload []byte) (*FateQuestionResult, error) {
	r := NewReader(payload)
	eventID, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read event_id: %v", ErrInvalidPayload, err)
	}
	story, err := r.ReadString()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read story: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read answer count: %v", ErrInvalidPayload, err)
	}
	opts := make([]FateOption, 0, count)
	for i := 0; i < int(count); i++ {
		answerID, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read answer_id #%d: %v", ErrInvalidPayload, i, err)
		}
		sign, err := r.ReadString()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read answer_sign #%d: %v", ErrInvalidPayload, i, err)
		}
		text, err := r.ReadString()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read answer_text #%d: %v", ErrInvalidPayload, i, err)
		}
		opts = append(opts, FateOption{AnswerID: answerID, Sign: sign, Text: text})
	}
	msg, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read msg: %v", ErrInvalidPayload, err)
	}

	res := &FateQuestionResult{
		EventID: eventID,
		Story:   story,
		Options: opts,
		Msg:     msg,
	}
	// 尾部扩展字段在部分区服缺省, 解析失败时保留零值即可。
	if v, err := r.ReadInt32(); err == nil {
		res.CurAnswerTimes = v
	}
	if v, err := r.ReadInt32(); err == nil {
		res.TolAnswerTimes = v
	}
	if v, err := r.ReadInt32(); err == nil {
		res.JoinCount = v
	}
	return res, nil
}

// ParseFateAnswerResult 从服务端回包中解析结算奖励文本、奖励明细与最新活动状态。
func ParseFateAnswerResult(payload []byte) (*FateAnswerResult, error) {
	r := NewReader(payload)
	msg, err := r.ReadString()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read message: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read award count: %v", ErrInvalidPayload, err)
	}
	awards := make([]FateAward, 0, count)
	for i := 0; i < int(count); i++ {
		awardType, err := r.ReadUint8()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read award type #%d: %v", ErrInvalidPayload, i, err)
		}
		value, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read award value #%d: %v", ErrInvalidPayload, i, err)
		}
		awards = append(awards, FateAward{Type: awardType, Value: value})
	}
	infoOffset := len(payload) - r.Remaining()
	info, err := ParseFateGetInfoResult(payload[infoOffset:])
	if err != nil {
		return nil, fmt.Errorf("failed to parse post-answer activity info: %w", err)
	}

	return &FateAnswerResult{
		Message: msg,
		Awards:  awards,
		Info:    *info,
	}, nil
}

// BuildFateGetInfoResultPacket 构造仙旅奇缘状态回包 (测试与 Mock 服务端使用)。
func BuildFateGetInfoResultPacket(res FateGetInfoResult) *Packet {
	w := NewWriter()
	w.WriteInt32(res.JoinCount)
	w.WriteInt32(res.TotalCount)
	w.WriteInt32(res.CdTimer)
	w.WriteInt32(res.CdIngot)
	return NewPacket(ActionIDFateGetInfo, w.Bytes())
}

// BuildFateQuestionResultPacket 构造仙旅奇缘事件题目回包 (测试与 Mock 服务端使用)。
func BuildFateQuestionResultPacket(res FateQuestionResult) *Packet {
	w := NewWriter()
	w.WriteInt32(res.EventID)
	w.WriteString(res.Story)
	w.WriteUint16(uint16(len(res.Options)))
	for _, opt := range res.Options {
		w.WriteInt32(opt.AnswerID)
		w.WriteString(opt.Sign)
		w.WriteString(opt.Text)
	}
	w.WriteUint8(res.Msg)
	w.WriteInt32(res.CurAnswerTimes)
	w.WriteInt32(res.TolAnswerTimes)
	w.WriteInt32(res.JoinCount)
	return NewPacket(ActionIDFateGetQuestion, w.Bytes())
}

// BuildFateLotteryDrawResultPacket 构造仙旅奇缘开宝箱回包 (测试与 Mock 服务端使用)。
func BuildFateLotteryDrawResultPacket(res FateLotteryDrawResult) *Packet {
	w := NewWriter()
	w.WriteUint8(res.Msg)
	w.WriteUint16(uint16(len(res.Awards)))
	for _, award := range res.Awards {
		w.WriteInt32(award.ItemID)
		w.WriteInt32(award.Num)
	}
	return NewPacket(ActionIDFateLotteryDraw, w.Bytes())
}

// BuildFateAnswerResultPacket 构造仙旅奇缘答题结算回包 (测试与 Mock 服务端使用)。
func BuildFateAnswerResultPacket(res FateAnswerResult) *Packet {
	w := NewWriter()
	w.WriteString(res.Message)
	w.WriteUint16(uint16(len(res.Awards)))
	for _, award := range res.Awards {
		w.WriteUint8(award.Type)
		w.WriteInt32(award.Value)
	}
	w.WriteInt32(res.Info.JoinCount)
	w.WriteInt32(res.Info.TotalCount)
	w.WriteInt32(res.Info.CdTimer)
	w.WriteInt32(res.Info.CdIngot)
	return NewPacket(ActionIDFateAnswer, w.Bytes())
}
