// Package protocol 提供魔界远征 (Mod_DevilExpedition_Base, Module 429 / 0x1AD) 协议编解码。
//
// 协议基准: 2026-11-14 官方客户端抓包 /tmp/mojieyuanzheng.pcapng 与
// /home/tongchen/rea-sxd/proto_full/scripts/com/protocols/Mod_DevilExpedition_Base.as。
//
// 单人远征链路:
//
//	activity_status(1) → open_match_panel(2) → start_expedition_single(63)
//	→ enter_town(13) → get_mission_data_m5/6/7(57/68/74)
//	→ move_to(17) → fight_monster_m5/6/7(58/69/75) → quit(29)
//
// 所有请求尾部带 4 字节 PrevAct；构建时写入占位并由会话层覆盖。
package protocol

import "fmt"

const (
	// ActionIDDevilActivityStatus 活动状态 (429/1)
	ActionIDDevilActivityStatus uint32 = 0x01AD0001
	// ActionIDDevilMatchPanel 匹配/签到面板 (429/2)
	ActionIDDevilMatchPanel uint32 = 0x01AD0002
	// ActionIDDevilCloseMatchPanel 关闭匹配面板 (429/3)
	ActionIDDevilCloseMatchPanel uint32 = 0x01AD0003
	// ActionIDDevilNotifyEnterMission 服务端下发进入关卡 (429/12)
	ActionIDDevilNotifyEnterMission uint32 = 0x01AD000C
	// ActionIDDevilEnterTown 进入关卡城镇 (429/13)
	ActionIDDevilEnterTown uint32 = 0x01AD000D
	// ActionIDDevilMoveTo 移动 (429/17)
	ActionIDDevilMoveTo uint32 = 0x01AD0011
	// ActionIDDevilGetPlayers 关卡玩家列表 (429/19)
	ActionIDDevilGetPlayers uint32 = 0x01AD0013
	// ActionIDDevilGetDeployInfo 布阵信息 (429/20)
	ActionIDDevilGetDeployInfo uint32 = 0x01AD0014
	// ActionIDDevilGetTeamStatus 队伍/远征状态 (429/28)
	ActionIDDevilGetTeamStatus uint32 = 0x01AD001C
	// ActionIDDevilQuit 退出本轮远征 (429/29)
	ActionIDDevilQuit uint32 = 0x01AD001D
	// ActionIDDevilGetMissionDataM5 魔神伙伴关卡数据 (429/57)
	ActionIDDevilGetMissionDataM5 uint32 = 0x01AD0039
	// ActionIDDevilFightMonsterM5 魔神伙伴战斗 (429/58)
	ActionIDDevilFightMonsterM5 uint32 = 0x01AD003A
	// ActionIDDevilStartExpeditionSingle 单人远征 (429/63)
	ActionIDDevilStartExpeditionSingle uint32 = 0x01AD003F
	// ActionIDDevilRejectBuyBox 拒绝购买宝箱 (429/66)
	ActionIDDevilRejectBuyBox uint32 = 0x01AD0042
	// ActionIDDevilGetMissionDataM6 双生伙伴关卡数据 (429/68)
	ActionIDDevilGetMissionDataM6 uint32 = 0x01AD0044
	// ActionIDDevilFightMonsterM6 双生伙伴战斗 (429/69)
	ActionIDDevilFightMonsterM6 uint32 = 0x01AD0045
	// ActionIDDevilGetSkipWarInfo 跳过战斗信息 (429/73)
	ActionIDDevilGetSkipWarInfo uint32 = 0x01AD0049
	// ActionIDDevilGetMissionDataM7 隐仙伙伴关卡数据 (429/74)
	ActionIDDevilGetMissionDataM7 uint32 = 0x01AD004A
	// ActionIDDevilFightMonsterM7 隐仙伙伴战斗 (429/75)
	ActionIDDevilFightMonsterM7 uint32 = 0x01AD004B
)

// 活动状态 (activity_status 回包)。
const (
	DevilActivityOpen  uint8 = 0 // DE_OPEN
	DevilActivityClose uint8 = 1 // DE_CLOSE
)

// 单人远征类型 (start_expedition_single 首字节)。
const (
	// DevilMissionYinYang 双生伙伴 (阴阳殿)
	DevilMissionYinYang uint8 = 68
	// DevilMissionMonsterGod 魔神伙伴
	DevilMissionMonsterGod uint8 = 67
	// DevilMissionHermit 隐仙伙伴
	DevilMissionHermit uint8 = 69
)

// 关键结果码 (Mod_DevilExpedition_Base)。
const (
	DevilResultSuccess         uint8 = 2
	DevilResultFailed          uint8 = 3
	DevilResultNotEnoughItem   uint8 = 10
	DevilResultBossDead        uint8 = 11
	DevilResultItemPackFull    uint8 = 65
	DevilResultAlreadyFight    uint8 = 76
	DevilResultNoLife          uint8 = 78
	DevilResultMonsterTeamDead uint8 = 79
	DevilResultNoTimes         uint8 = 81
)

// DevilSoul 表示匹配面板中一条伙伴魔灵的本周进度。
type DevilSoul struct {
	RoleID      int32
	TotalSoul   int32
	WeekSoul    int32
	WeekMaxSoul int32
}

// DevilMatchPanel 表示魔界远征匹配面板 (429/2)。
type DevilMatchPanel struct {
	Result        uint8
	IsComplete    uint8
	LevelSection  int32
	TumoLingNum   int32
	LeftJoin      int32
	Souls         []DevilSoul
	TotalStone    int32
	WeekStone     int32
	WeekStoneMax  int32
	TotalStone2   int32
	WeekStone2    int32
	WeekStone2Max int32
}

// DevilRefine 表示关卡炼化进度。
//
// M5 为 [role_id, get_soul, current_bout, need_bout, week_soul, max_week_soul]；
// M6/M7 为 [get_stone, current_bout, need_bout, week_stone, max_week_stone]。
type DevilRefine struct {
	RoleID      int32
	Got         int32
	CurrentBout int32
	NeedBout    int32
	Week        int32
	WeekMax     int32
}

// DevilMissionData 表示 get_mission_data_m5/6/7 的关键字段。
type DevilMissionData struct {
	Result   uint8
	Refines  []DevilRefine
	TeamIDs  []int32
	BoxCount int
}

func devilPrevActPacket(actionID uint32, prefix []byte) *Packet {
	payload := make([]byte, 0, len(prefix)+4)
	payload = append(payload, prefix...)
	payload = append(payload, 0, 0, 0, 0)
	pkt := NewPacket(actionID, payload)
	pkt.HasPrevAct = true
	return pkt
}

// BuildDevilActivityStatusPacket 构造活动状态查询 (429/1)。
func BuildDevilActivityStatusPacket() *Packet {
	return devilPrevActPacket(ActionIDDevilActivityStatus, nil)
}

// BuildDevilMatchPanelPacket 构造匹配面板查询 (429/2)。
func BuildDevilMatchPanelPacket() *Packet {
	return devilPrevActPacket(ActionIDDevilMatchPanel, nil)
}

// BuildDevilStartSinglePacket 构造单人远征 (429/63)：[Byte mission_type]。
func BuildDevilStartSinglePacket(missionType uint8) *Packet {
	return devilPrevActPacket(ActionIDDevilStartExpeditionSingle, []byte{missionType})
}

// BuildDevilEnterTownPacket 构造进入关卡城镇 (429/13)：[Int town_id]。
func BuildDevilEnterTownPacket(townID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(townID)
	return devilPrevActPacket(ActionIDDevilEnterTown, w.Bytes())
}

// BuildDevilMoveToPacket 构造移动 (429/17)：[Short fromX][Short fromY][Short toX][Short toY]。
func BuildDevilMoveToPacket(fromX, fromY, toX, toY uint16) *Packet {
	w := NewWriter()
	w.WriteUint16(fromX)
	w.WriteUint16(fromY)
	w.WriteUint16(toX)
	w.WriteUint16(toY)
	return devilPrevActPacket(ActionIDDevilMoveTo, w.Bytes())
}

// BuildDevilCloseMatchPanelPacket 构造关闭匹配面板 (429/3)。
func BuildDevilCloseMatchPanelPacket() *Packet {
	return devilPrevActPacket(ActionIDDevilCloseMatchPanel, nil)
}

// BuildDevilGetSkipWarInfoPacket 构造跳过战斗信息查询 (429/73)。
func BuildDevilGetSkipWarInfoPacket() *Packet {
	return devilPrevActPacket(ActionIDDevilGetSkipWarInfo, nil)
}

// BuildDevilGetDeployInfoPacket 构造布阵信息查询 (429/20)。
func BuildDevilGetDeployInfoPacket() *Packet {
	return devilPrevActPacket(ActionIDDevilGetDeployInfo, nil)
}

// BuildDevilGetPlayersPacket 构造关卡玩家列表查询 (429/19)。
func BuildDevilGetPlayersPacket() *Packet {
	return devilPrevActPacket(ActionIDDevilGetPlayers, nil)
}

// BuildDevilGetTeamStatusPacket 构造队伍/远征状态查询 (429/28)。
func BuildDevilGetTeamStatusPacket() *Packet {
	return devilPrevActPacket(ActionIDDevilGetTeamStatus, nil)
}

// BuildDevilQuitPacket 构造退出本轮远征 (429/29)。
func BuildDevilQuitPacket() *Packet {
	return devilPrevActPacket(ActionIDDevilQuit, nil)
}

// BuildDevilRejectBuyBoxPacket 构造拒绝购买宝箱 (429/66)。
func BuildDevilRejectBuyBoxPacket() *Packet {
	return devilPrevActPacket(ActionIDDevilRejectBuyBox, nil)
}

// DevilMissionDataAction 返回指定伙伴类型对应的关卡数据动作。
func DevilMissionDataAction(missionType uint8) uint32 {
	switch missionType {
	case DevilMissionMonsterGod:
		return ActionIDDevilGetMissionDataM5
	case DevilMissionHermit:
		return ActionIDDevilGetMissionDataM7
	default:
		return ActionIDDevilGetMissionDataM6
	}
}

// DevilFightAction 返回指定伙伴类型对应的战斗动作。
func DevilFightAction(missionType uint8) uint32 {
	switch missionType {
	case DevilMissionMonsterGod:
		return ActionIDDevilFightMonsterM5
	case DevilMissionHermit:
		return ActionIDDevilFightMonsterM7
	default:
		return ActionIDDevilFightMonsterM6
	}
}

// DevilMissionName 返回伙伴类型名称。
func DevilMissionName(missionType uint8) string {
	switch missionType {
	case DevilMissionMonsterGod:
		return "魔神伙伴"
	case DevilMissionHermit:
		return "隐仙伙伴"
	default:
		return "双生伙伴"
	}
}

// BuildDevilFightMonsterPacket 构造战斗请求 (429/58|69|75)：[Int monster_team_id]。
func BuildDevilFightMonsterPacket(missionType uint8, teamID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(teamID)
	return devilPrevActPacket(DevilFightAction(missionType), w.Bytes())
}

// BuildDevilGetMissionDataPacket 构造关卡数据查询 (429/57|68|74)。
func BuildDevilGetMissionDataPacket(missionType uint8) *Packet {
	return devilPrevActPacket(DevilMissionDataAction(missionType), nil)
}

// ParseDevilActivityStatus 解析活动状态 (429/1)：首字节 DE_OPEN=0 / DE_CLOSE=1。
func ParseDevilActivityStatus(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty devil activity status", ErrInvalidPayload)
	}
	return payload[0], nil
}

// ParseDevilResult 解析单字节结果码回包。
func ParseDevilResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty devil result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// ParseDevilMatchPanel 解析 429/2 匹配面板。
func ParseDevilMatchPanel(payload []byte) (*DevilMatchPanel, error) {
	p := DecompressPayloadIfNeeded(payload)
	r := NewReader(p)
	out := &DevilMatchPanel{}
	var err error
	if out.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: devil panel result: %v", ErrInvalidPayload, err)
	}
	if out.IsComplete, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: devil panel is_complete: %v", ErrInvalidPayload, err)
	}
	if _, err = r.ReadInt32(); err != nil { // boss_id
		return nil, fmt.Errorf("%w: devil panel boss_id: %v", ErrInvalidPayload, err)
	}
	if _, err = r.ReadInt64(); err != nil { // max_health
		return nil, fmt.Errorf("%w: devil panel max_health: %v", ErrInvalidPayload, err)
	}
	if _, err = r.ReadInt64(); err != nil { // left_health
		return nil, fmt.Errorf("%w: devil panel left_health: %v", ErrInvalidPayload, err)
	}
	if out.LevelSection, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: devil panel level_section: %v", ErrInvalidPayload, err)
	}
	if _, err = r.ReadInt64(); err != nil { // total_hurt
		return nil, fmt.Errorf("%w: devil panel total_hurt: %v", ErrInvalidPayload, err)
	}
	if _, err = r.ReadInt64(); err != nil { // top_hurt
		return nil, fmt.Errorf("%w: devil panel top_hurt: %v", ErrInvalidPayload, err)
	}
	if _, err = r.ReadInt32(); err != nil { // total_times
		return nil, fmt.Errorf("%w: devil panel total_times: %v", ErrInvalidPayload, err)
	}
	if out.TumoLingNum, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: devil panel tumoling: %v", ErrInvalidPayload, err)
	}
	if out.LeftJoin, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: devil panel left_join: %v", ErrInvalidPayload, err)
	}
	// top_bout / total_bout
	if _, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	if _, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	soulCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: devil panel soul count: %v", ErrInvalidPayload, err)
	}
	out.Souls = make([]DevilSoul, 0, soulCount)
	for i := 0; i < int(soulCount); i++ {
		var s DevilSoul
		if s.RoleID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: devil soul[%d] role: %v", ErrInvalidPayload, i, err)
		}
		if s.TotalSoul, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		if s.WeekSoul, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		if s.WeekMaxSoul, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		out.Souls = append(out.Souls, s)
	}
	// team_arr：跳过 [Int,String,String,String,String,Int,Int,Int,Byte]
	teamCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: devil panel team count: %v", ErrInvalidPayload, err)
	}
	for i := 0; i < int(teamCount); i++ {
		if _, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		for j := 0; j < 4; j++ {
			if _, err = r.ReadString(); err != nil {
				return nil, err
			}
		}
		for j := 0; j < 3; j++ {
			if _, err = r.ReadInt32(); err != nil {
				return nil, err
			}
		}
		if _, err = r.ReadUint8(); err != nil {
			return nil, err
		}
	}
	if _, err = r.ReadUint8(); err != nil { // is_unlock
		return nil, err
	}
	if _, err = r.ReadInt32(); err != nil { // fast_times
		return nil, err
	}
	if _, err = r.ReadUint8(); err != nil { // fast_need_soul
		return nil, err
	}
	if _, err = r.ReadUint8(); err != nil { // qijue_open
		return nil, err
	}
	if out.TotalStone, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	if out.WeekStone, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	if out.WeekStoneMax, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	if out.TotalStone2, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	if out.WeekStone2, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	if out.WeekStone2Max, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	return out, nil
}

// ParseDevilMissionData 解析 429/57|68|74 关卡数据的关键字段。
func ParseDevilMissionData(missionType uint8, payload []byte) (*DevilMissionData, error) {
	p := DecompressPayloadIfNeeded(payload)
	r := NewReader(p)
	out := &DevilMissionData{}
	var err error
	if out.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: devil mission result: %v", ErrInvalidPayload, err)
	}
	// members：20 字段
	memberCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: devil members count: %v", ErrInvalidPayload, err)
	}
	for i := 0; i < int(memberCount); i++ {
		if err = skipDevilMember(r); err != nil {
			return nil, fmt.Errorf("%w: devil member[%d]: %v", ErrInvalidPayload, i, err)
		}
	}
	// teams：[Int id, Int monster_id]
	teamCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: devil teams count: %v", ErrInvalidPayload, err)
	}
	out.TeamIDs = make([]int32, 0, teamCount)
	for i := 0; i < int(teamCount); i++ {
		id, e := r.ReadInt32()
		if e != nil {
			return nil, fmt.Errorf("%w: devil team[%d] id: %v", ErrInvalidPayload, i, e)
		}
		if _, e = r.ReadInt32(); e != nil {
			return nil, fmt.Errorf("%w: devil team[%d] monster: %v", ErrInvalidPayload, i, e)
		}
		out.TeamIDs = append(out.TeamIDs, id)
	}
	// records：[UByte log_type, [Long] var_list]
	recordCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: devil records count: %v", ErrInvalidPayload, err)
	}
	for i := 0; i < int(recordCount); i++ {
		if _, err = r.ReadUint8(); err != nil {
			return nil, err
		}
		n, e := r.ReadUint16()
		if e != nil {
			return nil, e
		}
		for j := 0; j < int(n); j++ {
			if _, err = r.ReadInt64(); err != nil {
				return nil, err
			}
		}
	}
	// refines：M5 六字段，M6/M7 五字段
	refineCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: devil refine count: %v", ErrInvalidPayload, err)
	}
	out.Refines = make([]DevilRefine, 0, refineCount)
	for i := 0; i < int(refineCount); i++ {
		var ref DevilRefine
		if missionType == DevilMissionMonsterGod {
			if ref.RoleID, err = r.ReadInt32(); err != nil {
				return nil, err
			}
			if ref.Got, err = r.ReadInt32(); err != nil {
				return nil, err
			}
		} else {
			if ref.Got, err = r.ReadInt32(); err != nil {
				return nil, err
			}
		}
		if ref.CurrentBout, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		if ref.NeedBout, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		if ref.Week, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		if ref.WeekMax, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		out.Refines = append(out.Refines, ref)
	}
	// 尾部：level_section / team_life / (show_buy_tips) / boxes
	if _, err = r.ReadInt32(); err != nil { // level_section
		return nil, err
	}
	if _, err = r.ReadInt32(); err != nil { // team_life
		return nil, err
	}
	if _, err = r.ReadInt32(); err != nil { // show_buy_tips（三类关卡均为 Int）
		return nil, err
	}
	boxCount, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	out.BoxCount = int(boxCount)
	return out, nil
}

func skipDevilMember(r *Reader) error {
	// player_id, is_leader, level, nickname, life, head_id, is_leave, role_id,
	// revive_time, mission_id, area_id, boss_life, damage(Long), isComplete,
	// inspire_daoyuan, inspire_ingot, up_power(Long), has_shield, bout, is_substitute
	if _, err := r.ReadInt32(); err != nil {
		return err
	}
	if _, err := r.ReadUint8(); err != nil {
		return err
	}
	if _, err := r.ReadInt32(); err != nil {
		return err
	}
	if _, err := r.ReadString(); err != nil {
		return err
	}
	if _, err := r.ReadUint8(); err != nil {
		return err
	}
	if _, err := r.ReadInt32(); err != nil {
		return err
	}
	if _, err := r.ReadUint8(); err != nil {
		return err
	}
	for i := 0; i < 5; i++ { // role_id, revive_time, mission_id, area_id, boss_life
		if _, err := r.ReadInt32(); err != nil {
			return err
		}
	}
	if _, err := r.ReadInt64(); err != nil { // damage
		return err
	}
	for i := 0; i < 3; i++ { // isComplete, inspire_daoyuan, inspire_ingot
		if _, err := r.ReadUint8(); err != nil {
			return err
		}
	}
	if _, err := r.ReadInt64(); err != nil { // up_power
		return err
	}
	if _, err := r.ReadUint8(); err != nil { // has_shield
		return err
	}
	if _, err := r.ReadInt32(); err != nil { // bout
		return err
	}
	if _, err := r.ReadUint8(); err != nil { // is_substitute
		return err
	}
	return nil
}

// DevilResultText 返回魔界远征结果码文本。
func DevilResultText(code uint8) string {
	switch code {
	case DevilActivityOpen:
		return "活动开放"
	case DevilActivityClose:
		return "活动关闭"
	case DevilResultSuccess:
		return "成功"
	case DevilResultFailed:
		return "失败"
	case DevilResultNotEnoughItem:
		return "屠魔令不足"
	case DevilResultBossDead:
		return "Boss 已被击杀"
	case DevilResultItemPackFull:
		return "背包已满"
	case DevilResultAlreadyFight:
		return "已战斗过"
	case DevilResultNoLife:
		return "没有生命次数"
	case DevilResultMonsterTeamDead:
		return "怪物队伍已被击败"
	case DevilResultNoTimes:
		return "次数不足"
	default:
		return "未知结果码"
	}
}

// BuildDevilMatchPanelResultPacket 构造匹配面板回包，供离线 mock 与单测使用。
func BuildDevilMatchPanelResultPacket(p DevilMatchPanel) *Packet {
	w := NewWriter()
	w.WriteUint8(p.Result)
	w.WriteUint8(p.IsComplete)
	w.WriteInt32(0) // boss_id
	w.WriteInt64(0) // max_health
	w.WriteInt64(0) // left_health
	w.WriteInt32(p.LevelSection)
	w.WriteInt64(0) // total_hurt
	w.WriteInt64(0) // top_hurt
	w.WriteInt32(0) // total_times
	w.WriteInt32(p.TumoLingNum)
	w.WriteInt32(p.LeftJoin)
	w.WriteInt32(0) // top_bout
	w.WriteInt32(0) // total_bout
	w.WriteUint16(uint16(len(p.Souls)))
	for _, s := range p.Souls {
		w.WriteInt32(s.RoleID)
		w.WriteInt32(s.TotalSoul)
		w.WriteInt32(s.WeekSoul)
		w.WriteInt32(s.WeekMaxSoul)
	}
	w.WriteUint16(0) // team_arr
	w.WriteUint8(1)  // is_unlock
	w.WriteInt32(0)  // fast_times
	w.WriteUint8(0)  // fast_need_soul
	w.WriteUint8(0)  // qijue_open
	w.WriteInt32(p.TotalStone)
	w.WriteInt32(p.WeekStone)
	w.WriteInt32(p.WeekStoneMax)
	w.WriteInt32(p.TotalStone2)
	w.WriteInt32(p.WeekStone2)
	w.WriteInt32(p.WeekStone2Max)
	return NewPacket(ActionIDDevilMatchPanel, w.Bytes())
}

// BuildDevilMissionDataResultPacket 构造关卡数据回包，供离线 mock 与单测使用。
func BuildDevilMissionDataResultPacket(missionType uint8, result uint8, teams []int32, refines []DevilRefine) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	w.WriteUint16(0) // members
	w.WriteUint16(uint16(len(teams)))
	for _, id := range teams {
		w.WriteInt32(id)
		w.WriteInt32(0)
	}
	w.WriteUint16(0) // records
	w.WriteUint16(uint16(len(refines)))
	for _, r := range refines {
		if missionType == DevilMissionMonsterGod {
			w.WriteInt32(r.RoleID)
			w.WriteInt32(r.Got)
		} else {
			w.WriteInt32(r.Got)
		}
		w.WriteInt32(r.CurrentBout)
		w.WriteInt32(r.NeedBout)
		w.WriteInt32(r.Week)
		w.WriteInt32(r.WeekMax)
	}
	w.WriteInt32(0)  // level_section
	w.WriteInt32(0)  // team_life
	w.WriteInt32(0)  // show_buy_tips
	w.WriteUint16(0) // boxes
	return NewPacket(DevilMissionDataAction(missionType), w.Bytes())
}
