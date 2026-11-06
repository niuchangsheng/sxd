// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现魔王试炼 (Mod_BeelzebubTrials_Base, Module 57 / 0x0039) 核心协议报文编解码。
package protocol

import (
	"errors"
	"fmt"
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

// beelzebubBossNames 魔王试炼 Boss 名录映射表 (ID 1 ~ 159，对齐 BeelzeBubTrialsBossType)。
var beelzebubBossNames = map[int32]string{
	1: "魔化大魔王", 2: "魔化东皇", 3: "魔神蚩尤", 4: "魔化孢子王", 5: "魔化血魔",
	6: "魔化龅牙", 7: "幽鬼", 8: "将臣", 9: "黑寡妇", 10: "九尾妖狐",
	11: "雷兽", 12: "句芒", 13: "魔化刑天", 14: "天妖星", 15: "鬼眼",
	16: "镇墓兽", 17: "魔化天将", 18: "魔化杨戬", 19: "魔萨菲罗斯", 20: "魔天夕道人",
	21: "魔化海龙王", 22: "魔化鲲神", 23: "降龙罗汉", 24: "魔化百叶佛", 29: "魔宫本武藏",
	30: "魔化秦始皇", 31: "魔化徐福", 32: "魔伯牙子期", 33: "魔化辉煌帝", 34: "魔化旱魃",
	35: "魔化达芬奇", 36: "魔化画师燕", 37: "魔化雷龙", 38: "魔化黑龙", 39: "魔化黄袍怪",
	40: "魔化鹏魔王", 41: "魔剑", 42: "紫霞仙子", 43: "牛魔王", 44: "拓跋烈",
	45: "无崖子", 46: "金角大王", 47: "执念心魔", 48: "妄念心魔", 49: "魔豺狼族长",
	50: "魔比蒙巨兽", 51: "魔空桑仙子", 52: "魔化九凤", 53: "魔化蜂王", 54: "魔牡丹仙子",
	55: "魔化鹦鹉螺", 56: "远古冰晶魔", 57: "魔化河伯", 58: "魔化红孩儿", 59: "魔铁扇公主",
	60: "魔海马将军", 61: "魔化冰蛤", 62: "魔化金蛤", 63: "魔化蛇傀", 64: "魔化利齿狼",
	65: "魔化玄鸟", 66: "魔化小蝶", 67: "魔蓝翎侍卫", 68: "魔大内侍卫", 69: "魔化天泣",
	70: "魔先辈英魂", 71: "魔掌门元识", 72: "魔掌门神识", 73: "魔白泽幻兽", 74: "魔太古猿君",
	75: "魔化燃魁", 76: "魔化赤焰兽", 77: "魔雪山巨猿", 78: "魔化兔妖", 79: "魔化应龙",
	80: "魔化冰晶兽", 81: "魔催命判官", 82: "魔五道将军", 83: "魔化日游", 84: "魔化夜游",
	85: "魔化阿波", 86: "魔化龟仙人", 87: "魔化巨灵神", 88: "魔托塔天王", 89: "魔化开明兽",
	90: "魔化李易", 91: "魔化李白", 92: "魔化杜甫", 93: "魔沼泽巨兽", 94: "魔化金蚕王",
	95: "魔图腾兽鹰", 96: "魔图腾兽狼", 97: "魔化金太权", 98: "魔化田禹治", 99: "魔化鬼魔将",
	100: "魔化毒魔将", 101: "魔化罗睺兽", 102: "魔化魅魔", 103: "魔化雷修罗", 104: "魔化斩修罗",
	105: "魔化剑客", 106: "魔化火铳手", 107: "魔七段棋师", 108: "魔九段圣手", 109: "魔化卫庄",
	110: "少司命", 111: "魔蛮荒神巫", 112: "魔化岩龙", 113: "魔化木龙", 114: "魔化火龙",
	115: "魔化风龙", 116: "魔化蝎子精", 117: "魔化琵琶精", 118: "魔虎力大仙", 119: "魔鹿力大仙",
	120: "魔化小师妹", 121: "牡丹魔幻象", 122: "仇恨魔幻象", 123: "好胜魔幻象", 124: "魔化霸蛮王",
	125: "魔化申公豹", 126: "魔化纣王", 127: "魔陆压道君", 128: "魔化姜子牙", 129: "魔化天罡星",
	130: "魔化地煞星", 131: "魔化奎木狼", 132: "魔化斗木獬", 133: "魔化氐土貉", 134: "魔化土行孙",
	135: "魔化斗金牛", 136: "魔太白金星", 137: "鬼金羊", 138: "魔扛鼎巨人", 139: "魔魂眼冰王",
	140: "魔化黑狱", 141: "魔化雷素素", 142: "魔化雷小鼓", 143: "魔绝大公子", 144: "美猴王",
	145: "书中仙人", 146: "幻将军", 147: "先知风伯", 148: "火焰神", 149: "金角葫芦",
	150: "蝠妖女", 151: "狂雷战将", 152: "鬼斧", 153: "魔化玉兔", 154: "魔化织女",
	155: "魔化月老", 156: "魔黑水将军", 157: "魔化雀神", 158: "魔化蓝采和", 159: "龙之魔女",
}

// BeelzebubTrialsBossName 根据魔王编号返回 Boss 中文名称。
func BeelzebubTrialsBossName(bossID int32) string {
	if name, ok := beelzebubBossNames[bossID]; ok {
		return name
	}
	return fmt.Sprintf("试炼魔王(%d)", bossID)
}

// beelzebubBossLevels 魔王试炼 Boss 开放等级映射表 (ID 1 ~ 159，对齐 BeelzeBubTrialsBossType.Bosses)。
var beelzebubBossLevels = map[int32]int32{
	1: 100, 2: 120, 3: 150, 4: 130, 5: 140, 6: 150, 7: 15, 8: 15, 9: 20, 10: 20,
	11: 30, 12: 30, 13: 40, 14: 40, 15: 50, 16: 50, 17: 60, 18: 60, 19: 70, 20: 70,
	21: 80, 22: 80, 23: 90, 24: 90, 29: 100, 30: 110, 31: 110, 32: 120, 33: 130, 34: 140,
	35: 145, 36: 145, 37: 155, 38: 155, 39: 160, 40: 160, 41: 30, 42: 60, 43: 90, 44: 120,
	45: 145, 46: 160, 47: 165, 48: 165, 49: 70, 50: 70, 51: 70, 52: 70, 53: 80, 54: 80,
	55: 80, 56: 90, 57: 90, 58: 90, 59: 90, 60: 80, 61: 15, 62: 15, 63: 15, 64: 15,
	65: 20, 66: 20, 67: 20, 68: 20, 69: 30, 70: 30, 71: 30, 72: 30, 73: 40, 74: 40,
	75: 40, 76: 40, 77: 50, 78: 50, 79: 50, 80: 50, 81: 60, 82: 60, 83: 60, 84: 60,
	85: 100, 86: 100, 87: 100, 88: 100, 89: 110, 90: 110, 91: 110, 92: 110, 93: 120, 94: 120,
	95: 120, 96: 120, 97: 130, 98: 130, 99: 130, 100: 130, 101: 140, 102: 140, 103: 140, 104: 140,
	105: 145, 106: 145, 107: 145, 108: 145, 109: 150, 110: 150, 111: 150, 112: 155, 113: 155, 114: 155,
	115: 155, 116: 160, 117: 160, 118: 160, 119: 160, 120: 165, 121: 165, 122: 165, 123: 165, 124: 150,
	125: 170, 126: 170, 127: 170, 128: 170, 129: 170, 130: 170, 131: 175, 132: 175, 133: 175, 134: 175,
	135: 175, 136: 175, 137: 175, 138: 180, 139: 180, 140: 180, 141: 180, 142: 180, 143: 180, 144: 100,
	145: 110, 146: 120, 147: 130, 148: 140, 149: 150, 150: 160, 151: 170, 152: 180, 153: 185, 154: 185,
	155: 185, 156: 185, 157: 185, 158: 185, 159: 185,
}

// BeelzebubTrialsBossLevel 根据魔王编号返回 Boss 开放/挑战等级。
func BeelzebubTrialsBossLevel(bossID int32) int32 {
	if lv, ok := beelzebubBossLevels[bossID]; ok {
		return lv
	}
	return 0
}

// beelzebubBossMaxTimes 魔王试炼关卡累计可挑战次数 (对齐官方
// BeelzeBubTrialsBossType.Bosses[bossID][15])。
//
// 面板回包第二列 (times) 达到该值后，官方客户端即把关卡标记为“已完成”；
// 服务端仍然允许建队与战斗，但不再产出声望/铜钱/道行等奖励，自动化应跳过以免空转。
// 神秘层关卡上限固定为 1。
var beelzebubBossMaxTimes = map[int32]int32{
	1: 7, 2: 7, 3: 10, 4: 10, 5: 10, 6: 10, 7: 3, 8: 3, 9: 3, 10: 3,
	11: 3, 12: 3, 13: 5, 14: 5, 15: 5, 16: 5, 17: 5, 18: 5, 19: 5, 20: 5,
	21: 5, 22: 5, 23: 5, 24: 5, 29: 7, 30: 7, 31: 7, 32: 7, 33: 10, 34: 10,
	35: 10, 36: 10, 37: 10, 38: 10, 39: 10, 40: 10, 41: 1, 42: 1, 43: 1, 44: 1,
	45: 1, 46: 1, 47: 15, 48: 15, 49: 5, 50: 5, 51: 5, 52: 5, 53: 5, 54: 5,
	55: 5, 56: 5, 57: 5, 58: 5, 59: 5, 60: 5, 61: 3, 62: 3, 63: 3, 64: 3,
	65: 3, 66: 3, 67: 3, 68: 3, 69: 3, 70: 3, 71: 3, 72: 3, 73: 5, 74: 5,
	75: 5, 76: 5, 77: 5, 78: 5, 79: 5, 80: 5, 81: 5, 82: 5, 83: 5, 84: 5,
	85: 7, 86: 7, 87: 7, 88: 7, 89: 7, 90: 7, 91: 7, 92: 7, 93: 7, 94: 7,
	95: 7, 96: 7, 97: 10, 98: 10, 99: 10, 100: 10, 101: 10, 102: 10, 103: 10, 104: 10,
	105: 10, 106: 10, 107: 10, 108: 10, 109: 10, 110: 10, 111: 10, 112: 10, 113: 10, 114: 10,
	115: 10, 116: 10, 117: 10, 118: 10, 119: 10, 120: 15, 121: 15, 122: 15, 123: 15, 124: 10,
	125: 15, 126: 15, 127: 15, 128: 15, 129: 15, 130: 15, 131: 15, 132: 15, 133: 15, 134: 15,
	135: 15, 136: 15, 137: 1, 138: 20, 139: 20, 140: 20, 141: 20, 142: 20, 143: 20, 144: 1,
	145: 1, 146: 1, 147: 1, 148: 1, 149: 1, 150: 1, 151: 1, 152: 1, 153: 20, 154: 20,
	155: 20, 156: 20, 157: 20, 158: 20, 159: 1,
}

// BeelzebubTrialsBossMaxTimes 返回该魔王的累计可挑战次数；未知关卡返回 0。
func BeelzebubTrialsBossMaxTimes(bossID int32) int32 {
	return beelzebubBossMaxTimes[bossID]
}

// beelzebubBossSecrets 魔王试炼神秘层标记映射表 (对齐 BeelzeBubTrialsBossType.Bosses 第 10 列)。
//
//	0: 普通关卡
//	1: 神秘层关卡 (界面归入"神秘层"分组)
//	2: 神秘层关卡，且显示名称需追加"<等级>级"
var beelzebubBossSecrets = map[int32]uint8{
	41: 1, 42: 1, 43: 1, 44: 1, 45: 1, 46: 1, 137: 1, 159: 1,
	144: 2, 145: 2, 146: 2, 147: 2, 148: 2, 149: 2, 150: 2, 151: 2, 152: 2,
}

// BeelzebubTrialsBossSecret 返回 Boss 的神秘层标记 (0=普通，1=神秘层，2=神秘层且名称带等级)。
func BeelzebubTrialsBossSecret(bossID int32) uint8 {
	return beelzebubBossSecrets[bossID]
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
	maxTimes, ok := beelzebubBossMaxTimes[b.BossID]
	if !ok {
		// 未知关卡（新版本新增内容）不做过度过滤，交由服务端裁决。
		return false
	}
	return b.Times >= maxTimes
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
