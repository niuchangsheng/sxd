// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现圣域跨服登录与圣域修炼房协议：
//   - Mod_SaintAreaLogin_Base (Module 293, 圣域登录网关命名空间)
//   - Mod_SaPracticeRoom_Base (Module 407, 圣域修炼房)
//
// 圣域登录与仙界跨服类似：主服 get_login_info 下发临时凭据，客户端另建 TCP
// 长连接后调用 login。圣域修炼房动作必须在该独立连接上执行。
package protocol

import "fmt"

const (
	// ModuleSaintAreaLogin 圣域登录模块编号 (0x0125，圣域连接命名空间)
	ModuleSaintAreaLogin uint16 = 293
	// ModuleSaPracticeRoom 圣域修炼房模块编号 (0x0197)
	ModuleSaPracticeRoom uint16 = 407
)

// 圣域登录动作编号 (Mod_SaintAreaLogin_Base)。
const (
	// ActionIDSaintAreaLogin 圣域连接登录 (Action 0 -> 0x01250000)
	ActionIDSaintAreaLogin uint32 = 0x01250000
	// ActionIDSaintAreaLoginInfo 主服查询圣域登录凭据 (Action 1 -> 0x01250001)
	ActionIDSaintAreaLoginInfo uint32 = 0x01250001
	// ActionIDSaintAreaStatus 查询圣域开启状态 (Action 2 -> 0x01250002)
	ActionIDSaintAreaStatus uint32 = 0x01250002
)

// 圣域修炼房动作编号 (Mod_SaPracticeRoom_Base)。
const (
	// ActionIDSaPracticeRoomGetFreeInfo 查询免费修炼房信息 (Action 16 -> 0x01970010)
	ActionIDSaPracticeRoomGetFreeInfo uint32 = 0x01970010
	// ActionIDSaPracticeRoomSitDown 入座修炼 (Action 2 -> 0x01970002)
	ActionIDSaPracticeRoomSitDown uint32 = 0x01970002
	// ActionIDSaPracticeRoomExpFlag 查询是否有可领取经验 (Action 7 -> 0x01970007)
	ActionIDSaPracticeRoomExpFlag uint32 = 0x01970007
	// ActionIDSaPracticeRoomGetExp 领取修炼经验 (Action 6 -> 0x01970006)
	ActionIDSaPracticeRoomGetExp uint32 = 0x01970006
	// ActionIDSaPracticeRoomExit 退出修炼房 (Action 9 -> 0x01970009)
	ActionIDSaPracticeRoomExit uint32 = 0x01970009
)

// 圣域状态码 (Mod_SaintAreaLogin_Base)。
const (
	// SaintAreaResultSuccess 登录成功
	SaintAreaResultSuccess uint8 = 0
	// SaintAreaResultFailed 登录失败
	SaintAreaResultFailed uint8 = 1
	// SaintAreaStatusOpen 圣域已开启
	SaintAreaStatusOpen uint8 = 2
	// SaintAreaStatusMaintain 圣域维护中
	SaintAreaStatusMaintain uint8 = 3
	// SaintAreaStatusClose 圣域已关闭
	SaintAreaStatusClose uint8 = 4
)

// 圣域修炼房结果码 (Mod_SaPracticeRoom_Base)。
const (
	// SaPracticeRoomResultSuccess 成功
	SaPracticeRoomResultSuccess uint8 = 4
	// SaPracticeRoomResultRoomSeatError 房间/座位错误
	SaPracticeRoomResultRoomSeatError uint8 = 5
	// SaPracticeRoomResultAlreadyInUse 已在房间中
	SaPracticeRoomResultAlreadyInUse uint8 = 6
	// SaPracticeRoomResultExpNotGotten 经验未领取
	SaPracticeRoomResultExpNotGotten uint8 = 7
	// SaPracticeRoomResultFunctionNoOpen 功能未开放
	SaPracticeRoomResultFunctionNoOpen uint8 = 8
	// SaPracticeRoomResultAlreadySatDown 已经坐下
	SaPracticeRoomResultAlreadySatDown uint8 = 9
	// SaPracticeRoomResultNoSeat 没有空位
	SaPracticeRoomResultNoSeat uint8 = 10
	// SaPracticeRoomResultNoExp 没有可领取经验
	SaPracticeRoomResultNoExp uint8 = 19
	// SaPracticeRoomResultNoEnoughDaoYuan 道缘不足
	SaPracticeRoomResultNoEnoughDaoYuan uint8 = 21
)

// SaintAreaCredentials 表示主服下发的圣域连接凭据。
type SaintAreaCredentials struct {
	// Host 圣域网关地址
	Host string
	// Port 圣域网关端口
	Port uint16
	// ServerName 圣域区服标识
	ServerName string
	// Time 登录时间戳
	Time int32
	// PassCode 登录验签串
	PassCode string
	// ServerTownName 圣域城镇名称
	ServerTownName string
	// IsDragonClear 龙脉清理标志
	IsDragonClear int32
}

// SaintAreaStatus 表示圣域开启状态。
type SaintAreaStatus struct {
	// Status 状态码 (2 开启 / 3 维护 / 4 关闭)
	Status uint8
	// CloseTimes 关闭时间窗口 [start, end, dragon]
	CloseTimes []SaintAreaCloseTime
}

// SaintAreaCloseTime 表示一个圣域关闭时间窗口。
type SaintAreaCloseTime struct {
	// StartTime 开始时间
	StartTime int32
	// EndTime 结束时间
	EndTime int32
	// DragonClear 龙脉清理标志
	DragonClear uint8
}

// SaFreeRoomInfo 表示圣域免费修炼房状态。
type SaFreeRoomInfo struct {
	// TotalPage 总页数
	TotalPage int16
	// TotalPlayer 总人数
	TotalPlayer int16
	// PracticeTime 修炼时长
	PracticeTime int32
	// Exp 累积经验
	Exp int64
	// RobTime 可抢夺次数
	RobTime int16
	// PlayerPage 当前页
	PlayerPage int16
	// RoomQuality 当前所在房间品质 (0 表示未入座)
	RoomQuality int8
	// ExpPlusFlag 是否有经验加成
	ExpPlusFlag uint8
	// ExpPlusPercent 经验加成百分比
	ExpPlusPercent int8
	// NeedIngot 领取经验所需元宝
	NeedIngot int32
	// NeedDaoYuan 领取经验所需道缘
	NeedDaoYuan int32
	// NoChangeCoinFlag 不消耗铜钱标志
	NoChangeCoinFlag int32
}

// BuildSaintAreaLoginPacket 构造圣域连接登录封包 (0x01250000)。
func BuildSaintAreaLoginPacket(serverName string, playerID int32, originNickName string, tm int32, passCode string) *Packet {
	w := NewWriter()
	w.WriteString(serverName)
	w.WriteInt32(playerID)
	w.WriteString(originNickName)
	w.WriteInt32(tm)
	w.WriteString(passCode)
	return NewPacket(ActionIDSaintAreaLogin, w.Bytes())
}

// BuildSaintAreaLoginInfoPacket 构造主服查询圣域凭据封包 (0x01250001)。
func BuildSaintAreaLoginInfoPacket() *Packet {
	return NewPacket(ActionIDSaintAreaLoginInfo, nil)
}

// BuildSaintAreaStatusPacket 构造查询圣域状态封包 (0x01250002)。
func BuildSaintAreaStatusPacket() *Packet {
	return NewPacket(ActionIDSaintAreaStatus, nil)
}

// ParseSaintAreaCredentials 解析圣域连接凭据。
func ParseSaintAreaCredentials(payload []byte) (*SaintAreaCredentials, error) {
	r := NewReader(payload)
	c := &SaintAreaCredentials{}
	var err error
	if c.Host, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: failed to read saint host: %v", ErrInvalidPayload, err)
	}
	if c.Port, err = r.ReadUint16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read saint port: %v", ErrInvalidPayload, err)
	}
	if c.ServerName, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: failed to read saint server: %v", ErrInvalidPayload, err)
	}
	if c.Time, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read saint time: %v", ErrInvalidPayload, err)
	}
	if c.PassCode, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: failed to read saint passcode: %v", ErrInvalidPayload, err)
	}
	if c.ServerTownName, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: failed to read saint town: %v", ErrInvalidPayload, err)
	}
	if c.IsDragonClear, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read saint dragon: %v", ErrInvalidPayload, err)
	}
	return c, nil
}

// ParseSaintAreaLoginResult 解析圣域登录结果: UByte result, Int player_id, Int server_time。
func ParseSaintAreaLoginResult(payload []byte) (uint8, int32, int32, error) {
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("%w: failed to read saint login result: %v", ErrInvalidPayload, err)
	}
	playerID, err := r.ReadInt32()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("%w: failed to read saint player_id: %v", ErrInvalidPayload, err)
	}
	serverTime, err := r.ReadInt32()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("%w: failed to read saint server_time: %v", ErrInvalidPayload, err)
	}
	return result, playerID, serverTime, nil
}

// ParseSaintAreaStatusResult 解析圣域状态。
func ParseSaintAreaStatusResult(payload []byte) (*SaintAreaStatus, error) {
	r := NewReader(payload)
	st := &SaintAreaStatus{}
	var err error
	if st.Status, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read saint status: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read saint close time count: %v", ErrInvalidPayload, err)
	}
	for i := 0; i < int(count); i++ {
		var w SaintAreaCloseTime
		if w.StartTime, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read close[%d].start: %v", ErrInvalidPayload, i, err)
		}
		if w.EndTime, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read close[%d].end: %v", ErrInvalidPayload, i, err)
		}
		if w.DragonClear, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read close[%d].dragon: %v", ErrInvalidPayload, i, err)
		}
		st.CloseTimes = append(st.CloseTimes, w)
	}
	return st, nil
}

// BuildSaPracticeRoomFreeInfoPacket 构造查询免费修炼房封包 (0x01970010)。
func BuildSaPracticeRoomFreeInfoPacket() *Packet {
	return NewPacket(ActionIDSaPracticeRoomGetFreeInfo, nil)
}

// BuildSaPracticeRoomSitDownPacket 构造入座封包 (0x01970002)。
func BuildSaPracticeRoomSitDownPacket(quality, seat uint8) *Packet {
	return NewPacket(ActionIDSaPracticeRoomSitDown, []byte{quality, seat})
}

// BuildSaPracticeRoomExpFlagPacket 构造查询可领取经验封包 (0x01970007)。
func BuildSaPracticeRoomExpFlagPacket() *Packet {
	return NewPacket(ActionIDSaPracticeRoomExpFlag, nil)
}

// BuildSaPracticeRoomGetExpPacket 构造领取修炼经验封包 (0x01970006)。
func BuildSaPracticeRoomGetExpPacket(useType uint8) *Packet {
	return NewPacket(ActionIDSaPracticeRoomGetExp, []byte{useType})
}

// BuildSaPracticeRoomExitPacket 构造退出修炼房封包 (0x01970009)。
func BuildSaPracticeRoomExitPacket() *Packet {
	return NewPacket(ActionIDSaPracticeRoomExit, nil)
}

// ParseSaFreeRoomInfoResult 解析免费修炼房信息。
func ParseSaFreeRoomInfoResult(payload []byte) (*SaFreeRoomInfo, error) {
	r := NewReader(payload)
	info := &SaFreeRoomInfo{}
	var err error
	if info.TotalPage, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read total_page: %v", ErrInvalidPayload, err)
	}
	if info.TotalPlayer, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read total_player: %v", ErrInvalidPayload, err)
	}
	if info.PracticeTime, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read practice_time: %v", ErrInvalidPayload, err)
	}
	if info.Exp, err = r.ReadInt64(); err != nil {
		return nil, fmt.Errorf("%w: failed to read room exp: %v", ErrInvalidPayload, err)
	}
	if info.RobTime, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read rob_time: %v", ErrInvalidPayload, err)
	}
	if info.PlayerPage, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read player_page: %v", ErrInvalidPayload, err)
	}
	if info.RoomQuality, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read room_quality: %v", ErrInvalidPayload, err)
	}
	if info.ExpPlusFlag, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read exp_plus_flag: %v", ErrInvalidPayload, err)
	}
	if info.ExpPlusPercent, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read exp_plus_percent: %v", ErrInvalidPayload, err)
	}
	if info.NeedIngot, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read need_ingot: %v", ErrInvalidPayload, err)
	}
	if info.NeedDaoYuan, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read need_dao_yuan: %v", ErrInvalidPayload, err)
	}
	if info.NoChangeCoinFlag, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read no_change_coin_flag: %v", ErrInvalidPayload, err)
	}
	return info, nil
}

// ParseSaPracticeRoomActionResult 解析单结果码动作。
func ParseSaPracticeRoomActionResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty sa practice room action result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// ParseSaPracticeRoomExpResult 解析领取经验回包: UByte result, Long exp。
func ParseSaPracticeRoomExpResult(payload []byte) (uint8, int64, error) {
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return 0, 0, fmt.Errorf("%w: failed to read exp result: %v", ErrInvalidPayload, err)
	}
	if result != SaPracticeRoomResultSuccess {
		return result, 0, nil
	}
	exp, err := r.ReadInt64()
	if err != nil {
		return 0, 0, fmt.Errorf("%w: failed to read gained exp: %v", ErrInvalidPayload, err)
	}
	return result, exp, nil
}

// BuildSaFreeRoomInfoResultPacket 构造免费修炼房回包，供离线 mock 与单测使用。
func BuildSaFreeRoomInfoResultPacket(info SaFreeRoomInfo) *Packet {
	w := NewWriter()
	w.WriteInt16(info.TotalPage)
	w.WriteInt16(info.TotalPlayer)
	w.WriteInt32(info.PracticeTime)
	w.WriteInt64(info.Exp)
	w.WriteInt16(info.RobTime)
	w.WriteInt16(info.PlayerPage)
	w.WriteInt8(info.RoomQuality)
	w.WriteUint8(info.ExpPlusFlag)
	w.WriteInt8(info.ExpPlusPercent)
	w.WriteInt32(info.NeedIngot)
	w.WriteInt32(info.NeedDaoYuan)
	w.WriteInt32(info.NoChangeCoinFlag)
	return NewPacket(ActionIDSaPracticeRoomGetFreeInfo, w.Bytes())
}

// BuildSaPracticeRoomActionResultPacket 构造单结果码回包，供离线 mock 与单测使用。
func BuildSaPracticeRoomActionResultPacket(actionID uint32, code uint8) *Packet {
	return NewPacket(actionID, []byte{code})
}

// BuildSaPracticeRoomExpResultPacket 构造领取经验回包，供离线 mock 与单测使用。
func BuildSaPracticeRoomExpResultPacket(code uint8, exp int64) *Packet {
	w := NewWriter()
	w.WriteUint8(code)
	if code == SaPracticeRoomResultSuccess {
		w.WriteInt64(exp)
	}
	return NewPacket(ActionIDSaPracticeRoomGetExp, w.Bytes())
}
