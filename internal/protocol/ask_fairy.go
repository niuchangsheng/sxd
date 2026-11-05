// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方问灵/回归福利协议 (Mod_AskFairy_Base, Module 503 / 0x1F7)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_AskFairy_Base 与
// com.datas.AskFairyData：每日可问灵一次，服务端下发随机奖励候选，
// 玩家选择一个领取；问灵等级较高时随机池包含元宝 (20/30)。
// 自动化只使用免费次数，不消耗元宝。
package protocol

import "fmt"

const (
	// ActionIDAskFairyInfo 查询问灵/回归信息 (Module 503, Action 0 -> 0x01F70000)
	ActionIDAskFairyInfo uint32 = 0x01F70000
	// ActionIDAskFairyRecallAward 领取回归奖励 (Module 503, Action 1 -> 0x01F70001)
	ActionIDAskFairyRecallAward uint32 = 0x01F70001
	// ActionIDAskFairyDo 执行问灵 (Module 503, Action 3 -> 0x01F70003)
	ActionIDAskFairyDo uint32 = 0x01F70003
	// ActionIDAskFairyGain 领取问灵奖励 (Module 503, Action 4 -> 0x01F70004)
	ActionIDAskFairyGain uint32 = 0x01F70004
	// ActionIDAskFairyLevelAward 领取问灵等级奖励 (Module 503, Action 7 -> 0x01F70007)
	ActionIDAskFairyLevelAward uint32 = 0x01F70007
)

// Mod_AskFairy_Base 结果码。
const (
	// AskFairyResultSuccess 成功
	AskFairyResultSuccess uint8 = 0
	// AskFairyResultPackFull 背包已满
	AskFairyResultPackFull uint8 = 1
	// AskFairyResultHasGainRecall 已领取回归奖励
	AskFairyResultHasGainRecall uint8 = 2
	// AskFairyResultFailed 失败
	AskFairyResultFailed uint8 = 3
	// AskFairyResultCountLimit 次数上限
	AskFairyResultCountLimit uint8 = 4
	// AskFairyResultHasAsk 今日已问灵
	AskFairyResultHasAsk uint8 = 5
	// AskFairyResultCannotChoose 不可选择该奖励
	AskFairyResultCannotChoose uint8 = 6
	// AskFairyResultLinkFatePackFull 命格背包已满
	AskFairyResultLinkFatePackFull uint8 = 7
	// AskFairyResultNotEnoughSign 签文不足
	AskFairyResultNotEnoughSign uint8 = 8
	// AskFairyResultNotEnoughDay 登录天数不足
	AskFairyResultNotEnoughDay uint8 = 9
	// AskFairyResultHasGain 已领取
	AskFairyResultHasGain uint8 = 10
)

// AskFairyRandomAward 表示问灵的一个随机奖励候选。
type AskFairyRandomAward struct {
	// AwardID 候选奖励编号 (领取时作为参数)
	AwardID int32
	// ItemID 道具编号 (3345 为元宝)
	ItemID int32
	// ItemCount 数量
	ItemCount int32
}

// AskFairyInfo 表示问灵/回归面板状态。
//
// 线格式: Int first_login_time, Int total_login_day, Int partner_num, Int level,
// Long fight_power, Int has_gain_award, Int is_shared, Int max_ask_num,
// Int has_ask_num, Int random_sign_id,
// [Int award_id, Int item_id, Int item_count] random_award,
// Int left_day, Int mount_count, Int title_count, Int cur_level。
type AskFairyInfo struct {
	// FirstLoginTime 首次登录时间
	FirstLoginTime int32
	// TotalLoginDay 累计登录天数
	TotalLoginDay int32
	// PartnerNum 伙伴数量
	PartnerNum int32
	// Level 角色等级
	Level int32
	// FightPower 战力
	FightPower int64
	// HasGainAward 是否已领取回归奖励
	HasGainAward int32
	// IsShared 是否已分享
	IsShared int32
	// MaxAskNum 问灵次数上限
	MaxAskNum int32
	// HasAskNum 已问灵次数
	HasAskNum int32
	// RandomSignID 随机签文编号
	RandomSignID int32
	// RandomAwards 本次问灵的随机奖励候选
	RandomAwards []AskFairyRandomAward
	// LeftDay 活动剩余天数
	LeftDay int32
	// MountCount 坐骑数量
	MountCount int32
	// TitleCount 称号数量
	TitleCount int32
	// CurLevel 问灵等级
	CurLevel int32
}

// BuildAskFairyInfoPacket 构造查询问灵状态封包 (0x01F70000)。
func BuildAskFairyInfoPacket() *Packet {
	return NewPacket(ActionIDAskFairyInfo, nil)
}

// BuildAskFairyRecallAwardPacket 构造领取回归奖励封包 (0x01F70001)。
func BuildAskFairyRecallAwardPacket() *Packet {
	return NewPacket(ActionIDAskFairyRecallAward, nil)
}

// BuildAskFairyDoPacket 构造执行问灵封包 (0x01F70003)。
func BuildAskFairyDoPacket() *Packet {
	return NewPacket(ActionIDAskFairyDo, nil)
}

// BuildAskFairyGainPacket 构造领取问灵奖励封包 (0x01F70004)。
func BuildAskFairyGainPacket(awardID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(awardID)
	return NewPacket(ActionIDAskFairyGain, w.Bytes())
}

// BuildAskFairyLevelAwardPacket 构造领取问灵等级奖励封包 (0x01F70007)。
func BuildAskFairyLevelAwardPacket() *Packet {
	return NewPacket(ActionIDAskFairyLevelAward, nil)
}

// ParseAskFairyInfoResult 解析问灵状态回包。
func ParseAskFairyInfoResult(payload []byte) (*AskFairyInfo, error) {
	r := NewReader(payload)
	res := &AskFairyInfo{}
	var err error
	if res.FirstLoginTime, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read ask_fairy first_login_time: %v", ErrInvalidPayload, err)
	}
	if res.TotalLoginDay, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read ask_fairy total_login_day: %v", ErrInvalidPayload, err)
	}
	if res.PartnerNum, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read ask_fairy partner_num: %v", ErrInvalidPayload, err)
	}
	if res.Level, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read ask_fairy level: %v", ErrInvalidPayload, err)
	}
	if res.FightPower, err = r.ReadInt64(); err != nil {
		return nil, fmt.Errorf("%w: failed to read ask_fairy fight_power: %v", ErrInvalidPayload, err)
	}
	if res.HasGainAward, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read ask_fairy has_gain_award: %v", ErrInvalidPayload, err)
	}
	if res.IsShared, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read ask_fairy is_shared: %v", ErrInvalidPayload, err)
	}
	if res.MaxAskNum, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read ask_fairy max_ask_num: %v", ErrInvalidPayload, err)
	}
	if res.HasAskNum, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read ask_fairy has_ask_num: %v", ErrInvalidPayload, err)
	}
	if res.RandomSignID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read ask_fairy random_sign_id: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read ask_fairy random_award count: %v", ErrInvalidPayload, err)
	}
	res.RandomAwards = make([]AskFairyRandomAward, 0, count)
	for i := 0; i < int(count); i++ {
		var a AskFairyRandomAward
		if a.AwardID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read ask_fairy random_award[%d].award_id: %v", ErrInvalidPayload, i, err)
		}
		if a.ItemID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read ask_fairy random_award[%d].item_id: %v", ErrInvalidPayload, i, err)
		}
		if a.ItemCount, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read ask_fairy random_award[%d].item_count: %v", ErrInvalidPayload, i, err)
		}
		res.RandomAwards = append(res.RandomAwards, a)
	}
	if res.LeftDay, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read ask_fairy left_day: %v", ErrInvalidPayload, err)
	}
	if res.MountCount, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read ask_fairy mount_count: %v", ErrInvalidPayload, err)
	}
	if res.TitleCount, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read ask_fairy title_count: %v", ErrInvalidPayload, err)
	}
	if res.CurLevel, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read ask_fairy cur_level: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// ParseAskFairyActionResult 解析问灵相关动作的结果码 (单字节)。
func ParseAskFairyActionResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty ask_fairy result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// BuildAskFairyInfoResultPacket 构造问灵状态回包，供离线 mock 与单测使用。
func BuildAskFairyInfoResultPacket(info AskFairyInfo) *Packet {
	w := NewWriter()
	w.WriteInt32(info.FirstLoginTime)
	w.WriteInt32(info.TotalLoginDay)
	w.WriteInt32(info.PartnerNum)
	w.WriteInt32(info.Level)
	w.WriteInt64(info.FightPower)
	w.WriteInt32(info.HasGainAward)
	w.WriteInt32(info.IsShared)
	w.WriteInt32(info.MaxAskNum)
	w.WriteInt32(info.HasAskNum)
	w.WriteInt32(info.RandomSignID)
	w.WriteUint16(uint16(len(info.RandomAwards)))
	for _, a := range info.RandomAwards {
		w.WriteInt32(a.AwardID)
		w.WriteInt32(a.ItemID)
		w.WriteInt32(a.ItemCount)
	}
	w.WriteInt32(info.LeftDay)
	w.WriteInt32(info.MountCount)
	w.WriteInt32(info.TitleCount)
	w.WriteInt32(info.CurLevel)
	return NewPacket(ActionIDAskFairyInfo, w.Bytes())
}

// BuildAskFairyActionResultPacket 构造问灵动作回包，供离线 mock 与单测使用。
func BuildAskFairyActionResultPacket(actionID uint32, result uint8) *Packet {
	return NewPacket(actionID, []byte{result})
}
