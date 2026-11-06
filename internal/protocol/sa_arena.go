// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方圣域竞技场协议 (Mod_SaArena_Base, Module 298 / 0x012A)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_SaArena_Base 与 com.datas.SaArenaData：
//   - get_info     Act 0 -> 0x012A0000  查询擂台席位与剩余可抢占次数
//   - rob_the_seat Act 1 -> 0x012A0001  [UByte seq] 抢占指定席位
package protocol

import "fmt"

const (
	// ModuleSaArena 圣域竞技场模块编号 (0x012A)
	ModuleSaArena uint16 = 298
)

// 圣域竞技场动作编号 (Mod_SaArena_Base)。
const (
	// ActionIDSaArenaGetInfo 查询圣域竞技场信息 (Action 0 -> 0x012A0000)
	ActionIDSaArenaGetInfo uint32 = 0x012A0000
	// ActionIDSaArenaRobSeat 抢占圣域竞技场席位 (Action 1 -> 0x012A0001)
	ActionIDSaArenaRobSeat uint32 = 0x012A0001
)

// 圣域竞技场结果码 (Mod_SaArena_Base)。
const (
	// SaArenaResultSuccess 抢占成功
	SaArenaResultSuccess uint8 = 0
	// SaArenaResultSuccessFree 免费抢占成功
	SaArenaResultSuccessFree uint8 = 1
	// SaArenaResultOnSeat 已在席位上
	SaArenaResultOnSeat uint8 = 2
	// SaArenaResultActivityClose 活动已关闭
	SaArenaResultActivityClose uint8 = 3
	// SaArenaResultCDTime 冷却中
	SaArenaResultCDTime uint8 = 4
	// SaArenaResultCountUsedUp 次数已用尽
	SaArenaResultCountUsedUp uint8 = 5
	// SaArenaResultMyself 不能抢占自己
	SaArenaResultMyself uint8 = 7
	// SaArenaResultPackFull 背包已满
	SaArenaResultPackFull uint8 = 13
	// SaArenaResultFailed 失败
	SaArenaResultFailed uint8 = 14
)

// SaArenaSeat 表示圣域竞技场的一个席位。
type SaArenaSeat struct {
	// Seq 席位序号 (1 起)
	Seq uint8
	// SaPlayerID 圣域玩家 ID (0 表示空席位)
	SaPlayerID int32
	// RoleID 角色编号
	RoleID int32
	// Level 等级
	Level int16
	// ClothesID 时装编号
	ClothesID int32
	// Nickname 昵称
	Nickname string
	// StageName 阶段名称
	StageName string
	// ServerName 区服名
	ServerName string
	// DefenseCount 防守次数
	DefenseCount int32
	// WarPower 战力
	WarPower int64
	// SupremeVIPPrefix 至尊 VIP 前缀
	SupremeVIPPrefix int32
}

// SaArenaInfo 表示圣域竞技场面板。
type SaArenaInfo struct {
	// Seats 席位列表
	Seats []SaArenaSeat
	// AvailableCount 剩余可抢占次数
	AvailableCount int16
}

// BuildSaArenaGetInfoPacket 构造查询圣域竞技场封包 (0x012A0000)。
func BuildSaArenaGetInfoPacket() *Packet {
	return NewPacket(ActionIDSaArenaGetInfo, nil)
}

// BuildSaArenaRobSeatPacket 构造抢占席位封包 (0x012A0001)。
func BuildSaArenaRobSeatPacket(seq uint8) *Packet {
	return NewPacket(ActionIDSaArenaRobSeat, []byte{seq})
}

// ParseSaArenaGetInfoResult 解析圣域竞技场面板。
func ParseSaArenaGetInfoResult(payload []byte) (*SaArenaInfo, error) {
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read sa seat count: %v", ErrInvalidPayload, err)
	}
	info := &SaArenaInfo{Seats: make([]SaArenaSeat, 0, count)}
	for i := 0; i < int(count); i++ {
		var s SaArenaSeat
		if s.Seq, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read seat[%d].seq: %v", ErrInvalidPayload, i, err)
		}
		if s.SaPlayerID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read seat[%d].sa_player_id: %v", ErrInvalidPayload, i, err)
		}
		if s.RoleID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read seat[%d].role_id: %v", ErrInvalidPayload, i, err)
		}
		if s.Level, err = r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("%w: failed to read seat[%d].level: %v", ErrInvalidPayload, i, err)
		}
		if s.ClothesID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read seat[%d].clothes_id: %v", ErrInvalidPayload, i, err)
		}
		if s.Nickname, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: failed to read seat[%d].nickname: %v", ErrInvalidPayload, i, err)
		}
		if s.StageName, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: failed to read seat[%d].stage_name: %v", ErrInvalidPayload, i, err)
		}
		if s.ServerName, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: failed to read seat[%d].server_name: %v", ErrInvalidPayload, i, err)
		}
		if s.DefenseCount, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read seat[%d].defense_count: %v", ErrInvalidPayload, i, err)
		}
		if s.WarPower, err = r.ReadInt64(); err != nil {
			return nil, fmt.Errorf("%w: failed to read seat[%d].war_power: %v", ErrInvalidPayload, i, err)
		}
		if s.SupremeVIPPrefix, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read seat[%d].vip_prefix: %v", ErrInvalidPayload, i, err)
		}
		info.Seats = append(info.Seats, s)
	}
	if info.AvailableCount, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sa available_count: %v", ErrInvalidPayload, err)
	}
	return info, nil
}

// SaArenaRobResult 表示抢占席位的结果。
type SaArenaRobResult struct {
	// Result 结果码
	Result uint8
	// AwardScore 本次获得的积分
	AwardScore int16
	// DeployType 战报布阵类型
	DeployType uint8
	// WinPlayerID 战斗胜利方玩家 ID
	WinPlayerID int32
}

// IsWin 判断 playerID 是否赢得本次抢占战斗。
func (r *SaArenaRobResult) IsWin(playerID int32) bool {
	return r != nil && (r.Result == SaArenaResultSuccess || r.Result == SaArenaResultSuccessFree) && r.WinPlayerID == playerID
}

// ParseSaArenaRobResult 解析抢占席位回包。
func ParseSaArenaRobResult(payload []byte) (*SaArenaRobResult, error) {
	r := NewReader(payload)
	res := &SaArenaRobResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sa rob result: %v", ErrInvalidPayload, err)
	}
	if res.Result != SaArenaResultSuccess && res.Result != SaArenaResultSuccessFree {
		return res, nil
	}
	if res.AwardScore, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read sa award score: %v", ErrInvalidPayload, err)
	}
	// 战报列表：客户端只消费第一个战报单元的前两个字段。
	if r.Remaining() >= 5 {
		if res.DeployType, err = r.ReadUint8(); err == nil {
			res.WinPlayerID, _ = r.ReadInt32()
		}
	}
	return res, nil
}

// BuildSaArenaGetInfoResultPacket 构造圣域竞技场信息回包，供离线 mock 与单测使用。
func BuildSaArenaGetInfoResultPacket(info SaArenaInfo) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(info.Seats)))
	for _, s := range info.Seats {
		w.WriteUint8(s.Seq)
		w.WriteInt32(s.SaPlayerID)
		w.WriteInt32(s.RoleID)
		w.WriteInt16(s.Level)
		w.WriteInt32(s.ClothesID)
		w.WriteString(s.Nickname)
		w.WriteString(s.StageName)
		w.WriteString(s.ServerName)
		w.WriteInt32(s.DefenseCount)
		w.WriteInt64(s.WarPower)
		w.WriteInt32(s.SupremeVIPPrefix)
	}
	w.WriteInt16(info.AvailableCount)
	return NewPacket(ActionIDSaArenaGetInfo, w.Bytes())
}

// BuildSaArenaRobResultPacket 构造抢占回包，供离线 mock 与单测使用。
func BuildSaArenaRobResultPacket(res SaArenaRobResult) *Packet {
	w := NewWriter()
	w.WriteUint8(res.Result)
	if res.Result == SaArenaResultSuccess || res.Result == SaArenaResultSuccessFree {
		w.WriteInt16(res.AwardScore)
		w.WriteUint8(res.DeployType)
		w.WriteInt32(res.WinPlayerID)
		w.WriteUint16(0)
	}
	return NewPacket(ActionIDSaArenaRobSeat, w.Bytes())
}
