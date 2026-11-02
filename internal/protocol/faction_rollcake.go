// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现帮派吉星高照 (Mod_Faction_Base.faction_roll_cake_*) 的请求封包与回包解析，
// 协议描述符对齐 2026 客户端 Main.swf 中 Mod_Faction_Base 的 AS3 声明:
//   - faction_roll_cake_remain_times: module 10, action 44, response [ByteUtil]
//   - faction_roll_cake_info:         module 10, action 45, response [IntUtil, IntUtil, ByteUtil, IntUtil, [IntUtil, StringUtil, IntUtil]]
//   - roll_cake:                      module 10, action 46, response [UByteUtil, ByteUtil, IntUtil, IntUtil, [ByteUtil], ByteUtil]
package protocol

import "fmt"

// FactionRollCakeMember 表示帮派吉星高照战绩列表中的一名成员。
type FactionRollCakeMember struct {
	// PlayerID 成员角色编号
	PlayerID int32
	// NickName 成员昵称
	NickName string
	// Score 成员本周最佳成绩
	Score int32
}

// FactionRollCakeInfo 表示帮派吉星高照面板信息 (Mod 10, Act 45 回包)。
type FactionRollCakeInfo struct {
	// WeekBestScore 本周个人最佳成绩
	WeekBestScore int32
	// TodayBestScore 今日个人最佳成绩
	TodayBestScore int32
	// RemainTimes 今日剩余投掷次数
	RemainTimes int8
	// FactionScore 帮派累计成绩
	FactionScore int32
	// Members 帮派成员成绩排行
	Members []FactionRollCakeMember
}

// FactionRollCakeResult 表示一次摇骰结算 (Mod 10, Act 46 回包)。
type FactionRollCakeResult struct {
	// Msg 业务结果码 (RollCakeResultSuccess 等)
	Msg uint8
	// Type 奖励类型编号
	Type int8
	// Score 本次投掷得分
	Score int32
	// TodayScore 今日累计得分
	TodayScore int32
	// Dice 本次投掷的各颗骰子点数
	Dice []int8
	// RemainTimes 投掷后今日剩余次数
	RemainTimes int8
}

// BuildFactionRollCakeRemainTimesPacket 构造查询吉星高照剩余投掷次数封包 (Mod 10, Act 44)。
func BuildFactionRollCakeRemainTimesPacket(prevAct uint32) *Packet {
	return newPrevActPacket(ActionIDFactionRollCakeRemainTimes, prevAct)
}

// ParseFactionRollCakeRemainTimesResult 解析剩余投掷次数回包:
// int8 remain_times。
func ParseFactionRollCakeRemainTimesResult(payload []byte) (int8, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	remain, err := r.ReadInt8()
	if err != nil {
		return 0, fmt.Errorf("%w: failed to read roll cake remain_times: %v", ErrInvalidPayload, err)
	}
	return remain, nil
}

// BuildFactionRollCakeInfoPacket 构造查询吉星高照战绩信息封包 (Mod 10, Act 45)。
func BuildFactionRollCakeInfoPacket(prevAct uint32) *Packet {
	return newPrevActPacket(ActionIDFactionRollCakeInfo, prevAct)
}

// ParseFactionRollCakeInfoResult 解析吉星高照战绩信息回包:
// int32 week_best + int32 today_best + int8 remain_times + int32 faction_score
// + uint16 count + count*(int32 player_id + string nickname + int32 score)。
func ParseFactionRollCakeInfoResult(payload []byte) (*FactionRollCakeInfo, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))

	weekBest, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read week_best_score: %v", ErrInvalidPayload, err)
	}
	todayBest, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read today_best_score: %v", ErrInvalidPayload, err)
	}
	remain, err := r.ReadInt8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read remain_times: %v", ErrInvalidPayload, err)
	}
	factionScore, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read faction_score: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read member count: %v", ErrInvalidPayload, err)
	}

	members := make([]FactionRollCakeMember, 0, count)
	for i := 0; i < int(count); i++ {
		playerID, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read member[%d] player_id: %v", ErrInvalidPayload, i, err)
		}
		nickName, err := r.ReadString()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read member[%d] nickname: %v", ErrInvalidPayload, i, err)
		}
		score, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read member[%d] score: %v", ErrInvalidPayload, i, err)
		}
		members = append(members, FactionRollCakeMember{
			PlayerID: playerID,
			NickName: nickName,
			Score:    score,
		})
	}

	return &FactionRollCakeInfo{
		WeekBestScore:  weekBest,
		TodayBestScore: todayBest,
		RemainTimes:    remain,
		FactionScore:   factionScore,
		Members:        members,
	}, nil
}

// BuildFactionRollCakePacket 构造投掷一次吉星高照骰子封包 (Mod 10, Act 46)。
func BuildFactionRollCakePacket(prevAct uint32) *Packet {
	return newPrevActPacket(ActionIDFactionRollCake, prevAct)
}

// ParseFactionRollCakeResult 解析摇骰回包:
// uint8 msg + int8 type + int32 score + int32 today_score + uint16 count + count*int8 dice
// + int8 remain_times。
func ParseFactionRollCakeResult(payload []byte) (*FactionRollCakeResult, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))

	msg, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read roll cake msg: %v", ErrInvalidPayload, err)
	}
	typ, err := r.ReadInt8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read roll cake type: %v", ErrInvalidPayload, err)
	}
	score, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read roll cake score: %v", ErrInvalidPayload, err)
	}
	todayScore, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read roll cake today_score: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read dice count: %v", ErrInvalidPayload, err)
	}

	dice := make([]int8, 0, count)
	for i := 0; i < int(count); i++ {
		num, err := r.ReadInt8()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read dice[%d]: %v", ErrInvalidPayload, i, err)
		}
		dice = append(dice, num)
	}

	remain, err := r.ReadInt8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read roll cake remain_times: %v", ErrInvalidPayload, err)
	}

	return &FactionRollCakeResult{
		Msg:         msg,
		Type:        typ,
		Score:       score,
		TodayScore:  todayScore,
		Dice:        dice,
		RemainTimes: remain,
	}, nil
}

// newPrevActPacket 构造仅携带前序协议号的四字节请求载荷。
func newPrevActPacket(actionID uint32, prevAct uint32) *Packet {
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w := NewWriter()
	w.WriteUint32(prevAct)
	return NewPacket(actionID, w.Bytes())
}
