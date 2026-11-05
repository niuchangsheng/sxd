// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方在线奖励协议 (Mod_OnlineReward_Base, Module 712 / 0x2C8)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_OnlineReward_Base 与
// com.datas.OnlineRewardData：按当日在线时长解锁多档奖励，可一键领取；
// 最高档实机配置为元宝 ×100，且领取不消耗任何元宝。
package protocol

import "fmt"

const (
	// ActionIDOnlineRewardPanel 查询在线奖励面板 (Module 712, Action 0 -> 0x02C80000)
	ActionIDOnlineRewardPanel uint32 = 0x02C80000
	// ActionIDOnlineRewardOneKey 一键领取在线奖励 (Module 712, Action 1 -> 0x02C80001)
	ActionIDOnlineRewardOneKey uint32 = 0x02C80001
)

// Mod_OnlineReward_Base 结果码。
const (
	// OnlineRewardResultSuccess 领取成功
	OnlineRewardResultSuccess uint8 = 0
	// OnlineRewardResultFailed 领取失败
	OnlineRewardResultFailed uint8 = 1
	// OnlineRewardResultPackFull 背包已满
	OnlineRewardResultPackFull uint8 = 2
)

// OnlineRewardItem 表示在线奖励单档中的一项奖励。
type OnlineRewardItem struct {
	// ItemID 道具/资源编号 (3345 为元宝道具)
	ItemID int32
	// Num 数量
	Num int64
	// IsBigAward 是否为大奖
	IsBigAward uint8
}

// OnlineRewardTier 表示一个在线时长奖励档位。
type OnlineRewardTier struct {
	// AwardID 档位编号
	AwardID int32
	// Items 该档奖励内容
	Items []OnlineRewardItem
	// IsGet 是否已领取
	IsGet uint8
	// NeedOnlineTime 解锁所需在线时长 (秒)
	NeedOnlineTime int32
}

// OnlineRewardPanel 表示在线奖励面板回包。
type OnlineRewardPanel struct {
	// StartTime 活动开始时间
	StartTime int32
	// EndTime 活动结束时间
	EndTime int32
	// Day 活动天数
	Day uint8
	// Tiers 各档奖励
	Tiers []OnlineRewardTier
	// OnlineTime 今日已累计在线时长 (秒)
	OnlineTime int32
}

// BuildOnlineRewardPanelPacket 构造查询在线奖励面板封包 (0x02C80000)。
func BuildOnlineRewardPanelPacket() *Packet {
	return NewPacket(ActionIDOnlineRewardPanel, nil)
}

// BuildOnlineRewardOneKeyPacket 构造一键领取在线奖励封包 (0x02C80001)。
func BuildOnlineRewardOneKeyPacket() *Packet {
	return NewPacket(ActionIDOnlineRewardOneKey, nil)
}

// ParseOnlineRewardPanelResult 解析在线奖励面板回包。
func ParseOnlineRewardPanelResult(payload []byte) (*OnlineRewardPanel, error) {
	r := NewReader(payload)
	res := &OnlineRewardPanel{}
	var err error
	if res.StartTime, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read online reward start_time: %v", ErrInvalidPayload, err)
	}
	if res.EndTime, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read online reward end_time: %v", ErrInvalidPayload, err)
	}
	if res.Day, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read online reward day: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read online reward tier count: %v", ErrInvalidPayload, err)
	}
	res.Tiers = make([]OnlineRewardTier, 0, count)
	for i := 0; i < int(count); i++ {
		var tier OnlineRewardTier
		if tier.AwardID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read online reward tier[%d].award_id: %v", ErrInvalidPayload, i, err)
		}
		itemCount, err := r.ReadUint16()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read online reward tier[%d].item count: %v", ErrInvalidPayload, i, err)
		}
		tier.Items = make([]OnlineRewardItem, 0, itemCount)
		for j := 0; j < int(itemCount); j++ {
			var item OnlineRewardItem
			if item.ItemID, err = r.ReadInt32(); err != nil {
				return nil, fmt.Errorf("%w: failed to read online reward tier[%d].item[%d].item_id: %v", ErrInvalidPayload, i, j, err)
			}
			if item.Num, err = r.ReadInt64(); err != nil {
				return nil, fmt.Errorf("%w: failed to read online reward tier[%d].item[%d].num: %v", ErrInvalidPayload, i, j, err)
			}
			if item.IsBigAward, err = r.ReadUint8(); err != nil {
				return nil, fmt.Errorf("%w: failed to read online reward tier[%d].item[%d].is_big_award: %v", ErrInvalidPayload, i, j, err)
			}
			tier.Items = append(tier.Items, item)
		}
		if tier.IsGet, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read online reward tier[%d].is_get: %v", ErrInvalidPayload, i, err)
		}
		if tier.NeedOnlineTime, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read online reward tier[%d].need_online_time: %v", ErrInvalidPayload, i, err)
		}
		res.Tiers = append(res.Tiers, tier)
	}
	if res.OnlineTime, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read online reward online_time: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// ParseOnlineRewardOneKeyResult 解析一键领取结果: UByte result, [Int award_id]。
func ParseOnlineRewardOneKeyResult(payload []byte) (uint8, []int32, error) {
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return 0, nil, fmt.Errorf("%w: failed to read online reward result: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return result, nil, fmt.Errorf("%w: failed to read online reward award count: %v", ErrInvalidPayload, err)
	}
	awards := make([]int32, 0, count)
	for i := 0; i < int(count); i++ {
		id, err := r.ReadInt32()
		if err != nil {
			return result, nil, fmt.Errorf("%w: failed to read online reward award[%d]: %v", ErrInvalidPayload, i, err)
		}
		awards = append(awards, id)
	}
	return result, awards, nil
}
