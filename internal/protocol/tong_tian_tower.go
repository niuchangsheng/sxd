// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方通天塔协议 (Mod_TongTianTower_Base, Module 648 / 0x0288)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_TongTianTower_Base 与
// com.datas.TongTianTowerData：
//   - panel_info          Act 0  -> 0x02880000  查询重天/层数/挑战次数/突破状态
//   - challenge           Act 1  -> 0x02880001  [UByte team_type, Int monster_team_id] 列表
//   - practice            Act 2  -> 0x02880002  Int heaven, Int floor, Int battle_timer
//   - break_up            Act 5  -> 0x02880005  突破当前重天
//   - cancel_practice     Act 9  -> 0x02880009  停止扫荡
//   - notify              Act 11 -> 0x0288000B  扫荡/挑战推送
package protocol

import "fmt"

const (
	// ModuleTongTianTower 通天塔模块编号 (0x0288)
	ModuleTongTianTower uint16 = 648
)

// 通天塔动作编号 (Mod_TongTianTower_Base)。
const (
	// ActionIDTongTianTowerPanel 查询通天塔面板 (Action 0 -> 0x02880000)
	ActionIDTongTianTowerPanel uint32 = 0x02880000
	// ActionIDTongTianTowerChallenge 挑战当前层 (Action 1 -> 0x02880001)
	ActionIDTongTianTowerChallenge uint32 = 0x02880001
	// ActionIDTongTianTowerPractice 开始扫荡当前层 (Action 2 -> 0x02880002)
	ActionIDTongTianTowerPractice uint32 = 0x02880002
	// ActionIDTongTianTowerBreakUp 突破当前重天 (Action 5 -> 0x02880005)
	ActionIDTongTianTowerBreakUp uint32 = 0x02880005
	// ActionIDTongTianTowerCancelPractice 停止扫荡 (Action 9 -> 0x02880009)
	ActionIDTongTianTowerCancelPractice uint32 = 0x02880009
)

// 通天塔队伍类型 (Mod_TongTianTower_Base)。
const (
	// TongTianTowerMainTeam 主队伍
	TongTianTowerMainTeam uint8 = 0
	// TongTianTowerSoulTeam 灵结队伍
	TongTianTowerSoulTeam uint8 = 1
)

// 通天塔结果码 (Mod_TongTianTower_Base)。
const (
	// TongTianTowerResultSuccess 成功
	TongTianTowerResultSuccess uint8 = 2
	// TongTianTowerResultFailed 失败
	TongTianTowerResultFailed uint8 = 3
	// TongTianTowerResultTopFloor 已达最高层
	TongTianTowerResultTopFloor uint8 = 4
	// TongTianTowerResultBagFull 背包已满
	TongTianTowerResultBagFull uint8 = 5
	// TongTianTowerResultCDTime 冷却中
	TongTianTowerResultCDTime uint8 = 6
	// TongTianTowerResultNotEnoughPower 体力不足
	TongTianTowerResultNotEnoughPower uint8 = 7
	// TongTianTowerResultChallengeTimesLimit 挑战次数已用完
	TongTianTowerResultChallengeTimesLimit uint8 = 8
	// TongTianTowerResultNeedBreakUp 需要先突破当前重天
	TongTianTowerResultNeedBreakUp uint8 = 9
	// TongTianTowerResultMonsterTeamError 怪物队伍错误
	TongTianTowerResultMonsterTeamError uint8 = 10
	// TongTianTowerResultTeamTypeError 队伍类型错误
	TongTianTowerResultTeamTypeError uint8 = 11
	// TongTianTowerResultNoComplete 当前层未通关
	TongTianTowerResultNoComplete uint8 = 12
	// TongTianTowerResultInPractice 正在扫荡中
	TongTianTowerResultInPractice uint8 = 13
	// TongTianTowerResultMaterialNumLimit 材料数量达到上限
	TongTianTowerResultMaterialNumLimit uint8 = 14
	// TongTianTowerResultConditionLimit 突破条件不满足
	TongTianTowerResultConditionLimit uint8 = 16
	// TongTianTowerResultNotNeedBreakUp 不需要突破
	TongTianTowerResultNotNeedBreakUp uint8 = 17
	// TongTianTowerResultAlreadyBreakUp 已经突破
	TongTianTowerResultAlreadyBreakUp uint8 = 18
	// TongTianTowerResultPracticeEnd 扫荡结束
	TongTianTowerResultPracticeEnd uint8 = 20
)

// TongTianTowerPanel 表示通天塔主面板。
//
// 字段顺序严格对齐 Mod_TongTianTower_Base.panel_info.response：
// Int heaven, floor, get_ingot, award_ingot_limit, challenge_times, is_break_up,
// difficulty, completed_num, break_up_time_left, cur_heaven, cur_floor,
// material_dop_num, Byte is_weekend_discount, Int top_rank, top_time,
// hire_role_id, sky_palace_level。
type TongTianTowerPanel struct {
	// Heaven 当前重天 (0 表示已通关)
	Heaven int32
	// Floor 当前层
	Floor int32
	// GetIngot 已领取元宝数
	GetIngot int32
	// AwardIngotLimit 元宝奖励上限
	AwardIngotLimit int32
	// ChallengeTimes 剩余挑战次数
	ChallengeTimes int32
	// IsBreakUp 是否可以突破 (1 表示可突破)
	IsBreakUp int32
	// Difficulty 当前难度
	Difficulty int32
	// CompletedNum 今日已通关层数
	CompletedNum int32
	// BreakUpTimeLeft 突破剩余时间
	BreakUpTimeLeft int32
	// CurHeaven 当前可扫荡重天
	CurHeaven int32
	// CurFloor 当前可扫荡层
	CurFloor int32
	// MaterialDopNum 材料掉落次数
	MaterialDopNum int32
	// IsWeekendDiscount 周末折扣标志
	IsWeekendDiscount uint8
	// TopRank 排行
	TopRank int32
	// TopTime 排行时间
	TopTime int32
	// HireRoleID 雇佣角色 ID
	HireRoleID int32
	// SkyPalaceLevel 天宫等级
	SkyPalaceLevel int32
}

// TongTianTowerTeam 表示挑战时选择的一个队伍。
type TongTianTowerTeam struct {
	// TeamType 队伍类型 (0 主队伍 / 1 灵结队伍)
	TeamType uint8
	// MonsterTeamID 怪物队伍 ID (取自每层静态表)
	MonsterTeamID int32
}

// TongTianTowerChallengeResult 表示一次挑战的即时结果。
type TongTianTowerChallengeResult struct {
	// Result 结果码 (TongTianTowerResultXxx)
	Result uint8
	// AwardItemIDs 本次获得物品 ID 列表
	AwardItemIDs []int32
	// AwardItemNums 与 AwardItemIDs 一一对应的数量
	AwardItemNums []int32
	// DeployType 战报布阵类型
	DeployType uint8
	// WinPlayerID 战斗胜利方玩家 ID
	WinPlayerID int32
}

// Success 判断服务端是否受理本次挑战 (Result == SUCCESS)。
func (r *TongTianTowerChallengeResult) Success() bool {
	return r != nil && r.Result == TongTianTowerResultSuccess
}

// IsWin 判断 playerID 是否赢得本次战斗。
func (r *TongTianTowerChallengeResult) IsWin(playerID int32) bool {
	return r != nil && r.Success() && r.WinPlayerID == playerID
}

// TongTianTowerPracticeResult 表示扫荡开始结果。
type TongTianTowerPracticeResult struct {
	// Result 结果码
	Result uint8
	// PracticeTime 预计扫荡时长
	PracticeTime int32
	// Power 当前体力
	Power int32
}

// TongTianTowerMonsterTeamID 返回指定重天/层的怪物队伍 ID。
//
// 数据来自官方 TongTianTowerTypeData.TowerArray；重天与层数均从 1 开始。
// 未命中的层数返回 0，调用方应跳过挑战而不是发送 0。
func TongTianTowerMonsterTeamID(heaven, floor int32) int32 {
	idx := (heaven-1)*100 + floor
	if idx <= 0 || idx >= int32(len(tongTianTowerMonsterTeamIDs)) {
		return 0
	}
	return tongTianTowerMonsterTeamIDs[idx]
}

// BuildTongTianTowerPanelPacket 构造查询通天塔面板封包 (0x02880000)。
func BuildTongTianTowerPanelPacket() *Packet {
	return NewPacket(ActionIDTongTianTowerPanel, nil)
}

// BuildTongTianTowerChallengePacket 构造挑战封包 (0x02880001)。
func BuildTongTianTowerChallengePacket(teams []TongTianTowerTeam) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(teams)))
	for _, t := range teams {
		w.WriteUint8(t.TeamType)
		w.WriteInt32(t.MonsterTeamID)
	}
	return NewPacket(ActionIDTongTianTowerChallenge, w.Bytes())
}

// BuildTongTianTowerPracticePacket 构造扫荡封包 (0x02880002)。
//
// battleTimer 为本次扫荡时长，客户端以秒为单位传给服务端。
func BuildTongTianTowerPracticePacket(heaven, floor, battleTimer int32) *Packet {
	w := NewWriter()
	w.WriteInt32(heaven)
	w.WriteInt32(floor)
	w.WriteInt32(battleTimer)
	return NewPacket(ActionIDTongTianTowerPractice, w.Bytes())
}

// BuildTongTianTowerBreakUpPacket 构造突破封包 (0x02880005)。
func BuildTongTianTowerBreakUpPacket() *Packet {
	return NewPacket(ActionIDTongTianTowerBreakUp, nil)
}

// BuildTongTianTowerCancelPracticePacket 构造停止扫荡封包 (0x02880009)。
func BuildTongTianTowerCancelPracticePacket() *Packet {
	return NewPacket(ActionIDTongTianTowerCancelPractice, nil)
}

// ParseTongTianTowerPanelResult 解析通天塔面板。
func ParseTongTianTowerPanelResult(payload []byte) (*TongTianTowerPanel, error) {
	r := NewReader(payload)
	p := &TongTianTowerPanel{}
	var err error
	for i, field := range []*int32{
		&p.Heaven, &p.Floor, &p.GetIngot, &p.AwardIngotLimit, &p.ChallengeTimes,
		&p.IsBreakUp, &p.Difficulty, &p.CompletedNum, &p.BreakUpTimeLeft,
		&p.CurHeaven, &p.CurFloor, &p.MaterialDopNum,
	} {
		if *field, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read panel field %d: %v", ErrInvalidPayload, i, err)
		}
	}
	if p.IsWeekendDiscount, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read is_weekend_discount: %v", ErrInvalidPayload, err)
	}
	for i, field := range []*int32{&p.TopRank, &p.TopTime, &p.HireRoleID, &p.SkyPalaceLevel} {
		if *field, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read panel tail field %d: %v", ErrInvalidPayload, i, err)
		}
	}
	return p, nil
}

// ParseTongTianTowerChallengeResult 解析挑战回包。
//
// 官方 schema: UByte result, [ [Int item_id, Int item_num] ], [Byte deploy_type, Int win_player_id, ...战报]。
func ParseTongTianTowerChallengeResult(payload []byte) (*TongTianTowerChallengeResult, error) {
	r := NewReader(payload)
	res := &TongTianTowerChallengeResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read challenge result: %v", ErrInvalidPayload, err)
	}
	if res.Result != TongTianTowerResultSuccess {
		return res, nil
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read award count: %v", ErrInvalidPayload, err)
	}
	for i := 0; i < int(count); i++ {
		itemID, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read award[%d].item_id: %v", ErrInvalidPayload, i, err)
		}
		itemNum, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read award[%d].item_num: %v", ErrInvalidPayload, i, err)
		}
		res.AwardItemIDs = append(res.AwardItemIDs, itemID)
		res.AwardItemNums = append(res.AwardItemNums, itemNum)
	}
	if res.DeployType, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read deploy_type: %v", ErrInvalidPayload, err)
	}
	if res.WinPlayerID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read win_player_id: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// ParseTongTianTowerPracticeResult 解析扫荡开始回包。
func ParseTongTianTowerPracticeResult(payload []byte) (*TongTianTowerPracticeResult, error) {
	r := NewReader(payload)
	res := &TongTianTowerPracticeResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read practice result: %v", ErrInvalidPayload, err)
	}
	if res.Result != TongTianTowerResultSuccess {
		return res, nil
	}
	if res.PracticeTime, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read practice_time: %v", ErrInvalidPayload, err)
	}
	if res.Power, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read practice power: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// ParseTongTianTowerActionCode 解析仅返回单个结果码的动作 (break_up / cancel_practice)。
func ParseTongTianTowerActionCode(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty tong tian tower action result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// BuildTongTianTowerPanelResultPacket 构造面板回包，供离线 mock 与单测使用。
func BuildTongTianTowerPanelResultPacket(p TongTianTowerPanel) *Packet {
	w := NewWriter()
	for _, v := range []int32{
		p.Heaven, p.Floor, p.GetIngot, p.AwardIngotLimit, p.ChallengeTimes,
		p.IsBreakUp, p.Difficulty, p.CompletedNum, p.BreakUpTimeLeft,
		p.CurHeaven, p.CurFloor, p.MaterialDopNum,
	} {
		w.WriteInt32(v)
	}
	w.WriteUint8(p.IsWeekendDiscount)
	for _, v := range []int32{p.TopRank, p.TopTime, p.HireRoleID, p.SkyPalaceLevel} {
		w.WriteInt32(v)
	}
	return NewPacket(ActionIDTongTianTowerPanel, w.Bytes())
}

// BuildTongTianTowerChallengeResultPacket 构造挑战回包，供离线 mock 与单测使用。
func BuildTongTianTowerChallengeResultPacket(res TongTianTowerChallengeResult) *Packet {
	w := NewWriter()
	w.WriteUint8(res.Result)
	if res.Result == TongTianTowerResultSuccess {
		w.WriteUint16(uint16(len(res.AwardItemIDs)))
		for i := range res.AwardItemIDs {
			w.WriteInt32(res.AwardItemIDs[i])
			var num int32
			if i < len(res.AwardItemNums) {
				num = res.AwardItemNums[i]
			}
			w.WriteInt32(num)
		}
		w.WriteUint8(res.DeployType)
		w.WriteInt32(res.WinPlayerID)
	}
	return NewPacket(ActionIDTongTianTowerChallenge, w.Bytes())
}

// BuildTongTianTowerPracticeResultPacket 构造扫荡回包，供离线 mock 与单测使用。
func BuildTongTianTowerPracticeResultPacket(res TongTianTowerPracticeResult) *Packet {
	w := NewWriter()
	w.WriteUint8(res.Result)
	if res.Result == TongTianTowerResultSuccess {
		w.WriteInt32(res.PracticeTime)
		w.WriteInt32(res.Power)
	}
	return NewPacket(ActionIDTongTianTowerPractice, w.Bytes())
}

// BuildTongTianTowerActionCodePacket 构造单结果码回包，供离线 mock 与单测使用。
func BuildTongTianTowerActionCodePacket(actionID uint32, code uint8) *Packet {
	return NewPacket(actionID, []byte{code})
}
