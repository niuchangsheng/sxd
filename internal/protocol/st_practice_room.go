// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现仙界练功房协议 (Mod_StPracticeRoom_Base, Module 117 / 0x0075)。
// 该模块仅在 Mod 94 登录的仙界跨服连接上可用；主服网关不会响应。
// 字段布局与消息码严格对齐官方 2026 客户端 Main.swf 的
// com.protocols.Mod_StPracticeRoom_Base 描述符。
//
// @author Ateng
// @since 2026-11-04
package protocol

import (
	"fmt"
)

const (
	// ModuleStPracticeRoom 仙界练功房跨服模块编号 (0x0075)
	ModuleStPracticeRoom uint16 = 117

	// StPracticeRoomFreeQuality 普通练功场 (自由练功区) 的房间品质编号。
	//
	// 星级房间品质为 1..5，数值越大经验加成越高；品质 100 为不限制席位的
	// 普通练功场。官方客户端常量见 com.assist.server.StPracticeRoomType.freeQuality。
	StPracticeRoomFreeQuality uint8 = 100
)

// 仙界练功房消息码 (Mod_StPracticeRoom_Base)。
const (
	// ActionIDStPracticeRoomGetRooms 查询练功房房间列表 (Action 0 -> 0x00750000)
	ActionIDStPracticeRoomGetRooms uint32 = 0x00750000
	// ActionIDStPracticeRoomGetRoomInfo 查询指定品质房间的席位与经验状态 (Action 1 -> 0x00750001)
	ActionIDStPracticeRoomGetRoomInfo uint32 = 0x00750001
	// ActionIDStPracticeRoomSitDown 坐上空闲席位开始练功 (Action 2 -> 0x00750002)
	ActionIDStPracticeRoomSitDown uint32 = 0x00750002
	// ActionIDStPracticeRoomGetSeatInfo 查询指定席位是否有人 (Action 3 -> 0x00750003)
	ActionIDStPracticeRoomGetSeatInfo uint32 = 0x00750003
	// ActionIDStPracticeRoomRobSeat 发起席位争夺战 (Action 4 -> 0x00750004)
	ActionIDStPracticeRoomRobSeat uint32 = 0x00750004
	// ActionIDStPracticeRoomBuyRobTime 购买额外争夺次数 (Action 5 -> 0x00750005)
	ActionIDStPracticeRoomBuyRobTime uint32 = 0x00750005
	// ActionIDStPracticeRoomGetPracticeExp 领取练功累积经验 (Action 6 -> 0x00750006)
	ActionIDStPracticeRoomGetPracticeExp uint32 = 0x00750006
	// ActionIDStPracticeRoomGetExpFlag 查询是否存在未领取经验 (Action 7 -> 0x00750007)
	ActionIDStPracticeRoomGetExpFlag uint32 = 0x00750007
	// ActionIDStPracticeRoomEnterRoom 进入练功房面板 (Action 8 -> 0x00750008)
	ActionIDStPracticeRoomEnterRoom uint32 = 0x00750008
	// ActionIDStPracticeRoomExitRoom 退出练功房面板 (Action 9 -> 0x00750009)
	ActionIDStPracticeRoomExitRoom uint32 = 0x00750009
	// ActionIDStPracticeRoomCleanRobCDTime 消耗元宝清除争夺冷却 (Action 10 -> 0x0075000A)
	ActionIDStPracticeRoomCleanRobCDTime uint32 = 0x0075000A
	// ActionIDStPracticeRoomGetRobCDTime 查询争夺冷却与清冷却元宝价格 (Action 11 -> 0x0075000B)
	ActionIDStPracticeRoomGetRobCDTime uint32 = 0x0075000B
	// ActionIDStPracticeRoomNotifyRefreshExp 服务端推送练功经验刷新 (Action 12 -> 0x0075000C)
	ActionIDStPracticeRoomNotifyRefreshExp uint32 = 0x0075000C
	// ActionIDStPracticeRoomPlayerLeaveSeat 主动离开当前席位 (Action 13 -> 0x0075000D)
	ActionIDStPracticeRoomPlayerLeaveSeat uint32 = 0x0075000D
	// ActionIDStPracticeRoomGetPlayerRoomQuality 查询玩家当前所在房间品质 (Action 14 -> 0x0075000E)
	ActionIDStPracticeRoomGetPlayerRoomQuality uint32 = 0x0075000E
	// ActionIDStPracticeRoomNotifyPracticeRoomMsg 练功房状态变更通知 (Action 15 -> 0x0075000F)
	ActionIDStPracticeRoomNotifyPracticeRoomMsg uint32 = 0x0075000F
	// ActionIDStPracticeRoomGetFreeRoomInfo 查询普通练功场分页与经验状态 (Action 16 -> 0x00750010)
	ActionIDStPracticeRoomGetFreeRoomInfo uint32 = 0x00750010
	// ActionIDStPracticeRoomGetFreeRoomInfoByPage 查询普通练功场指定页席位 (Action 17 -> 0x00750011)
	ActionIDStPracticeRoomGetFreeRoomInfoByPage uint32 = 0x00750011
)

// 仙界练功房业务结果码 (Mod_StPracticeRoom_Base)。
//
// 注意该模块常量并非从 0 开始：FALSE/YES/NO 同时被查询类回包复用。
// 例如 get_exp_flag 返回 YES(2) 表示有未领取经验，NO(3) 表示没有。
const (
	// StPracticeRoomTrue 布尔真 (0)
	StPracticeRoomTrue uint8 = 0
	// StPracticeRoomFalse 布尔假 (1)
	StPracticeRoomFalse uint8 = 1
	// StPracticeRoomYes 是 / 有未领取经验 (2)
	StPracticeRoomYes uint8 = 2
	// StPracticeRoomNo 否 / 无未领取经验 (3)
	StPracticeRoomNo uint8 = 3
	// StPracticeRoomSuccess 操作成功 (4)
	StPracticeRoomSuccess uint8 = 4
	// StPracticeRoomRoomSeatError 房间或席位编号错误 (5)
	StPracticeRoomRoomSeatError uint8 = 5
	// StPracticeRoomAlreadyInUsed 席位已被占用 (6)
	StPracticeRoomAlreadyInUsed uint8 = 6
	// StPracticeRoomExpNotGotten 尚有未领取经验, 不能进行该操作 (7)
	StPracticeRoomExpNotGotten uint8 = 7
	// StPracticeRoomFunctionNoOpen 功能未开放 (8)
	StPracticeRoomFunctionNoOpen uint8 = 8
	// StPracticeRoomAlreadySatDown 已经在练功状态, 不能重复入座 (9)
	StPracticeRoomAlreadySatDown uint8 = 9
	// StPracticeRoomSomebody 席位已有人 (10)
	StPracticeRoomSomebody uint8 = 10
	// StPracticeRoomNobody 席位为空 (11)
	StPracticeRoomNobody uint8 = 11
	// StPracticeRoomRobTimeUsedUp 今日争夺次数已用完 (12)
	StPracticeRoomRobTimeUsedUp uint8 = 12
	// StPracticeRoomCDTime 争夺冷却时间未到 (13)
	StPracticeRoomCDTime uint8 = 13
	// StPracticeRoomNoWar 目标席位已空, 无需争夺 (14)
	StPracticeRoomNoWar uint8 = 14
	// StPracticeRoomNoEnoughIngot 元宝不足 (15)
	StPracticeRoomNoEnoughIngot uint8 = 15
	// StPracticeRoomNoExpToGet 没有经验可领取 (16)
	StPracticeRoomNoExpToGet uint8 = 16
	// StPracticeRoomFailure 操作失败 (17)
	StPracticeRoomFailure uint8 = 17
	// StPracticeRoomNoInPractice 当前不在练功状态 (18)
	StPracticeRoomNoInPractice uint8 = 18
	// StPracticeRoomMaxPracticeTime 单次练功时长已达上限 (19)
	StPracticeRoomMaxPracticeTime uint8 = 19
	// StPracticeRoomBeRobbed 席位已被其他玩家夺走 (20)
	StPracticeRoomBeRobbed uint8 = 20
)

// stPracticeRoomSeatCounts 各品质房间的固定席位数。
//
// 来源: 官方 Main.swf com.assist.server.StPracticeRoomType.Qualities:
//
//	1:[6,20,30], 2:[7,40,50], 3:[8,60,80], 4:[10,100,120], 5:[12,150,200], 100:[18,10,0]
//
// 普通练功场 (100) 为分页大房间，18 为单页展示容量，不代表席位编号上限。
var stPracticeRoomSeatCounts = map[uint8]int{
	1:   6,
	2:   7,
	3:   8,
	4:   10,
	5:   12,
	100: 18,
}

// StPracticeRoomSeatCount 返回指定品质房间的固定席位数。
// 未知品质返回 0，调用方应视为不可用房间。
func StPracticeRoomSeatCount(quality uint8) int {
	return stPracticeRoomSeatCounts[quality]
}

// StPracticeRoomIsStarQuality 判断品质是否为 1..5 星级房间。
func StPracticeRoomIsStarQuality(quality uint8) bool {
	return quality >= 1 && quality <= 5
}

// StPracticeRoomQualityRank 返回用于比较收益高低的房间等级。
//
// 星级房间品质越大收益越高；普通练功场 (100) 收益最低，统一记为 0。
func StPracticeRoomQualityRank(quality uint8) int {
	if StPracticeRoomIsStarQuality(quality) {
		return int(quality)
	}
	return 0
}

// StPracticeRoomEntry 代表房间列表中的单个房间。
//
// 线格式: Byte quality, Short count（count 为当前房间内的玩家数量）。
type StPracticeRoomEntry struct {
	// Quality 房间品质 (1..5 星级; 100 普通练功场)
	Quality uint8
	// Count 当前房间内的玩家数量
	Count int16
}

// StPracticeRoomList 代表 get_rooms 回包。
//
// 线格式:
//
//	Uint16 roomCount, roomCount × (Byte quality, Short count),
//	Uint8 in_practice_flag (YES/NO), Byte quality (当前所在房间品质, 0 表示不在任何房间)
type StPracticeRoomList struct {
	// Rooms 全部房间列表
	Rooms []StPracticeRoomEntry
	// InPracticeFlag YES(2) 表示当前正在练功, NO(3) 表示未在练功
	InPracticeFlag uint8
	// Quality 当前所在房间品质 (0 表示不在任何房间)
	Quality int8
}

// InPractice 判断玩家当前是否处于练功状态。
func (l StPracticeRoomList) InPractice() bool {
	return l.InPracticeFlag == StPracticeRoomYes
}

// StPracticeRoomSeat 代表房间内的一个席位。
//
// 线格式:
//
//	Byte seat_seq, Int player_id, Int role_id, Int equip_id,
//	String nick_name, String stage_name, String server_name,
//	Long attack_power, Int level, UByte enemy_flag, Int supreme_vip_prefix
type StPracticeRoomSeat struct {
	// SeatSeq 席位序号 (0 起，通常 0 为至尊位)
	SeatSeq int8
	// PlayerID 跨服玩家 ID
	PlayerID int32
	// RoleID 职业/造型编号
	RoleID int32
	// EquipID 当前装备/时装编号
	EquipID int32
	// NickName 玩家昵称
	NickName string
	// StageName 区服阶段名
	StageName string
	// ServerName 区服全名
	ServerName string
	// AttackPower 玩家战力
	AttackPower int64
	// Level 玩家等级
	Level int32
	// EnemyFlag 是否为敌对阵营 (True=0)
	EnemyFlag uint8
	// SupremeVIPPrefix 至尊 VIP 前缀标识
	SupremeVIPPrefix int32
}

// StPracticeRoomInfo 代表 get_room_info 星级房间回包。
//
// 线格式:
//
//	Uint16 seatCount, seatCount × 席位记录,
//	Int practice_time, Long exp, Short rob_time,
//	Byte room_quality, UByte exp_plus_flag, Byte exp_plus_percent
type StPracticeRoomInfo struct {
	// Seats 已入座的玩家席位列表
	Seats []StPracticeRoomSeat
	// PracticeTime 当前累计练功时长 (秒)
	PracticeTime int32
	// Exp 当前累计待领取经验
	Exp int64
	// RobTime 今日剩余争夺次数
	RobTime int16
	// RoomQuality 房间品质
	RoomQuality int8
	// ExpPlusFlag 经验加成标记 (YES=2 表示有加成)
	ExpPlusFlag uint8
	// ExpPlusPercent 经验加成百分比
	ExpPlusPercent int8
}

// StPracticeRoomFreeInfo 代表普通练功场 get_free_room_info 回包。
//
// 线格式:
//
//	Short total_page, Short total_player, Int practice_time, Long exp,
//	Short rob_time, Short player_page, Byte room_quality,
//	UByte exp_plus_flag, Byte exp_plus_percent
type StPracticeRoomFreeInfo struct {
	// TotalPage 总页数
	TotalPage int16
	// TotalPlayer 普通练功场当前总人数
	TotalPlayer int16
	// PracticeTime 当前累计练功时长 (秒)
	PracticeTime int32
	// Exp 当前累计待领取经验
	Exp int64
	// RobTime 今日剩余争夺次数
	RobTime int16
	// PlayerPage 玩家自己所在页
	PlayerPage int16
	// RoomQuality 当前所在房间品质
	RoomQuality int8
	// ExpPlusFlag 经验加成标记 (YES=2 表示有加成)
	ExpPlusFlag uint8
	// ExpPlusPercent 经验加成百分比
	ExpPlusPercent int8
}

// StPracticeRoomExpResult 代表 get_room_practice_exp 回包。
//
// 线格式: UByte result, Long exp
type StPracticeRoomExpResult struct {
	// Result 操作结果码 (Success 或 NoExpToGet)
	Result uint8
	// Exp 本次领取的经验
	Exp int64
}

// StPracticeRoomRobCD 代表 get_rob_cd_time 回包。
//
// 线格式: Short cd, Short ingot
type StPracticeRoomRobCD struct {
	// CD 剩余争夺冷却秒数
	CD int16
	// Ingot 清除冷却所需元宝数量
	Ingot int16
}

// newStPracticeRoomPacket 构造带 PrevAct 尾部的跨服请求封包。
//
// 官方 DataBase.netCall 在 SOCKETSERVERTOWN 连接上会自动追加 4 字节
// 前序 Module+Action；Go 会话层通过 HasPrevAct 标记在发送时用最近一次业务
// ActionID 覆盖末尾 4 字节，因此这里仅预留 4 字节占位。
//
// 仅适用于空业务载荷的查询/面板动作。带 Byte/Short 业务字段的动作
// （get_room_info / sit_down / get_seat_info / rob_the_seat 等）在线上
// 处理器中执行严格长度校验，追加 PrevAct 会被静默丢弃，必须使用
// newStPracticeRoomStrictPacket。
func newStPracticeRoomPacket(actionID uint32, payload []byte) *Packet {
	body := make([]byte, len(payload)+4)
	copy(body, payload)
	return &Packet{ActionID: actionID, Payload: body, HasPrevAct: true}
}

// newStPracticeRoomStrictPacket 构造不带 PrevAct 的严格长度业务请求封包。
//
// 2026-11-04 s1 实机验证:
//   - get_room_info 请求 5 字节（quality + PrevAct）无任何回包；
//   - 同一请求改为严格 1 字节后立即返回完整席位列表；
//   - sit_down 同样只接受严格 2 字节（quality + seat）。
func newStPracticeRoomStrictPacket(actionID uint32, payload []byte) *Packet {
	return &Packet{ActionID: actionID, Payload: payload}
}

// BuildStPracticeRoomGetRoomsPacket 构造查询房间列表请求封包 (0x00750000)。
func BuildStPracticeRoomGetRoomsPacket() *Packet {
	return newStPracticeRoomPacket(ActionIDStPracticeRoomGetRooms, nil)
}

// BuildStPracticeRoomGetRoomInfoPacket 构造查询指定品质房间信息请求封包 (0x00750001)。
func BuildStPracticeRoomGetRoomInfoPacket(quality uint8) *Packet {
	return newStPracticeRoomStrictPacket(ActionIDStPracticeRoomGetRoomInfo, []byte{quality})
}

// BuildStPracticeRoomSitDownPacket 构造坐上指定席位请求封包 (0x00750002)。
func BuildStPracticeRoomSitDownPacket(quality, seat uint8) *Packet {
	return newStPracticeRoomStrictPacket(ActionIDStPracticeRoomSitDown, []byte{quality, seat})
}

// BuildStPracticeRoomGetSeatInfoPacket 构造查询指定席位状态请求封包 (0x00750003)。
func BuildStPracticeRoomGetSeatInfoPacket(quality, seat uint8) *Packet {
	return newStPracticeRoomStrictPacket(ActionIDStPracticeRoomGetSeatInfo, []byte{quality, seat})
}

// BuildStPracticeRoomRobSeatPacket 构造争夺指定席位请求封包 (0x00750004)。
func BuildStPracticeRoomRobSeatPacket(quality, seat uint8) *Packet {
	return newStPracticeRoomStrictPacket(ActionIDStPracticeRoomRobSeat, []byte{quality, seat})
}

// BuildStPracticeRoomBuyRobTimePacket 构造购买额外争夺次数请求封包 (0x00750005)。
func BuildStPracticeRoomBuyRobTimePacket() *Packet {
	return newStPracticeRoomPacket(ActionIDStPracticeRoomBuyRobTime, nil)
}

// BuildStPracticeRoomGetPracticeExpPacket 构造领取练功经验请求封包 (0x00750006)。
func BuildStPracticeRoomGetPracticeExpPacket() *Packet {
	return newStPracticeRoomPacket(ActionIDStPracticeRoomGetPracticeExp, nil)
}

// BuildStPracticeRoomGetExpFlagPacket 构造查询未领取经验标记请求封包 (0x00750007)。
func BuildStPracticeRoomGetExpFlagPacket() *Packet {
	return newStPracticeRoomPacket(ActionIDStPracticeRoomGetExpFlag, nil)
}

// BuildStPracticeRoomEnterRoomPacket 构造进入练功房面板请求封包 (0x00750008)。
func BuildStPracticeRoomEnterRoomPacket() *Packet {
	return newStPracticeRoomPacket(ActionIDStPracticeRoomEnterRoom, nil)
}

// BuildStPracticeRoomExitRoomPacket 构造退出练功房面板请求封包 (0x00750009)。
func BuildStPracticeRoomExitRoomPacket() *Packet {
	return newStPracticeRoomPacket(ActionIDStPracticeRoomExitRoom, nil)
}

// BuildStPracticeRoomCleanRobCDTimePacket 构造清除争夺冷却请求封包 (0x0075000A)。
func BuildStPracticeRoomCleanRobCDTimePacket() *Packet {
	return newStPracticeRoomPacket(ActionIDStPracticeRoomCleanRobCDTime, nil)
}

// BuildStPracticeRoomGetRobCDTimePacket 构造查询争夺冷却请求封包 (0x0075000B)。
func BuildStPracticeRoomGetRobCDTimePacket() *Packet {
	return newStPracticeRoomPacket(ActionIDStPracticeRoomGetRobCDTime, nil)
}

// BuildStPracticeRoomPlayerLeaveSeatPacket 构造主动离开席位请求封包 (0x0075000D)。
func BuildStPracticeRoomPlayerLeaveSeatPacket() *Packet {
	return newStPracticeRoomPacket(ActionIDStPracticeRoomPlayerLeaveSeat, nil)
}

// BuildStPracticeRoomGetPlayerRoomQualityPacket 构造查询玩家当前房间品质请求封包 (0x0075000E)。
func BuildStPracticeRoomGetPlayerRoomQualityPacket() *Packet {
	return newStPracticeRoomPacket(ActionIDStPracticeRoomGetPlayerRoomQuality, nil)
}

// BuildStPracticeRoomGetFreeRoomInfoPacket 构造查询普通练功场信息请求封包 (0x00750010)。
func BuildStPracticeRoomGetFreeRoomInfoPacket() *Packet {
	return newStPracticeRoomPacket(ActionIDStPracticeRoomGetFreeRoomInfo, nil)
}

// BuildStPracticeRoomGetFreeRoomInfoByPagePacket 构造查询普通练功场分页席位请求封包 (0x00750011)。
func BuildStPracticeRoomGetFreeRoomInfoByPagePacket(page int16) *Packet {
	w := NewWriter()
	w.WriteInt16(page)
	return newStPracticeRoomStrictPacket(ActionIDStPracticeRoomGetFreeRoomInfoByPage, w.Bytes())
}

// ParseStPracticeRoomList 解析 get_rooms 回包。
func ParseStPracticeRoomList(payload []byte) (*StPracticeRoomList, error) {
	r := NewReader(payload)

	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read st practice room count: %v", ErrInvalidPayload, err)
	}
	rooms := make([]StPracticeRoomEntry, 0, count)
	for i := 0; i < int(count); i++ {
		quality, err := r.ReadUint8()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read st practice room quality: %v", ErrInvalidPayload, err)
		}
		roomCount, err := r.ReadInt16()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read st practice room player count: %v", ErrInvalidPayload, err)
		}
		rooms = append(rooms, StPracticeRoomEntry{Quality: quality, Count: roomCount})
	}

	inPracticeFlag, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read st practice room in_practice_flag: %v", ErrInvalidPayload, err)
	}
	quality, err := r.ReadInt8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read st practice room current quality: %v", ErrInvalidPayload, err)
	}

	return &StPracticeRoomList{
		Rooms:          rooms,
		InPracticeFlag: inPracticeFlag,
		Quality:        quality,
	}, nil
}

// ParseStPracticeRoomInfo 解析 get_room_info 星级房间回包。
func ParseStPracticeRoomInfo(payload []byte) (*StPracticeRoomInfo, error) {
	r := NewReader(payload)

	seatCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read st practice room seat count: %v", ErrInvalidPayload, err)
	}
	seats := make([]StPracticeRoomSeat, 0, seatCount)
	for i := 0; i < int(seatCount); i++ {
		seat, err := readStPracticeRoomSeat(r)
		if err != nil {
			return nil, err
		}
		seats = append(seats, seat)
	}

	practiceTime, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read st practice room practice_time: %v", ErrInvalidPayload, err)
	}
	exp, err := r.ReadInt64()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read st practice room exp: %v", ErrInvalidPayload, err)
	}
	robTime, err := r.ReadInt16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read st practice room rob_time: %v", ErrInvalidPayload, err)
	}
	roomQuality, err := r.ReadInt8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read st practice room room_quality: %v", ErrInvalidPayload, err)
	}
	expPlusFlag, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read st practice room exp_plus_flag: %v", ErrInvalidPayload, err)
	}
	expPlusPercent, err := r.ReadInt8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read st practice room exp_plus_percent: %v", ErrInvalidPayload, err)
	}

	return &StPracticeRoomInfo{
		Seats:          seats,
		PracticeTime:   practiceTime,
		Exp:            exp,
		RobTime:        robTime,
		RoomQuality:    roomQuality,
		ExpPlusFlag:    expPlusFlag,
		ExpPlusPercent: expPlusPercent,
	}, nil
}

// ParseStPracticeRoomFreeInfo 解析普通练功场 get_free_room_info 回包。
func ParseStPracticeRoomFreeInfo(payload []byte) (*StPracticeRoomFreeInfo, error) {
	r := NewReader(payload)

	var info StPracticeRoomFreeInfo
	var err error
	if info.TotalPage, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read st practice room total_page: %v", ErrInvalidPayload, err)
	}
	if info.TotalPlayer, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read st practice room total_player: %v", ErrInvalidPayload, err)
	}
	if info.PracticeTime, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read st practice room practice_time: %v", ErrInvalidPayload, err)
	}
	if info.Exp, err = r.ReadInt64(); err != nil {
		return nil, fmt.Errorf("%w: failed to read st practice room free exp: %v", ErrInvalidPayload, err)
	}
	if info.RobTime, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read st practice room free rob_time: %v", ErrInvalidPayload, err)
	}
	if info.PlayerPage, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read st practice room player_page: %v", ErrInvalidPayload, err)
	}
	if info.RoomQuality, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read st practice room free room_quality: %v", ErrInvalidPayload, err)
	}
	if info.ExpPlusFlag, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read st practice room free exp_plus_flag: %v", ErrInvalidPayload, err)
	}
	if info.ExpPlusPercent, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read st practice room free exp_plus_percent: %v", ErrInvalidPayload, err)
	}
	return &info, nil
}

// readStPracticeRoomSeat 读取单个席位记录。
func readStPracticeRoomSeat(r *Reader) (StPracticeRoomSeat, error) {
	var seat StPracticeRoomSeat
	var err error
	if seat.SeatSeq, err = r.ReadInt8(); err != nil {
		return seat, fmt.Errorf("%w: failed to read st practice room seat_seq: %v", ErrInvalidPayload, err)
	}
	if seat.PlayerID, err = r.ReadInt32(); err != nil {
		return seat, fmt.Errorf("%w: failed to read st practice room player_id: %v", ErrInvalidPayload, err)
	}
	if seat.RoleID, err = r.ReadInt32(); err != nil {
		return seat, fmt.Errorf("%w: failed to read st practice room role_id: %v", ErrInvalidPayload, err)
	}
	if seat.EquipID, err = r.ReadInt32(); err != nil {
		return seat, fmt.Errorf("%w: failed to read st practice room equip_id: %v", ErrInvalidPayload, err)
	}
	if seat.NickName, err = r.ReadString(); err != nil {
		return seat, fmt.Errorf("%w: failed to read st practice room nick_name: %v", ErrInvalidPayload, err)
	}
	if seat.StageName, err = r.ReadString(); err != nil {
		return seat, fmt.Errorf("%w: failed to read st practice room stage_name: %v", ErrInvalidPayload, err)
	}
	if seat.ServerName, err = r.ReadString(); err != nil {
		return seat, fmt.Errorf("%w: failed to read st practice room server_name: %v", ErrInvalidPayload, err)
	}
	if seat.AttackPower, err = r.ReadInt64(); err != nil {
		return seat, fmt.Errorf("%w: failed to read st practice room attack_power: %v", ErrInvalidPayload, err)
	}
	if seat.Level, err = r.ReadInt32(); err != nil {
		return seat, fmt.Errorf("%w: failed to read st practice room level: %v", ErrInvalidPayload, err)
	}
	if seat.EnemyFlag, err = r.ReadUint8(); err != nil {
		return seat, fmt.Errorf("%w: failed to read st practice room enemy_flag: %v", ErrInvalidPayload, err)
	}
	if seat.SupremeVIPPrefix, err = r.ReadInt32(); err != nil {
		return seat, fmt.Errorf("%w: failed to read st practice room supreme_vip_prefix: %v", ErrInvalidPayload, err)
	}
	return seat, nil
}

// ParseStPracticeRoomActionResult 解析首字节为 UByte 结果码的操作回包。
//
// 适用于 sit_down / enter_room / exit_room / player_leave_seat /
// clean_rob_cd_time / rob_the_seat 等动作。
func ParseStPracticeRoomActionResult(payload []byte) (uint8, error) {
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return 0, fmt.Errorf("%w: failed to read st practice room result: %v", ErrInvalidPayload, err)
	}
	return result, nil
}

// ParseStPracticeRoomExpResult 解析 get_room_practice_exp 回包。
//
// 业务失败时服务端通常仍会下发 8 字节经验值；若载荷仅含结果码，
// 则 Exp 记为 0 而不是直接报错，避免把明确的业务失败误判为协议异常。
func ParseStPracticeRoomExpResult(payload []byte) (*StPracticeRoomExpResult, error) {
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read st practice room exp result: %v", ErrInvalidPayload, err)
	}
	out := &StPracticeRoomExpResult{Result: result}
	if r.Remaining() >= 8 {
		exp, err := r.ReadInt64()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read st practice room gained exp: %v", ErrInvalidPayload, err)
		}
		out.Exp = exp
	}
	return out, nil
}

// ParseStPracticeRoomSeatStatus 解析 get_seat_info 回包 (Somebody/Nobody)。
func ParseStPracticeRoomSeatStatus(payload []byte) (uint8, error) {
	return ParseStPracticeRoomActionResult(payload)
}

// ParseStPracticeRoomRobCD 解析 get_rob_cd_time 回包。
func ParseStPracticeRoomRobCD(payload []byte) (*StPracticeRoomRobCD, error) {
	r := NewReader(payload)
	cd, err := r.ReadInt16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read st practice room rob cd: %v", ErrInvalidPayload, err)
	}
	ingot, err := r.ReadInt16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read st practice room rob cd ingot: %v", ErrInvalidPayload, err)
	}
	return &StPracticeRoomRobCD{CD: cd, Ingot: ingot}, nil
}
