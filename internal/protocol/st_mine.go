// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方仙界矿山协议 (Mod_StMine_Base, Module 264 / 0x108)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_StMine_Base 与
// com.datas.StMineData：查询矿山信息后进入当前矿脉、领取收益并离开。
// 自动化不调用 buy_rob_count / clean_rob_cd_time / increase_protect_time。
package protocol

import "fmt"

const (
	// ActionIDStMineInfo 查询矿山信息 (Module 264, Action 0 -> 0x01080000)
	ActionIDStMineInfo uint32 = 0x01080000
	// ActionIDStMineEnter 进入矿山 (Module 264, Action 1 -> 0x01080001)
	ActionIDStMineEnter uint32 = 0x01080001
	// ActionIDStMineLeave 离开矿山 (Module 264, Action 2 -> 0x01080002)
	ActionIDStMineLeave uint32 = 0x01080002
	// ActionIDStMineRobHole 抢夺/占领矿洞 (Module 264, Action 3 -> 0x01080003)
	ActionIDStMineRobHole uint32 = 0x01080003
	// ActionIDStMineLeaveHole 放弃当前矿洞 (Module 264, Action 4 -> 0x01080004)
	ActionIDStMineLeaveHole uint32 = 0x01080004
	// ActionIDStMineRecapture 抢回被夺矿洞 (Module 264, Action 5 -> 0x01080005)
	ActionIDStMineRecapture uint32 = 0x01080005
	// ActionIDStMineReceiveAward 领取矿山收益 (Module 264, Action 7 -> 0x01080007)
	ActionIDStMineReceiveAward uint32 = 0x01080007
)

// Mod_StMine_Base 结果码 (关键值)。
const (
	// StMineResultSuccess 成功
	StMineResultSuccess uint8 = 9
	// StMineResultFunctionUnopen 功能未开放
	StMineResultFunctionUnopen uint8 = 10
	// StMineResultSuperTownClosed 超级城镇关闭
	StMineResultSuperTownClosed uint8 = 11
	// StMineResultResting 休息中
	StMineResultResting uint8 = 12
	// StMineResultFailed 失败
	StMineResultFailed uint8 = 13
	// StMineResultMountainLevelUnlock 矿脉等级未解锁
	StMineResultMountainLevelUnlock uint8 = 16
	// StMineResultRobCD 抢夺冷却中
	StMineResultRobCD uint8 = 18
	// StMineResultRobCountLimit 抢夺次数已达上限
	StMineResultRobCountLimit uint8 = 19
	// StMineResultAwardHasNotReceive 当前收益尚未领取
	StMineResultAwardHasNotReceive uint8 = 20
	// StMineResultPlayerBeProtected 目标处于保护期
	StMineResultPlayerBeProtected uint8 = 21
	// StMineResultPlayerBeRobbed 目标已被他人抢夺
	StMineResultPlayerBeRobbed uint8 = 22
	// StMineResultPlayerHasLeave 目标已离开矿洞
	StMineResultPlayerHasLeave uint8 = 23
	// StMineResultPlayerBeRecaptured 矿洞已被原主抢回
	StMineResultPlayerBeRecaptured uint8 = 24
	// StMineResultRecaptureOverTime 抢回期限已过
	StMineResultRecaptureOverTime uint8 = 25
	// StMineResultHasNoAward 暂无可领取收益
	StMineResultHasNoAward uint8 = 26
	// StMineResultLackOfIngot 元宝不足 (自动化不触发)
	StMineResultLackOfIngot uint8 = 27
)

// 矿洞状态 (StMineData.hole.status / player_mine_hole_info.status)。
const (
	// StMineHoleEmpty 空矿洞，可直接占领
	StMineHoleEmpty uint8 = 0
	// StMineHoleRob 被其他玩家占据，可抢夺
	StMineHoleRob uint8 = 1
	// StMineHoleBeRobbed 矿洞被夺，可在期限内抢回
	StMineHoleBeRobbed uint8 = 2
	// StMineHoleCanBeRob 可抢夺状态
	StMineHoleCanBeRob uint8 = 4
)

// StMinePlayerHole 表示角色当前占据的矿洞状态 (enter_mine_mountain 回包 16 个字段)。
type StMinePlayerHole struct {
	Status               uint8
	HoleID               uint16
	RobOverTimestamp     int32
	NextAwardTimestamp   int32
	ProtectOverTimestamp int32
	ItemID               int32
	ItemCount            int32
	NeedMinutes          uint16
	ReceiveCount         int32
	IngotRobFlag         uint8
	DaoyuanRobFlag       uint8
	RecaptureTimestamp   int32
	BeRecaptureTimestamp int32
	EnemyRoleID          int32
	EnemyNickname        string
	EnemyServername      string
}

// StMineHole 表示矿山中的单个矿洞 (enter_mine_mountain 回包列表项)。
type StMineHole struct {
	HoleID               uint16
	LuckyFlag            uint8
	Type                 uint8
	Status               uint8
	PlayerID             int32
	NickName             string
	PlayerLevel          uint16
	ServerName           string
	ProtectOverTimestamp int32
	RobOverTimestamp     int32
	ReceiveCount         int32
}

// StMineHolesInfo 表示进入矿山后的完整矿洞信息。
type StMineHolesInfo struct {
	Result           uint8
	SkillLevel       uint8
	SkillValue       int32
	RemainRobCount   uint16
	BuyNeedIngot     uint8
	NextRobTimestamp int32
	PlayerHole       StMinePlayerHole
	Holes            []StMineHole
	FeedbackMult     int32
}

// StMineMountain 表示一条矿脉的容量信息。
type StMineMountain struct {
	// Level 矿脉等级
	Level uint8
	// UsedCount 已占用矿洞数
	UsedCount uint8
	// TotalCount 矿洞总数
	TotalCount uint8
}

// StMineInfo 表示矿山概览。
type StMineInfo struct {
	// Result 结果码
	Result uint8
	// Level 矿脉等级
	Level uint8
	// TotalSkill 矿山熟练度
	TotalSkill int32
	// CurMountain 当前矿脉编号 (0 表示尚未选择)
	CurMountain uint8
	// Mountains 各矿脉容量列表
	Mountains []StMineMountain
}

// BuildStMineInfoPacket 构造查询矿山信息封包 (0x01080000)。
func BuildStMineInfoPacket() *Packet {
	return NewPacket(ActionIDStMineInfo, nil)
}

// BuildStMineEnterPacket 构造进入矿山封包 (0x01080001)。
func BuildStMineEnterPacket(mountain uint8) *Packet {
	return NewPacket(ActionIDStMineEnter, []byte{mountain})
}

// BuildStMineReceiveAwardPacket 构造领取收益封包 (0x01080007)。
func BuildStMineReceiveAwardPacket() *Packet {
	return NewPacket(ActionIDStMineReceiveAward, nil)
}

// BuildStMineLeavePacket 构造离开矿山封包 (0x01080002)。
func BuildStMineLeavePacket() *Packet {
	return NewPacket(ActionIDStMineLeave, nil)
}

// ParseStMineInfoResult 解析矿山概览。
func ParseStMineInfoResult(payload []byte) (*StMineInfo, error) {
	r := NewReader(payload)
	res := &StMineInfo{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read st mine result: %v", ErrInvalidPayload, err)
	}
	if res.Level, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read st mine level: %v", ErrInvalidPayload, err)
	}
	if res.TotalSkill, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read st mine total_skill: %v", ErrInvalidPayload, err)
	}
	if res.CurMountain, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read st mine cur_mountain: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read st mine mountain list count: %v", ErrInvalidPayload, err)
	}
	res.Mountains = make([]StMineMountain, 0, count)
	for i := 0; i < int(count); i++ {
		var m StMineMountain
		if m.Level, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read st mine mountain[%d].level: %v", ErrInvalidPayload, i, err)
		}
		if m.UsedCount, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read st mine mountain[%d].used: %v", ErrInvalidPayload, i, err)
		}
		if m.TotalCount, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read st mine mountain[%d].total: %v", ErrInvalidPayload, i, err)
		}
		res.Mountains = append(res.Mountains, m)
	}
	// lucky_open_flag / lucky_flag 不消费。
	return res, nil
}

// ParseStMineActionResult 解析进入/领取/离开的结果码 (首字节)。
func ParseStMineActionResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty st mine result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// BuildStMineInfoResultPacket 构造矿山概览回包，供离线 mock 与单测使用。
func BuildStMineInfoResultPacket(info StMineInfo) *Packet {
	w := NewWriter()
	w.WriteUint8(info.Result)
	w.WriteUint8(info.Level)
	w.WriteInt32(0)
	w.WriteUint8(info.CurMountain)
	w.WriteUint16(0)
	w.WriteUint8(0)
	w.WriteUint8(0)
	return NewPacket(ActionIDStMineInfo, w.Bytes())
}

// BuildStMineActionResultPacket 构造矿山动作回包，供离线 mock 与单测使用。
func BuildStMineActionResultPacket(actionID uint32, result uint8) *Packet {
	return NewPacket(actionID, []byte{result})
}

// BuildStMineRobHolePacket 构造抢夺/占领矿洞封包 (264/3)。
//
// 官方描述符 request=[Short hole_id, Int robbed_player_id]；空矿洞传 0。
func BuildStMineRobHolePacket(holeID uint16, robbedPlayerID int32) *Packet {
	w := NewWriter()
	w.WriteUint16(holeID)
	w.WriteInt32(robbedPlayerID)
	return NewPacket(ActionIDStMineRobHole, w.Bytes())
}

// BuildStMineLeaveHolePacket 构造放弃当前矿洞封包 (264/4)。
func BuildStMineLeaveHolePacket() *Packet {
	return NewPacket(ActionIDStMineLeaveHole, nil)
}

// BuildStMineRecapturePacket 构造抢回被夺矿洞封包 (264/5)。
func BuildStMineRecapturePacket() *Packet {
	return NewPacket(ActionIDStMineRecapture, nil)
}

// ParseStMineEnterResponse 解析进入矿山回包 (264/1)。
//
// 字段顺序严格对齐 Mod_StMine_Base.enter_mine_mountain.response：
//
//	UByte result, Byte skill_level, Int skill_value, Short remain_rob_count,
//	Byte buy_need_ingot, Int next_rob_timestamp,
//	16× player_mine_hole_info, [hole 列表], Int feedback_mult
func ParseStMineEnterResponse(payload []byte) (*StMineHolesInfo, error) {
	p := DecompressPayloadIfNeeded(payload)
	r := NewReader(p)
	res := &StMineHolesInfo{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: st mine enter result: %v", ErrInvalidPayload, err)
	}
	if res.SkillLevel, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: st mine skill level: %v", ErrInvalidPayload, err)
	}
	if res.SkillValue, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: st mine skill value: %v", ErrInvalidPayload, err)
	}
	if res.RemainRobCount, err = r.ReadUint16(); err != nil {
		return nil, fmt.Errorf("%w: st mine remain rob count: %v", ErrInvalidPayload, err)
	}
	if res.BuyNeedIngot, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: st mine buy need ingot: %v", ErrInvalidPayload, err)
	}
	if res.NextRobTimestamp, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: st mine next rob timestamp: %v", ErrInvalidPayload, err)
	}
	h := &res.PlayerHole
	reads := []struct {
		name string
		fn   func() error
	}{
		{"status", func() error { var e error; h.Status, e = r.ReadUint8(); return e }},
		{"hole_id", func() error { var e error; h.HoleID, e = r.ReadUint16(); return e }},
		{"rob_over_timestamp", func() error { var e error; h.RobOverTimestamp, e = r.ReadInt32(); return e }},
		{"next_award_timestamp", func() error { var e error; h.NextAwardTimestamp, e = r.ReadInt32(); return e }},
		{"protect_over_timestamp", func() error { var e error; h.ProtectOverTimestamp, e = r.ReadInt32(); return e }},
		{"item_id", func() error { var e error; h.ItemID, e = r.ReadInt32(); return e }},
		{"item_count", func() error { var e error; h.ItemCount, e = r.ReadInt32(); return e }},
		{"need_minutes", func() error { var e error; h.NeedMinutes, e = r.ReadUint16(); return e }},
		{"receive_count", func() error { var e error; h.ReceiveCount, e = r.ReadInt32(); return e }},
		{"ingot_rob_flag", func() error { var e error; h.IngotRobFlag, e = r.ReadUint8(); return e }},
		{"daoyuan_rob_flag", func() error { var e error; h.DaoyuanRobFlag, e = r.ReadUint8(); return e }},
		{"recapture_timestamp", func() error { var e error; h.RecaptureTimestamp, e = r.ReadInt32(); return e }},
		{"be_recapture_timestamp", func() error { var e error; h.BeRecaptureTimestamp, e = r.ReadInt32(); return e }},
		{"enemy_role_id", func() error { var e error; h.EnemyRoleID, e = r.ReadInt32(); return e }},
		{"enemy_nickname", func() error { var e error; h.EnemyNickname, e = r.ReadString(); return e }},
		{"enemy_servername", func() error { var e error; h.EnemyServername, e = r.ReadString(); return e }},
	}
	for _, f := range reads {
		if err = f.fn(); err != nil {
			return nil, fmt.Errorf("%w: st mine player hole %s: %v", ErrInvalidPayload, f.name, err)
		}
	}

	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: st mine hole count: %v", ErrInvalidPayload, err)
	}
	res.Holes = make([]StMineHole, 0, count)
	for i := 0; i < int(count); i++ {
		var hole StMineHole
		step := []struct {
			name string
			fn   func() error
		}{
			{"hole_id", func() error { var e error; hole.HoleID, e = r.ReadUint16(); return e }},
			{"lucky_flag", func() error { var e error; hole.LuckyFlag, e = r.ReadUint8(); return e }},
			{"type", func() error { var e error; hole.Type, e = r.ReadUint8(); return e }},
			{"status", func() error { var e error; hole.Status, e = r.ReadUint8(); return e }},
			{"player_id", func() error { var e error; hole.PlayerID, e = r.ReadInt32(); return e }},
			{"nick_name", func() error { var e error; hole.NickName, e = r.ReadString(); return e }},
			{"player_level", func() error { var e error; hole.PlayerLevel, e = r.ReadUint16(); return e }},
			{"server_name", func() error { var e error; hole.ServerName, e = r.ReadString(); return e }},
			{"protect_over_timestamp", func() error { var e error; hole.ProtectOverTimestamp, e = r.ReadInt32(); return e }},
			{"rob_over_timestamp", func() error { var e error; hole.RobOverTimestamp, e = r.ReadInt32(); return e }},
			{"receive_count", func() error { var e error; hole.ReceiveCount, e = r.ReadInt32(); return e }},
		}
		for _, f := range step {
			if err = f.fn(); err != nil {
				return nil, fmt.Errorf("%w: st mine hole[%d] %s: %v", ErrInvalidPayload, i, f.name, err)
			}
		}
		res.Holes = append(res.Holes, hole)
	}
	if res.FeedbackMult, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: st mine feedback mult: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// StMineResultText 返回仙界矿山结果码文本。
func StMineResultText(code uint8) string {
	switch code {
	case StMineResultSuccess:
		return "成功"
	case StMineResultFunctionUnopen:
		return "功能未开放"
	case StMineResultSuperTownClosed:
		return "超级城镇关闭"
	case StMineResultResting:
		return "休息中"
	case StMineResultFailed:
		return "失败"
	case StMineResultMountainLevelUnlock:
		return "矿脉等级未解锁"
	case StMineResultRobCD:
		return "抢夺冷却中"
	case StMineResultRobCountLimit:
		return "抢夺次数上限"
	case StMineResultAwardHasNotReceive:
		return "当前收益未领取"
	case StMineResultPlayerBeProtected:
		return "目标处于保护期"
	case StMineResultPlayerBeRobbed:
		return "目标已被抢夺"
	case StMineResultPlayerHasLeave:
		return "目标已离开"
	case StMineResultPlayerBeRecaptured:
		return "矿洞已被原主抢回"
	case StMineResultRecaptureOverTime:
		return "抢回期限已过"
	case StMineResultHasNoAward:
		return "暂无可领取收益"
	case StMineResultLackOfIngot:
		return "元宝不足"
	default:
		return "未知结果码"
	}
}

// BuildStMineEnterResultPacket 构造进入矿山回包，供离线 mock 与单测使用。
func BuildStMineEnterResultPacket(info StMineHolesInfo) *Packet {
	w := NewWriter()
	w.WriteUint8(info.Result)
	w.WriteUint8(info.SkillLevel)
	w.WriteInt32(info.SkillValue)
	w.WriteUint16(info.RemainRobCount)
	w.WriteUint8(info.BuyNeedIngot)
	w.WriteInt32(info.NextRobTimestamp)
	ph := info.PlayerHole
	w.WriteUint8(ph.Status)
	w.WriteUint16(ph.HoleID)
	w.WriteInt32(ph.RobOverTimestamp)
	w.WriteInt32(ph.NextAwardTimestamp)
	w.WriteInt32(ph.ProtectOverTimestamp)
	w.WriteInt32(ph.ItemID)
	w.WriteInt32(ph.ItemCount)
	w.WriteUint16(ph.NeedMinutes)
	w.WriteInt32(ph.ReceiveCount)
	w.WriteUint8(ph.IngotRobFlag)
	w.WriteUint8(ph.DaoyuanRobFlag)
	w.WriteInt32(ph.RecaptureTimestamp)
	w.WriteInt32(ph.BeRecaptureTimestamp)
	w.WriteInt32(ph.EnemyRoleID)
	w.WriteString(ph.EnemyNickname)
	w.WriteString(ph.EnemyServername)
	w.WriteUint16(uint16(len(info.Holes)))
	for _, h := range info.Holes {
		w.WriteUint16(h.HoleID)
		w.WriteUint8(h.LuckyFlag)
		w.WriteUint8(h.Type)
		w.WriteUint8(h.Status)
		w.WriteInt32(h.PlayerID)
		w.WriteString(h.NickName)
		w.WriteUint16(h.PlayerLevel)
		w.WriteString(h.ServerName)
		w.WriteInt32(h.ProtectOverTimestamp)
		w.WriteInt32(h.RobOverTimestamp)
		w.WriteInt32(h.ReceiveCount)
	}
	w.WriteInt32(info.FeedbackMult)
	return NewPacket(ActionIDStMineEnter, w.Bytes())
}

// BuildStMineRobHoleResultPacket 构造抢夺矿洞回包，供离线 mock 与单测使用。
func BuildStMineRobHoleResultPacket(result uint8) *Packet {
	return NewPacket(ActionIDStMineRobHole, []byte{result})
}
