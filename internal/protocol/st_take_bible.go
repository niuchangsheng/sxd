// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方仙界跨服西天取经协议 (Mod_StTakeBible_Base, Module 114 / 0x0072)。
// 该模块仅在 Mod 94 登录的仙界跨服连接上可用，主服网关不会响应；
// 字段布局与消息码严格对齐官方 AS3 客户端反编译产物。
//
// @author Ateng
// @since 2026-11-02
package protocol

import (
	"fmt"
)

const (
	// ActionIDStTakeBibleCrossOpen 打开取经界面 (Module 114, Action 0 -> 0x00720000)
	ActionIDStTakeBibleCrossOpen uint32 = 0x00720000
	// ActionIDStTakeBibleCrossClose 关闭取经界面 (Module 114, Action 1 -> 0x00720001)
	ActionIDStTakeBibleCrossClose uint32 = 0x00720001
	// ActionIDStTakeBibleCrossGetInfo 查询取经准备详情 (Module 114, Action 2 -> 0x00720002)
	ActionIDStTakeBibleCrossGetInfo uint32 = 0x00720002
	// ActionIDStTakeBibleCrossRefresh 刷新取经使者品质 (Module 114, Action 8 -> 0x00720008)
	ActionIDStTakeBibleCrossRefresh uint32 = 0x00720008
	// ActionIDStTakeBibleCrossStart 正式启程开始护送 (Module 114, Action 10 -> 0x0072000A)
	ActionIDStTakeBibleCrossStart uint32 = 0x0072000A
	// ActionIDStTakeBibleCrossUpdateData 拉取护送进度增量数据 (Module 114, Action 15 -> 0x0072000F)
	ActionIDStTakeBibleCrossUpdateData uint32 = 0x0072000F
)

// Mod_StTakeBible_Base 消息码 (msg 字段)。
const (
	// StTakeBibleCrossMsgSuccess 操作成功
	StTakeBibleCrossMsgSuccess uint8 = 9
	// StTakeBibleCrossMsgTakeBibleStatus 当前已处于护送状态
	StTakeBibleCrossMsgTakeBibleStatus uint8 = 13
	// StTakeBibleCrossMsgNotEnoughIngot 元宝不足
	StTakeBibleCrossMsgNotEnoughIngot uint8 = 16
	// StTakeBibleCrossMsgFullTakeBibleTimes 今日护送取经次数已满
	StTakeBibleCrossMsgFullTakeBibleTimes uint8 = 18
	// StTakeBibleCrossMsgProtectTime 处于护送保护时间
	StTakeBibleCrossMsgProtectTime uint8 = 20
	// StTakeBibleCrossMsgFailed 操作失败
	StTakeBibleCrossMsgFailed uint8 = 38
)

// 取经使者品质 (protection / canProtection 字段)。
const (
	// StTakeBibleCrossProtectionBaiLongMa 白龙马 (最低收益)
	StTakeBibleCrossProtectionBaiLongMa uint8 = 1
	// StTakeBibleCrossProtectionShaWuJing 沙悟净
	StTakeBibleCrossProtectionShaWuJing uint8 = 2
	// StTakeBibleCrossProtectionZhuBaJie 猪八戒
	StTakeBibleCrossProtectionZhuBaJie uint8 = 3
	// StTakeBibleCrossProtectionSunWuKong 孙悟空
	StTakeBibleCrossProtectionSunWuKong uint8 = 4
	// StTakeBibleCrossProtectionTangSeng 唐僧 (最高收益)
	StTakeBibleCrossProtectionTangSeng uint8 = 5
)

// StTakeBibleCrossProtectionName 返回取经使者品质的中文名称。
func StTakeBibleCrossProtectionName(protection uint8) string {
	switch protection {
	case StTakeBibleCrossProtectionBaiLongMa:
		return "白龙马"
	case StTakeBibleCrossProtectionShaWuJing:
		return "沙悟净"
	case StTakeBibleCrossProtectionZhuBaJie:
		return "猪八戒"
	case StTakeBibleCrossProtectionSunWuKong:
		return "孙悟空"
	case StTakeBibleCrossProtectionTangSeng:
		return "唐僧"
	default:
		return "未知使者"
	}
}

// StTakeBibleCrossProtection 代表候选取经使者 (保护人) 的收益信息。
type StTakeBibleCrossProtection struct {
	// Protection 使者品质编号 (1=白龙马, 2=沙悟净, 3=猪八戒, 4=孙悟空, 5=唐僧)
	Protection uint8
	// TakeTime 护送耗时 (秒)
	TakeTime int8
	// AwardCoin 护送完成可获得的铜钱
	AwardCoin int32
	// AwardFame 护送完成可获得的声望
	AwardFame int16
	// AwardStExp 护送完成可获得的阅历
	AwardStExp int16
	// NpcLevel 使者等级
	NpcLevel int8
	// NpcExpe 使者经验
	NpcExpe int16
}

// StTakeBibleCrossProtectPlayer 代表可申请护送保护的好友信息。
type StTakeBibleCrossProtectPlayer struct {
	// PlayerID 好友角色 ID
	PlayerID int32
	// NickName 好友带区服后缀的完整名称
	NickName string
	// RemainProtectTimes 今日剩余保护次数
	RemainProtectTimes int8
	// TotalProtectTimes 今日总保护次数
	TotalProtectTimes int8
	// ProtectStatus 保护状态 (1=保护中)
	ProtectStatus int8
}

// StTakeBibleCrossInfo 代表取经准备详情 (get_take_bible_info 回包)。
type StTakeBibleCrossInfo struct {
	// ProtectionList 候选取经使者收益列表
	ProtectionList []StTakeBibleCrossProtection
	// ProtectPlayerList 可申请保护的好友列表
	ProtectPlayerList []StTakeBibleCrossProtectPlayer
	// TakeBibleTimes 今日已护送次数
	TakeBibleTimes uint8
	// TotalTakeBibleTimes 今日总护送次数上限
	TotalTakeBibleTimes int8
	// RefreshPrice 下次刷新使者所需元宝
	RefreshPrice int16
	// TakeBibleStatus 当前护送状态
	TakeBibleStatus int8
	// CanProtection 当前使者品质 (0 表示无需刷新)
	CanProtection uint8
	// ProtectPlayer 当前护送的发起玩家名称
	ProtectPlayer string
	// Bless 鼓舞祝福层数
	Bless int8
	// Buff 刷新产生的临时加成层数
	Buff int8
}

// BuildStTakeBibleCrossGetInfoPacket 构造查询仙界取经详情封包 (0x00720002)。
func BuildStTakeBibleCrossGetInfoPacket() *Packet {
	return NewPacket(ActionIDStTakeBibleCrossGetInfo, nil)
}

// BuildStTakeBibleCrossRefreshPacket 构造刷新仙界取经使者封包 (0x00720008)。
func BuildStTakeBibleCrossRefreshPacket() *Packet {
	return NewPacket(ActionIDStTakeBibleCrossRefresh, nil)
}

// BuildStTakeBibleCrossStartPacket 构造仙界取经正式启程封包 (0x0072000A)。
func BuildStTakeBibleCrossStartPacket() *Packet {
	return NewPacket(ActionIDStTakeBibleCrossStart, nil)
}

// ParseStTakeBibleCrossInfoResult 解析仙界取经详情回包。
//
// 线格式 (Mod_StTakeBible_Base.get_take_bible_info response):
//
//	[ [UByte protection, Byte takeTime, Int awardCoin, Short awardFame, Short awardStExp, Byte npcLevel, Short npcExpe] ],
//	[ [Int playerID, String nickName, Byte remainProtectTimes, Byte totalProtectTimes, Byte protectStatus] ],
//	UByte takeBibleTimes, Byte totalTakeBibleTimes, Short refreshPrice, Byte takeBibleStatus,
//	UByte canProtection, String protectPlayer, Byte bless, Byte buff
func ParseStTakeBibleCrossInfoResult(payload []byte) (*StTakeBibleCrossInfo, error) {
	r := NewReader(payload)
	res := &StTakeBibleCrossInfo{}

	protectionCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read protection count: %v", ErrInvalidPayload, err)
	}
	res.ProtectionList = make([]StTakeBibleCrossProtection, 0, protectionCount)
	for i := 0; i < int(protectionCount); i++ {
		var p StTakeBibleCrossProtection
		if p.Protection, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read protection[%d].protection: %v", ErrInvalidPayload, i, err)
		}
		if p.TakeTime, err = r.ReadInt8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read protection[%d].take_time: %v", ErrInvalidPayload, i, err)
		}
		if p.AwardCoin, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read protection[%d].award_coin: %v", ErrInvalidPayload, i, err)
		}
		if p.AwardFame, err = r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("%w: failed to read protection[%d].award_fame: %v", ErrInvalidPayload, i, err)
		}
		if p.AwardStExp, err = r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("%w: failed to read protection[%d].award_st_exp: %v", ErrInvalidPayload, i, err)
		}
		if p.NpcLevel, err = r.ReadInt8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read protection[%d].npc_level: %v", ErrInvalidPayload, i, err)
		}
		if p.NpcExpe, err = r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("%w: failed to read protection[%d].npc_expe: %v", ErrInvalidPayload, i, err)
		}
		res.ProtectionList = append(res.ProtectionList, p)
	}

	playerCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read protect player count: %v", ErrInvalidPayload, err)
	}
	res.ProtectPlayerList = make([]StTakeBibleCrossProtectPlayer, 0, playerCount)
	for i := 0; i < int(playerCount); i++ {
		var p StTakeBibleCrossProtectPlayer
		if p.PlayerID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read protect_player[%d].player_id: %v", ErrInvalidPayload, i, err)
		}
		if p.NickName, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: failed to read protect_player[%d].nick_name: %v", ErrInvalidPayload, i, err)
		}
		if p.RemainProtectTimes, err = r.ReadInt8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read protect_player[%d].remain_times: %v", ErrInvalidPayload, i, err)
		}
		if p.TotalProtectTimes, err = r.ReadInt8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read protect_player[%d].total_times: %v", ErrInvalidPayload, i, err)
		}
		if p.ProtectStatus, err = r.ReadInt8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read protect_player[%d].status: %v", ErrInvalidPayload, i, err)
		}
		res.ProtectPlayerList = append(res.ProtectPlayerList, p)
	}

	if res.TakeBibleTimes, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read take_bible_times: %v", ErrInvalidPayload, err)
	}
	if res.TotalTakeBibleTimes, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read total_take_bible_times: %v", ErrInvalidPayload, err)
	}
	if res.RefreshPrice, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read refresh_price: %v", ErrInvalidPayload, err)
	}
	if res.TakeBibleStatus, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read take_bible_status: %v", ErrInvalidPayload, err)
	}
	if res.CanProtection, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read can_protection: %v", ErrInvalidPayload, err)
	}
	if res.ProtectPlayer, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: failed to read protect_player: %v", ErrInvalidPayload, err)
	}
	if res.Bless, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read bless: %v", ErrInvalidPayload, err)
	}
	if res.Buff, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read buff: %v", ErrInvalidPayload, err)
	}

	return res, nil
}

// ParseStTakeBibleCrossStartResult 解析仙界取经启程回包 (0x0072000A)。
// 返回消息码与启程后获得的使者道具编号。
func ParseStTakeBibleCrossStartResult(payload []byte) (uint8, int8, error) {
	r := NewReader(payload)
	msg, err := r.ReadUint8()
	if err != nil {
		return 0, 0, fmt.Errorf("%w: failed to read start msg: %v", ErrInvalidPayload, err)
	}
	// item_id 仅成功时下发；部分节点失败回包只含消息码。
	itemID, err := r.ReadInt8()
	if err != nil {
		return msg, 0, nil
	}
	return msg, itemID, nil
}

// ParseStTakeBibleCrossRefreshResult 解析仙界取经使者刷新回包 (0x00720008)。
// 返回消息码、刷新后的使者品质与下次刷新价格。
func ParseStTakeBibleCrossRefreshResult(payload []byte) (uint8, uint8, int16, error) {
	r := NewReader(payload)
	msg, err := r.ReadUint8()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("%w: failed to read refresh msg: %v", ErrInvalidPayload, err)
	}
	protection, err := r.ReadUint8()
	if err != nil {
		return msg, 0, 0, fmt.Errorf("%w: failed to read refresh protection: %v", ErrInvalidPayload, err)
	}
	nextPrice, err := r.ReadInt16()
	if err != nil {
		return msg, protection, 0, fmt.Errorf("%w: failed to read refresh next_price: %v", ErrInvalidPayload, err)
	}
	return msg, protection, nextPrice, nil
}
