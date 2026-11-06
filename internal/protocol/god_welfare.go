// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方天神赐福协议 (Mod_GodWelfare_Base, Module 328 / 0x0148)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_GodWelfare_Base 与
// com.views.GodWelfareView：
//   - get_god_welfare_info Act 0 -> 0x01480000
//   - get_level_award_info Act 1 -> 0x01480001
//   - get_award             Act 2 -> 0x01480002  [Short level]
package protocol

import "fmt"

const (
	// ModuleGodWelfare 天神赐福模块编号 (0x0148)
	ModuleGodWelfare uint16 = 328
)

// 天神赐福动作编号 (Mod_GodWelfare_Base)。
const (
	// ActionIDGodWelfareInfo 查询天神赐福信息 (Action 0 -> 0x01480000)
	ActionIDGodWelfareInfo uint32 = 0x01480000
	// ActionIDGodWelfareLevelAwardInfo 查询等级奖励列表 (Action 1 -> 0x01480001)
	ActionIDGodWelfareLevelAwardInfo uint32 = 0x01480001
	// ActionIDGodWelfareGetAward 领取等级奖励 (Action 2 -> 0x01480002)
	ActionIDGodWelfareGetAward uint32 = 0x01480002
)

// 天神赐福结果码 (Mod_GodWelfare_Base)。
const (
	// GodWelfareResultSuccess 成功
	GodWelfareResultSuccess uint8 = 2
	// GodWelfareResultHadGet 已领取
	GodWelfareResultHadGet uint8 = 3
	// GodWelfareResultLevelLimit 等级不足
	GodWelfareResultLevelLimit uint8 = 4
	// GodWelfareResultItemPackFull 背包已满
	GodWelfareResultItemPackFull uint8 = 5
	// GodWelfareResultFailure 失败
	GodWelfareResultFailure uint8 = 6
)

// GodWelfareInfo 表示天神赐福状态。
type GodWelfareInfo struct {
	// Level 当前赐福等级
	Level int16
	// IsGeted 是否已领取
	IsGeted uint8
	// StartTime 开始时间
	StartTime string
	// EndTime 结束时间
	EndTime string
	// RemainSeconds 剩余秒数
	RemainSeconds int16
	// Point 当前赐福点数
	Point int32
	// MaxPoint 赐福点数上限
	MaxPoint int32
}

// GodWelfareLevelAward 表示一档等级奖励。
type GodWelfareLevelAward struct {
	// Level 等级
	Level int16
	// NeedValue 所需数值
	NeedValue int32
	// AwardValue 奖励数值
	AwardValue int32
	// IsGet 是否已领取
	IsGet uint8
}

// BuildGodWelfareInfoPacket 构造查询天神赐福信息封包 (0x01480000)。
func BuildGodWelfareInfoPacket() *Packet {
	return NewPacket(ActionIDGodWelfareInfo, nil)
}

// BuildGodWelfareLevelAwardInfoPacket 构造查询等级奖励列表封包 (0x01480001)。
func BuildGodWelfareLevelAwardInfoPacket() *Packet {
	return NewPacket(ActionIDGodWelfareLevelAwardInfo, nil)
}

// BuildGodWelfareGetAwardPacket 构造领取等级奖励封包 (0x01480002)。
func BuildGodWelfareGetAwardPacket(level int16) *Packet {
	w := NewWriter()
	w.WriteInt16(level)
	return NewPacket(ActionIDGodWelfareGetAward, w.Bytes())
}

// ParseGodWelfareInfoResult 解析天神赐福信息。
func ParseGodWelfareInfoResult(payload []byte) (*GodWelfareInfo, error) {
	r := NewReader(payload)
	info := &GodWelfareInfo{}
	var err error
	if info.Level, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read god welfare level: %v", ErrInvalidPayload, err)
	}
	if info.IsGeted, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read god welfare is_geted: %v", ErrInvalidPayload, err)
	}
	if info.StartTime, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: failed to read god welfare start: %v", ErrInvalidPayload, err)
	}
	if info.EndTime, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: failed to read god welfare end: %v", ErrInvalidPayload, err)
	}
	if info.RemainSeconds, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read god welfare remain: %v", ErrInvalidPayload, err)
	}
	if info.Point, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read god welfare point: %v", ErrInvalidPayload, err)
	}
	if info.MaxPoint, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read god welfare max_point: %v", ErrInvalidPayload, err)
	}
	return info, nil
}

// ParseGodWelfareLevelAwardInfoResult 解析等级奖励列表。
func ParseGodWelfareLevelAwardInfoResult(payload []byte) ([]GodWelfareLevelAward, error) {
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read god welfare award count: %v", ErrInvalidPayload, err)
	}
	awards := make([]GodWelfareLevelAward, 0, count)
	for i := 0; i < int(count); i++ {
		var a GodWelfareLevelAward
		if a.Level, err = r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("%w: failed to read award[%d].level: %v", ErrInvalidPayload, i, err)
		}
		if a.NeedValue, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read award[%d].need: %v", ErrInvalidPayload, i, err)
		}
		if a.AwardValue, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read award[%d].award: %v", ErrInvalidPayload, i, err)
		}
		if a.IsGet, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read award[%d].is_get: %v", ErrInvalidPayload, i, err)
		}
		awards = append(awards, a)
	}
	return awards, nil
}

// ParseGodWelfareGetAwardResult 解析领取等级奖励结果。
func ParseGodWelfareGetAwardResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty god welfare award result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// BuildGodWelfareInfoResultPacket 构造天神赐福信息回包，供离线 mock 与单测使用。
func BuildGodWelfareInfoResultPacket(info GodWelfareInfo) *Packet {
	w := NewWriter()
	w.WriteInt16(info.Level)
	w.WriteUint8(info.IsGeted)
	w.WriteString(info.StartTime)
	w.WriteString(info.EndTime)
	w.WriteInt16(info.RemainSeconds)
	w.WriteInt32(info.Point)
	w.WriteInt32(info.MaxPoint)
	return NewPacket(ActionIDGodWelfareInfo, w.Bytes())
}

// BuildGodWelfareLevelAwardInfoResultPacket 构造等级奖励列表回包，供离线 mock 与单测使用。
func BuildGodWelfareLevelAwardInfoResultPacket(awards []GodWelfareLevelAward) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(awards)))
	for _, a := range awards {
		w.WriteInt16(a.Level)
		w.WriteInt32(a.NeedValue)
		w.WriteInt32(a.AwardValue)
		w.WriteUint8(a.IsGet)
	}
	return NewPacket(ActionIDGodWelfareLevelAwardInfo, w.Bytes())
}

// BuildGodWelfareGetAwardResultPacket 构造领取回包，供离线 mock 与单测使用。
func BuildGodWelfareGetAwardResultPacket(code uint8) *Packet {
	return NewPacket(ActionIDGodWelfareGetAward, []byte{code})
}
