// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方尊像膜拜协议 (Mod_WorshipMars_Base, Module 41 / 0x29)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_WorshipMars_Base 与
// com.datas.WorshipMarsData：查询膜拜信息与剩余次数后上香。
// 供品 1 (白檀香) 免费；2/3 需元宝，自动化只使用免费供品。
package protocol

import "fmt"

const (
	// ActionIDWorshipMarsInfo 查询膜拜信息 (Module 41, Action 0 -> 0x00290000)
	ActionIDWorshipMarsInfo uint32 = 0x00290000
	// ActionIDWorshipMarsIncense 上香膜拜 (Module 41, Action 1 -> 0x00290001)
	ActionIDWorshipMarsIncense uint32 = 0x00290001
	// ActionIDWorshipMarsRemain 查询剩余膜拜次数 (Module 41, Action 4 -> 0x00290004)
	ActionIDWorshipMarsRemain uint32 = 0x00290004
)

// Mod_WorshipMars_Base 结果码。
const (
	// WorshipMarsResultSuccess 成功
	WorshipMarsResultSuccess uint8 = 0
	// WorshipMarsResultOfferingsNotExist 供品不存在
	WorshipMarsResultOfferingsNotExist uint8 = 1
	// WorshipMarsResultCountUsed 次数已用尽
	WorshipMarsResultCountUsed uint8 = 2
	// WorshipMarsResultInsufficientIngot 元宝不足
	WorshipMarsResultInsufficientIngot uint8 = 3
	// WorshipMarsResultNotEnoughVIP VIP 等级不足
	WorshipMarsResultNotEnoughVIP uint8 = 4
	// WorshipMarsResultInsufficientSkill 阅历不足
	WorshipMarsResultInsufficientSkill uint8 = 5
	// WorshipMarsResultBlessingTimes 祝福次数已满
	WorshipMarsResultBlessingTimes uint8 = 6
	// WorshipMarsResultExpAdd 经验加成已满
	WorshipMarsResultExpAdd uint8 = 7
)

// 免费供品编号 (MarsType)。
const (
	// WorshipMarsOfferingFree 白檀香 (免费)
	WorshipMarsOfferingFree int32 = 1
)

// WorshipMarsInfo 表示膜拜面板信息。
//
// 线格式: Int mars_level, Int mars_exp, Int require_exp, Int incense_count,
// Int total_count, Byte is_cover,
// [Int player_id, String player_name, Int incense_id, Int exp, Int time] log_list。
type WorshipMarsInfo struct {
	// MarsLevel 武神等级
	MarsLevel int32
	// MarsExp 武神经验
	MarsExp int32
	// RequireExp 升级所需经验
	RequireExp int32
	// IncenseCount 已上香次数
	IncenseCount int32
	// TotalCount 总上香次数
	TotalCount int32
	// IsCover 是否可覆盖
	IsCover uint8
	// LogEntryCount 上香日志条数 (内容不消费)
	LogEntryCount int32
}

// BuildWorshipMarsInfoPacket 构造查询膜拜信息封包 (0x00290000)。
func BuildWorshipMarsInfoPacket() *Packet {
	return NewPacket(ActionIDWorshipMarsInfo, nil)
}

// BuildWorshipMarsIncensePacket 构造上香封包 (0x00290001)。
//
// 官方 Main.swf 请求仅 [Int offering_id]，不追加 PrevAct。
func BuildWorshipMarsIncensePacket(offeringID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(offeringID)
	return NewPacket(ActionIDWorshipMarsIncense, w.Bytes())
}

// BuildWorshipMarsRemainPacket 构造查询剩余次数封包 (0x00290004)。
func BuildWorshipMarsRemainPacket() *Packet {
	return NewPacket(ActionIDWorshipMarsRemain, nil)
}

// ParseWorshipMarsInfoResult 解析膜拜信息回包，跳过日志内容。
func ParseWorshipMarsInfoResult(payload []byte) (*WorshipMarsInfo, error) {
	r := NewReader(payload)
	res := &WorshipMarsInfo{}
	var err error
	if res.MarsLevel, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read mars_level: %v", ErrInvalidPayload, err)
	}
	if res.MarsExp, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read mars_exp: %v", ErrInvalidPayload, err)
	}
	if res.RequireExp, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read require_exp: %v", ErrInvalidPayload, err)
	}
	if res.IncenseCount, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read incense_count: %v", ErrInvalidPayload, err)
	}
	if res.TotalCount, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read total_count: %v", ErrInvalidPayload, err)
	}
	if res.IsCover, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read is_cover: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read incense log count: %v", ErrInvalidPayload, err)
	}
	res.LogEntryCount = int32(count)
	for i := 0; i < int(count); i++ {
		if _, err := r.ReadInt32(); err != nil { // player_id
			return nil, fmt.Errorf("%w: failed to read incense log[%d].player_id: %v", ErrInvalidPayload, i, err)
		}
		if _, err := r.ReadString(); err != nil { // player_name
			return nil, fmt.Errorf("%w: failed to read incense log[%d].player_name: %v", ErrInvalidPayload, i, err)
		}
		if _, err := r.ReadInt32(); err != nil { // incense_id
			return nil, fmt.Errorf("%w: failed to read incense log[%d].incense_id: %v", ErrInvalidPayload, i, err)
		}
		if _, err := r.ReadInt32(); err != nil { // exp
			return nil, fmt.Errorf("%w: failed to read incense log[%d].exp: %v", ErrInvalidPayload, i, err)
		}
		if _, err := r.ReadInt32(); err != nil { // time
			return nil, fmt.Errorf("%w: failed to read incense log[%d].time: %v", ErrInvalidPayload, i, err)
		}
	}
	return res, nil
}

// ParseWorshipMarsIncenseResult 解析上香结果 (单字节结果码)。
func ParseWorshipMarsIncenseResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty worship mars result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// ParseWorshipMarsRemainResult 解析剩余膜拜次数 (Int remain_incense_times)。
func ParseWorshipMarsRemainResult(payload []byte) (int32, error) {
	r := NewReader(payload)
	n, err := r.ReadInt32()
	if err != nil {
		return 0, fmt.Errorf("%w: failed to read remain_incense_times: %v", ErrInvalidPayload, err)
	}
	return n, nil
}

// BuildWorshipMarsInfoResultPacket 构造膜拜信息回包，供离线 mock 与单测使用。
func BuildWorshipMarsInfoResultPacket(info WorshipMarsInfo) *Packet {
	w := NewWriter()
	w.WriteInt32(info.MarsLevel)
	w.WriteInt32(info.MarsExp)
	w.WriteInt32(info.RequireExp)
	w.WriteInt32(info.IncenseCount)
	w.WriteInt32(info.TotalCount)
	w.WriteUint8(info.IsCover)
	w.WriteUint16(0)
	return NewPacket(ActionIDWorshipMarsInfo, w.Bytes())
}

// BuildWorshipMarsIncenseResultPacket 构造上香回包，供离线 mock 与单测使用。
func BuildWorshipMarsIncenseResultPacket(result uint8) *Packet {
	return NewPacket(ActionIDWorshipMarsIncense, []byte{result})
}

// BuildWorshipMarsRemainResultPacket 构造剩余次数回包，供离线 mock 与单测使用。
func BuildWorshipMarsRemainResultPacket(n int32) *Packet {
	w := NewWriter()
	w.WriteInt32(n)
	return NewPacket(ActionIDWorshipMarsRemain, w.Bytes())
}
