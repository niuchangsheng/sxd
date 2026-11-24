// 本文件实现官方界渊之战 (Mod_AbyssClash_Base, Module 818 / 0x0332) 协议。
//
// 依据官方 Main.swf 反编译的 com.protocols.Mod_AbyssClash_Base、
// com.datas.AbyssClashData 与 2026-11-24 抓包 /tmp/jieyuanzhizhan.pcapng：
//
//	is_func_open      Act 0  -> 0x03320000  [Int open_flag][Int step][Int super_town_server_id]
//	step1_panel_info  Act 1  -> 0x03320001  第一阶段面板 (3 个怪物/Boss 与剩余挑战次数)
//	step2_panel_info  Act 2  -> 0x03320002  第二阶段面板 (含 is_boss/boss_can_fight_num)
//	get_monster_team_info Act 3 -> 0x03320003 [Int step_monster_id]
//	start_fight       Act 4  -> 0x03320004  [Int step_monster_id] -> 战报
//	award_info        Act 5  -> 0x03320005
//	fight_rank        Act 6  -> 0x03320006
//	score_store_info  Act 7  -> 0x03320007
//	score_exchange    Act 8  -> 0x03320008
//	get_st_top3_list  Act 9  -> 0x03320009
//	notify_client_func_open Act 10 -> 0x0332000A
//
// 面板/挑战请求走 SOCKETSERVERTOWN（抓包为 8170...:8219 跨服会话），
// is_func_open 走主服会话；所有请求尾部带 4 字节 PrevAct。
package protocol

import (
	"encoding/binary"
	"fmt"
)

const (
	// ModuleAbyssClash 界渊之战模块编号 (0x0332)
	ModuleAbyssClash uint16 = 818
)

// 界渊之战动作编号。
const (
	ActionIDAbyssClashFuncOpen      uint32 = 0x03320000
	ActionIDAbyssClashStep1Panel    uint32 = 0x03320001
	ActionIDAbyssClashStep2Panel    uint32 = 0x03320002
	ActionIDAbyssClashMonsterTeam   uint32 = 0x03320003
	ActionIDAbyssClashStartFight    uint32 = 0x03320004
	ActionIDAbyssClashAwardInfo     uint32 = 0x03320005
	ActionIDAbyssClashFightRank     uint32 = 0x03320006
	ActionIDAbyssClashScoreStore    uint32 = 0x03320007
	ActionIDAbyssClashScoreExchange uint32 = 0x03320008
	ActionIDAbyssClashTop3          uint32 = 0x03320009
	ActionIDAbyssClashNotifyOpen    uint32 = 0x0332000A
)

// 界渊之战结果码 (Mod_AbyssClash_Base)。
const (
	AbyssClashResultSuccess         uint8 = 0
	AbyssClashResultFuncNotOpen     uint8 = 1
	AbyssClashResultWrongStep       uint8 = 2
	AbyssClashResultFailed          uint8 = 3
	AbyssClashResultWrongID         uint8 = 4
	AbyssClashResultNoFightNum      uint8 = 5
	AbyssClashResultBossCanNotFight uint8 = 6
	AbyssClashResultNumLimit        uint8 = 7
	AbyssClashResultNotEnoughScore  uint8 = 8
	AbyssClashResultPackageFull     uint8 = 9
)

// AbyssClashFuncOpen 表示 0x03320000 功能开放状态。
type AbyssClashFuncOpen struct {
	OpenFlag                int32
	Step                    int32
	PlayerSuperTownServerID int32
}

// AbyssClashMonster 表示面板中的一个怪物/Boss。
type AbyssClashMonster struct {
	// StepMonsterID 关卡怪物 ID (挑战请求参数)
	StepMonsterID int32
	// MonsterTeamID 怪物队伍 ID
	MonsterTeamID int32
	// IsBoss 是否为 Boss (仅第二阶段有效)
	IsBoss int32
	// BossCanFightNum Boss 剩余可挑战次数 (仅第二阶段有效)
	BossCanFightNum int32
	// WinNum 已胜利次数
	WinNum int32
	// LastHealth / MaxHealth 剩余/最大血量
	LastHealth int64
	MaxHealth  int64
	// WarSpecialID 战斗特殊规则
	WarSpecialID int32
}

// AbyssClashPanel 表示第一/第二阶段面板。
type AbyssClashPanel struct {
	// Step 面板阶段 (1 或 2)
	Step int32
	// Result 结果码
	Result uint8
	// IsFirstOpen 是否首次开启
	IsFirstOpen int32
	// Score 当前积分
	Score int32
	// FightNum 今日剩余挑战次数
	FightNum int32
	// CostPower 单次挑战消耗体力
	CostPower int32
	// NextStepTS 下一阶段时间戳
	NextStepTS int32
	// PlayerWinNum 玩家胜利次数 (第二阶段)
	PlayerWinNum int32
	// Monsters 怪物/Boss 列表
	Monsters []AbyssClashMonster
	// AwardTimes 领奖次数 (第二阶段)
	AwardTimes int32
	// ZoneID / STTeamID / TotalScore 赛区与总分
	ZoneID     int32
	STTeamID   int32
	TotalScore int32
}

// AbyssClashFightResult 表示 0x03320004 回包中自动化需要的字段。
type AbyssClashFightResult struct {
	// Result 结果码
	Result uint8
	// TotalDamage 本次总伤害
	TotalDamage int64
	// WinFlag 1=胜利, 0=失败
	WinFlag int32
}

func abyssClashPrevPacket(actionID uint32, prefix []byte) *Packet {
	payload := make([]byte, 0, len(prefix)+4)
	payload = append(payload, prefix...)
	payload = append(payload, 0, 0, 0, 0)
	pkt := NewPacket(actionID, payload)
	pkt.HasPrevAct = true
	return pkt
}

// BuildAbyssClashFuncOpenPacket 构造功能开放查询 (0x03320000)。
func BuildAbyssClashFuncOpenPacket() *Packet {
	return abyssClashPrevPacket(ActionIDAbyssClashFuncOpen, nil)
}

// BuildAbyssClashStep1PanelPacket 构造第一阶段面板请求 (0x03320001)。
func BuildAbyssClashStep1PanelPacket() *Packet {
	return abyssClashPrevPacket(ActionIDAbyssClashStep1Panel, nil)
}

// BuildAbyssClashStep2PanelPacket 构造第二阶段面板请求 (0x03320002)。
func BuildAbyssClashStep2PanelPacket() *Packet {
	return abyssClashPrevPacket(ActionIDAbyssClashStep2Panel, nil)
}

// BuildAbyssClashMonsterTeamPacket 构造怪物队伍信息请求 (0x03320003)。
func BuildAbyssClashMonsterTeamPacket(stepMonsterID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(stepMonsterID)
	return abyssClashPrevPacket(ActionIDAbyssClashMonsterTeam, w.Bytes())
}

// BuildAbyssClashStartFightPacket 构造挑战请求 (0x03320004)。
func BuildAbyssClashStartFightPacket(stepMonsterID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(stepMonsterID)
	return abyssClashPrevPacket(ActionIDAbyssClashStartFight, w.Bytes())
}

// ParseAbyssClashFuncOpen 解析 0x03320000 回包。
func ParseAbyssClashFuncOpen(payload []byte) (*AbyssClashFuncOpen, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	out := &AbyssClashFuncOpen{}
	var err error
	if out.OpenFlag, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: abyss clash open_flag: %v", ErrInvalidPayload, err)
	}
	if out.Step, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: abyss clash step: %v", ErrInvalidPayload, err)
	}
	if out.PlayerSuperTownServerID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: abyss clash super_town_server_id: %v", ErrInvalidPayload, err)
	}
	return out, nil
}

// ParseAbyssClashStep1Panel 解析 0x03320001 回包。
func ParseAbyssClashStep1Panel(payload []byte) (*AbyssClashPanel, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	out := &AbyssClashPanel{Step: 1}
	var err error
	if out.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: abyss clash step1 result: %v", ErrInvalidPayload, err)
	}
	if out.Result != AbyssClashResultSuccess {
		return out, nil
	}
	for _, dst := range []*int32{&out.IsFirstOpen, &out.Score, &out.FightNum, &out.CostPower, &out.NextStepTS} {
		if *dst, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: abyss clash step1 int: %v", ErrInvalidPayload, err)
		}
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: abyss clash step1 monster count: %v", ErrInvalidPayload, err)
	}
	for i := 0; i < int(count); i++ {
		var m AbyssClashMonster
		for _, dst := range []*int32{&m.StepMonsterID, &m.MonsterTeamID, &m.WinNum} {
			if *dst, err = r.ReadInt32(); err != nil {
				return nil, fmt.Errorf("%w: abyss clash step1 monster[%d]: %v", ErrInvalidPayload, i, err)
			}
		}
		if m.LastHealth, err = r.ReadInt64(); err != nil {
			return nil, fmt.Errorf("%w: abyss clash step1 monster[%d] last_hp: %v", ErrInvalidPayload, i, err)
		}
		if m.MaxHealth, err = r.ReadInt64(); err != nil {
			return nil, fmt.Errorf("%w: abyss clash step1 monster[%d] max_hp: %v", ErrInvalidPayload, i, err)
		}
		if m.WarSpecialID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: abyss clash step1 monster[%d] special: %v", ErrInvalidPayload, i, err)
		}
		out.Monsters = append(out.Monsters, m)
	}
	// map_player_list 仅为地图展示，读取以推进到赛区字段。
	if err := skipAbyssClashMapPlayers(r); err != nil {
		return nil, err
	}
	for _, dst := range []*int32{&out.ZoneID, &out.STTeamID, &out.TotalScore} {
		if *dst, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: abyss clash step1 tail: %v", ErrInvalidPayload, err)
		}
	}
	return out, nil
}

// ParseAbyssClashStep2Panel 解析 0x03320002 回包。
func ParseAbyssClashStep2Panel(payload []byte) (*AbyssClashPanel, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	out := &AbyssClashPanel{Step: 2}
	var err error
	if out.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: abyss clash step2 result: %v", ErrInvalidPayload, err)
	}
	if out.Result != AbyssClashResultSuccess {
		return out, nil
	}
	for _, dst := range []*int32{&out.IsFirstOpen, &out.Score, &out.FightNum, &out.CostPower, &out.NextStepTS, &out.PlayerWinNum} {
		if *dst, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: abyss clash step2 int: %v", ErrInvalidPayload, err)
		}
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: abyss clash step2 monster count: %v", ErrInvalidPayload, err)
	}
	for i := 0; i < int(count); i++ {
		var m AbyssClashMonster
		for _, dst := range []*int32{&m.StepMonsterID, &m.MonsterTeamID, &m.IsBoss, &m.BossCanFightNum, &m.WinNum} {
			if *dst, err = r.ReadInt32(); err != nil {
				return nil, fmt.Errorf("%w: abyss clash step2 monster[%d]: %v", ErrInvalidPayload, i, err)
			}
		}
		if m.LastHealth, err = r.ReadInt64(); err != nil {
			return nil, fmt.Errorf("%w: abyss clash step2 monster[%d] last_hp: %v", ErrInvalidPayload, i, err)
		}
		if m.MaxHealth, err = r.ReadInt64(); err != nil {
			return nil, fmt.Errorf("%w: abyss clash step2 monster[%d] max_hp: %v", ErrInvalidPayload, i, err)
		}
		out.Monsters = append(out.Monsters, m)
	}
	if out.AwardTimes, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: abyss clash step2 award_times: %v", ErrInvalidPayload, err)
	}
	// boss_top_3: [Int st_player_id][String nickname][String server_name][Int role_id][Int head_id][Int rim_id]
	topCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: abyss clash step2 top3 count: %v", ErrInvalidPayload, err)
	}
	for i := 0; i < int(topCount); i++ {
		if _, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: abyss clash top3[%d]: %v", ErrInvalidPayload, i, err)
		}
		for j := 0; j < 2; j++ {
			if _, err = r.ReadString(); err != nil {
				return nil, fmt.Errorf("%w: abyss clash top3[%d] string: %v", ErrInvalidPayload, i, err)
			}
		}
		for j := 0; j < 3; j++ {
			if _, err = r.ReadInt32(); err != nil {
				return nil, fmt.Errorf("%w: abyss clash top3[%d] int: %v", ErrInvalidPayload, i, err)
			}
		}
	}
	if err := skipAbyssClashMapPlayers(r); err != nil {
		return nil, err
	}
	for _, dst := range []*int32{&out.ZoneID, &out.STTeamID, &out.TotalScore} {
		if *dst, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: abyss clash step2 tail: %v", ErrInvalidPayload, err)
		}
	}
	return out, nil
}

// skipAbyssClashMapPlayers 读取并丢弃 map_player_list:
// [Int st_player_id][String nickname][Int role_id][Int cloth_id][Int mount_id]。
func skipAbyssClashMapPlayers(r *Reader) error {
	count, err := r.ReadUint16()
	if err != nil {
		return fmt.Errorf("%w: abyss clash map player count: %v", ErrInvalidPayload, err)
	}
	for i := 0; i < int(count); i++ {
		if _, err = r.ReadInt32(); err != nil {
			return fmt.Errorf("%w: abyss clash map player[%d]: %v", ErrInvalidPayload, i, err)
		}
		if _, err = r.ReadString(); err != nil {
			return fmt.Errorf("%w: abyss clash map player[%d] name: %v", ErrInvalidPayload, i, err)
		}
		for j := 0; j < 3; j++ {
			if _, err = r.ReadInt32(); err != nil {
				return fmt.Errorf("%w: abyss clash map player[%d] int: %v", ErrInvalidPayload, i, err)
			}
		}
	}
	return nil
}

// ParseAbyssClashMonsterTeamResult 读取 0x03320003 回包首字节结果码。
func ParseAbyssClashMonsterTeamResult(payload []byte) (uint8, error) {
	p := DecompressPayloadIfNeeded(payload)
	if len(p) < 1 {
		return 0, fmt.Errorf("%w: abyss clash monster team result empty", ErrInvalidPayload)
	}
	return p[0], nil
}

// ParseAbyssClashFightResult 解析 0x03320004 回包。
//
// 成功回包结构为 [UByte result][WarData][Long total_damage][Int win_flag]；
// 自动化只取结果码与末尾 12 字节，无需解析完整战报。
func ParseAbyssClashFightResult(payload []byte) (*AbyssClashFightResult, error) {
	p := DecompressPayloadIfNeeded(payload)
	if len(p) < 1 {
		return nil, fmt.Errorf("%w: abyss clash fight result empty", ErrInvalidPayload)
	}
	out := &AbyssClashFightResult{Result: p[0]}
	if out.Result != AbyssClashResultSuccess {
		return out, nil
	}
	if len(p) < 12 {
		return nil, fmt.Errorf("%w: abyss clash fight result too short: %d", ErrInvalidPayload, len(p))
	}
	out.TotalDamage = int64(binary.BigEndian.Uint64(p[len(p)-12 : len(p)-4]))
	out.WinFlag = int32(binary.BigEndian.Uint32(p[len(p)-4:]))
	return out, nil
}

// AbyssClashResultText 返回结果码文本。
func AbyssClashResultText(code uint8) string {
	switch code {
	case AbyssClashResultSuccess:
		return "成功"
	case AbyssClashResultFuncNotOpen:
		return "功能未开放"
	case AbyssClashResultWrongStep:
		return "阶段错误"
	case AbyssClashResultFailed:
		return "失败"
	case AbyssClashResultWrongID:
		return "怪物ID错误"
	case AbyssClashResultNoFightNum:
		return "挑战次数不足"
	case AbyssClashResultBossCanNotFight:
		return "Boss不可挑战"
	case AbyssClashResultNumLimit:
		return "次数上限"
	case AbyssClashResultNotEnoughScore:
		return "积分不足"
	case AbyssClashResultPackageFull:
		return "背包已满"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// BuildAbyssClashFuncOpenResultPacket 构造功能开放回包，供单测使用。
func BuildAbyssClashFuncOpenResultPacket(open *AbyssClashFuncOpen) *Packet {
	w := NewWriter()
	w.WriteInt32(open.OpenFlag)
	w.WriteInt32(open.Step)
	w.WriteInt32(open.PlayerSuperTownServerID)
	return NewPacket(ActionIDAbyssClashFuncOpen, w.Bytes())
}

// BuildAbyssClashStep1PanelResultPacket 构造第一阶段面板回包，供单测使用。
func BuildAbyssClashStep1PanelResultPacket(panel *AbyssClashPanel) *Packet {
	w := NewWriter()
	w.WriteUint8(panel.Result)
	if panel.Result == AbyssClashResultSuccess {
		w.WriteInt32(panel.IsFirstOpen)
		w.WriteInt32(panel.Score)
		w.WriteInt32(panel.FightNum)
		w.WriteInt32(panel.CostPower)
		w.WriteInt32(panel.NextStepTS)
		w.WriteUint16(uint16(len(panel.Monsters)))
		for _, m := range panel.Monsters {
			w.WriteInt32(m.StepMonsterID)
			w.WriteInt32(m.MonsterTeamID)
			w.WriteInt32(m.WinNum)
			w.WriteInt64(m.LastHealth)
			w.WriteInt64(m.MaxHealth)
			w.WriteInt32(m.WarSpecialID)
		}
		w.WriteUint16(0) // map_player_list
		w.WriteInt32(panel.ZoneID)
		w.WriteInt32(panel.STTeamID)
		w.WriteInt32(panel.TotalScore)
	}
	return NewPacket(ActionIDAbyssClashStep1Panel, w.Bytes())
}

// BuildAbyssClashStep2PanelResultPacket 构造第二阶段面板回包，供单测使用。
func BuildAbyssClashStep2PanelResultPacket(panel *AbyssClashPanel) *Packet {
	w := NewWriter()
	w.WriteUint8(panel.Result)
	if panel.Result == AbyssClashResultSuccess {
		w.WriteInt32(panel.IsFirstOpen)
		w.WriteInt32(panel.Score)
		w.WriteInt32(panel.FightNum)
		w.WriteInt32(panel.CostPower)
		w.WriteInt32(panel.NextStepTS)
		w.WriteInt32(panel.PlayerWinNum)
		w.WriteUint16(uint16(len(panel.Monsters)))
		for _, m := range panel.Monsters {
			w.WriteInt32(m.StepMonsterID)
			w.WriteInt32(m.MonsterTeamID)
			w.WriteInt32(m.IsBoss)
			w.WriteInt32(m.BossCanFightNum)
			w.WriteInt32(m.WinNum)
			w.WriteInt64(m.LastHealth)
			w.WriteInt64(m.MaxHealth)
		}
		w.WriteInt32(panel.AwardTimes)
		w.WriteUint16(0) // boss_top_3
		w.WriteUint16(0) // map_player_list
		w.WriteInt32(panel.ZoneID)
		w.WriteInt32(panel.STTeamID)
		w.WriteInt32(panel.TotalScore)
	}
	return NewPacket(ActionIDAbyssClashStep2Panel, w.Bytes())
}

// BuildAbyssClashMonsterTeamResultPacket 构造怪物队伍信息回包，供单测使用。
func BuildAbyssClashMonsterTeamResultPacket(result uint8) *Packet {
	return NewPacket(ActionIDAbyssClashMonsterTeam, []byte{result})
}

// BuildAbyssClashFightResultPacket 构造挑战回包，供单测使用。
func BuildAbyssClashFightResultPacket(result uint8, totalDamage int64, winFlag int32) *Packet {
	if result != AbyssClashResultSuccess {
		return NewPacket(ActionIDAbyssClashStartFight, []byte{result})
	}
	body := make([]byte, 0, 13)
	body = append(body, result)
	body = append(body, make([]byte, 8)...) // 占位 WarData
	var tail [12]byte
	binary.BigEndian.PutUint64(tail[0:8], uint64(totalDamage))
	binary.BigEndian.PutUint32(tail[8:12], uint32(winFlag))
	body = append(body, tail[:]...)
	return NewPacket(ActionIDAbyssClashStartFight, body)
}
