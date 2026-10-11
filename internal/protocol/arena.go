// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// @author Ateng
// @since 2026-10-09
package protocol

import (
	"fmt"
)

// ArenaOpponent 代表竞技场中可挑战的对手信息。
type ArenaOpponent struct {
	// Rank 对手在竞技场的排名
	Rank int32
	// RoleID 对手角色 ID
	RoleID int32
	// Name 对手角色名称
	Name string
	// FightValue 对手战力数值
	FightValue int32
}

// ArenaGetTimesRequest 代表查询竞技场剩余挑战次数请求。
type ArenaGetTimesRequest struct {
	// PrevAct 前序协议编号
	PrevAct uint32
}

// ArenaGetTimesResult 代表竞技场剩余挑战次数回包。
type ArenaGetTimesResult struct {
	// RemainingTimes 当前剩余可用挑战次数
	RemainingTimes int32
}

// ArenaGetRankingRequest 代表查询当前玩家竞技场排名请求。
type ArenaGetRankingRequest struct {
	// PrevAct 前序协议编号
	PrevAct uint32
}

// ArenaGetRankingResult 代表玩家竞技场排名回包。
type ArenaGetRankingResult struct {
	// Ranking 当前竞技场排名 (0 为未上榜)
	Ranking int32
}

// ArenaGetOpponentsRequest 代表查询竞技场对手列表请求。
type ArenaGetOpponentsRequest struct {
	// PrevAct 前序协议编号
	PrevAct uint32
}

// ArenaGetOpponentsResult 代表竞技场对手列表回包。
type ArenaGetOpponentsResult struct {
	// Opponents 可挑战对手切片
	Opponents []ArenaOpponent
}

// ArenaChallengeRequest 代表发起竞技场挑战请求 (ActionID=0x001C0002)。
type ArenaChallengeRequest struct {
	// TargetRank 挑战目标的竞技场排名序号
	TargetRank int32
	// PrevAct 前序协议编号 (如 0x001C0001)
	PrevAct uint32
}

// ArenaChallengeResult 代表发起竞技场挑战后的即时战报结算。
type ArenaChallengeResult struct {
	// Success 是否挑战获胜
	Success bool
	// NewRank 挑战结算后的全新个人排名
	NewRank int32
}

// BuildArenaGetTimesPacket 构造查询竞技场剩余次数封包。
func BuildArenaGetTimesPacket(req ArenaGetTimesRequest) (*Packet, error) {
	w := NewWriter()
	prevAct := req.PrevAct
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDArenaGetTimes, w.Bytes()), nil
}

// ParseArenaGetTimesRequest 从载荷反序列化查询剩余次数请求。
func ParseArenaGetTimesRequest(payload []byte) (*ArenaGetTimesRequest, error) {
	r := NewReader(payload)
	prevAct, err := r.ReadUint32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read prev_act: %v", ErrInvalidPayload, err)
	}
	return &ArenaGetTimesRequest{PrevAct: prevAct}, nil
}

// BuildArenaGetTimesResultPacket 构造竞技场剩余次数回包封包。
func BuildArenaGetTimesResultPacket(times int32) (*Packet, error) {
	w := NewWriter()
	w.WriteInt32(times)
	return NewPacket(ActionIDArenaGetTimes, w.Bytes()), nil
}

// ParseArenaGetTimesResult 从载荷反序列化剩余次数回包。
func ParseArenaGetTimesResult(payload []byte) (*ArenaGetTimesResult, error) {
	if len(payload) < 4 {
		return nil, fmt.Errorf("%w: payload too short for arena times", ErrInvalidPayload)
	}
	r := NewReader(payload)
	times, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read remaining times: %v", ErrInvalidPayload, err)
	}
	return &ArenaGetTimesResult{RemainingTimes: times}, nil
}

// BuildArenaGetOpponentsPacket 构造查询对手列表请求封包。
func BuildArenaGetOpponentsPacket(req ArenaGetOpponentsRequest) (*Packet, error) {
	w := NewWriter()
	prevAct := req.PrevAct
	if prevAct == 0 {
		prevAct = ActionIDArenaGetTimes
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDArenaGetOpponents, w.Bytes()), nil
}

// ParseArenaGetOpponentsRequest 从载荷反序列化查询对手列表请求。
func ParseArenaGetOpponentsRequest(payload []byte) (*ArenaGetOpponentsRequest, error) {
	r := NewReader(payload)
	prevAct, err := r.ReadUint32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read prev_act: %v", ErrInvalidPayload, err)
	}
	return &ArenaGetOpponentsRequest{PrevAct: prevAct}, nil
}

// BuildArenaChallengePacket 构造发起竞技场挑战请求封包。
func BuildArenaChallengePacket(req ArenaChallengeRequest) (*Packet, error) {
	w := NewWriter()
	// 1. 写入 TargetRank (4B 大端序整型)
	w.WriteInt32(req.TargetRank)
	// 2. 写入 PrevAct (4B 大端序协议号)
	prevAct := req.PrevAct
	if prevAct == 0 {
		prevAct = ActionIDArenaGetOpponents
	}
	w.WriteUint32(prevAct)

	return NewPacket(ActionIDArenaChallenge, w.Bytes()), nil
}

// ParseArenaChallengeRequest 从载荷反序列化发起挑战请求。
func ParseArenaChallengeRequest(payload []byte) (*ArenaChallengeRequest, error) {
	if len(payload) < 8 {
		return nil, fmt.Errorf("%w: payload too short for arena challenge", ErrInvalidPayload)
	}
	r := NewReader(payload)
	rank, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read target_rank: %v", ErrInvalidPayload, err)
	}
	prevAct, err := r.ReadUint32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read prev_act: %v", ErrInvalidPayload, err)
	}
	return &ArenaChallengeRequest{
		TargetRank: rank,
		PrevAct:    prevAct,
	}, nil
}

// BuildArenaGetOpponentsResultPacket 构造竞技场对手列表回包封包。
func BuildArenaGetOpponentsResultPacket(opponents []ArenaOpponent) (*Packet, error) {
	w := NewWriter()
	w.WriteUint16(uint16(len(opponents)))
	for _, opp := range opponents {
		w.WriteInt32(opp.Rank)
		w.WriteInt32(opp.RoleID)
		w.WriteString(opp.Name)
		w.WriteInt32(opp.FightValue)
	}
	return NewPacket(ActionIDArenaGetOpponents, w.Bytes()), nil
}

// ParseArenaGetOpponentsResult 从载荷反序列化对手列表回包。
func ParseArenaGetOpponentsResult(payload []byte) (*ArenaGetOpponentsResult, error) {
	if len(payload) < 2 {
		return &ArenaGetOpponentsResult{}, nil
	}
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read opponent count: %v", ErrInvalidPayload, err)
	}
	opps := make([]ArenaOpponent, 0, count)
	for i := 0; i < int(count); i++ {
		if r.Remaining() < 10 {
			break
		}
		rank, err := r.ReadInt32()
		if err != nil {
			break
		}
		roleID, err := r.ReadInt32()
		if err != nil {
			break
		}
		name, err := r.ReadString()
		if err != nil {
			break
		}
		fightVal, err := r.ReadInt32()
		if err != nil {
			break
		}
		opps = append(opps, ArenaOpponent{
			Rank:       rank,
			RoleID:     roleID,
			Name:       name,
			FightValue: fightVal,
		})
	}
	return &ArenaGetOpponentsResult{Opponents: opps}, nil
}

// BuildArenaChallengeResultPacket 构造竞技场挑战战报结算封包。
func BuildArenaChallengeResultPacket(res ArenaChallengeResult) (*Packet, error) {
	w := NewWriter()
	var flag uint8
	if res.Success {
		flag = 1
	}
	w.WriteUint8(flag)
	w.WriteInt32(res.NewRank)
	return NewPacket(ActionIDArenaChallenge, w.Bytes()), nil
}

// ParseArenaChallengeResult 从载荷反序列化挑战结算回包。
func ParseArenaChallengeResult(payload []byte) (*ArenaChallengeResult, error) {
	if len(payload) == 0 {
		return &ArenaChallengeResult{Success: true, NewRank: 0}, nil
	}
	r := NewReader(payload)
	flag, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read challenge flag: %v", ErrInvalidPayload, err)
	}
	newRank, _ := r.ReadInt32()
	return &ArenaChallengeResult{
		Success: flag == 1,
		NewRank: newRank,
	}, nil
}
