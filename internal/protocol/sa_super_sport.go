// 本文件实现官方圣域竞技场 (Mod_SaSuperSport_Base, Module 384 / 0x0180) 协议。
//
// 依据官方 Main.swf 反编译的 com.protocols.Mod_SaSuperSport_Base、
// com.datas.SaSuperSportData 与 2026-11-24 抓包 /tmp/shengyujingjichang.pcapng：
//
//	race_info   Act 0 -> 0x01800000  查询积分/名次、剩余挑战次数与 5 名对手
//	rank_list   Act 1 -> 0x01800001  排行榜
//	rank_award  Act 2 -> 0x01800002  排名奖励
//	challage    Act 3 -> 0x01800003  [Byte slot][Int battle_id] 发起挑战
//	shop_info   Act 4 -> 0x01800004  商店
//	buy         Act 5 -> 0x01800005  购买
//	get_sport_report   Act 6 -> 0x01800006  战报列表
//	new_sport_report   Act 7 -> 0x01800007  新战报推送
//	get_player_war_report Act 8 -> 0x01800008 他人战报
//
// 所有请求尾部带 4 字节 PrevAct；竞技场协议必须走圣域独立连接
// (DataBase.SOCKETSAINTAREA，抓包为 81.70.56.121:8220)。
package protocol

import "fmt"

const (
	// ModuleSaSuperSport 圣域竞技场模块编号 (0x0180)
	ModuleSaSuperSport uint16 = 384
)

// 圣域竞技场动作编号 (Mod_SaSuperSport_Base.Actions)。
const (
	ActionIDSaSuperSportRaceInfo  uint32 = 0x01800000
	ActionIDSaSuperSportRankList  uint32 = 0x01800001
	ActionIDSaSuperSportRankAward uint32 = 0x01800002
	ActionIDSaSuperSportChallenge uint32 = 0x01800003
	ActionIDSaSuperSportShopInfo  uint32 = 0x01800004
	ActionIDSaSuperSportBuy       uint32 = 0x01800005
	ActionIDSaSuperSportReport    uint32 = 0x01800006
	ActionIDSaSuperSportNewReport uint32 = 0x01800007
	ActionIDSaSuperSportWarReport uint32 = 0x01800008
)

// 圣域竞技场结果码 (Mod_SaSuperSport_Base)。
const (
	// SaSuperSportRaceScore 积分赛 (race_state)
	SaSuperSportRaceScore uint8 = 0
	// SaSuperSportRaceRank 排名赛 (race_state)
	SaSuperSportRaceRank uint8 = 1
	// SaSuperSportResultSuccess 挑战成功
	SaSuperSportResultSuccess uint8 = 2
	// SaSuperSportResultError 通用失败
	SaSuperSportResultError uint8 = 3
	// SaSuperSportResultNoChallengeTimes 挑战次数不足
	SaSuperSportResultNoChallengeTimes uint8 = 4
	// SaSuperSportResultNoShengHun 圣魂不足
	SaSuperSportResultNoShengHun uint8 = 5
	// SaSuperSportResultPackFull 背包已满
	SaSuperSportResultPackFull uint8 = 6
	// SaSuperSportResultTimesLimit 次数上限
	SaSuperSportResultTimesLimit uint8 = 7
)

// SaSuperSportOpponent 表示 race_info 中的一名对手。
//
// 线格式: [Int player_id][Byte slot][String platform][String server]
//
//	[String nickname][Long war_power][Int a][Int b]
//	[Int battle_id][Int level][Int e]
type SaSuperSportOpponent struct {
	// PlayerID 对手玩家 ID
	PlayerID int32
	// Slot 挑战位序号 (1~5，challage 请求首字节)
	Slot uint8
	// Platform 平台标识
	Platform string
	// Server 区服
	Server string
	// Nickname 昵称
	Nickname string
	// WarPower 战力
	WarPower int64
	// A 熟练度/积分等展示字段
	A int32
	// B 展示字段
	B int32
	// BattleID 挑战战斗 ID (challage 请求第二个字段)
	BattleID int32
	// Level 等级
	Level int32
	// E 保留字段
	E int32
}

// SaSuperSportRaceInfo 表示圣域竞技场面板 (0x01800000)。
type SaSuperSportRaceInfo struct {
	// RaceState 比赛状态: 0=积分赛, 1=排名赛
	RaceState uint8
	// LeftTimes 今日剩余挑战次数
	LeftTimes int32
	// Score 积分/熟练度
	Score int32
	// Rank 名次
	Rank int32
	// Extra 额外展示字段
	Extra int32
	// Opponents 可挑战对手列表
	Opponents []SaSuperSportOpponent
}

func saSuperSportPrevPacket(actionID uint32, prefix []byte) *Packet {
	payload := make([]byte, 0, len(prefix)+4)
	payload = append(payload, prefix...)
	payload = append(payload, 0, 0, 0, 0)
	pkt := NewPacket(actionID, payload)
	pkt.HasPrevAct = true
	return pkt
}

// BuildSaSuperSportRaceInfoPacket 构造圣域竞技场面板请求 (0x01800000)。
func BuildSaSuperSportRaceInfoPacket() *Packet {
	return saSuperSportPrevPacket(ActionIDSaSuperSportRaceInfo, nil)
}

// BuildSaSuperSportChallengePacket 构造挑战请求 (0x01800003)。
//
// slot 为对手记录中的 Byte 字段 (1~5)，battleID 为记录中的 battle_id。
func BuildSaSuperSportChallengePacket(slot uint8, battleID int32) *Packet {
	w := NewWriter()
	w.WriteUint8(slot)
	w.WriteInt32(battleID)
	return saSuperSportPrevPacket(ActionIDSaSuperSportChallenge, w.Bytes())
}

// ParseSaSuperSportRaceInfo 解析 0x01800000 回包。
func ParseSaSuperSportRaceInfo(payload []byte) (*SaSuperSportRaceInfo, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	out := &SaSuperSportRaceInfo{}
	var err error
	if out.RaceState, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: sa super sport race_state: %v", ErrInvalidPayload, err)
	}
	for _, dst := range []*int32{&out.LeftTimes, &out.Score, &out.Rank, &out.Extra} {
		if *dst, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: sa super sport int: %v", ErrInvalidPayload, err)
		}
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: sa super sport opponent count: %v", ErrInvalidPayload, err)
	}
	for i := 0; i < int(count); i++ {
		var o SaSuperSportOpponent
		if o.PlayerID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: opponent[%d].player_id: %v", ErrInvalidPayload, i, err)
		}
		if o.Slot, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: opponent[%d].slot: %v", ErrInvalidPayload, i, err)
		}
		if o.Platform, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: opponent[%d].platform: %v", ErrInvalidPayload, i, err)
		}
		if o.Server, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: opponent[%d].server: %v", ErrInvalidPayload, i, err)
		}
		if o.Nickname, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: opponent[%d].nickname: %v", ErrInvalidPayload, i, err)
		}
		if o.WarPower, err = r.ReadInt64(); err != nil {
			return nil, fmt.Errorf("%w: opponent[%d].war_power: %v", ErrInvalidPayload, i, err)
		}
		for _, dst := range []*int32{&o.A, &o.B, &o.BattleID, &o.Level, &o.E} {
			if *dst, err = r.ReadInt32(); err != nil {
				return nil, fmt.Errorf("%w: opponent[%d].field: %v", ErrInvalidPayload, i, err)
			}
		}
		out.Opponents = append(out.Opponents, o)
	}
	return out, nil
}

// ParseSaSuperSportChallengeResult 读取 0x01800003 回包首字节结果码。
//
// 成功回包其余部分为完整战报，自动化不需要解析。
func ParseSaSuperSportChallengeResult(payload []byte) (uint8, error) {
	p := DecompressPayloadIfNeeded(payload)
	if len(p) < 1 {
		return 0, fmt.Errorf("%w: sa super sport challenge result empty", ErrInvalidPayload)
	}
	return p[0], nil
}

// SaSuperSportResultText 返回结果码文本。
func SaSuperSportResultText(code uint8) string {
	switch code {
	case SaSuperSportResultSuccess:
		return "挑战成功"
	case SaSuperSportResultError:
		return "失败"
	case SaSuperSportResultNoChallengeTimes:
		return "挑战次数不足"
	case SaSuperSportResultNoShengHun:
		return "圣魂不足"
	case SaSuperSportResultPackFull:
		return "背包已满"
	case SaSuperSportResultTimesLimit:
		return "次数上限"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// BuildSaSuperSportRaceInfoResultPacket 构造面板回包，供单测使用。
func BuildSaSuperSportRaceInfoResultPacket(info *SaSuperSportRaceInfo) *Packet {
	w := NewWriter()
	w.WriteUint8(info.RaceState)
	w.WriteInt32(info.LeftTimes)
	w.WriteInt32(info.Score)
	w.WriteInt32(info.Rank)
	w.WriteInt32(info.Extra)
	w.WriteUint16(uint16(len(info.Opponents)))
	for _, o := range info.Opponents {
		w.WriteInt32(o.PlayerID)
		w.WriteUint8(o.Slot)
		w.WriteString(o.Platform)
		w.WriteString(o.Server)
		w.WriteString(o.Nickname)
		w.WriteInt64(o.WarPower)
		w.WriteInt32(o.A)
		w.WriteInt32(o.B)
		w.WriteInt32(o.BattleID)
		w.WriteInt32(o.Level)
		w.WriteInt32(o.E)
	}
	return NewPacket(ActionIDSaSuperSportRaceInfo, w.Bytes())
}

// BuildSaSuperSportChallengeResultPacket 构造挑战结果回包，供单测使用。
func BuildSaSuperSportChallengeResultPacket(result uint8) *Packet {
	return NewPacket(ActionIDSaSuperSportChallenge, []byte{result})
}
