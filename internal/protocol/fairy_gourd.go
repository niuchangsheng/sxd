// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方仙葫芦协议 (Mod_FairyGourd_Base, Module 220 / 0xDC)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_FairyGourd_Base 与
// com.datas.FairyGourdData：按日期累积铜钱/经验收益，逐日领取。
// 自动化只领取已累积收益，不消耗元宝。
package protocol

import "fmt"

const (
	// ActionIDFairyGourdDate 查询仙葫芦活动时间 (Module 220, Action 0 -> 0x00DC0000)
	ActionIDFairyGourdDate uint32 = 0x00DC0000
	// ActionIDFairyGourdList 查询仙葫芦收益列表 (Module 220, Action 1 -> 0x00DC0001)
	ActionIDFairyGourdList uint32 = 0x00DC0001
	// ActionIDFairyGourdClaim 领取指定日期收益 (Module 220, Action 2 -> 0x00DC0002)
	ActionIDFairyGourdClaim uint32 = 0x00DC0002
)

// Mod_FairyGourd_Base 结果码。
const (
	// FairyGourdResultSuccess 领取成功
	FairyGourdResultSuccess uint8 = 0
	// FairyGourdResultTimeError 时间不符
	FairyGourdResultTimeError uint8 = 1
	// FairyGourdResultNoGrid 背包空间不足
	FairyGourdResultNoGrid uint8 = 2
	// FairyGourdResultAlreadyGet 已领取
	FairyGourdResultAlreadyGet uint8 = 3
	// FairyGourdResultFailure 领取失败
	FairyGourdResultFailure uint8 = 4
)

// FairyGourdDate 表示活动时间信息。
type FairyGourdDate struct {
	// StartTime 活动开始时间
	StartTime int32
	// EndTime 活动结束时间
	EndTime int32
	// GetTime 最近领取时间
	GetTime int32
}

// FairyGourdEntry 表示某一天的累积收益。
type FairyGourdEntry struct {
	// Coin 累积铜钱
	Coin int64
	// Exp 累积经验
	Exp int64
	// Date 收益日期标识
	Date int32
}

// BuildFairyGourdDatePacket 构造查询活动时间封包 (0x00DC0000)。
func BuildFairyGourdDatePacket() *Packet {
	return NewPacket(ActionIDFairyGourdDate, nil)
}

// BuildFairyGourdListPacket 构造查询收益列表封包 (0x00DC0001)。
func BuildFairyGourdListPacket() *Packet {
	return NewPacket(ActionIDFairyGourdList, nil)
}

// BuildFairyGourdClaimPacket 构造领取指定日期收益封包 (0x00DC0002)。
func BuildFairyGourdClaimPacket(date int32) *Packet {
	w := NewWriter()
	w.WriteInt32(date)
	return NewPacket(ActionIDFairyGourdClaim, w.Bytes())
}

// ParseFairyGourdDateResult 解析活动时间回包。
func ParseFairyGourdDateResult(payload []byte) (*FairyGourdDate, error) {
	r := NewReader(payload)
	res := &FairyGourdDate{}
	var err error
	if res.StartTime, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read fairy gourd start_time: %v", ErrInvalidPayload, err)
	}
	if res.EndTime, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read fairy gourd end_time: %v", ErrInvalidPayload, err)
	}
	if res.GetTime, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read fairy gourd get_time: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// ParseFairyGourdListResult 解析收益列表回包。
func ParseFairyGourdListResult(payload []byte) ([]FairyGourdEntry, error) {
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read fairy gourd count: %v", ErrInvalidPayload, err)
	}
	out := make([]FairyGourdEntry, 0, count)
	for i := 0; i < int(count); i++ {
		var e FairyGourdEntry
		if e.Coin, err = r.ReadInt64(); err != nil {
			return nil, fmt.Errorf("%w: failed to read fairy gourd[%d].coin: %v", ErrInvalidPayload, i, err)
		}
		if e.Exp, err = r.ReadInt64(); err != nil {
			return nil, fmt.Errorf("%w: failed to read fairy gourd[%d].exp: %v", ErrInvalidPayload, i, err)
		}
		if e.Date, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read fairy gourd[%d].date: %v", ErrInvalidPayload, i, err)
		}
		out = append(out, e)
	}
	return out, nil
}

// ParseFairyGourdClaimResult 解析领取结果 (单字节结果码)。
func ParseFairyGourdClaimResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty fairy gourd result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// BuildFairyGourdDateResultPacket 构造活动时间回包，供离线 mock 与单测使用。
func BuildFairyGourdDateResultPacket(d FairyGourdDate) *Packet {
	w := NewWriter()
	w.WriteInt32(d.StartTime)
	w.WriteInt32(d.EndTime)
	w.WriteInt32(d.GetTime)
	return NewPacket(ActionIDFairyGourdDate, w.Bytes())
}

// BuildFairyGourdListResultPacket 构造收益列表回包，供离线 mock 与单测使用。
func BuildFairyGourdListResultPacket(entries []FairyGourdEntry) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(entries)))
	for _, e := range entries {
		w.WriteInt64(e.Coin)
		w.WriteInt64(e.Exp)
		w.WriteInt32(e.Date)
	}
	return NewPacket(ActionIDFairyGourdList, w.Bytes())
}

// BuildFairyGourdClaimResultPacket 构造领取回包，供离线 mock 与单测使用。
func BuildFairyGourdClaimResultPacket(result uint8) *Packet {
	return NewPacket(ActionIDFairyGourdClaim, []byte{result})
}
