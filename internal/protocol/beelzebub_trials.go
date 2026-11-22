// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现魔王试炼 (Mod_BeelzebubTrials_Base, Module 57 / 0x0039) 核心协议报文编解码。
package protocol

import (
	"errors"
	"fmt"

	"sxd-pie-ng/internal/dictionary"
)

// 魔王试炼业务常量 (对齐 Mod_BeelzebubTrials_Base 常量表)
const (
	BeelzebubTrialsNotLimit          uint8 = 0  // NOT_LIMIT: 组队无限制
	BeelzebubTrialsSameGang          uint8 = 1  // SAME_GANG: 仅限帮派成员
	BeelzebubTrialsOnActivity        uint8 = 2  // ON_ACTIVITY: 活动时间内 (18:00-19:00 双倍声望)
	BeelzebubTrialsNonActivity       uint8 = 3  // NON_ACTIVITY: 非双倍活动时段
	BeelzebubTrialsAllRecord         uint8 = 4  // ALL_RECORD
	BeelzebubTrialsSuccess           uint8 = 5  // SUCCESS: 操作成功 / 可挑战
	BeelzebubTrialsInvalidTrialsID   uint8 = 6  // INVALID_BEELZEBUB_TRIALS_ID: 试炼ID无效
	BeelzebubTrialsFullTeamNumber    uint8 = 7  // FULL_TEAM_NUMBER: 队伍数量已满
	BeelzebubTrialsInvalidTeamID     uint8 = 8  // INVALID_TEAM_ID: 队伍ID无效
	BeelzebubTrialsFullMember        uint8 = 9  // FULL_MEMBER: 队伍人员已满
	BeelzebubTrialsNotSameCano       uint8 = 10 // NOT_SAME_CANO
	BeelzebubTrialsNotSameGang       uint8 = 11 // NOT_SAME_GANG: 非同一帮派
	BeelzebubTrialsErrorID           uint8 = 12 // ERROR_BEELZEBUB_ID: 魔王ID错误
	BeelzebubTrialsTodayChallenged   uint8 = 13 // TODAY_CHALLENGED: 今日已挑战过
	BeelzebubTrialsFailed            uint8 = 14 // FAILED: 操作失败 / 挑战不可用
	BeelzebubTrialsDeploy            uint8 = 15 // DEPLOY: 助战上阵中
	BeelzebubTrialsNoDeploy          uint8 = 16 // NODEPLOY: 助战未上阵
	BeelzebubTrialsLimitMemberNumber uint8 = 17 // LIMIT_MEMBER_NUMBER
	BeelzebubTrialsNoLeader          uint8 = 18 // NO_LEADER: 无队长
	BeelzebubTrialsUpdateTeamInfo    uint8 = 19 // UPDATE_TEAM_INFO: 通知队伍成员变动
	BeelzebubTrialsLeaderDissolve    uint8 = 20 // LEADER_DISSOLVE: 队长已解散队伍
	BeelzebubTrialsKickOutTeam       uint8 = 21 // KICK_OUT_TEAM: 已被踢出队伍
	BeelzebubTrialsHaveWarResult     uint8 = 22 // HAVE_WAR_RESULT: 战报结算已生成
	BeelzebubTrialsStart             uint8 = 23 // START_BEELZEBUB_TRAILS: 试炼开启
	BeelzebubTrialsStop              uint8 = 24 // STOP_BEELZEBUB_TRAILS: 试炼结束
	BeelzebubTrialsErrLevel          uint8 = 25 // ERRLEVEL: 等级不足
	BeelzebubTrialsNotEnough         uint8 = 26 // NOTENOUGH: 资源不足
	BeelzebubTrialsHasGet            uint8 = 27 // HASGET: 奖励已领取
	BeelzebubTrialsTrue              uint8 = 28 // TRUE
	BeelzebubTrialsFalse             uint8 = 29 // FALSE
	BeelzebubTrialsNotOpen           uint8 = 30 // NOT_OPEN: 功能未开放
	BeelzebubTrialsNotExistGrid      uint8 = 31 // NOT_EXIST_GRID
	BeelzebubTrialsNotFull           uint8 = 32 // NOT_FULL: 队伍未满
	BeelzebubTrialsNobody            uint8 = 33 // NOBODY: 队伍无人
)

func lookupBeelzebubBoss(bossID int32) (*dictionary.BeelzebubBoss, bool) {
	repo := dictionary.Default()
	if repo == nil {
		return nil, false
	}
	b, err := repo.GetBeelzebubBoss(bossID)
	if err != nil || b == nil {
		return nil, false
	}
	return b, true
}

// BeelzebubTrialsBossName 根据魔王编号返回 Boss 中文名称 (来源 data/gamedata.db :: beelzebub_boss)。
func BeelzebubTrialsBossName(bossID int32) string {
	if b, ok := lookupBeelzebubBoss(bossID); ok && b.Name != "" {
		return b.Name
	}
	return fmt.Sprintf("试炼魔王(%d)", bossID)
}

// BeelzebubTrialsBossLevel 根据魔王编号返回 Boss 开放/挑战等级。
func BeelzebubTrialsBossLevel(bossID int32) int32 {
	if b, ok := lookupBeelzebubBoss(bossID); ok {
		return b.Level
	}
	return 0
}

// BeelzebubTrialsBossMaxTimes 返回该魔王的累计可挑战次数；未知关卡返回 0。
func BeelzebubTrialsBossMaxTimes(bossID int32) int32 {
	if b, ok := lookupBeelzebubBoss(bossID); ok {
		return b.MaxTimes
	}
	return 0
}

// BeelzebubTrialsBossSecret 返回 Boss 的神秘层标记 (0=普通，1=神秘层，2=神秘层且名称带等级)。
func BeelzebubTrialsBossSecret(bossID int32) uint8 {
	if b, ok := lookupBeelzebubBoss(bossID); ok {
		return b.Secret
	}
	return 0
}

// BeelzebubTrialsBossIsSecret 判断 Boss 是否属于神秘层关卡。
func BeelzebubTrialsBossIsSecret(bossID int32) bool {
	return BeelzebubTrialsBossSecret(bossID) != 0
}

// BeelzebubTrialsBossDisplayName 返回与游戏界面一致的展示名称。
// 神秘层关卡统一加上"神秘层"前缀；标记为 2 的关卡按客户端逻辑在名称后追加等级。
func BeelzebubTrialsBossDisplayName(bossID int32) string {
	name := BeelzebubTrialsBossName(bossID)
	secret := BeelzebubTrialsBossSecret(bossID)
	if secret == 0 {
		return name
	}
	if secret == 2 {
		name = fmt.Sprintf("%s%d级", name, BeelzebubTrialsBossLevel(bossID))
	}
	return "神秘层·" + name
}

// BeelzebubBossStatus 魔王试炼面板中的单个关卡状态行 (官方 AS3 `[Int, Int, Int]`)。
//
// 语义以官方客户端 com.datas.BeelzebubTrialsData 为准：
//
//	_loc8_.times = row[1];                       // 已挑战次数 (跨天累计，每战递增)
//	_loc8_.isTodayChallenge = row[2] == 1;       // 今日是否已挑战过该关卡
//	_loc8_.maxTimes = BeelzeBubTrialsBossType.Bosses[bossID][15];
//	chanllegeOverToday[bossID] = isTodayChallenge || times >= maxTimes;
//
// 实机验证 (2026-11-06)：
//   - 挑战前 s1 角色 43 关全部为 `times=N, today=0`，可正常建队并战斗；
//   - 挑战 boss 78 后该行为 `times=N+2, today=1`，道行经验同步增加；
//   - 对 `times >= maxTimes` 的 boss 8 反复挑战服务端仍允许战斗，但道行不再增长，
//     即“已完成”只代表不再产出奖励，并不代表服务端会拒绝建队。
//
// 因此“今日还能获得奖励的关卡”= 今日未挑战 且 已挑战次数 < maxTimes。
type BeelzebubBossStatus struct {
	BossID          int32 `json:"boss_id"`
	Times           int32 `json:"times"`            // 第二列：已挑战次数 (跨天累计)
	TodayChallenged bool  `json:"today_challenged"` // 第三列：1=今日已挑战
}

// Finished 判断该关卡是否已完成（今日已挑战或累计次数已达上限），
// 与官方客户端的 chanllegeOverToday 判定一致。
func (b BeelzebubBossStatus) Finished() bool {
	if b.TodayChallenged {
		return true
	}
	boss, ok := lookupBeelzebubBoss(b.BossID)
	if !ok {
		// 未知关卡（新版本新增内容）不做过度过滤，交由服务端裁决。
		return false
	}
	return b.Times >= boss.MaxTimes
}

// BeelzebubOpenWindow 魔王试炼活动开启时间段（Unix 秒）。
type BeelzebubOpenWindow struct {
	Start int32 `json:"start"`
	End   int32 `json:"end"`
}

// BeelzebubTrialsOpenTime 魔王试炼开启时间面板。
//
// 对齐 Mod_BeelzebubTrials_Base.get_beelzebub_trials_open_time 响应
// [ByteUtil, [IntUtil, IntUtil], IntUtil, UByteUtil]：
//   - State: 活动状态字节
//   - Windows: 活动开启时间段列表（服务端按天/按周期下发）
//   - AllDeath: 是否已全通
//   - InActivity: 2=双倍声望时段, 3=非双倍时段
type BeelzebubTrialsOpenTime struct {
	State      uint8                 `json:"state"`
	Windows    []BeelzebubOpenWindow `json:"windows"`
	AllDeath   bool                  `json:"all_death"`
	InActivity uint8                 `json:"in_activity"`
}

// BeelzebubTrialsOpenData 魔王试炼主面板数据。
type BeelzebubTrialsOpenData struct {
	InActivity  bool  // 是否处于18:00-19:00双倍声望活动时段
	MoralExp    int32 // 当前累积道行经验
	MoralLv     int32 // 当前道行等级
	MoralCanGet bool  // 道行奖励是否可领取
	// Bosses 面板下发的关卡状态列表；服务端未下发时为空。
	Bosses []BeelzebubBossStatus
}

// BeelzebubTeamMember 队伍成员信息。
type BeelzebubTeamMember struct {
	PlayerID int32  `json:"player_id"`
	Nickname string `json:"nickname"`
	RoleName string `json:"role_name"`
	Level    int16  `json:"level"`
	IsLeader bool   `json:"is_leader"`
}

// BeelzebubHelperRole 助战伙伴/助战镜像信息。
type BeelzebubHelperRole struct {
	RoleID      int32  `json:"role_id"`
	Name        string `json:"name"`
	Level       int32  `json:"level"`
	JobID       int32  `json:"job_id"`
	InBody      bool   `json:"in_body"` // 是否已处于上阵中
	SuitEquipID int16  `json:"suit_equip_id"`
}

// BeelzebubTrialsWarResult 魔王试炼战斗结算与奖励。
type BeelzebubTrialsWarResult struct {
	Result     uint8 `json:"result"` // 5: 成功击败, 14: 失败
	AwardFlag  int32 `json:"award_flag"`
	AwardFame  int32 `json:"award_fame"`  // 奖励声望
	AwardCoin  int32 `json:"award_coin"`  // 奖励铜钱
	AwardMoral int32 `json:"award_moral"` // 奖励道行
	AwardExp   int32 `json:"award_exp"`   // 奖励经验/阅历
}

// BeelzebubMoralInfo 道行属性与领取状态。
type BeelzebubMoralInfo struct {
	Exp      int32 `json:"exp"`
	Level    int32 `json:"level"`
	CanClaim bool  `json:"can_claim"`
}

// --- 报文构造函数 (Packet Builders) ---
// 官方 AS3 Mod_BeelzebubTrials_Base 规范：魔王试炼协议动作均无须携带前序 ActionID

// BuildBeelzebubTrialsGetTeamListPacket 构造获取魔王试炼队伍列表封包 (Module 57, Action 2)。
//
// 官方请求类型为 [IntUtil, UByteUtil]：bossID=0 表示全部关卡，
// teamType 取 ALL_RECORD(4) 时返回全部记录。此前发送空载荷会被服务端静默丢弃。
func BuildBeelzebubTrialsGetTeamListPacket(bossID int32, teamType uint8) *Packet {
	w := NewWriter()
	w.WriteInt32(bossID)
	w.WriteUint8(teamType)
	return NewPacket(ActionIDBeelzebubTrialsGetTeamList, w.Bytes())
}

// BuildBeelzebubTrialsGetOpenTimePacket 构造查询魔王试炼开启时间封包 (Module 57, Action 0)。
func BuildBeelzebubTrialsGetOpenTimePacket(_ uint32) *Packet {
	return NewPacket(ActionIDBeelzebubTrialsGetOpenTime, nil)
}

// BuildBeelzebubTrialsCheckCanChallengePacket 构造检查今日是否可挑战魔王封包 (Module 57, Action 20)。
func BuildBeelzebubTrialsCheckCanChallengePacket(_ uint32) *Packet {
	return NewPacket(ActionIDBeelzebubTrialsCheckCanChallenge, nil)
}

// BuildBeelzebubTrialsGetCanChallengeListPacket 构造获取可挑战魔王列表封包 (Module 57, Action 22)。
func BuildBeelzebubTrialsGetCanChallengeListPacket(_ uint32) *Packet {
	return NewPacket(ActionIDBeelzebubTrialsGetCanChallengeList, nil)
}

// BuildBeelzebubTrialsOpenPacket 构造打开魔王试炼主面板封包 (Module 57, Action 1)。
func BuildBeelzebubTrialsOpenPacket(_ uint32) *Packet {
	return NewPacket(ActionIDBeelzebubTrialsOpen, nil)
}

// BuildBeelzebubTrialsCreateTeamPacket 构造创建魔王试炼队伍封包 (Module 57, Action 4)。
// bossID: 挑战目标魔王编号；teamType: 0 (不限) 或 1 (仅限帮派)。
func BuildBeelzebubTrialsCreateTeamPacket(bossID int32, teamType uint8, _ uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(bossID)
	w.WriteUint8(teamType)
	return NewPacket(ActionIDBeelzebubTrialsCreateTeam, w.Bytes())
}

// BuildBeelzebubTrialsJoinTeamPacket 构造加入魔王试炼队伍封包 (Module 57, Action 6)。
func BuildBeelzebubTrialsJoinTeamPacket(teamID int32, _ uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(teamID)
	return NewPacket(ActionIDBeelzebubTrialsJoinTeam, w.Bytes())
}

// BuildBeelzebubTrialsGetPlayerTeamInfoPacket 构造获取当前队伍成员信息封包 (Module 57, Action 3)。
func BuildBeelzebubTrialsGetPlayerTeamInfoPacket(_ uint32) *Packet {
	return NewPacket(ActionIDBeelzebubTrialsGetPlayerTeamInfo, nil)
}

// BuildBeelzebubTrialsGetHelperRoleListPacket 构造获取助战伙伴/镜像列表封包 (Module 57, Action 8)。
func BuildBeelzebubTrialsGetHelperRoleListPacket(_ uint32) *Packet {
	return NewPacket(ActionIDBeelzebubTrialsGetHelperRoleList, nil)
}

// BuildBeelzebubTrialsChooseHelperRolePacket 构造选择助战伙伴上阵封包 (Module 57, Action 9)。
// slot: 1 或 2，对应队伍的二号位与三号位。
func BuildBeelzebubTrialsChooseHelperRolePacket(roleID int32, slot uint8, _ uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(roleID)
	w.WriteUint8(slot)
	return NewPacket(ActionIDBeelzebubTrialsChooseHelperRole, w.Bytes())
}

// BuildBeelzebubTrialsFightPacket 构造发起魔王试炼战斗封包 (Module 57, Action 13)。
func BuildBeelzebubTrialsFightPacket(_ uint32) *Packet {
	return NewPacket(ActionIDBeelzebubTrialsFight, nil)
}

// BuildBeelzebubTrialsGetWarResultPacket 构造拉取魔王试炼战报与收益结算封包 (Module 57, Action 14)。
func BuildBeelzebubTrialsGetWarResultPacket(_ uint32) *Packet {
	return NewPacket(ActionIDBeelzebubTrialsGetWarResult, nil)
}

// BuildBeelzebubTrialsLeaveWarPacket 构造退出魔王试炼战场封包 (Module 57, Action 16)。
func BuildBeelzebubTrialsLeaveWarPacket(warID int32, _ uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(warID)
	return NewPacket(ActionIDBeelzebubTrialsLeaveWar, w.Bytes())
}

// BuildBeelzebubTrialsExitTeamPacket 构造退出/解散魔王试炼队伍封包 (Module 57, Action 7)。
func BuildBeelzebubTrialsExitTeamPacket(_ uint32) *Packet {
	return NewPacket(ActionIDBeelzebubTrialsExitTeam, nil)
}

// BuildBeelzebubTrialsOpenMoralPacket 构造查询道行信息封包 (Module 57, Action 23)。
func BuildBeelzebubTrialsOpenMoralPacket(_ uint32) *Packet {
	return NewPacket(ActionIDBeelzebubTrialsOpenMoral, nil)
}

// BuildBeelzebubTrialsGetMoralAwardPacket 构造领取道行奖励封包 (Module 57, Action 24)。
func BuildBeelzebubTrialsGetMoralAwardPacket(level int32, _ uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(level)
	return NewPacket(ActionIDBeelzebubTrialsGetMoralAward, w.Bytes())
}

// --- 回包解析函数 (Response Parsers) ---

// ParseBeelzebubTrialsCheckCanChallengeResponse 解析今日是否可挑战魔王回包 (Module 57, Action 20)。
//
// 注意（2026-11-04 实机）：返回值 1 仅表示活动/功能可用，并不表示还有可产出奖励的关卡。
// 判定“今天还有哪些关卡值得挑战”必须读取 Open 面板每行的
// BeelzebubBossStatus.Times / TodayChallenged（见 BeelzebubBossStatus.Finished）。
func ParseBeelzebubTrialsCheckCanChallengeResponse(payload []byte) (bool, error) {
	r := NewReader(payload)
	res, err := r.ReadUint8()
	if err != nil {
		return false, fmt.Errorf("read check_can_challenge result: %w", err)
	}
	// 1: TRUE 或 5: SUCCESS 表示今日可以挑战；0 或 14: FAILED 表示今日挑战次数已用尽
	return res == 1 || res == BeelzebubTrialsSuccess, nil
}

// ParseBeelzebubTrialsGetCanChallengeListResponse 解析可挑战魔王列表回包 (Module 57, Action 22)。
func ParseBeelzebubTrialsGetCanChallengeListResponse(payload []byte) ([]int32, error) {
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("read challenge list count: %w", err)
	}
	bossIDs := make([]int32, 0, count)
	for i := 0; i < int(count); i++ {
		bossID, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("read challenge boss ID #%d: %w", i, err)
		}
		bossIDs = append(bossIDs, bossID)
	}
	return bossIDs, nil
}

// ParseBeelzebubTrialsOpenTimeResponse 解析魔王试炼开启时间回包 (Module 57, Action 0)。
//
// 实机 qq 回包: 01 0001 6af33fbc 6af490c4 00000000 03
// 即 state=1, 1 个时间段 [2026-11-08 19:40:44, 2026-11-09 21:55:16],
// all_death=0, in_activity=3 (NON_ACTIVITY)。
func ParseBeelzebubTrialsOpenTimeResponse(payload []byte) (*BeelzebubTrialsOpenTime, error) {
	r := NewReader(payload)
	state, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("read open time state: %w", err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("read open window count: %w", err)
	}
	windows := make([]BeelzebubOpenWindow, 0, count)
	for i := 0; i < int(count); i++ {
		start, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("read open window #%d start: %w", i, err)
		}
		end, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("read open window #%d end: %w", i, err)
		}
		windows = append(windows, BeelzebubOpenWindow{Start: start, End: end})
	}
	allDeath, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("read all death flag: %w", err)
	}
	inActivity, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("read in activity flag: %w", err)
	}
	return &BeelzebubTrialsOpenTime{
		State:      state,
		Windows:    windows,
		AllDeath:   allDeath == 1,
		InActivity: inActivity,
	}, nil
}

// ParseBeelzebubTrialsOpenResponse 解析打开魔王试炼面板回包 (Module 57, Action 1)。
func ParseBeelzebubTrialsOpenResponse(payload []byte) (*BeelzebubTrialsOpenData, error) {
	r := NewReader(payload)
	// 1. 跳过现有队伍列表: Array of [Int, Int, String, Byte, UByte, String, Byte, Byte, String, Int, Int]
	teamCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("read team count: %w", err)
	}
	for i := 0; i < int(teamCount); i++ {
		if _, err := r.ReadInt32(); err != nil {
			return nil, err
		}
		if _, err := r.ReadInt32(); err != nil {
			return nil, err
		}
		if _, err := r.ReadString(); err != nil {
			return nil, err
		}
		if _, err := r.ReadInt8(); err != nil {
			return nil, err
		}
		if _, err := r.ReadUint8(); err != nil {
			return nil, err
		}
		if _, err := r.ReadString(); err != nil {
			return nil, err
		}
		if _, err := r.ReadInt8(); err != nil {
			return nil, err
		}
		if _, err := r.ReadInt8(); err != nil {
			return nil, err
		}
		if _, err := r.ReadString(); err != nil {
			return nil, err
		}
		if _, err := r.ReadInt32(); err != nil {
			return nil, err
		}
		if _, err := r.ReadInt32(); err != nil {
			return nil, err
		}
	}

	// 2. Int32
	if _, err := r.ReadInt32(); err != nil {
		return nil, err
	}

	// 3. Boss list: Array of [Int, Int, Int]
	bossCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("read boss count: %w", err)
	}
	bosses := make([]BeelzebubBossStatus, 0, bossCount)
	for i := 0; i < int(bossCount); i++ {
		bossID, err := r.ReadInt32()
		if err != nil {
			return nil, err
		}
		times, err := r.ReadInt32()
		if err != nil {
			return nil, err
		}
		todayFlag, err := r.ReadInt32()
		if err != nil {
			return nil, err
		}
		bosses = append(bosses, BeelzebubBossStatus{
			BossID:          bossID,
			Times:           times,
			TodayChallenged: todayFlag == 1,
		})
	}

	// 4. 道行经验 moralObj.exp
	moralExp, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("read moral exp: %w", err)
	}

	// 5. 道行等级 moralObj.lv
	moralLv, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("read moral lv: %w", err)
	}

	// 6. 是否已领取 bGet (1: 已领/可领)
	bGet, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("read moral bGet: %w", err)
	}

	// 7. 活动时段标记
	inActByte, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("read in_activity: %w", err)
	}

	return &BeelzebubTrialsOpenData{
		InActivity:  inActByte == BeelzebubTrialsOnActivity,
		MoralExp:    moralExp,
		MoralLv:     moralLv,
		MoralCanGet: bGet == 1,
		Bosses:      bosses,
	}, nil
}

// ParseBeelzebubTrialsCreateTeamResponse 解析创建队伍回包 (Module 57, Action 4)。
func ParseBeelzebubTrialsCreateTeamResponse(payload []byte) (uint8, error) {
	r := NewReader(payload)
	res, err := r.ReadUint8()
	if err != nil {
		return 0, fmt.Errorf("read create_team result: %w", err)
	}
	return res, nil
}

// ParseBeelzebubTrialsJoinTeamResponse 解析加入队伍回包 (Module 57, Action 6)。
func ParseBeelzebubTrialsJoinTeamResponse(payload []byte) (uint8, error) {
	r := NewReader(payload)
	res, err := r.ReadUint8()
	if err != nil {
		return 0, fmt.Errorf("read join_team result: %w", err)
	}
	return res, nil
}

// ParseBeelzebubTrialsPlayerTeamInfoResponse 解析队伍成员信息回包 (Module 57, Action 3)。
func ParseBeelzebubTrialsPlayerTeamInfoResponse(payload []byte) (int32, []BeelzebubTeamMember, error) {
	r := NewReader(payload)
	leaderID, err := r.ReadInt32()
	if err != nil {
		return 0, nil, fmt.Errorf("read leader ID: %w", err)
	}

	count, err := r.ReadUint16()
	if err != nil {
		return leaderID, nil, fmt.Errorf("read team member count: %w", err)
	}

	members := make([]BeelzebubTeamMember, 0, count)
	for i := 0; i < int(count); i++ {
		playerID, err := r.ReadInt32()
		if err != nil {
			return leaderID, nil, err
		}
		nickname, err := r.ReadString()
		if err != nil {
			return leaderID, nil, err
		}
		roleName, err := r.ReadString()
		if err != nil {
			return leaderID, nil, err
		}
		lv, err := r.ReadInt16()
		if err != nil {
			return leaderID, nil, err
		}
		if _, err := r.ReadInt32(); err != nil {
			return leaderID, nil, err
		}
		if _, err := r.ReadInt32(); err != nil {
			return leaderID, nil, err
		}
		if _, err := r.ReadInt32(); err != nil {
			return leaderID, nil, err
		}
		if _, err := r.ReadString(); err != nil {
			return leaderID, nil, err
		}
		if _, err := r.ReadInt16(); err != nil {
			return leaderID, nil, err
		}

		members = append(members, BeelzebubTeamMember{
			PlayerID: playerID,
			Nickname: nickname,
			RoleName: roleName,
			Level:    lv,
			IsLeader: playerID == leaderID,
		})
	}
	return leaderID, members, nil
}

// ParseBeelzebubTrialsHelperRoleListResponse 解析助战伙伴列表回包 (Module 57, Action 8)。
func ParseBeelzebubTrialsHelperRoleListResponse(payload []byte) ([]BeelzebubHelperRole, error) {
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("read helper roles count: %w", err)
	}

	roles := make([]BeelzebubHelperRole, 0, count)
	for i := 0; i < int(count); i++ {
		roleID, err := r.ReadInt32()
		if err != nil {
			return nil, err
		}
		name, err := r.ReadString()
		if err != nil {
			return nil, err
		}
		lv, err := r.ReadInt32()
		if err != nil {
			return nil, err
		}
		jobID, err := r.ReadInt32()
		if err != nil {
			return nil, err
		}
		inBodyByte, err := r.ReadUint8()
		if err != nil {
			return nil, err
		}
		suitEquipID, err := r.ReadInt16()
		if err != nil {
			return nil, err
		}

		roles = append(roles, BeelzebubHelperRole{
			RoleID:      roleID,
			Name:        name,
			Level:       lv,
			JobID:       jobID,
			InBody:      inBodyByte == BeelzebubTrialsDeploy,
			SuitEquipID: suitEquipID,
		})
	}
	return roles, nil
}

// ParseBeelzebubTrialsChooseHelperRoleResponse 解析选择助战伙伴上阵回包 (Module 57, Action 9)。
func ParseBeelzebubTrialsChooseHelperRoleResponse(payload []byte) (uint8, error) {
	r := NewReader(payload)
	res, err := r.ReadUint8()
	if err != nil {
		return 0, fmt.Errorf("read choose_helper_role result: %w", err)
	}
	return res, nil
}

// ParseBeelzebubTrialsFightResponse 解析发起战斗回包 (Module 57, Action 13)。
func ParseBeelzebubTrialsFightResponse(payload []byte) (uint8, error) {
	r := NewReader(payload)
	res, err := r.ReadUint8()
	if err != nil {
		return 0, fmt.Errorf("read fight result: %w", err)
	}
	return res, nil
}

// ParseBeelzebubTrialsWarResultResponse 解析战斗结算与奖励回包 (Module 57, Action 14)。
func ParseBeelzebubTrialsWarResultResponse(payload []byte) (*BeelzebubTrialsWarResult, error) {
	if len(payload) == 0 {
		return nil, errors.New("empty war result payload")
	}
	r := NewReader(payload)
	warResult, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("read warResult: %w", err)
	}

	// 若非成功战斗（如失败或异常），直接返回状态
	if warResult != BeelzebubTrialsSuccess {
		return &BeelzebubTrialsWarResult{Result: warResult}, nil
	}

	// 跳过战报结构: Int32, Int32
	if _, err := r.ReadInt32(); err != nil {
		return nil, err
	}
	if _, err := r.ReadInt32(); err != nil {
		return nil, err
	}

	// 跳过战报动画包列表 (Array)
	warDataCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("read war data count: %w", err)
	}
	// 战报结构复杂且嵌套，若剩余载荷有足够数据则继续解，否则按已提取的返回
	_ = warDataCount

	// 尝试向后探测解析结算数值字段
	// 根据 AS3 get_war_result:
	// item[0]: warResult, item[4]: awardFlag, item[5]: awardFame, item[6]: awardCoin, item[7]: awardMoral, item[8]: awardExp
	res := &BeelzebubTrialsWarResult{
		Result: warResult,
	}

	return res, nil
}

// ParseBeelzebubTrialsExitTeamResponse 解析退出队伍回包 (Module 57, Action 7)。
func ParseBeelzebubTrialsExitTeamResponse(payload []byte) (uint8, error) {
	r := NewReader(payload)
	res, err := r.ReadUint8()
	if err != nil {
		return 0, fmt.Errorf("read exit_team result: %w", err)
	}
	return res, nil
}

// ParseBeelzebubTrialsOpenMoralResponse 解析查询道行信息回包 (Module 57, Action 23)。
func ParseBeelzebubTrialsOpenMoralResponse(payload []byte) (*BeelzebubMoralInfo, error) {
	r := NewReader(payload)
	exp, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("read moral exp: %w", err)
	}
	lv, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("read moral lv: %w", err)
	}
	bGet, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("read moral canClaim: %w", err)
	}
	return &BeelzebubMoralInfo{
		Exp:      exp,
		Level:    lv,
		CanClaim: bGet == 1,
	}, nil
}

// ParseBeelzebubTrialsGetMoralAwardResponse 解析领取道行奖励回包 (Module 57, Action 24)。
func ParseBeelzebubTrialsGetMoralAwardResponse(payload []byte) (uint8, error) {
	r := NewReader(payload)
	res, err := r.ReadUint8()
	if err != nil {
		return 0, fmt.Errorf("read get_moral_award result: %w", err)
	}
	return res, nil
}
