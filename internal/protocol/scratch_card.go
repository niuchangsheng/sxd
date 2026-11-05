// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方幸运刮刮卡协议 (Mod_ScratchCard_Base, Module 185 / 0xB9)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_ScratchCard_Base 与
// com.datas.ScratchCardData：按每日刮卡次数刮卡，奖励可能是积分、元宝、
// 形象卡或普通道具。自动化只消耗免费刮卡次数，不消耗元宝。
package protocol

import "fmt"

const (
	// ActionIDScratchCardInfo 查询刮刮卡活动状态 (Module 185, Action 0 -> 0x00B90000)
	ActionIDScratchCardInfo uint32 = 0x00B90000
	// ActionIDScratchCardShave 执行一次刮卡 (Module 185, Action 1 -> 0x00B90001)
	ActionIDScratchCardShave uint32 = 0x00B90001
)

// Mod_ScratchCard_Base 结果码。
const (
	// ScratchCardResultSuccess 刮卡成功 (普通奖励)
	ScratchCardResultSuccess uint8 = 2
	// ScratchCardResultNotEnoughTimes 刮卡次数不足
	ScratchCardResultNotEnoughTimes uint8 = 3
	// ScratchCardResultPackFull 背包已满
	ScratchCardResultPackFull uint8 = 4
	// ScratchCardResultFailed 刮卡失败
	ScratchCardResultFailed uint8 = 5
	// ScratchCardResultPoint 获得积分
	ScratchCardResultPoint uint8 = 6
	// ScratchCardResultIngot 获得元宝
	ScratchCardResultIngot uint8 = 7
	// ScratchCardResultImage 获得形象卡
	ScratchCardResultImage uint8 = 8
	// ScratchCardResultAward 获得奖励
	ScratchCardResultAward uint8 = 9
	// ScratchCardResultItemPackFull 物品背包已满
	ScratchCardResultItemPackFull uint8 = 12
	// ScratchCardResultFatePackFull 命格背包已满
	ScratchCardResultFatePackFull uint8 = 13
	// ScratchCardResultNotExistCard 刮卡道具不存在
	ScratchCardResultNotExistCard uint8 = 14
)

// ScratchCardInfo 表示刮刮卡活动状态。
//
// 线格式: UByte is_in_activity, Int end_time, Short active_degree, Byte card_times。
type ScratchCardInfo struct {
	// InActivity 是否在活动期内
	InActivity uint8
	// EndTime 活动结束时间 (0 表示无固定结束时间)
	EndTime int32
	// ActiveDegree 今日活跃度
	ActiveDegree int16
	// CardTimes 剩余免费刮卡次数
	CardTimes uint8
}

// ScratchCardShaveResult 表示一次刮卡结果。
type ScratchCardShaveResult struct {
	// Result 结果码
	Result uint8
	// ItemID 奖励道具编号 (元宝为 3345)
	ItemID int32
}

// BuildScratchCardInfoPacket 构造查询刮刮卡状态封包 (0x00B90000)。
func BuildScratchCardInfoPacket() *Packet {
	return NewPacket(ActionIDScratchCardInfo, nil)
}

// BuildScratchCardShavePacket 构造执行刮卡封包 (0x00B90001)。
func BuildScratchCardShavePacket() *Packet {
	return NewPacket(ActionIDScratchCardShave, nil)
}

// ParseScratchCardInfoResult 解析刮刮卡状态回包。
func ParseScratchCardInfoResult(payload []byte) (*ScratchCardInfo, error) {
	r := NewReader(payload)
	res := &ScratchCardInfo{}
	var err error
	if res.InActivity, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read scratch is_in_activity: %v", ErrInvalidPayload, err)
	}
	if res.EndTime, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read scratch end_time: %v", ErrInvalidPayload, err)
	}
	if res.ActiveDegree, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read scratch active_degree: %v", ErrInvalidPayload, err)
	}
	if res.CardTimes, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read scratch card_times: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// ParseScratchCardShaveResult 解析刮卡回包: UByte result, Int item_id。
func ParseScratchCardShaveResult(payload []byte) (*ScratchCardShaveResult, error) {
	r := NewReader(payload)
	res := &ScratchCardShaveResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read scratch result: %v", ErrInvalidPayload, err)
	}
	if res.ItemID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read scratch item_id: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// BuildScratchCardInfoResultPacket 构造刮刮卡状态回包，供离线 mock 与单测使用。
func BuildScratchCardInfoResultPacket(info ScratchCardInfo) *Packet {
	w := NewWriter()
	w.WriteUint8(info.InActivity)
	w.WriteInt32(info.EndTime)
	w.WriteInt16(info.ActiveDegree)
	w.WriteUint8(info.CardTimes)
	return NewPacket(ActionIDScratchCardInfo, w.Bytes())
}

// BuildScratchCardShaveResultPacket 构造刮卡回包，供离线 mock 与单测使用。
func BuildScratchCardShaveResultPacket(result uint8, itemID int32) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	w.WriteInt32(itemID)
	return NewPacket(ActionIDScratchCardShave, w.Bytes())
}
