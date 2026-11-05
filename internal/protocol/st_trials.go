// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方精灵试炼协议 (Mod_StTrials_Base, Module 205 / 0xCD)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_StTrials_Base 与
// com.datas.StTrialsData：先读各模式剩余挑战次数，再按层领奖与挑战。
package protocol

import "fmt"

const (
	// ActionIDStTrialsActiveInfo 查询试炼活动信息 (Module 205, Action 0 -> 0x00CD0000)
	ActionIDStTrialsActiveInfo uint32 = 0x00CD0000
	// ActionIDStTrialsModeStatus 查询各模式状态 (Module 205, Action 1 -> 0x00CD0001)
	ActionIDStTrialsModeStatus uint32 = 0x00CD0001
	// ActionIDStTrialsModeInfo 查询指定模式详情 (Module 205, Action 2 -> 0x00CD0002)
	ActionIDStTrialsModeInfo uint32 = 0x00CD0002
	// ActionIDStTrialsGetAward 领取试炼奖励 (Module 205, Action 3 -> 0x00CD0003)
	ActionIDStTrialsGetAward uint32 = 0x00CD0003
	// ActionIDStTrialsChallenge 发起试炼挑战 (Module 205, Action 5 -> 0x00CD0005)
	ActionIDStTrialsChallenge uint32 = 0x00CD0005
)

// Mod_StTrials_Base 结果码。
const (
	// StTrialsResultSuccess 成功
	StTrialsResultSuccess uint8 = 0
	// StTrialsResultIsGet 已领取
	StTrialsResultIsGet uint8 = 1
	// StTrialsResultTimeOut 时间结束
	StTrialsResultTimeOut uint8 = 2
	// StTrialsResultItemPackFull 物品背包已满
	StTrialsResultItemPackFull uint8 = 3
	// StTrialsResultFatePackFull 命格背包已满
	StTrialsResultFatePackFull uint8 = 4
	// StTrialsResultFivePackFull 五行背包已满
	StTrialsResultFivePackFull uint8 = 5
	// StTrialsResultError 错误
	StTrialsResultError uint8 = 6
	// StTrialsResultFaild 挑战失败
	StTrialsResultFaild uint8 = 7
	// StTrialsResultCannotChallenge 不可挑战
	StTrialsResultCannotChallenge uint8 = 8
)

// StTrialsMode 表示一个试炼模式的剩余次数与状态。
type StTrialsMode struct {
	// ModeType 模式编号
	ModeType uint8
	// RemainChallengeTimes 剩余挑战次数
	RemainChallengeTimes uint8
	// ModeStatus 模式状态
	ModeStatus uint8
}

// StTrialsModeInfo 表示指定模式的当前进度。
type StTrialsModeInfo struct {
	// Floor 当前层
	Floor uint8
	// Pass 当前通关标记
	Pass uint8
	// AwardStatus 奖励状态
	AwardStatus uint8
}

// StTrialsAward 表示一项试炼奖励。
type StTrialsAward struct {
	// Type 奖励类型
	Type int32
	// ID 奖励编号
	ID int32
	// Num 数量
	Num int32
}

// BuildStTrialsModeStatusPacket 构造查询模式状态封包 (0x00CD0001)。
func BuildStTrialsModeStatusPacket() *Packet {
	return NewPacket(ActionIDStTrialsModeStatus, nil)
}

// BuildStTrialsModeInfoPacket 构造查询模式详情封包 (0x00CD0002)。
func BuildStTrialsModeInfoPacket(mode uint8) *Packet {
	return NewPacket(ActionIDStTrialsModeInfo, []byte{mode})
}

// BuildStTrialsGetAwardPacket 构造领取奖励封包 (0x00CD0003)。
func BuildStTrialsGetAwardPacket(floor, pass, mode uint8) *Packet {
	return NewPacket(ActionIDStTrialsGetAward, []byte{floor, pass, mode})
}

// BuildStTrialsChallengePacket 构造挑战封包 (0x00CD0005)。
func BuildStTrialsChallengePacket(mode uint8) *Packet {
	return NewPacket(ActionIDStTrialsChallenge, []byte{mode})
}

// ParseStTrialsModeStatusResult 解析模式状态列表。
func ParseStTrialsModeStatusResult(payload []byte) ([]StTrialsMode, error) {
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read st trials mode count: %v", ErrInvalidPayload, err)
	}
	out := make([]StTrialsMode, 0, count)
	for i := 0; i < int(count); i++ {
		var m StTrialsMode
		if m.ModeType, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read mode[%d].mode_type: %v", ErrInvalidPayload, i, err)
		}
		if m.RemainChallengeTimes, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read mode[%d].remain_challenge_times: %v", ErrInvalidPayload, i, err)
		}
		if m.ModeStatus, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read mode[%d].mode_status: %v", ErrInvalidPayload, i, err)
		}
		out = append(out, m)
	}
	return out, nil
}

// ParseStTrialsModeInfoResult 解析模式详情，跳过玩家展示字段。
func ParseStTrialsModeInfoResult(payload []byte) (*StTrialsModeInfo, error) {
	r := NewReader(payload)
	res := &StTrialsModeInfo{}
	var err error
	if res.Floor, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read st trials floor: %v", ErrInvalidPayload, err)
	}
	if res.Pass, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read st trials pass: %v", ErrInvalidPayload, err)
	}
	if res.AwardStatus, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read st trials award_status: %v", ErrInvalidPayload, err)
	}
	// 其余字段为展示信息 (nickname/level/war_power/...)，本流程不消费。
	return res, nil
}

// ParseStTrialsAwardResult 解析领奖结果: UByte result + [Int type, Int id, Int num]。
func ParseStTrialsAwardResult(payload []byte) (uint8, []StTrialsAward, error) {
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return 0, nil, fmt.Errorf("%w: failed to read st trials award result: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return result, nil, fmt.Errorf("%w: failed to read st trials award count: %v", ErrInvalidPayload, err)
	}
	awards := make([]StTrialsAward, 0, count)
	for i := 0; i < int(count); i++ {
		var a StTrialsAward
		if a.Type, err = r.ReadInt32(); err != nil {
			return result, nil, fmt.Errorf("%w: failed to read award[%d].type: %v", ErrInvalidPayload, i, err)
		}
		if a.ID, err = r.ReadInt32(); err != nil {
			return result, nil, fmt.Errorf("%w: failed to read award[%d].id: %v", ErrInvalidPayload, i, err)
		}
		if a.Num, err = r.ReadInt32(); err != nil {
			return result, nil, fmt.Errorf("%w: failed to read award[%d].num: %v", ErrInvalidPayload, i, err)
		}
		awards = append(awards, a)
	}
	return result, awards, nil
}

// ParseStTrialsChallengeResult 解析挑战结果 (UByte challenge_result)。
func ParseStTrialsChallengeResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty st trials challenge result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// BuildStTrialsModeStatusResultPacket 构造模式状态回包，供离线 mock 与单测使用。
func BuildStTrialsModeStatusResultPacket(modes []StTrialsMode) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(modes)))
	for _, m := range modes {
		w.WriteUint8(m.ModeType)
		w.WriteUint8(m.RemainChallengeTimes)
		w.WriteUint8(m.ModeStatus)
	}
	return NewPacket(ActionIDStTrialsModeStatus, w.Bytes())
}

// BuildStTrialsModeInfoResultPacket 构造模式详情回包，供离线 mock 与单测使用。
func BuildStTrialsModeInfoResultPacket(info StTrialsModeInfo) *Packet {
	w := NewWriter()
	w.WriteUint8(info.Floor)
	w.WriteUint8(info.Pass)
	w.WriteUint8(info.AwardStatus)
	w.WriteString("")
	w.WriteInt32(0)
	w.WriteInt64(0)
	w.WriteInt32(0)
	w.WriteString("")
	w.WriteString("")
	w.WriteInt32(0)
	w.WriteString("")
	return NewPacket(ActionIDStTrialsModeInfo, w.Bytes())
}

// BuildStTrialsAwardResultPacket 构造领奖回包，供离线 mock 与单测使用。
func BuildStTrialsAwardResultPacket(result uint8) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	w.WriteUint16(0)
	return NewPacket(ActionIDStTrialsGetAward, w.Bytes())
}

// BuildStTrialsChallengeResultPacket 构造挑战回包，供离线 mock 与单测使用。
func BuildStTrialsChallengeResultPacket(result uint8) *Packet {
	return NewPacket(ActionIDStTrialsChallenge, []byte{result})
}
