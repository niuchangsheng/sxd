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
	// ActionIDStTakeBibleCrossGetPlayer 查询取经路上指定玩家队伍详情 (Module 114, Action 9 -> 0x00720009)
	ActionIDStTakeBibleCrossGetPlayer uint32 = 0x00720009
	// ActionIDStTakeBibleCrossRefresh 刷新取经使者品质 (Module 114, Action 8 -> 0x00720008)
	ActionIDStTakeBibleCrossRefresh uint32 = 0x00720008
	// ActionIDStTakeBibleCrossStart 正式启程开始护送 (Module 114, Action 10 -> 0x0072000A)
	ActionIDStTakeBibleCrossStart uint32 = 0x0072000A
	// ActionIDStTakeBibleCrossRob 发起取经拦截打劫 (Module 114, Action 11 -> 0x0072000B)
	ActionIDStTakeBibleCrossRob uint32 = 0x0072000B
	// ActionIDStTakeBibleCrossUpdateData 拉取护送进度增量数据 (Module 114, Action 15 -> 0x0072000F)
	ActionIDStTakeBibleCrossUpdateData uint32 = 0x0072000F
	// ActionIDStTakeBibleCrossGetRobCD 查询打劫冷却与清冷却元宝价格 (Module 114, Action 16 -> 0x00720010)
	ActionIDStTakeBibleCrossGetRobCD uint32 = 0x00720010
	// ActionIDStTakeBibleCrossGetRecentRobPlayer 查询最近被劫玩家列表 (Module 114, Action 19 -> 0x00720013)
	ActionIDStTakeBibleCrossGetRecentRobPlayer uint32 = 0x00720013
	// ActionIDStTakeBibleCrossBuyRobTimes 购买额外打劫次数 (Module 114, Action 22 -> 0x00720016)
	ActionIDStTakeBibleCrossBuyRobTimes uint32 = 0x00720016
)

// Mod_StTakeBible_Base 消息码 (msg 字段)。
const (
	// StTakeBibleCrossMsgSuccess 操作成功
	StTakeBibleCrossMsgSuccess uint8 = 9
	// StTakeBibleCrossMsgInvalidPlayerID 玩家编号无效
	StTakeBibleCrossMsgInvalidPlayerID uint8 = 10
	// StTakeBibleCrossMsgFullProtectTimes 护送好友次数已满
	StTakeBibleCrossMsgFullProtectTimes uint8 = 11
	// StTakeBibleCrossMsgNotOnline 目标玩家不在线
	StTakeBibleCrossMsgNotOnline uint8 = 12
	// StTakeBibleCrossMsgTakeBibleStatus 当前已处于护送状态
	StTakeBibleCrossMsgTakeBibleStatus uint8 = 13
	// StTakeBibleCrossMsgCancelApply 好友已取消护送申请
	StTakeBibleCrossMsgCancelApply uint8 = 14
	// StTakeBibleCrossMsgProtectStatus 目标玩家已经在护送中
	StTakeBibleCrossMsgProtectStatus uint8 = 15
	// StTakeBibleCrossMsgNotEnoughIngot 元宝不足
	StTakeBibleCrossMsgNotEnoughIngot uint8 = 16
	// StTakeBibleCrossMsgRequestVip VIP 等级不足
	StTakeBibleCrossMsgRequestVip uint8 = 17
	// StTakeBibleCrossMsgFullTakeBibleTimes 今日护送取经次数已满
	StTakeBibleCrossMsgFullTakeBibleTimes uint8 = 18
	// StTakeBibleCrossMsgRerob 已经打劫过该队伍
	StTakeBibleCrossMsgRerob uint8 = 19
	// StTakeBibleCrossMsgProtectTime 处于护送保护时间
	StTakeBibleCrossMsgProtectTime uint8 = 20
	// StTakeBibleCrossMsgFullRobTimes 今日打劫次数已满
	StTakeBibleCrossMsgFullRobTimes uint8 = 21
	// StTakeBibleCrossMsgHaveCDTime 打劫冷却中
	StTakeBibleCrossMsgHaveCDTime uint8 = 22
	// StTakeBibleCrossMsgBeRobAll 目标队伍已被抢光
	StTakeBibleCrossMsgBeRobAll uint8 = 23
	// StTakeBibleCrossMsgInvalidSequenceID 队伍序列号无效
	StTakeBibleCrossMsgInvalidSequenceID uint8 = 24
	// StTakeBibleCrossMsgFailed 操作失败
	StTakeBibleCrossMsgFailed uint8 = 38
)

// StTakeBibleCrossRobMsgName 返回打劫结果码的中文描述，便于日志定位。
func StTakeBibleCrossRobMsgName(msg uint8) string {
	switch msg {
	case StTakeBibleCrossMsgSuccess:
		return "打劫成功"
	case StTakeBibleCrossMsgInvalidPlayerID:
		return "玩家编号无效"
	case StTakeBibleCrossMsgNotOnline:
		return "目标不在线"
	case StTakeBibleCrossMsgProtectStatus:
		return "目标正在护送中"
	case StTakeBibleCrossMsgRerob:
		return "已经打劫过该队伍"
	case StTakeBibleCrossMsgProtectTime:
		return "目标处于保护时间"
	case StTakeBibleCrossMsgFullRobTimes:
		return "今日打劫次数已满"
	case StTakeBibleCrossMsgHaveCDTime:
		return "打劫冷却中"
	case StTakeBibleCrossMsgBeRobAll:
		return "目标队伍已被抢光"
	case StTakeBibleCrossMsgInvalidSequenceID:
		return "队伍序列号无效"
	case StTakeBibleCrossMsgTakeBibleStatus:
		return "当前处于护送状态"
	case StTakeBibleCrossMsgFailed:
		return "操作失败"
	default:
		return "未知结果"
	}
}

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
	// NpcExpe 使者经验。
	//
	// 官方 AS3 描述符写的是 Short，但实机仙界节点 (s0 跨服) 每个使者条目
	// 实际下发 15 字节，即该字段为 4 字节 Int；若按 Short 读取会导致后续
	// 使者条目与好友列表整体错位 (实测报 unexpected EOF)。
	NpcExpe int32
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

// StTakeBibleCrossRoadTeam 代表取经路上一支正在护送的队伍。
//
// 字段顺序严格对齐官方 AS3 Mod_StTakeBible_Base.open_take_bible 描述符：
// Int player_id, UByte protection, Int start_time, Int arrival_time,
// Short distance, Int protect_player_id, Byte sequence_id, Byte is_deeds_so。
type StTakeBibleCrossRoadTeam struct {
	// PlayerID 护送发起玩家角色 ID
	PlayerID int32
	// Protection 取经使者品质 (1=白龙马 .. 5=唐僧)
	Protection uint8
	// StartTime 队伍出发服务器时间戳
	StartTime int32
	// ArrivalTime 队伍预计到达服务器时间戳
	ArrivalTime int32
	// Distance 当前距离终点的进度值
	Distance int16
	// ProtectPlayerID 护送保护人角色 ID (0 表示无保护人)
	ProtectPlayerID int32
	// SequenceID 队伍序列号 (1/2，用于同玩家多队伍区分)
	SequenceID int8
	// IsDeedsSo 是否为取经功德队伍 (1=是)
	IsDeedsSo int8
}

// StTakeBibleCrossLastRob 代表最近一次被劫记录。
type StTakeBibleCrossLastRob struct {
	// SequenceID 被劫队伍序列号
	SequenceID int8
	// RemainRecaptureTime 剩余可反劫时间 (秒)
	RemainRecaptureTime int16
	// LastRobNickname 打劫者原始昵称
	LastRobNickname string
	// LastRobStageName 打劫者区服阶段名
	LastRobStageName string
	// LastRobServerName 打劫者区服全称
	LastRobServerName string
	// RobRoleID 打劫者角色 ID
	RobRoleID int16
}

// StTakeBibleCrossRoadInfo 代表取经路况快照 (open_take_bible 回包)。
type StTakeBibleCrossRoadInfo struct {
	// Teams 当前正在护送的全部队伍
	Teams []StTakeBibleCrossRoadTeam
	// ProtectPlayerID 当前指定的保护好友 ID
	ProtectPlayerID int32
	// CanRobTimes 今日剩余打劫次数
	CanRobTimes int16
	// CanProtectTimes 今日剩余保护次数
	CanProtectTimes int8
	// CanTakeBibleTimes 今日剩余护送次数
	CanTakeBibleTimes int8
	// QuicklyTimes 今日剩余加速次数
	QuicklyTimes int8
	// Bless 当前鼓舞祝福层数
	Bless int8
	// Type 如来活动状态 (6=关闭, 7=可召唤, 8=开启)
	Type uint8
	// Time 如来活动相关时间戳
	Time int32
	// LastRobs 最近被劫记录列表
	LastRobs []StTakeBibleCrossLastRob
	// Buff 刷新使者产生的临时加成层数
	Buff int8
}

// StTakeBibleCrossPlayerInfo 代表取经路上目标玩家队伍详情 (get_player_take_bible 回包)。
type StTakeBibleCrossPlayerInfo struct {
	// PlayerID 目标玩家角色 ID
	PlayerID int32
	// NickName 目标玩家原始昵称
	NickName string
	// StageName 目标玩家区服阶段名
	StageName string
	// ServerName 目标玩家区服全称
	ServerName string
	// Level 目标玩家等级
	Level int16
	// BeRobTimes 目标队伍今日已被打劫次数
	BeRobTimes int8
	// TotalCanRobTimes 目标队伍今日可被打破次数上限
	TotalCanRobTimes int8
	// ProtectNickname 目标队伍保护人昵称
	ProtectNickname string
	// RobCoin 打劫成功可获得的铜钱
	RobCoin int32
	// RobFame 打劫成功可获得的声望
	RobFame int16
	// RobStExp 打劫成功可获得的阅历
	RobStExp int16
	// AwardCoin 护送完成可获得铜钱
	AwardCoin int32
	// AwardFame 护送完成可获得声望
	AwardFame int16
	// AwardStExp 护送完成可获得阅历
	AwardStExp int16
	// IsRobbed 当前角色是否已经打劫过该队伍 (1=已打劫)
	IsRobbed int8
	// ProtectTime 目标队伍剩余保护时间 (秒)
	ProtectTime int16
	// NPCLevel 使者等级
	NPCLevel int8
	// RobExpe 使者当前经验
	RobExpe int16
	// AwardExpe 使者护送完成可获经验
	AwardExpe int16
	// SequenceID 队伍序列号
	SequenceID int8
}

// StTakeBibleCrossPlayerRef 代表战报中携带的玩家区服展示信息。
type StTakeBibleCrossPlayerRef struct {
	// PlayerID 玩家角色 ID
	PlayerID int32
	// NickName 玩家原始昵称
	NickName string
	// StageName 玩家区服阶段名
	StageName string
	// ServerName 玩家区服全称
	ServerName string
}

// StTakeBibleCrossRobResult 代表一次取经打劫的结果 (rob 回包前缀字段)。
//
// 回包后续仍包含完整战报数据 (warData)，业务层通常无需解析，
// 因此这里仅读取到战报编号为止。
type StTakeBibleCrossRobResult struct {
	// Msg 结果码 (9=成功)
	Msg uint8
	// StageServer 战报关联的玩家区服信息列表 (官方客户端取第 2 条作为被劫队伍)
	StageServer []StTakeBibleCrossPlayerRef
	// TargetPlayerID 被劫玩家角色 ID
	TargetPlayerID int32
	// TargetNickName 被劫玩家原始昵称
	TargetNickName string
	// TargetStageName 被劫玩家区服阶段名
	TargetStageName string
	// TargetServerName 被劫玩家区服全称
	TargetServerName string
	// RobCoin 本次打劫获得铜钱
	RobCoin int32
	// RobFame 本次打劫获得声望
	RobFame int16
	// RobStExp 本次打劫获得阅历
	RobStExp int16
	// WarReportID 战报编号
	WarReportID int32
	// DeployType 战报单元布阵类型 (官方 WarData 与竞技场共用结构)
	DeployType uint8
	// WinPlayerID 战报胜方玩家 ID (等于本方 PlayerID 表示打劫获胜)
	WinPlayerID int32
	// WarParsed 是否成功解析出战报胜负位；节点仅回传前缀时为 false
	WarParsed bool
}

// IsWin 判断本次打劫战斗中 playerID 一方是否获胜。
//
// rob 回包的 msg=SUCCESS 只代表服务端受理并完成了战斗计算，并不代表打劫获胜；
// 真正的胜负由战报单元第 2 个字段 winPlayerID 决定。失败时服务端下发
// rob_coin/rob_fame/rob_st_exp 均为 0，且不会扣除每日打劫次数。
func (r *StTakeBibleCrossRobResult) IsWin(playerID int32) bool {
	return r != nil && r.Msg == StTakeBibleCrossMsgSuccess && r.WarParsed && r.WinPlayerID == playerID
}

// StTakeBibleCrossUpdateData 代表护送/打劫剩余次数增量同步 (0x0072000F)。
type StTakeBibleCrossUpdateData struct {
	// CanRobTimes 今日剩余打劫次数
	CanRobTimes int16
	// CanTakeBibleTimes 今日剩余护送次数
	CanTakeBibleTimes uint8
}

// StTakeBibleCrossRobCD 代表打劫冷却信息 (get_rob_take_bible_cd_time 回包)。
type StTakeBibleCrossRobCD struct {
	// CDSeconds 剩余冷却秒数 (0 表示可立即打劫)
	CDSeconds int16
	// ClearIngot 清除冷却所需元宝
	ClearIngot int16
}

// BuildStTakeBibleCrossOpenPacket 构造打开仙界取经路况面板封包 (0x00720000)。
func BuildStTakeBibleCrossOpenPacket() *Packet {
	return NewPacket(ActionIDStTakeBibleCrossOpen, nil)
}

// BuildStTakeBibleCrossClosePacket 构造关闭仙界取经路况面板封包 (0x00720001)。
func BuildStTakeBibleCrossClosePacket() *Packet {
	return NewPacket(ActionIDStTakeBibleCrossClose, nil)
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

// BuildStTakeBibleCrossGetPlayerPacket 构造查询取经路上指定玩家队伍详情封包 (0x00720009)。
//
// 请求线格式对齐官方 AS3 描述符：Int player_id, Short level, Byte type, Byte sequence_id。
// type 为 1 表示角色已开启圣域/仙界城镇，普通角色传 0。
func BuildStTakeBibleCrossGetPlayerPacket(playerID int32, level int16, playerType uint8, sequenceID int8) *Packet {
	w := NewWriter()
	w.WriteInt32(playerID)
	w.WriteInt16(level)
	w.WriteUint8(playerType)
	w.WriteInt8(sequenceID)
	return NewPacket(ActionIDStTakeBibleCrossGetPlayer, w.Bytes())
}

// BuildStTakeBibleCrossRobPacket 构造取经拦截打劫封包 (0x0072000B)。
//
// 请求线格式：Int player_id, Byte sequence_id。
func BuildStTakeBibleCrossRobPacket(playerID int32, sequenceID int8) *Packet {
	w := NewWriter()
	w.WriteInt32(playerID)
	w.WriteInt8(sequenceID)
	return NewPacket(ActionIDStTakeBibleCrossRob, w.Bytes())
}

// BuildStTakeBibleCrossGetRobCDPacket 构造查询打劫冷却封包 (0x00720010)。
func BuildStTakeBibleCrossGetRobCDPacket() *Packet {
	return NewPacket(ActionIDStTakeBibleCrossGetRobCD, nil)
}

// BuildStTakeBibleCrossUpdateDataPacket 构造查询剩余护送/打劫次数封包 (0x0072000F)。
//
// 官方客户端在每次 rob 回包后都会调用该协议刷新面板上的剩余次数。
func BuildStTakeBibleCrossUpdateDataPacket() *Packet {
	return NewPacket(ActionIDStTakeBibleCrossUpdateData, nil)
}

// BuildStTakeBibleCrossGetRecentRobPlayerPacket 构造查询最近被劫玩家封包 (0x00720013)。
func BuildStTakeBibleCrossGetRecentRobPlayerPacket() *Packet {
	return NewPacket(ActionIDStTakeBibleCrossGetRecentRobPlayer, nil)
}

// ParseStTakeBibleCrossInfoResult 解析仙界取经详情回包。
//
// 线格式 (Mod_StTakeBible_Base.get_take_bible_info response):
//
//	[ [UByte protection, Byte takeTime, Int awardCoin, Short awardFame, Short awardStExp, Byte npcLevel, Int npcExpe] ],
//	[ [Int playerID, String nickName, Byte remainProtectTimes, Byte totalProtectTimes, Byte protectStatus] ],
//
// 注意：AS3 描述符将 npcExpe 声明为 Short，但实机仙界节点按 Int (4 字节) 下发，
// 每个使者条目共 15 字节；此处以实机回包为准。
//
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
		if p.NpcExpe, err = r.ReadInt32(); err != nil {
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

// ParseStTakeBibleCrossRoadResult 解析仙界取经路况快照回包 (0x00720000)。
//
// 线格式 (Mod_StTakeBible_Base.open_take_bible response):
//
//	[ [Int playerID, UByte protection, Int startTime, Int arrivalTime, Short distance,
//	   Int protectPlayerID, Byte sequenceID, Byte isDeedsSo] ],
//	Int protectPlayerID, Short canRobTimes, Byte canProtectTimes, Byte canTakeBibleTimes,
//	Byte quicklyTimes, Byte bless, UByte type, Int time,
//	[ [Byte sequenceID, Short remainRecaptureTime, String lastRobNickname,
//	   String lastRobStageName, String lastRobServerName, Short robRoleID] ],
//	Byte buff
func ParseStTakeBibleCrossRoadResult(payload []byte) (*StTakeBibleCrossRoadInfo, error) {
	r := NewReader(payload)
	res := &StTakeBibleCrossRoadInfo{}

	teamCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read road team count: %v", ErrInvalidPayload, err)
	}
	res.Teams = make([]StTakeBibleCrossRoadTeam, 0, teamCount)
	for i := 0; i < int(teamCount); i++ {
		var t StTakeBibleCrossRoadTeam
		if t.PlayerID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read road_team[%d].player_id: %v", ErrInvalidPayload, i, err)
		}
		if t.Protection, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read road_team[%d].protection: %v", ErrInvalidPayload, i, err)
		}
		if t.StartTime, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read road_team[%d].start_time: %v", ErrInvalidPayload, i, err)
		}
		if t.ArrivalTime, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read road_team[%d].arrival_time: %v", ErrInvalidPayload, i, err)
		}
		if t.Distance, err = r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("%w: failed to read road_team[%d].distance: %v", ErrInvalidPayload, i, err)
		}
		if t.ProtectPlayerID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read road_team[%d].protect_player_id: %v", ErrInvalidPayload, i, err)
		}
		if t.SequenceID, err = r.ReadInt8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read road_team[%d].sequence_id: %v", ErrInvalidPayload, i, err)
		}
		if t.IsDeedsSo, err = r.ReadInt8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read road_team[%d].is_deeds_so: %v", ErrInvalidPayload, i, err)
		}
		res.Teams = append(res.Teams, t)
	}

	if res.ProtectPlayerID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read road protect_player_id: %v", ErrInvalidPayload, err)
	}
	if res.CanRobTimes, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read road can_rob_times: %v", ErrInvalidPayload, err)
	}
	if res.CanProtectTimes, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read road can_protect_times: %v", ErrInvalidPayload, err)
	}
	if res.CanTakeBibleTimes, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read road can_take_bible_times: %v", ErrInvalidPayload, err)
	}
	if res.QuicklyTimes, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read road quickly_times: %v", ErrInvalidPayload, err)
	}
	if res.Bless, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read road bless: %v", ErrInvalidPayload, err)
	}
	if res.Type, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read road type: %v", ErrInvalidPayload, err)
	}
	if res.Time, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read road time: %v", ErrInvalidPayload, err)
	}

	lastRobCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read last rob count: %v", ErrInvalidPayload, err)
	}
	res.LastRobs = make([]StTakeBibleCrossLastRob, 0, lastRobCount)
	for i := 0; i < int(lastRobCount); i++ {
		var l StTakeBibleCrossLastRob
		if l.SequenceID, err = r.ReadInt8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read last_rob[%d].sequence_id: %v", ErrInvalidPayload, i, err)
		}
		if l.RemainRecaptureTime, err = r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("%w: failed to read last_rob[%d].remain_recapture_time: %v", ErrInvalidPayload, i, err)
		}
		if l.LastRobNickname, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: failed to read last_rob[%d].last_rob_nickname: %v", ErrInvalidPayload, i, err)
		}
		if l.LastRobStageName, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: failed to read last_rob[%d].last_rob_stage_name: %v", ErrInvalidPayload, i, err)
		}
		if l.LastRobServerName, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: failed to read last_rob[%d].last_rob_server_name: %v", ErrInvalidPayload, i, err)
		}
		if l.RobRoleID, err = r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("%w: failed to read last_rob[%d].rob_role_id: %v", ErrInvalidPayload, i, err)
		}
		res.LastRobs = append(res.LastRobs, l)
	}

	if res.Buff, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read road buff: %v", ErrInvalidPayload, err)
	}

	return res, nil
}

// ParseStTakeBibleCrossPlayerInfoResult 解析仙界取经路上目标队伍详情回包 (0x00720009)。
func ParseStTakeBibleCrossPlayerInfoResult(payload []byte) (*StTakeBibleCrossPlayerInfo, error) {
	r := NewReader(payload)
	res := &StTakeBibleCrossPlayerInfo{}

	var err error
	if res.PlayerID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read player_info.player_id: %v", ErrInvalidPayload, err)
	}
	if res.NickName, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: failed to read player_info.nick_name: %v", ErrInvalidPayload, err)
	}
	if res.StageName, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: failed to read player_info.stage_name: %v", ErrInvalidPayload, err)
	}
	if res.ServerName, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: failed to read player_info.server_name: %v", ErrInvalidPayload, err)
	}
	if res.Level, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read player_info.level: %v", ErrInvalidPayload, err)
	}
	if res.BeRobTimes, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read player_info.be_rob_times: %v", ErrInvalidPayload, err)
	}
	if res.TotalCanRobTimes, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read player_info.total_can_rob_times: %v", ErrInvalidPayload, err)
	}
	if res.ProtectNickname, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: failed to read player_info.protect_nickname: %v", ErrInvalidPayload, err)
	}
	if res.RobCoin, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read player_info.rob_coin: %v", ErrInvalidPayload, err)
	}
	if res.RobFame, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read player_info.rob_fame: %v", ErrInvalidPayload, err)
	}
	if res.RobStExp, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read player_info.rob_st_exp: %v", ErrInvalidPayload, err)
	}
	if res.AwardCoin, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read player_info.award_coin: %v", ErrInvalidPayload, err)
	}
	if res.AwardFame, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read player_info.award_fame: %v", ErrInvalidPayload, err)
	}
	if res.AwardStExp, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read player_info.award_st_exp: %v", ErrInvalidPayload, err)
	}
	if res.IsRobbed, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read player_info.is_robbed: %v", ErrInvalidPayload, err)
	}
	if res.ProtectTime, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read player_info.protect_time: %v", ErrInvalidPayload, err)
	}
	if res.NPCLevel, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read player_info.npc_level: %v", ErrInvalidPayload, err)
	}
	if res.RobExpe, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read player_info.rob_expe: %v", ErrInvalidPayload, err)
	}
	if res.AwardExpe, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read player_info.award_expe: %v", ErrInvalidPayload, err)
	}
	if res.SequenceID, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read player_info.sequence_id: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// ParseStTakeBibleCrossRobResult 解析仙界取经打劫回包 (0x0072000B) 的前缀字段。
//
// 回包完整结构在战报编号之后仍包含战报数据，业务侧通常无需消费；
// 这里只读取结果码、目标信息与奖励，避免解析大段战报造成不必要的耦合。
func ParseStTakeBibleCrossRobResult(payload []byte) (*StTakeBibleCrossRobResult, error) {
	r := NewReader(payload)
	res := &StTakeBibleCrossRobResult{}

	msg, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read rob msg: %v", ErrInvalidPayload, err)
	}
	res.Msg = msg
	// 部分节点在失败时只回传结果码，不再携带战报与奖励字段。
	if r.Remaining() == 0 {
		return res, nil
	}

	refCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read rob stage_server count: %v", ErrInvalidPayload, err)
	}
	res.StageServer = make([]StTakeBibleCrossPlayerRef, 0, refCount)
	for i := 0; i < int(refCount); i++ {
		var ref StTakeBibleCrossPlayerRef
		if ref.PlayerID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read rob stage_server[%d].player_id: %v", ErrInvalidPayload, i, err)
		}
		if ref.NickName, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: failed to read rob stage_server[%d].nick_name: %v", ErrInvalidPayload, i, err)
		}
		if ref.StageName, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: failed to read rob stage_server[%d].stage_name: %v", ErrInvalidPayload, i, err)
		}
		if ref.ServerName, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: failed to read rob stage_server[%d].server_name: %v", ErrInvalidPayload, i, err)
		}
		res.StageServer = append(res.StageServer, ref)
	}
	// 官方客户端以 stageServer[1] 作为被劫队伍展示信息；仅 1 条时降级取首条。
	if len(res.StageServer) > 1 {
		ref := res.StageServer[1]
		res.TargetPlayerID, res.TargetNickName = ref.PlayerID, ref.NickName
		res.TargetStageName, res.TargetServerName = ref.StageName, ref.ServerName
	} else if len(res.StageServer) == 1 {
		ref := res.StageServer[0]
		res.TargetPlayerID, res.TargetNickName = ref.PlayerID, ref.NickName
		res.TargetStageName, res.TargetServerName = ref.StageName, ref.ServerName
	}
	if res.RobCoin, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read rob coin: %v", ErrInvalidPayload, err)
	}
	if res.RobFame, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read rob fame: %v", ErrInvalidPayload, err)
	}
	if res.RobStExp, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read rob st_exp: %v", ErrInvalidPayload, err)
	}
	if res.WarReportID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read rob war_report_id: %v", ErrInvalidPayload, err)
	}

	// 战报列表 (官方 WarData 与仙界竞技场共用结构)：Uint16 单元数，
	// 每个单元前缀为 UByte deploy_type, Int win_player_id。
	// 胜负位用于判定打劫是否真正成功；战报体可达 8KB，后续内容业务侧无需消费，
	// 因此仅读取首单元前缀，解析失败也不影响回包前缀字段的语义。
	if r.Remaining() >= 2 {
		warCount, wErr := r.ReadUint16()
		if wErr == nil && warCount > 0 && r.Remaining() >= 5 {
			deployType, dErr := r.ReadUint8()
			winPlayerID, pErr := r.ReadInt32()
			if dErr == nil && pErr == nil {
				res.DeployType, res.WinPlayerID, res.WarParsed = deployType, winPlayerID, true
			}
		}
	}

	return res, nil
}

// ParseStTakeBibleCrossUpdateDataResult 解析剩余护送/打劫次数回包 (0x0072000F)。
func ParseStTakeBibleCrossUpdateDataResult(payload []byte) (*StTakeBibleCrossUpdateData, error) {
	r := NewReader(payload)
	res := &StTakeBibleCrossUpdateData{}

	var err error
	if res.CanRobTimes, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read take bible update can_rob_times: %v", ErrInvalidPayload, err)
	}
	if res.CanTakeBibleTimes, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read take bible update can_take_bible_times: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// ParseStTakeBibleCrossRobCDResult 解析仙界取经打劫冷却回包 (0x00720010)。
func ParseStTakeBibleCrossRobCDResult(payload []byte) (*StTakeBibleCrossRobCD, error) {
	r := NewReader(payload)
	res := &StTakeBibleCrossRobCD{}

	var err error
	if res.CDSeconds, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read rob cd_seconds: %v", ErrInvalidPayload, err)
	}
	if res.ClearIngot, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read rob clear_ingot: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// ParseStTakeBibleCrossRecentRobResult 解析仙界取经最近被劫玩家列表回包 (0x00720013)。
//
// 线格式：Short count, count × Int player_id。
func ParseStTakeBibleCrossRecentRobResult(payload []byte) ([]int32, error) {
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read recent_rob count: %v", ErrInvalidPayload, err)
	}
	players := make([]int32, 0, count)
	for i := 0; i < int(count); i++ {
		playerID, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read recent_rob[%d].player_id: %v", ErrInvalidPayload, i, err)
		}
		players = append(players, playerID)
	}
	return players, nil
}
