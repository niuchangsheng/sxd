// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方宗师修行协议 (Mod_MasterPractice_Base, Module 665 / 0x0299)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_MasterPractice_Base、
// com.datas.MasterPracticeData 与 com.views.master_practice.MasterPracticeBattleView：
//   - main_panel          Act 1 -> 0x02990001
//   - get_deploy_info     Act 2 -> 0x02990002
//   - get_monster_team_info Act 3 -> 0x02990003
//   - deploy              Act 4 -> 0x02990004  [Int player_role_id, UByte grid_type_id]
//     (grid_type_id=0 表示下阵; 已由官方客户端 CommonDeployView.up_deploy/down_deploy 验证)
//   - start_challenge     Act 5 -> 0x02990005  [Int job_id, Int battle_stars]
//   - active_job_stunt    Act 6 -> 0x02990006  [Int job_id]
//   - active_soul         Act 7 -> 0x02990007  [Int job_id]
//   - step_up             Act 8 -> 0x02990008  [Int job_id]
package protocol

import "fmt"

const (
	// ActionIDMasterPracticePanel 查询宗师修行主面板 (Action 1 -> 0x02990001)
	ActionIDMasterPracticePanel uint32 = 0x02990001
	// ActionIDMasterPracticeDeployInfo 查询布阵信息 (Action 2 -> 0x02990002)
	ActionIDMasterPracticeDeployInfo uint32 = 0x02990002
	// ActionIDMasterPracticeMonsterTeamInfo 查询怪物队伍信息 (Action 3 -> 0x02990003)
	ActionIDMasterPracticeMonsterTeamInfo uint32 = 0x02990003
	// ActionIDMasterPracticeDeploy 布阵 (Action 4 -> 0x02990004)
	ActionIDMasterPracticeDeploy uint32 = 0x02990004
	// ActionIDMasterPracticeChallenge 发起宗师试炼挑战 (Action 5 -> 0x02990005)
	ActionIDMasterPracticeChallenge uint32 = 0x02990005
	// ActionIDMasterPracticeActiveJobStunt 激活职业绝技 (Action 6 -> 0x02990006)
	ActionIDMasterPracticeActiveJobStunt uint32 = 0x02990006
	// ActionIDMasterPracticeActiveSoul 激活魂 (Action 7 -> 0x02990007)
	ActionIDMasterPracticeActiveSoul uint32 = 0x02990007
	// ActionIDMasterPracticeStepUp 境界突破 (Action 8 -> 0x02990008)
	ActionIDMasterPracticeStepUp uint32 = 0x02990008
)

// 宗师修行结果码 (Mod_MasterPractice_Base)。
const (
	// MasterPracticeResultSuccess 成功
	MasterPracticeResultSuccess uint8 = 0
	// MasterPracticeResultFailed 失败
	MasterPracticeResultFailed uint8 = 1
	// MasterPracticeResultNotExistGrid 格位不存在 (仅 deploy)
	MasterPracticeResultNotExistGrid uint8 = 2
	// MasterPracticeResultOnDeployLimit 上阵数量限制 (仅 deploy)
	MasterPracticeResultOnDeployLimit uint8 = 3
	// MasterPracticeResultLeaveTeam 需先离队/下阵 (仅 deploy)
	MasterPracticeResultLeaveTeam uint8 = 4
	// MasterPracticeResultNeedUpTibu1 需先上阵替补1 (仅 deploy)
	MasterPracticeResultNeedUpTibu1 uint8 = 5
	// MasterPracticeResultNeedDownTibu2 需先下阵替补2 (仅 deploy)
	MasterPracticeResultNeedDownTibu2 uint8 = 6
	// MasterPracticeResultUpPartnerLimit 上阵伙伴数量限制 (仅 deploy)
	MasterPracticeResultUpPartnerLimit uint8 = 7
	// MasterPracticeResultNoChallengeTimes 挑战次数不足
	MasterPracticeResultNoChallengeTimes uint8 = 8
	// MasterPracticeResultIsChallenged 今日已挑战
	MasterPracticeResultIsChallenged uint8 = 9
	// MasterPracticeResultMaxStars 已满星
	MasterPracticeResultMaxStars uint8 = 10
	// MasterPracticeResultJobError 职业错误
	MasterPracticeResultJobError uint8 = 11
	// MasterPracticeResultLessStars 星数不足
	MasterPracticeResultLessStars uint8 = 12
	// MasterPracticeResultIsActived 已激活
	MasterPracticeResultIsActived uint8 = 13
	// MasterPracticeResultLessItem 道具不足
	MasterPracticeResultLessItem uint8 = 14
	// MasterPracticeResultConditionLimited 条件不满足
	MasterPracticeResultConditionLimited uint8 = 15
	// MasterPracticeResultStepUpCD 突破冷却中
	MasterPracticeResultStepUpCD uint8 = 16
	// MasterPracticeResultNotActiveAll 炼魂未全部激活
	MasterPracticeResultNotActiveAll uint8 = 17
	// MasterPracticeResultTopStep 已达最高境界
	MasterPracticeResultTopStep uint8 = 18
	// MasterPracticeResultIsLocked 未解锁
	MasterPracticeResultIsLocked uint8 = 19
)

// MasterPracticeJobInfo 表示一个职业的宗师修行状态。
type MasterPracticeJobInfo struct {
	// JobID 职业编号
	JobID int32
	// StepID 当前境界
	StepID int32
	// TotalStars 累计星数
	TotalStars int32
	// StepUpCDTime 突破冷却时间戳
	StepUpCDTime int32
	// TodayChallenge 今日已挑战的星级
	TodayChallenge []int32
	// ActivedSouls 已激活魂
	ActivedSouls []int32
	// UseFeatures 已装配特性 [grid_id, feature_id]
	UseFeatures [][2]int32
}

// MasterPracticePanel 表示宗师修行主面板。
type MasterPracticePanel struct {
	// Result 结果码
	Result uint8
	// TodayChallengeTimes 今日已挑战/已通关的星级数量。
	// 注意：该字段不表示剩余挑战次数，也不随重复挑战同一星级变化；
	// 每日次数上限必须由服务端 NO_CHALLENGE_TIMES(8) 结果码判定。
	TodayChallengeTimes int32
	// ArtifactSkillLevel 神器技能等级
	ArtifactSkillLevel int32
	// HaveItems 拥有的材料 [item_id, num]
	HaveItems [][2]int32
	// Jobs 各职业修行状态
	Jobs []MasterPracticeJobInfo
}

// BuildMasterPracticePanelPacket 构造查询宗师修行主面板封包 (0x02990001)。
func BuildMasterPracticePanelPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDMasterPracticePanel, w.Bytes())
}

// BuildMasterPracticeDeployPacket 构造宗师修行布阵/下阵封包 (0x02990004)。
//
// 官方客户端 CommonDeployView.up_deploy 发送 [player_role_id Int32, grid_type_id UByte]
// 上阵；down_deploy 发送 [player_role_id Int32, 0] 下阵。
func BuildMasterPracticeDeployPacket(playerRoleID int32, gridTypeID uint8) *Packet {
	w := NewWriter()
	w.WriteInt32(playerRoleID)
	w.WriteUint8(gridTypeID)
	return NewPacket(ActionIDMasterPracticeDeploy, w.Bytes())
}

// BuildMasterPracticeChallengePacket 构造发起宗师试炼挑战封包 (0x02990005)。
func BuildMasterPracticeChallengePacket(jobID, battleStars, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(int32(jobID))
	w.WriteInt32(int32(battleStars))
	if prevAct != 0 {
		w.WriteUint32(prevAct)
	}
	return NewPacket(ActionIDMasterPracticeChallenge, w.Bytes())
}

// BuildMasterPracticeStepUpPacket 构造境界突破封包 (0x02990008)。
func BuildMasterPracticeStepUpPacket(jobID, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(int32(jobID))
	if prevAct != 0 {
		w.WriteUint32(prevAct)
	}
	return NewPacket(ActionIDMasterPracticeStepUp, w.Bytes())
}

// ParseMasterPracticePanelResult 解析宗师修行主面板。
func ParseMasterPracticePanelResult(payload []byte) (*MasterPracticePanel, error) {
	r := NewReader(payload)
	p := &MasterPracticePanel{}
	var err error
	if p.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read master practice result: %v", ErrInvalidPayload, err)
	}
	if p.Result != MasterPracticeResultSuccess {
		return p, nil
	}
	if p.TodayChallengeTimes, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read today_challenge_times: %v", ErrInvalidPayload, err)
	}
	if p.ArtifactSkillLevel, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read artifact_skill_level: %v", ErrInvalidPayload, err)
	}
	itemCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read have_item count: %v", ErrInvalidPayload, err)
	}
	p.HaveItems = make([][2]int32, 0, itemCount)
	for i := 0; i < int(itemCount); i++ {
		a, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read have_item[%d]: %v", ErrInvalidPayload, i, err)
		}
		b, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read have_item[%d] num: %v", ErrInvalidPayload, i, err)
		}
		p.HaveItems = append(p.HaveItems, [2]int32{a, b})
	}
	jobCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read job count: %v", ErrInvalidPayload, err)
	}
	p.Jobs = make([]MasterPracticeJobInfo, 0, jobCount)
	for i := 0; i < int(jobCount); i++ {
		var j MasterPracticeJobInfo
		for _, field := range []*int32{&j.JobID, &j.StepID, &j.TotalStars, &j.StepUpCDTime} {
			if *field, err = r.ReadInt32(); err != nil {
				return nil, fmt.Errorf("%w: failed to read job[%d] head: %v", ErrInvalidPayload, i, err)
			}
		}
		if j.TodayChallenge, err = readMPInt32List(r); err != nil {
			return nil, fmt.Errorf("%w: job[%d] today_challenge: %v", ErrInvalidPayload, i, err)
		}
		if j.ActivedSouls, err = readMPInt32List(r); err != nil {
			return nil, fmt.Errorf("%w: job[%d] actived_souls: %v", ErrInvalidPayload, i, err)
		}
		featCount, err := r.ReadUint16()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read job[%d] feature count: %v", ErrInvalidPayload, i, err)
		}
		j.UseFeatures = make([][2]int32, 0, featCount)
		for k := 0; k < int(featCount); k++ {
			a, err := r.ReadInt32()
			if err != nil {
				return nil, fmt.Errorf("%w: failed to read job[%d] feature[%d]: %v", ErrInvalidPayload, i, k, err)
			}
			b, err := r.ReadInt32()
			if err != nil {
				return nil, fmt.Errorf("%w: failed to read job[%d] feature[%d] value: %v", ErrInvalidPayload, i, k, err)
			}
			j.UseFeatures = append(j.UseFeatures, [2]int32{a, b})
		}
		p.Jobs = append(p.Jobs, j)
	}
	return p, nil
}

func readMPInt32List(r *Reader) ([]int32, error) {
	count, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	out := make([]int32, 0, count)
	for i := 0; i < int(count); i++ {
		v, err := r.ReadInt32()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// ParseMasterPracticeActionCode 解析仅返回单结果码的动作。
func ParseMasterPracticeActionCode(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty master practice action result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// BuildMasterPracticePanelResultPacket 构造宗师修行面板回包，供离线 mock 与单测使用。
func BuildMasterPracticePanelResultPacket(p MasterPracticePanel) *Packet {
	w := NewWriter()
	w.WriteUint8(p.Result)
	if p.Result == MasterPracticeResultSuccess {
		w.WriteInt32(p.TodayChallengeTimes)
		w.WriteInt32(p.ArtifactSkillLevel)
		w.WriteUint16(uint16(len(p.HaveItems)))
		for _, it := range p.HaveItems {
			w.WriteInt32(it[0])
			w.WriteInt32(it[1])
		}
		w.WriteUint16(uint16(len(p.Jobs)))
		for _, j := range p.Jobs {
			w.WriteInt32(j.JobID)
			w.WriteInt32(j.StepID)
			w.WriteInt32(j.TotalStars)
			w.WriteInt32(j.StepUpCDTime)
			w.WriteUint16(uint16(len(j.TodayChallenge)))
			for _, v := range j.TodayChallenge {
				w.WriteInt32(v)
			}
			w.WriteUint16(uint16(len(j.ActivedSouls)))
			for _, v := range j.ActivedSouls {
				w.WriteInt32(v)
			}
			w.WriteUint16(uint16(len(j.UseFeatures)))
			for _, f := range j.UseFeatures {
				w.WriteInt32(f[0])
				w.WriteInt32(f[1])
			}
		}
	}
	return NewPacket(ActionIDMasterPracticePanel, w.Bytes())
}

// MasterPracticeDeployRole 表示宗师修行布阵中的一个角色。
type MasterPracticeDeployRole struct {
	// PlayerRoleID 玩家角色实例 ID
	PlayerRoleID int32
	// IsMainRole 是否主角 (1=主角, 2=幻灵师伙伴)
	IsMainRole uint8
	// RoleID 角色编号 (用于查询职业 JobId)
	RoleID int32
	// RoleName 角色名
	RoleName string
	// RoleLevel 角色等级
	RoleLevel int32
	// IsDeploy 是否已上阵
	IsDeploy uint8
}

// MasterPracticeDeployGrid 表示宗师修行布阵的一个格位。
type MasterPracticeDeployGrid struct {
	// GridTypeID 格位类型 ID
	GridTypeID int32
	// PlayerRoleID 上阵的玩家角色实例 ID (0 表示空位)
	PlayerRoleID int32
}

// MasterPracticeDeployInfo 表示宗师修行当前布阵。
type MasterPracticeDeployInfo struct {
	// Result 结果码
	Result uint8
	// DeployLevel 布阵等级
	DeployLevel int32
	// Roles 全部可用角色
	Roles []MasterPracticeDeployRole
	// Grids 布阵格位
	Grids []MasterPracticeDeployGrid
}

// BuildMasterPracticeDeployInfoPacket 构造查询宗师修行布阵信息封包 (0x02990002)。
func BuildMasterPracticeDeployInfoPacket() *Packet {
	return NewPacket(ActionIDMasterPracticeDeployInfo, nil)
}

// ParseMasterPracticeDeployInfoResult 解析宗师修行布阵信息 (Act 2)。
func ParseMasterPracticeDeployInfoResult(payload []byte) (*MasterPracticeDeployInfo, error) {
	r := NewReader(payload)
	info := &MasterPracticeDeployInfo{}
	var err error
	if info.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read deploy result: %v", ErrInvalidPayload, err)
	}
	if info.Result != MasterPracticeResultSuccess {
		return info, nil
	}
	if info.DeployLevel, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read deploy_level: %v", ErrInvalidPayload, err)
	}
	roleCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read deploy role count: %v", ErrInvalidPayload, err)
	}
	info.Roles = make([]MasterPracticeDeployRole, 0, roleCount)
	for i := 0; i < int(roleCount); i++ {
		var role MasterPracticeDeployRole
		if role.PlayerRoleID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read deploy_role[%d].player_role_id: %v", ErrInvalidPayload, i, err)
		}
		if role.IsMainRole, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read deploy_role[%d].is_main_role: %v", ErrInvalidPayload, i, err)
		}
		for j := 0; j < 2; j++ { // health, max_health
			if _, err = r.ReadInt32(); err != nil {
				return nil, fmt.Errorf("%w: failed to read deploy_role[%d].health[%d]: %v", ErrInvalidPayload, i, j, err)
			}
		}
		if role.RoleID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read deploy_role[%d].role_id: %v", ErrInvalidPayload, i, err)
		}
		if role.RoleName, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: failed to read deploy_role[%d].role_name: %v", ErrInvalidPayload, i, err)
		}
		if role.RoleLevel, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read deploy_role[%d].role_level: %v", ErrInvalidPayload, i, err)
		}
		for j := 0; j < 2; j++ { // role_job_name, role_stunt
			if _, err = r.ReadString(); err != nil {
				return nil, fmt.Errorf("%w: failed to read deploy_role[%d].string[%d]: %v", ErrInvalidPayload, i, j, err)
			}
		}
		for j := 0; j < 3; j++ { // strength, agile, intellect
			if _, err = r.ReadInt32(); err != nil {
				return nil, fmt.Errorf("%w: failed to read deploy_role[%d].stat[%d]: %v", ErrInvalidPayload, i, j, err)
			}
		}
		if role.IsDeploy, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read deploy_role[%d].is_deploy: %v", ErrInvalidPayload, i, err)
		}
		if _, err = readMPInt32List(r); err != nil { // wear_on_supernatural_list
			return nil, fmt.Errorf("%w: failed to read deploy_role[%d].supernatural: %v", ErrInvalidPayload, i, err)
		}
		for j := 0; j < 3; j++ { // suit_equip_id, rebirth_skill_id, rebirth_skill_id2
			if _, err = r.ReadInt32(); err != nil {
				return nil, fmt.Errorf("%w: failed to read deploy_role[%d].tail[%d]: %v", ErrInvalidPayload, i, j, err)
			}
		}
		info.Roles = append(info.Roles, role)
	}
	gridCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read deploy grid count: %v", ErrInvalidPayload, err)
	}
	info.Grids = make([]MasterPracticeDeployGrid, 0, gridCount)
	for i := 0; i < int(gridCount); i++ {
		var g MasterPracticeDeployGrid
		if g.GridTypeID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read deploy_grid[%d].grid_type_id: %v", ErrInvalidPayload, i, err)
		}
		if g.PlayerRoleID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read deploy_grid[%d].player_role_id: %v", ErrInvalidPayload, i, err)
		}
		info.Grids = append(info.Grids, g)
	}
	return info, nil
}

// BuildMasterPracticeDeployInfoResultPacket 构造布阵信息回包，供离线 mock 与单测使用。
func BuildMasterPracticeDeployInfoResultPacket(info MasterPracticeDeployInfo) *Packet {
	w := NewWriter()
	w.WriteUint8(info.Result)
	if info.Result == MasterPracticeResultSuccess {
		w.WriteInt32(info.DeployLevel)
		w.WriteUint16(uint16(len(info.Roles)))
		for _, role := range info.Roles {
			w.WriteInt32(role.PlayerRoleID)
			w.WriteUint8(role.IsMainRole)
			w.WriteInt32(100)
			w.WriteInt32(100)
			w.WriteInt32(role.RoleID)
			w.WriteString(role.RoleName)
			w.WriteInt32(role.RoleLevel)
			w.WriteString("")
			w.WriteString("")
			w.WriteInt32(0)
			w.WriteInt32(0)
			w.WriteInt32(0)
			w.WriteUint8(role.IsDeploy)
			w.WriteUint16(0)
			w.WriteInt32(0)
			w.WriteInt32(0)
			w.WriteInt32(0)
		}
		w.WriteUint16(uint16(len(info.Grids)))
		for _, g := range info.Grids {
			w.WriteInt32(g.GridTypeID)
			w.WriteInt32(g.PlayerRoleID)
		}
		w.WriteUint16(0) // children
	}
	return NewPacket(ActionIDMasterPracticeDeployInfo, w.Bytes())
}

// BuildMasterPracticeActionCodePacket 构造单结果码回包，供离线 mock 与单测使用。
func BuildMasterPracticeActionCodePacket(actionID uint32, code uint8) *Packet {
	return NewPacket(actionID, []byte{code})
}
