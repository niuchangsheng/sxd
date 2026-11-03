// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方仙界竞技场协议 (Mod_StArena_Base, Module 214 / 0x00D6)。
// 该模块仅在 Mod 94 登录的仙界跨服连接上可用；主服网关不会响应。
// 字段布局与消息码严格对齐官方 AS3 客户端反编译产物。
//
// @author Ateng
// @since 2026-11-02
package protocol

import (
	"fmt"
)

const (
	// ModuleStArena 仙界竞技场跨服模块编号 (0x00D6)
	ModuleStArena uint16 = 214
)

// 仙界竞技场消息码 (Mod_StArena_Base)。
const (
	// ActionIDStArenaOpen 打开仙界竞技场面板 (Action 0 -> 0x00D60000)
	ActionIDStArenaOpen uint32 = 0x00D60000
	// ActionIDStArenaRefreshPlayerList 刷新可挑战对手列表 (Action 1 -> 0x00D60001)
	ActionIDStArenaRefreshPlayerList uint32 = 0x00D60001
	// ActionIDStArenaChallenge 向指定对手发起挑战 (Action 2 -> 0x00D60002)
	ActionIDStArenaChallenge uint32 = 0x00D60002
	// ActionIDStArenaClose 关闭仙界竞技场面板 (Action 4 -> 0x00D60004)
	ActionIDStArenaClose uint32 = 0x00D60004
)

// 仙界竞技场操作结果码 (Mod_StArena_Base)。
//
// 数值严格对齐官方 com.protocols.Mod_StArena_Base 的常量定义；
// 13 之后的码位是连续自增的，漏掉 FUNC_END 会让后续所有失败语义整体错位。
const (
	// StArenaResultSuccess 操作成功
	StArenaResultSuccess uint8 = 13
	// StArenaResultCDTime 挑战冷却中
	StArenaResultCDTime uint8 = 14
	// StArenaResultFuncEnd 功能已结束 (非积分赛阶段或活动关闭)
	StArenaResultFuncEnd uint8 = 15
	// StArenaResultFail 操作失败
	StArenaResultFail uint8 = 16
	// StArenaResultNotEnoughChallengeTimes 挑战次数不足
	StArenaResultNotEnoughChallengeTimes uint8 = 17
	// StArenaResultNotInList 挑战目标不在当前列表
	StArenaResultNotInList uint8 = 18
	// StArenaResultItemNotExist 兑换物品不存在
	StArenaResultItemNotExist uint8 = 19
	// StArenaResultNotEnoughExploit 荣誉不足
	StArenaResultNotEnoughExploit uint8 = 20
	// StArenaResultNotEnoughNum 数量不足
	StArenaResultNotEnoughNum uint8 = 21
	// StArenaResultNotEnoughFun 功能未开启
	StArenaResultNotEnoughFun uint8 = 22
	// StArenaResultPackFull 背包已满
	StArenaResultPackFull uint8 = 23
	// StArenaResultRebet 重复下注
	StArenaResultRebet uint8 = 24
)

// 仙界竞技场赛程阶段 (Mod_StArena_Base)。
const (
	// StArenaStageScoreRace 积分赛进行中
	StArenaStageScoreRace uint8 = 0
	// StArenaStageScoreRaceComplete 积分赛结束
	StArenaStageScoreRaceComplete uint8 = 1
	// StArenaStageCupRaceInit 杯赛初始化
	StArenaStageCupRaceInit uint8 = 2
	// StArenaStageWar16 16 强赛
	StArenaStageWar16 uint8 = 3
	// StArenaStageWar16Complete 16 强赛结束
	StArenaStageWar16Complete uint8 = 4
	// StArenaStageWar8 8 强赛
	StArenaStageWar8 uint8 = 5
	// StArenaStageWar8Complete 8 强赛结束
	StArenaStageWar8Complete uint8 = 6
	// StArenaStageWar4 4 强赛
	StArenaStageWar4 uint8 = 7
	// StArenaStageWar4Complete 4 强赛结束
	StArenaStageWar4Complete uint8 = 8
	// StArenaStageWar2 半决赛
	StArenaStageWar2 uint8 = 9
	// StArenaStageWar2Complete 半决赛结束
	StArenaStageWar2Complete uint8 = 10
	// StArenaStageWar1 决赛
	StArenaStageWar1 uint8 = 11
	// StArenaStageWar1Complete 决赛结束
	StArenaStageWar1Complete uint8 = 12
)

// StArenaOpponent 代表仙界竞技场中可挑战的对手。
//
// 线格式 (Mod_StArena_Base.open_st_arena 回包 challengeList 元素):
//
//	Int32 st_player_id, Int32 integral, Int32 role_id, Int16 level
type StArenaOpponent struct {
	// StPlayerID 跨服玩家 ID (挑战请求的稳定目标标识)
	StPlayerID int32
	// Integral 对手当前竞技场积分
	Integral int32
	// RoleID 对手职业/造型编号
	RoleID int32
	// Level 对手等级 (服务端未下发战力，等级是唯一的实力代理指标)
	Level int16
}

// StArenaWarReport 代表仙界竞技场面板中的一条历史战报。
//
// 线格式 (open_st_arena 回包 reportList 元素):
//
//	Int32 war_report_id, Byte is_read, Int32 attack_id, String attack_name,
//	Int32 defense_id, String defense_name, Int32 win_id, Int32 integral,
//	String version, Int32 time, Byte is_back
type StArenaWarReport struct {
	// WarReportID 战报唯一编号
	WarReportID int32
	// IsRead 是否已读 (0 未读, 1 已读)
	IsRead uint8
	// AttackID 进攻方玩家 ID
	AttackID int32
	// AttackName 进攻方角色名
	AttackName string
	// DefenseID 防守方玩家 ID
	DefenseID int32
	// DefenseName 防守方角色名
	DefenseName string
	// WinID 胜利方玩家 ID
	WinID int32
	// Integral 本次战斗积分变化值
	Integral int32
	// Version 战报版本标识
	Version string
	// Time 战报生成服务器时间戳
	Time int32
	// IsBack 是否为被挑战方视角 (1=是)
	IsBack uint8
}

// IsWin 判断该战报中 playerID 是否获胜。
func (r StArenaWarReport) IsWin(playerID int32) bool {
	return r.WinID == playerID
}

// StArenaRankEntry 代表仙界竞技场积分排行榜中的一名玩家。
//
// 线格式 (open_st_arena 回包 integralRankList 元素):
//
//	Int32 st_player_id, String nick_name, String stage_name,
//	String server_name, Int32 integral, Int32 feats
type StArenaRankEntry struct {
	// StPlayerID 跨服玩家 ID
	StPlayerID int32
	// NickName 玩家昵称
	NickName string
	// StageName 区服阶段名
	StageName string
	// ServerName 区服全称
	ServerName string
	// Integral 当前积分
	Integral int32
	// Feats 当前荣誉值
	Feats int32
}

// StArenaPanel 代表仙界竞技场打开面板回包 (open_st_arena)。
//
// 线格式 (逆向 Mod_StArena_Base.open_st_arena 描述符):
//
//	Int32 integral, Int16 reduce_challenge_count, Int32 award_feats,
//	Int32 award_get_time, Int32 reduce_refresh_time, Int32 proficiency,
//	[challengeList], [reportList], [integralRankList],
//	[Int32,Int32,Int32] dec_score_notify, Int32 week_fight_times,
//	Int32 need_fight_times, Int32 now_dec_score (尾部自动流程不需要，忽略)
//
// reduce_challenge_count 为本次操作后剩余挑战次数；
// reduce_refresh_time 为剩余可刷新对手次数；
// proficiency 为竞技场熟练度 (客户端自动跳过战斗用)，与刷新次数无关。
type StArenaPanel struct {
	// Integral 本方当前积分
	Integral int32
	// ReduceChallengeCount 剩余挑战次数
	ReduceChallengeCount int16
	// AwardFeats 待领取/已获得荣誉
	AwardFeats int32
	// AwardGetTime 荣誉奖励领取时间戳
	AwardGetTime int32
	// ReduceRefreshTime 剩余刷新对手次数
	ReduceRefreshTime int32
	// Proficiency 竞技场熟练度 (用于客户端自动跳过战斗，非刷新次数)
	Proficiency int32
	// ChallengeList 当前可挑战对手列表
	ChallengeList []StArenaOpponent
	// ReportList 历史战报列表
	ReportList []StArenaWarReport
	// IntegralRankList 积分排行榜
	IntegralRankList []StArenaRankEntry
}

// StArenaRefreshResult 代表刷新对手列表回包 (refresh_player_list)。
//
// 线格式:
//
//	Uint8 result, [challengeList], Int32 reduce_refresh_time
type StArenaRefreshResult struct {
	// Result 操作结果码 (StArenaResultSuccess 表示成功)
	Result uint8
	// ChallengeList 刷新后的可挑战对手列表
	ChallengeList []StArenaOpponent
	// ReduceRefreshTime 刷新后剩余刷新次数
	ReduceRefreshTime int32
}

// StArenaChallengeRequest 代表发起仙界竞技场挑战请求 (challenge)。
//
// 线格式: Int32 st_player_id, Int32 integral（严格 8 字节，不追加 PrevAct）。
// 两个字段均取自 open_st_arena 下发的 challengeList 记录，用于服务端校验目标仍在列表内。
type StArenaChallengeRequest struct {
	// StPlayerID 挑战目标跨服玩家 ID
	StPlayerID int32
	// Integral 挑战目标当前积分
	Integral int32
}

// StArenaChallengeResult 代表仙界竞技场挑战回包的首字段结果。
//
// challenge 回包为 Uint8 result 开头，其后紧跟完整战报与积分/荣誉奖励字段。
// 自动化流程只消费结果码，成功后再重新打开面板读取最新积分、剩余次数与战报，
// 从而避免解析体积庞大的战斗回放结构。
type StArenaChallengeResult struct {
	// Result 挑战结果码 (StArenaResultSuccess 表示战斗已结算)
	Result uint8
}

// Success 判断挑战是否被服务端受理并完成结算。
func (r StArenaChallengeResult) Success() bool {
	return r.Result == StArenaResultSuccess
}

// newStArenaPrevActPacket 构造带 PrevAct 尾部的跨服请求封包。
//
// 官方 DataBase.netCall 在 SOCKETSERVERTOWN 连接上会自动追加 4 字节
// 前序 Module+Action；Go 会话层通过 HasPrevAct 标记在发送时用最近一次业务
// ActionID 覆盖末尾 4 字节，因此这里仅需预留 4 字节占位。
//
// 仅 open_st_arena / refresh_player_list 使用该形式；challenge 要求严格 8 字节，
// 见 BuildStArenaChallengePacket。
func newStArenaPrevActPacket(actionID uint32, payload []byte) *Packet {
	body := make([]byte, len(payload)+4)
	copy(body, payload)
	return &Packet{ActionID: actionID, Payload: body, HasPrevAct: true}
}

// BuildStArenaOpenPacket 构造打开仙界竞技场面板请求封包 (0x00D60000)。
func BuildStArenaOpenPacket() *Packet {
	return newStArenaPrevActPacket(ActionIDStArenaOpen, nil)
}

// BuildStArenaRefreshPlayerListPacket 构造刷新可挑战对手列表请求封包 (0x00D60001)。
func BuildStArenaRefreshPlayerListPacket() *Packet {
	return newStArenaPrevActPacket(ActionIDStArenaRefreshPlayerList, nil)
}

// BuildStArenaChallengePacket 构造发起仙界竞技场挑战请求封包 (0x00D60002)。
//
// 与 open/refresh 不同，challenge 请求线格式必须是严格 8 字节
// (Int32 st_player_id, Int32 integral)，追加 4 字节 PrevAct 会被服务端
// 静默丢弃且无任何回包 (实机表现为 context deadline exceeded，且次数不扣减)。
func BuildStArenaChallengePacket(req StArenaChallengeRequest) (*Packet, error) {
	w := NewWriter()
	w.WriteInt32(req.StPlayerID)
	w.WriteInt32(req.Integral)
	return &Packet{ActionID: ActionIDStArenaChallenge, Payload: w.Bytes()}, nil
}

// ParseStArenaChallengeRequest 从载荷反序列化发起挑战请求。
func ParseStArenaChallengeRequest(payload []byte) (*StArenaChallengeRequest, error) {
	if len(payload) < 8 {
		return nil, fmt.Errorf("%w: payload too short for st arena challenge", ErrInvalidPayload)
	}
	r := NewReader(payload)
	playerID, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read st_player_id: %v", ErrInvalidPayload, err)
	}
	integral, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read integral: %v", ErrInvalidPayload, err)
	}
	return &StArenaChallengeRequest{StPlayerID: playerID, Integral: integral}, nil
}

// BuildStArenaClosePacket 构造关闭仙界竞技场面板请求封包 (0x00D60004)。
func BuildStArenaClosePacket() *Packet {
	return newStArenaPrevActPacket(ActionIDStArenaClose, nil)
}

// ParseStArenaPanelResult 从载荷反序列化仙界竞技场面板回包。
func ParseStArenaPanelResult(payload []byte) (*StArenaPanel, error) {
	r := NewReader(payload)
	res := &StArenaPanel{}
	var err error

	if res.Integral, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read integral: %v", ErrInvalidPayload, err)
	}
	if res.ReduceChallengeCount, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read reduce_challenge_count: %v", ErrInvalidPayload, err)
	}
	if res.AwardFeats, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read award_feats: %v", ErrInvalidPayload, err)
	}
	if res.AwardGetTime, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read award_get_time: %v", ErrInvalidPayload, err)
	}
	if res.ReduceRefreshTime, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read reduce_refresh_time: %v", ErrInvalidPayload, err)
	}
	// 官方描述符在 reduce_refresh_time 后还有 proficiency；旧布局漏读该字段
	// 会导致后续 challengeList/reportList/rankList 整体偏移 4 字节。
	if res.Proficiency, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read proficiency: %v", ErrInvalidPayload, err)
	}
	if res.ChallengeList, err = readStArenaOpponents(r); err != nil {
		return nil, err
	}
	if res.ReportList, err = readStArenaReports(r); err != nil {
		return nil, err
	}
	if res.IntegralRankList, err = readStArenaRankEntries(r); err != nil {
		return nil, err
	}
	return res, nil
}

// ParseStArenaRefreshResult 从载荷反序列化刷新对手列表回包。
func ParseStArenaRefreshResult(payload []byte) (*StArenaRefreshResult, error) {
	r := NewReader(payload)
	res := &StArenaRefreshResult{}
	var err error

	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read refresh result: %v", ErrInvalidPayload, err)
	}
	if res.ChallengeList, err = readStArenaOpponents(r); err != nil {
		return nil, err
	}
	if res.ReduceRefreshTime, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read reduce_refresh_time: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// ParseStArenaChallengeResult 从载荷反序列化仙界竞技场挑战结果。
//
// 仅消费首字节结果码；后续战报结构体积庞大且自动化流程无需展开。
func ParseStArenaChallengeResult(payload []byte) (*StArenaChallengeResult, error) {
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read challenge result: %v", ErrInvalidPayload, err)
	}
	return &StArenaChallengeResult{Result: result}, nil
}

// readStArenaOpponents 读取 u16 数量前缀的可挑战对手列表。
func readStArenaOpponents(r *Reader) ([]StArenaOpponent, error) {
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read st arena opponent count: %v", ErrInvalidPayload, err)
	}
	opponents := make([]StArenaOpponent, 0, int(count))
	for i := 0; i < int(count); i++ {
		var opp StArenaOpponent
		if opp.StPlayerID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read st arena opponent player_id: %v", ErrInvalidPayload, err)
		}
		if opp.Integral, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read st arena opponent integral: %v", ErrInvalidPayload, err)
		}
		if opp.RoleID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read st arena opponent role_id: %v", ErrInvalidPayload, err)
		}
		if opp.Level, err = r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("%w: failed to read st arena opponent level: %v", ErrInvalidPayload, err)
		}
		opponents = append(opponents, opp)
	}
	return opponents, nil
}

// readStArenaReports 读取 u16 数量前缀的历史战报列表。
func readStArenaReports(r *Reader) ([]StArenaWarReport, error) {
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read st arena report count: %v", ErrInvalidPayload, err)
	}
	reports := make([]StArenaWarReport, 0, int(count))
	for i := 0; i < int(count); i++ {
		var rep StArenaWarReport
		if rep.WarReportID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read st arena report id: %v", ErrInvalidPayload, err)
		}
		if rep.IsRead, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read st arena report is_read: %v", ErrInvalidPayload, err)
		}
		if rep.AttackID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read st arena report attack_id: %v", ErrInvalidPayload, err)
		}
		if rep.AttackName, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: failed to read st arena report attack_name: %v", ErrInvalidPayload, err)
		}
		if rep.DefenseID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read st arena report defense_id: %v", ErrInvalidPayload, err)
		}
		if rep.DefenseName, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: failed to read st arena report defense_name: %v", ErrInvalidPayload, err)
		}
		if rep.WinID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read st arena report win_id: %v", ErrInvalidPayload, err)
		}
		if rep.Integral, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read st arena report integral: %v", ErrInvalidPayload, err)
		}
		if rep.Version, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: failed to read st arena report version: %v", ErrInvalidPayload, err)
		}
		if rep.Time, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read st arena report time: %v", ErrInvalidPayload, err)
		}
		if rep.IsBack, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read st arena report is_back: %v", ErrInvalidPayload, err)
		}
		reports = append(reports, rep)
	}
	return reports, nil
}

// readStArenaRankEntries 读取 u16 数量前缀的积分排行榜。
func readStArenaRankEntries(r *Reader) ([]StArenaRankEntry, error) {
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read st arena rank count: %v", ErrInvalidPayload, err)
	}
	entries := make([]StArenaRankEntry, 0, int(count))
	for i := 0; i < int(count); i++ {
		var entry StArenaRankEntry
		if entry.StPlayerID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read st arena rank player_id: %v", ErrInvalidPayload, err)
		}
		if entry.NickName, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: failed to read st arena rank nick_name: %v", ErrInvalidPayload, err)
		}
		if entry.StageName, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: failed to read st arena rank stage_name: %v", ErrInvalidPayload, err)
		}
		if entry.ServerName, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: failed to read st arena rank server_name: %v", ErrInvalidPayload, err)
		}
		if entry.Integral, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read st arena rank integral: %v", ErrInvalidPayload, err)
		}
		if entry.Feats, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read st arena rank feats: %v", ErrInvalidPayload, err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}
