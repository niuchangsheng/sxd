// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现「九空无界」(Mod_NineRegions_Base, Module 58 / 0x003A) 协议编解码。
//
// 官方客户端流程 (com.views.nine_regions.NineRegions2View / NineRegionsLevel2View)：
//
//	open_panel            58/0  response=[Byte is_first, Int cur_jie, Int cur_level, Int hidden_level]
//	check_is_can_challenge 58/20 request=[Int jie, Int level] response=[Byte result]
//	start_challenge       58/1  request=[Int jie, Int level] response=[Byte first, UByte result]
//	create_region_team    58/5  request=[Int jie, Int level] response=[UByte result, Int team_id]
//	get_player_team_info  58/4  response=[Int leader_id, [[Int player_id, String name, String sign, Short, Int, Int, Int, String]]]
//	get_helper_role_list  58/9  response=[[Int role_id, String name, Int lv, Int job_id, UByte state]]
//	choose_helper_role    58/10 request=[Int role_id, Byte slot] response=[UByte result]
//	fight_region          58/13 request=[Int jie, Int level] response=[UByte result, [war_data...], Short, Int, Int, Int, Int, [Int,Byte]]
//	challenge_hidden_level 58/23 request=[Int hidden_level] response=[UByte result, ...war...]
//	get_player_cd_time    58/21 response=[Short cd_seconds]
package protocol

import "fmt"

// 九空无界动作编号 (Mod_NineRegions_Base)。
const (
	// ActionIDNineRegionsOpen 主面板 (Action 0 -> 0x003A0000)
	ActionIDNineRegionsOpen uint32 = 0x003A0000
	// ActionIDNineRegionsStartChallenge 开始挑战/校验关卡 (Action 1 -> 0x003A0001)
	ActionIDNineRegionsStartChallenge uint32 = 0x003A0001
	// ActionIDNineRegionsTeamInfo 查询当前队伍 (Action 4 -> 0x003A0004)
	ActionIDNineRegionsTeamInfo uint32 = 0x003A0004
	// ActionIDNineRegionsCreateTeam 创建九空队伍 (Action 5 -> 0x003A0005)
	ActionIDNineRegionsCreateTeam uint32 = 0x003A0005
	// ActionIDNineRegionsHelperList 查询助战角色列表 (Action 9 -> 0x003A0009)
	ActionIDNineRegionsHelperList uint32 = 0x003A0009
	// ActionIDNineRegionsChooseHelper 选择助战角色 (Action 10 -> 0x003A000A)
	ActionIDNineRegionsChooseHelper uint32 = 0x003A000A
	// ActionIDNineRegionsFight 挑战关卡 (Action 13 -> 0x003A000D)
	ActionIDNineRegionsFight uint32 = 0x003A000D
	// ActionIDNineRegionsCheck 挑战资格检查 (Action 20 -> 0x003A0014)
	ActionIDNineRegionsCheck uint32 = 0x003A0014
	// ActionIDNineRegionsCD 挑战冷却查询 (Action 21 -> 0x003A0015)
	ActionIDNineRegionsCD uint32 = 0x003A0015
	// ActionIDNineRegionsHidden 挑战隐藏关卡 (Action 23 -> 0x003A0017)
	ActionIDNineRegionsHidden uint32 = 0x003A0017
)

// 九空无界结果码 (Mod_NineRegions_Base)。
const (
	// NineRegionsResultChallengeSuccess 挑战成功/可挑战
	NineRegionsResultChallengeSuccess uint8 = 0
	// NineRegionsResultChallengeLevel 等级不足
	NineRegionsResultChallengeLevel uint8 = 1
	// NineRegionsResultChallengeTower 未通关六道轮回塔
	NineRegionsResultChallengeTower uint8 = 2
	// NineRegionsResultChallengeRegion 关卡未完成
	NineRegionsResultChallengeRegion uint8 = 3
	// NineRegionsResultChallengeOther 其他错误
	NineRegionsResultChallengeOther uint8 = 4
	// NineRegionsResultSuccess 成功
	NineRegionsResultSuccess uint8 = 5
	// NineRegionsResultFailed 失败
	NineRegionsResultFailed uint8 = 6
	// NineRegionsResultFullTeamNumber 队伍数量已满
	NineRegionsResultFullTeamNumber uint8 = 7
	// NineRegionsResultInvalidTeamID 无效队伍
	NineRegionsResultInvalidTeamID uint8 = 8
	// NineRegionsResultFullMember 队伍成员已满
	NineRegionsResultFullMember uint8 = 9
	// NineRegionsResultErrorRegion 关卡错误
	NineRegionsResultErrorRegion uint8 = 10
	// NineRegionsResultDeploy 已上阵
	NineRegionsResultDeploy uint8 = 11
	// NineRegionsResultNoDeploy 未上阵
	NineRegionsResultNoDeploy uint8 = 12
	// NineRegionsResultChallenged 今日已挑战
	NineRegionsResultChallenged uint8 = 13
	// NineRegionsResultLimitMemberNumber 队伍人数限制
	NineRegionsResultLimitMemberNumber uint8 = 14
	// NineRegionsResultNoLeader 不是队长
	NineRegionsResultNoLeader uint8 = 15
	// NineRegionsResultCantChallengeHidden 不可挑战隐藏关卡
	NineRegionsResultCantChallengeHidden uint8 = 21
)

// NineRegionsPanel 表示九空无界面板。
type NineRegionsPanel struct {
	// IsFirst 是否首次进入
	IsFirst bool
	// CurJie 当前界
	CurJie int32
	// CurLevel 当前关卡索引 (0 表示尚未开始，实际挑战坐标为 1)
	CurLevel int32
	// HiddenLevel 可挑战的隐藏关卡 ID (0 表示无)
	HiddenLevel int32
}

// NineRegionsStartResult 表示开始挑战校验结果。
type NineRegionsStartResult struct {
	// FirstChallenge 是否首次挑战
	FirstChallenge bool
	// Result 挑战结果码
	Result uint8
}

// NineRegionsTeamMember 表示九空队伍成员。
type NineRegionsTeamMember struct {
	// PlayerID 玩家 ID
	PlayerID int32
	// Name 玩家名
	Name string
	// Sign 角色签名
	Sign string
	// Level 等级
	Level int16
	// RoleID 职业编号
	RoleID int32
	// WarPower 战力
	WarPower int32
	// VipLevel VIP 等级
	VipLevel int32
	// Extra 额外信息
	Extra string
}

// NineRegionsTeamInfo 表示当前九空队伍。
type NineRegionsTeamInfo struct {
	// LeaderID 队长玩家 ID
	LeaderID int32
	// Members 队伍成员
	Members []NineRegionsTeamMember
}

// NineRegionsCreateTeamResult 表示创建队伍结果。
type NineRegionsCreateTeamResult struct {
	// Result 结果码
	Result uint8
	// TeamID 队伍 ID
	TeamID int32
}

// NineRegionsHelperRole 表示一个可选择的助战角色。
type NineRegionsHelperRole struct {
	// RoleID 角色编号
	RoleID int32
	// Name 角色名
	Name string
	// Level 等级
	Level int32
	// JobID 职业编号
	JobID int32
	// State 状态 (DEPLOY=11 已上阵 / NODEPLOY=12 未上阵)
	State uint8
}

// NineRegionsFightResult 表示挑战关卡结果。
//
// 回包携带完整战报；自动化只消费 result 与战报数量，避免重复实现战报解析。
type NineRegionsFightResult struct {
	// Result 结果码 (SUCCESS=5 表示战斗已执行)
	Result uint8
	// WarCount 战报数量
	WarCount int
}

// BuildNineRegionsOpenPacket 构造九空无界主面板查询封包。
func BuildNineRegionsOpenPacket() *Packet {
	return NewPacket(ActionIDNineRegionsOpen, nil)
}

// BuildNineRegionsStartChallengePacket 构造开始挑战校验封包。
//
// 官方 request=[Int jie, Int level]。面板 cur_level=0 时，客户端使用 (1,1)。
func BuildNineRegionsStartChallengePacket(jie, level int32) *Packet {
	w := NewWriter()
	w.WriteInt32(jie)
	w.WriteInt32(level)
	return NewPacket(ActionIDNineRegionsStartChallenge, w.Bytes())
}

// BuildNineRegionsCheckPacket 构造挑战资格检查封包。
func BuildNineRegionsCheckPacket(jie, level int32) *Packet {
	w := NewWriter()
	w.WriteInt32(jie)
	w.WriteInt32(level)
	return NewPacket(ActionIDNineRegionsCheck, w.Bytes())
}

// BuildNineRegionsCreateTeamPacket 构造创建九空队伍封包。
func BuildNineRegionsCreateTeamPacket(jie, level int32) *Packet {
	w := NewWriter()
	w.WriteInt32(jie)
	w.WriteInt32(level)
	return NewPacket(ActionIDNineRegionsCreateTeam, w.Bytes())
}

// BuildNineRegionsTeamInfoPacket 构造查询队伍信息封包。
func BuildNineRegionsTeamInfoPacket() *Packet {
	return NewPacket(ActionIDNineRegionsTeamInfo, nil)
}

// BuildNineRegionsHelperListPacket 构造查询助战角色封包。
func BuildNineRegionsHelperListPacket() *Packet {
	return NewPacket(ActionIDNineRegionsHelperList, nil)
}

// BuildNineRegionsChooseHelperPacket 构造选择助战角色封包。
//
// 官方 request=[Int role_id, Byte slot]。
func BuildNineRegionsChooseHelperPacket(roleID int32, slot uint8) *Packet {
	w := NewWriter()
	w.WriteInt32(roleID)
	w.WriteUint8(slot)
	return NewPacket(ActionIDNineRegionsChooseHelper, w.Bytes())
}

// BuildNineRegionsFightPacket 构造挑战关卡封包。
func BuildNineRegionsFightPacket(jie, level int32) *Packet {
	w := NewWriter()
	w.WriteInt32(jie)
	w.WriteInt32(level)
	return NewPacket(ActionIDNineRegionsFight, w.Bytes())
}

// BuildNineRegionsCDPacket 构造挑战冷却查询封包。
func BuildNineRegionsCDPacket() *Packet {
	return NewPacket(ActionIDNineRegionsCD, nil)
}

// BuildNineRegionsHiddenPacket 构造隐藏关卡挑战封包。
func BuildNineRegionsHiddenPacket(hiddenLevel int32) *Packet {
	w := NewWriter()
	w.WriteInt32(hiddenLevel)
	return NewPacket(ActionIDNineRegionsHidden, w.Bytes())
}

// ParseNineRegionsPanelResult 解析九空无界主面板。
func ParseNineRegionsPanelResult(payload []byte) (*NineRegionsPanel, error) {
	r := NewReader(payload)
	first, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: 九空无界 is_first 读取失败: %v", ErrInvalidPayload, err)
	}
	jie, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: 九空无界 cur_jie 读取失败: %v", ErrInvalidPayload, err)
	}
	level, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: 九空无界 cur_level 读取失败: %v", ErrInvalidPayload, err)
	}
	hidden, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: 九空无界 hidden_level 读取失败: %v", ErrInvalidPayload, err)
	}
	return &NineRegionsPanel{
		IsFirst:     first == 1,
		CurJie:      jie,
		CurLevel:    level,
		HiddenLevel: hidden,
	}, nil
}

// ParseNineRegionsStartResult 解析开始挑战校验结果。
func ParseNineRegionsStartResult(payload []byte) (*NineRegionsStartResult, error) {
	if len(payload) < 2 {
		return nil, fmt.Errorf("%w: 九空无界开始挑战回包过短", ErrInvalidPayload)
	}
	return &NineRegionsStartResult{
		FirstChallenge: payload[0] == 1,
		Result:         payload[1],
	}, nil
}

// ParseNineRegionsCreateTeamResult 解析创建队伍结果。
func ParseNineRegionsCreateTeamResult(payload []byte) (*NineRegionsCreateTeamResult, error) {
	r := NewReader(payload)
	res := &NineRegionsCreateTeamResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: 九空无界创建队伍结果读取失败: %v", ErrInvalidPayload, err)
	}
	if res.TeamID, err = r.ReadInt32(); err != nil {
		return res, nil
	}
	return res, nil
}

// ParseNineRegionsTeamInfoResult 解析队伍信息。
func ParseNineRegionsTeamInfoResult(payload []byte) (*NineRegionsTeamInfo, error) {
	r := NewReader(payload)
	info := &NineRegionsTeamInfo{}
	var err error
	if info.LeaderID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: 九空无界 leader_id 读取失败: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: 九空无界队伍人数读取失败: %v", ErrInvalidPayload, err)
	}
	info.Members = make([]NineRegionsTeamMember, 0, count)
	for i := 0; i < int(count); i++ {
		var m NineRegionsTeamMember
		if m.PlayerID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: 九空无界成员[%d] player_id 读取失败: %v", ErrInvalidPayload, i, err)
		}
		if m.Name, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: 九空无界成员[%d] name 读取失败: %v", ErrInvalidPayload, i, err)
		}
		if m.Sign, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: 九空无界成员[%d] sign 读取失败: %v", ErrInvalidPayload, i, err)
		}
		if m.Level, err = r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("%w: 九空无界成员[%d] level 读取失败: %v", ErrInvalidPayload, i, err)
		}
		if m.RoleID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: 九空无界成员[%d] role_id 读取失败: %v", ErrInvalidPayload, i, err)
		}
		if m.WarPower, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: 九空无界成员[%d] war_power 读取失败: %v", ErrInvalidPayload, i, err)
		}
		if m.VipLevel, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: 九空无界成员[%d] vip 读取失败: %v", ErrInvalidPayload, i, err)
		}
		if m.Extra, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: 九空无界成员[%d] extra 读取失败: %v", ErrInvalidPayload, i, err)
		}
		info.Members = append(info.Members, m)
	}
	return info, nil
}

// ParseNineRegionsHelperListResult 解析助战角色列表。
func ParseNineRegionsHelperListResult(payload []byte) ([]NineRegionsHelperRole, error) {
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: 九空无界助战人数读取失败: %v", ErrInvalidPayload, err)
	}
	roles := make([]NineRegionsHelperRole, 0, count)
	for i := 0; i < int(count); i++ {
		var role NineRegionsHelperRole
		if role.RoleID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: 九空无界助战[%d] role_id 读取失败: %v", ErrInvalidPayload, i, err)
		}
		if role.Name, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: 九空无界助战[%d] name 读取失败: %v", ErrInvalidPayload, i, err)
		}
		if role.Level, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: 九空无界助战[%d] level 读取失败: %v", ErrInvalidPayload, i, err)
		}
		if role.JobID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: 九空无界助战[%d] job 读取失败: %v", ErrInvalidPayload, i, err)
		}
		if role.State, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: 九空无界助战[%d] state 读取失败: %v", ErrInvalidPayload, i, err)
		}
		roles = append(roles, role)
	}
	return roles, nil
}

// ParseNineRegionsFightResult 解析挑战关卡结果。
//
// 回包结构: UByte result, [war_data...], Short, Int x4, [Int,Byte]。
// 自动化只读取 result 与战报数量。
func ParseNineRegionsFightResult(payload []byte) (*NineRegionsFightResult, error) {
	r := NewReader(payload)
	res := &NineRegionsFightResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: 九空无界挑战结果码读取失败: %v", ErrInvalidPayload, err)
	}
	if res.Result != NineRegionsResultSuccess {
		return res, nil
	}
	count, err := r.ReadUint16()
	if err != nil {
		return res, nil
	}
	res.WarCount = int(count)
	return res, nil
}

// ParseNineRegionsCDResult 解析挑战冷却秒数。
func ParseNineRegionsCDResult(payload []byte) (int16, error) {
	r := NewReader(payload)
	cd, err := r.ReadInt16()
	if err != nil {
		return 0, fmt.Errorf("%w: 九空无界CD读取失败: %v", ErrInvalidPayload, err)
	}
	return cd, nil
}

// ParseNineRegionsSimpleResult 解析单字节结果码 (check/choose_helper/等)。
func ParseNineRegionsSimpleResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: 九空无界结果码为空", ErrInvalidPayload)
	}
	return payload[0], nil
}

// NineRegionsResultText 返回九空无界结果码的可读文本。
func NineRegionsResultText(code uint8) string {
	switch code {
	case NineRegionsResultChallengeSuccess:
		return "可挑战"
	case NineRegionsResultChallengeLevel:
		return "等级不足"
	case NineRegionsResultChallengeTower:
		return "未通关六道轮回塔"
	case NineRegionsResultChallengeRegion:
		return "关卡未完成"
	case NineRegionsResultChallengeOther:
		return "其他条件不满足"
	case NineRegionsResultSuccess:
		return "成功"
	case NineRegionsResultFailed:
		return "失败"
	case NineRegionsResultFullTeamNumber:
		return "队伍数量已满"
	case NineRegionsResultInvalidTeamID:
		return "无效队伍"
	case NineRegionsResultFullMember:
		return "队伍成员已满"
	case NineRegionsResultErrorRegion:
		return "关卡错误"
	case NineRegionsResultDeploy:
		return "已上阵"
	case NineRegionsResultNoDeploy:
		return "未上阵"
	case NineRegionsResultChallenged:
		return "今日已挑战"
	case NineRegionsResultLimitMemberNumber:
		return "队伍人数限制"
	case NineRegionsResultNoLeader:
		return "不是队长"
	case NineRegionsResultCantChallengeHidden:
		return "不可挑战隐藏关卡"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// BuildNineRegionsPanelResultPacket 构造主面板回包，供离线 mock 与单测使用。
func BuildNineRegionsPanelResultPacket(panel NineRegionsPanel) *Packet {
	w := NewWriter()
	if panel.IsFirst {
		w.WriteUint8(1)
	} else {
		w.WriteUint8(0)
	}
	w.WriteInt32(panel.CurJie)
	w.WriteInt32(panel.CurLevel)
	w.WriteInt32(panel.HiddenLevel)
	return NewPacket(ActionIDNineRegionsOpen, w.Bytes())
}

// BuildNineRegionsStartResultPacket 构造开始挑战回包，供离线 mock 与单测使用。
func BuildNineRegionsStartResultPacket(res NineRegionsStartResult) *Packet {
	w := NewWriter()
	if res.FirstChallenge {
		w.WriteUint8(1)
	} else {
		w.WriteUint8(0)
	}
	w.WriteUint8(res.Result)
	return NewPacket(ActionIDNineRegionsStartChallenge, w.Bytes())
}

// BuildNineRegionsCreateTeamResultPacket 构造创建队伍回包，供离线 mock 与单测使用。
func BuildNineRegionsCreateTeamResultPacket(res NineRegionsCreateTeamResult) *Packet {
	w := NewWriter()
	w.WriteUint8(res.Result)
	w.WriteInt32(res.TeamID)
	return NewPacket(ActionIDNineRegionsCreateTeam, w.Bytes())
}

// BuildNineRegionsTeamInfoResultPacket 构造队伍信息回包，供离线 mock 与单测使用。
func BuildNineRegionsTeamInfoResultPacket(info NineRegionsTeamInfo) *Packet {
	w := NewWriter()
	w.WriteInt32(info.LeaderID)
	w.WriteUint16(uint16(len(info.Members)))
	for _, m := range info.Members {
		w.WriteInt32(m.PlayerID)
		w.WriteString(m.Name)
		w.WriteString(m.Sign)
		w.WriteInt16(m.Level)
		w.WriteInt32(m.RoleID)
		w.WriteInt32(m.WarPower)
		w.WriteInt32(m.VipLevel)
		w.WriteString(m.Extra)
	}
	return NewPacket(ActionIDNineRegionsTeamInfo, w.Bytes())
}

// BuildNineRegionsHelperListResultPacket 构造助战角色回包，供离线 mock 与单测使用。
func BuildNineRegionsHelperListResultPacket(roles []NineRegionsHelperRole) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(roles)))
	for _, role := range roles {
		w.WriteInt32(role.RoleID)
		w.WriteString(role.Name)
		w.WriteInt32(role.Level)
		w.WriteInt32(role.JobID)
		w.WriteUint8(role.State)
	}
	return NewPacket(ActionIDNineRegionsHelperList, w.Bytes())
}

// BuildNineRegionsFightResultPacket 构造挑战回包，供离线 mock 与单测使用。
func BuildNineRegionsFightResultPacket(result uint8, warCount int) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	if result == NineRegionsResultSuccess {
		w.WriteUint16(uint16(warCount))
		// 每个战报用一个最小的 Byte 占位，解析器只读取数量。
		for i := 0; i < warCount; i++ {
			w.WriteUint8(0)
		}
	}
	return NewPacket(ActionIDNineRegionsFight, w.Bytes())
}

// BuildNineRegionsCDResultPacket 构造冷却回包，供离线 mock 与单测使用。
func BuildNineRegionsCDResultPacket(cd int16) *Packet {
	w := NewWriter()
	w.WriteInt16(cd)
	return NewPacket(ActionIDNineRegionsCD, w.Bytes())
}

// BuildNineRegionsSimpleResultPacket 构造单字节结果回包，供离线 mock 与单测使用。
func BuildNineRegionsSimpleResultPacket(actionID uint32, result uint8) *Packet {
	return NewPacket(actionID, []byte{result})
}
