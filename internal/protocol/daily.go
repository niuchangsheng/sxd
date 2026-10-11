// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// @author Ateng
// @since 2026-10-11
package protocol

import (
	"fmt"
)

const (
	// ActionIDDailySignIn 每日签到与福利领取协议号 (Module 0, Action 73 -> 0x00000049)
	ActionIDDailySignIn uint32 = 0x00000049

	// ActionIDMailGetList 查询邮件列表协议号 (Module 18, Action 0 -> 0x00120000)
	ActionIDMailGetList uint32 = 0x00120000
	// ActionIDMailCollect 一键收取所有邮件附件协议号 (Module 18, Action 1 -> 0x00120001)
	ActionIDMailCollect uint32 = 0x00120001
)

// DailySignInRequest 代表查询或提交签到请求参数。
type DailySignInRequest struct {
	// PrevAct 前序动作号 (通常为 0 或 ActionIDPlayerGetInfo)
	PrevAct uint32
}

// DailySignInResult 代表每日签到执行结果与奖励。
type DailySignInResult struct {
	// Success 是否签到成功
	Success bool
	// AlreadySigned 今日是否已完成签到
	AlreadySigned bool
	// SignedDays 本月累计连续签到天数
	SignedDays int32
	// RewardCoins 获得的铜钱奖励
	RewardCoins int64
	// RewardIngots 获得的元宝奖励
	RewardIngots int32
	// Message 结算文字说明
	Message string
}

// BuildDailySignInPacket 构造每日签到查询/签到封包。
func BuildDailySignInPacket(req DailySignInRequest) *Packet {
	w := NewWriter()
	prevAct := req.PrevAct
	if prevAct == 0 {
		prevAct = ActionIDPlayerGetInfo
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDDailySignIn, w.Bytes())
}

// ParseDailySignInRequest 从载荷中反序列化签到请求。
func ParseDailySignInRequest(payload []byte) (*DailySignInRequest, error) {
	if len(payload) < 4 {
		return &DailySignInRequest{PrevAct: ActionIDPlayerGetInfo}, nil
	}
	r := NewReader(payload)
	prevAct, err := r.ReadUint32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read prev_act: %v", ErrInvalidPayload, err)
	}
	return &DailySignInRequest{PrevAct: prevAct}, nil
}

// BuildDailySignInResultPacket 构造每日签到响应封包。
func BuildDailySignInResultPacket(res DailySignInResult) *Packet {
	w := NewWriter()
	var flag uint8
	if res.Success {
		flag = 1
	}
	w.WriteUint8(flag)
	var signedFlag uint8
	if res.AlreadySigned {
		signedFlag = 1
	}
	w.WriteUint8(signedFlag)
	w.WriteInt32(res.SignedDays)
	w.WriteInt64(res.RewardCoins)
	w.WriteInt32(res.RewardIngots)
	w.WriteString(res.Message)
	return NewPacket(ActionIDDailySignIn, w.Bytes())
}

// ParseDailySignInResult 从载荷中反序列化签到回包。
func ParseDailySignInResult(payload []byte) (*DailySignInResult, error) {
	if len(payload) == 0 {
		return &DailySignInResult{Success: true, Message: "签到成功"}, nil
	}
	r := NewReader(payload)
	flag, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read flag: %v", ErrInvalidPayload, err)
	}
	alreadySigned, _ := r.ReadUint8()
	signedDays, _ := r.ReadInt32()
	coins, _ := r.ReadInt64()
	ingots, _ := r.ReadInt32()
	msg, _ := r.ReadString()

	return &DailySignInResult{
		Success:       flag == 1,
		AlreadySigned: alreadySigned == 1,
		SignedDays:    signedDays,
		RewardCoins:   coins,
		RewardIngots:  ingots,
		Message:       msg,
	}, nil
}

// MailCollectRequest 代表一键领取邮件附件请求。
type MailCollectRequest struct {
	// PrevAct 前序动作号
	PrevAct uint32
}

// MailCollectResult 代表一键领取邮件附件回包。
type MailCollectResult struct {
	// Success 是否成功领取
	Success bool
	// CollectedCount 成功提取附件的邮件数量
	CollectedCount int32
	// RewardCoins 累计领取的铜钱
	RewardCoins int64
	// Message 结算文字提示
	Message string
}

// BuildMailCollectPacket 构造一键领取邮件附件封包。
func BuildMailCollectPacket(req MailCollectRequest) *Packet {
	w := NewWriter()
	prevAct := req.PrevAct
	if prevAct == 0 {
		prevAct = ActionIDMailGetList
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDMailCollect, w.Bytes())
}

// ParseMailCollectRequest 从载荷中反序列化邮件提取请求。
func ParseMailCollectRequest(payload []byte) (*MailCollectRequest, error) {
	if len(payload) < 4 {
		return &MailCollectRequest{PrevAct: ActionIDMailGetList}, nil
	}
	r := NewReader(payload)
	prevAct, err := r.ReadUint32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read prev_act: %v", ErrInvalidPayload, err)
	}
	return &MailCollectRequest{PrevAct: prevAct}, nil
}

// BuildMailCollectResultPacket 构造一键提取邮件响应封包。
func BuildMailCollectResultPacket(res MailCollectResult) *Packet {
	w := NewWriter()
	var flag uint8
	if res.Success {
		flag = 1
	}
	w.WriteUint8(flag)
	w.WriteInt32(res.CollectedCount)
	w.WriteInt64(res.RewardCoins)
	w.WriteString(res.Message)
	return NewPacket(ActionIDMailCollect, w.Bytes())
}

// ParseMailCollectResult 从载荷中反序列化邮件提取回包。
func ParseMailCollectResult(payload []byte) (*MailCollectResult, error) {
	if len(payload) == 0 {
		return &MailCollectResult{Success: true, Message: "领取完成"}, nil
	}
	r := NewReader(payload)
	flag, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read flag: %v", ErrInvalidPayload, err)
	}
	count, _ := r.ReadInt32()
	coins, _ := r.ReadInt64()
	msg, _ := r.ReadString()

	return &MailCollectResult{
		Success:        flag == 1,
		CollectedCount: count,
		RewardCoins:    coins,
		Message:        msg,
	}, nil
}
