// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// @author Ateng
// @since 2026-10-09
package protocol

import (
	"fmt"
)

// ArenaOpponent 代表竞技场中可挑战的对手信息。
//
// 线格式 (逆向 Mod_SuperSport_Base.get_challenge_list 并经线上 0x001C0001 回包校验):
//
//	Int32 rank, Int32 playerID, Int32 roleID, Int16 level,
//	Uint16 nameLen, Bytes[nameLen] name, Int32 awardCoin, Int32 awardFame
//
// 其中 roleID 为职业/造型编号 (RoleType.getRoleSign 的入参)，
// awardCoin/awardFame 为击败该对手可获得的铜钱与声望奖励，并非竞技场积分。
type ArenaOpponent struct {
	// Rank 对手在竞技场的排名
	Rank int32
	// PlayerID 对手的玩家数字 ID
	PlayerID int32
	// RoleID 对手职业/造型编号
	RoleID int32
	// Level 对手等级
	Level int16
	// Name 对手角色名称
	Name string
	// AwardCoin 击败该对手可获得的铜钱
	AwardCoin int32
	// AwardFame 击败该对手可获得的声望
	AwardFame int32
}

// ArenaWarReport 代表竞技场面板中的一条历史战报。
//
// 线格式 (逆向 Mod_SuperSport_Base.open_super_sport 回包第 13 项):
//
//	Int32 warReportID, Uint8 isRead, Int32 attackPlayerID, String attackPlayerName,
//	Int32 defensePlayerID, String defensePlayerName, Int32 winPlayerID,
//	Int32 oldRank, Int32 newRank, String version, Int32 reportTimer
//
// 胜负判定: winPlayerID 等于本方 PlayerID 即为胜利 (isWin)；
// 名次变化: oldRank > newRank 为名次上升 (数字越小排名越靠前)，反之为下降。
type ArenaWarReport struct {
	// WarReportID 战报唯一编号，可回查详细战报
	WarReportID int32
	// IsRead 是否已读 (0 未读, 1 已读)
	IsRead uint8
	// AttackPlayerID 进攻方玩家 ID
	AttackPlayerID int32
	// AttackPlayerName 进攻方角色名
	AttackPlayerName string
	// DefensePlayerID 防守方玩家 ID
	DefensePlayerID int32
	// DefensePlayerName 防守方角色名
	DefensePlayerName string
	// WinPlayerID 胜利方玩家 ID
	WinPlayerID int32
	// OldRank 战斗前的排名
	OldRank int32
	// NewRank 战斗结算后的排名
	NewRank int32
	// Version 战报版本标识 (服务端按日期编号)
	Version string
	// ReportTimer 战报生成时间戳
	ReportTimer int32
}

// IsWin 判断该战报中 playerID 是否获胜。
func (r ArenaWarReport) IsWin(playerID int32) bool {
	return r.WinPlayerID == playerID
}

// RankDelta 返回排名变化量 (正数表示名次上升，负数表示名次下降)。
func (r ArenaWarReport) RankDelta() int32 {
	return r.OldRank - r.NewRank
}

// ArenaPanelRequest 代表打开本服竞技场面板请求 (ActionID=0x001C0000)。
type ArenaPanelRequest struct {
	// PrevAct 前序协议编号
	PrevAct uint32
}

// ArenaPanelResult 代表本服竞技场面板回包 (open_super_sport)。
type ArenaPanelResult struct {
	// MyRank 本人当前排名
	MyRank int32
	// WinCount 当前连胜次数
	WinCount int16
	// Fame 当前声望值
	Fame int32
	// FameLevel 声望等级
	FameLevel int16
	// RemainChallengeTimes 剩余免费挑战次数
	RemainChallengeTimes int16
	// RemainBuyTimes 剩余可购买次数
	RemainBuyTimes int16
	// NextBuyPrice 下次购买所需元宝
	NextBuyPrice int16
	// AwardCoin 赛季结算铜钱奖励
	AwardCoin int32
	// AwardFame 赛季结算声望奖励
	AwardFame int32
	// AwardTime 赛季结算时间戳
	AwardTime int32
	// OpenTimer 面板开放时间戳
	OpenTimer int32
	// CdTimer 挑战冷却时间戳
	CdTimer int32
	// Opponents 可挑战对手列表
	Opponents []ArenaOpponent
	// WarReports 历史战报列表 (按时间倒序)
	WarReports []ArenaWarReport
}

// FindWarReport 按战报编号查找战报，未找到返回 nil。
func (p *ArenaPanelResult) FindWarReport(reportID int32) *ArenaWarReport {
	for i := range p.WarReports {
		if p.WarReports[i].WarReportID == reportID {
			return &p.WarReports[i]
		}
	}
	return nil
}

// ArenaGetTimesRequest 代表查询竞技场剩余挑战次数请求。
type ArenaGetTimesRequest struct {
	// PrevAct 前序协议编号
	PrevAct uint32
}

// ArenaGetTimesResult 代表竞技场剩余挑战次数回包。
type ArenaGetTimesResult struct {
	// RemainingTimes 当前剩余可用挑战次数
	RemainingTimes int32
}

// ArenaGetRankingRequest 代表查询当前玩家竞技场排名请求。
type ArenaGetRankingRequest struct {
	// PrevAct 前序协议编号
	PrevAct uint32
}

// ArenaGetRankingResult 代表玩家竞技场排名回包。
type ArenaGetRankingResult struct {
	// Ranking 当前竞技场排名 (0 为未上榜)
	Ranking int32
}

// ArenaGetOpponentsRequest 代表查询竞技场对手列表请求。
type ArenaGetOpponentsRequest struct {
	// PrevAct 前序协议编号
	PrevAct uint32
}

// ArenaGetOpponentsResult 代表竞技场对手列表回包。
type ArenaGetOpponentsResult struct {
	// Opponents 可挑战对手切片
	Opponents []ArenaOpponent
}

// ArenaChallengeRequest 代表发起竞技场挑战请求 (ActionID=0x001C0002)。
//
// 实测服务端只接受 Int32(targetRank) 单字段载荷；额外携带 PrevAct 会被静默丢弃且不产生任何回包。
type ArenaChallengeRequest struct {
	// TargetRank 挑战目标的竞技场排名序号
	TargetRank int32
	// PrevAct 兼容保留字段，不参与线格式编码
	PrevAct uint32
}

// 竞技场挑战结算结果码 (Mod_SuperSport_Base.start_challenge 回包首字段)。
const (
	// ArenaResultSuccess 挑战被受理
	ArenaResultSuccess uint8 = 0
	// ArenaResultInvalidRank 挑战目标排名无效
	ArenaResultInvalidRank uint8 = 1
	// ArenaResultNoTimes 今日挑战次数不足
	ArenaResultNoTimes uint8 = 2
	// ArenaResultCoolingDown 挑战冷却中
	ArenaResultCoolingDown uint8 = 3
)

// ArenaChallengeResult 代表发起竞技场挑战后的即时战报结算 (ActionID=0x001C0002)。
//
// 回包结构 (逆向 Mod_SuperSport_Base.start_challenge 并经线上 52945 字节战报全量消费校验):
//
//	Uint8 result, Int16 remain, Int32 cdTimer,
//	[ [Int32 awardFame, Int32 awardCoin, Int32 awardGun, Int32 scrapID, Int32 reportID,
//	   [ [Uint8 deployType, Int32 winPlayerID, 双方阵容, 出手序列, Int32 x3] ],
//	   [ [Int32 itemID, Int32 count] ] ] ]
//
// result == 0 (SUCCESS) 表示挑战被受理；真正的胜负由战报单元第 2 个字段 winPlayerID 决定，
// winPlayerID 等于本方 PlayerID 即为胜利。新名次不在本回包内，需重新打开面板读取战报的
// oldRank/newRank (见 ArenaPanelResult)。
type ArenaChallengeResult struct {
	// Result 结算结果码 (0=SUCCESS, 1=排名无效, 2=次数不足, 3=冷却中, ...)
	Result uint8
	// Success 服务端是否受理了本次挑战 (Result == 0)
	Success bool
	// RemainTimes 剩余挑战次数
	RemainTimes int16
	// CdTimer 挑战冷却时间戳
	CdTimer int32
	// AwardFame 本次挑战获得的声望
	AwardFame int32
	// AwardCoin 本次挑战获得的铜钱
	AwardCoin int32
	// AwardGun 本次挑战获得的元宝
	AwardGun int32
	// ScrapID 本次挑战获得的碎片编号 (0 表示无)
	ScrapID int32
	// ReportID 本次挑战生成的战报编号，可回查面板同名战报
	ReportID int32
	// DeployType 战斗布阵类型 (战报单元第 1 字段)
	DeployType uint8
	// WinPlayerID 战斗胜利方玩家 ID (战报单元第 2 字段)
	WinPlayerID int32
	// WarReport 原始战报载荷 (完整战斗数据，含双方阵容与出手序列)
	WarReport []byte
}

// IsWin 判断本次挑战中 playerID 是否获胜。
func (r *ArenaChallengeResult) IsWin(playerID int32) bool {
	return r.Success && r.WinPlayerID == playerID
}

// BuildArenaGetTimesPacket 构造查询竞技场剩余次数封包。
func BuildArenaGetTimesPacket(req ArenaGetTimesRequest) (*Packet, error) {
	w := NewWriter()
	prevAct := req.PrevAct
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDArenaGetTimes, w.Bytes()), nil
}

// ParseArenaGetTimesRequest 从载荷反序列化查询剩余次数请求。
func ParseArenaGetTimesRequest(payload []byte) (*ArenaGetTimesRequest, error) {
	r := NewReader(payload)
	prevAct, err := r.ReadUint32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read prev_act: %v", ErrInvalidPayload, err)
	}
	return &ArenaGetTimesRequest{PrevAct: prevAct}, nil
}

// BuildArenaGetTimesResultPacket 构造竞技场剩余次数回包封包。
func BuildArenaGetTimesResultPacket(times int32) (*Packet, error) {
	w := NewWriter()
	w.WriteInt32(times)
	return NewPacket(ActionIDArenaGetTimes, w.Bytes()), nil
}

// ParseArenaGetTimesResult 从载荷反序列化剩余次数回包。
func ParseArenaGetTimesResult(payload []byte) (*ArenaGetTimesResult, error) {
	if len(payload) < 4 {
		return nil, fmt.Errorf("%w: payload too short for arena times", ErrInvalidPayload)
	}
	r := NewReader(payload)
	times, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read remaining times: %v", ErrInvalidPayload, err)
	}
	return &ArenaGetTimesResult{RemainingTimes: times}, nil
}

// BuildArenaGetOpponentsPacket 构造查询对手列表请求封包。
func BuildArenaGetOpponentsPacket(req ArenaGetOpponentsRequest) (*Packet, error) {
	w := NewWriter()
	prevAct := req.PrevAct
	if prevAct == 0 {
		prevAct = ActionIDArenaGetTimes
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDArenaGetOpponents, w.Bytes()), nil
}

// ParseArenaGetOpponentsRequest 从载荷反序列化查询对手列表请求。
func ParseArenaGetOpponentsRequest(payload []byte) (*ArenaGetOpponentsRequest, error) {
	r := NewReader(payload)
	prevAct, err := r.ReadUint32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read prev_act: %v", ErrInvalidPayload, err)
	}
	return &ArenaGetOpponentsRequest{PrevAct: prevAct}, nil
}

// BuildArenaChallengePacket 构造发起竞技场挑战请求封包。
func BuildArenaChallengePacket(req ArenaChallengeRequest) (*Packet, error) {
	w := NewWriter()
	// 线上实测: 仅接受 Int32(TargetRank) 4 字节；追加 PrevAct 会导致服务端静默忽略该请求
	w.WriteInt32(req.TargetRank)
	return NewPacket(ActionIDArenaChallenge, w.Bytes()), nil
}

// ParseArenaChallengeRequest 从载荷反序列化发起挑战请求。
func ParseArenaChallengeRequest(payload []byte) (*ArenaChallengeRequest, error) {
	if len(payload) < 4 {
		return nil, fmt.Errorf("%w: payload too short for arena challenge", ErrInvalidPayload)
	}
	r := NewReader(payload)
	rank, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read target_rank: %v", ErrInvalidPayload, err)
	}
	req := &ArenaChallengeRequest{TargetRank: rank}
	if r.Remaining() >= 4 {
		if prevAct, err := r.ReadUint32(); err == nil {
			req.PrevAct = prevAct
		}
	}
	return req, nil
}

// BuildArenaOpenPanelPacket 构造打开本服竞技场面板请求封包。
func BuildArenaOpenPanelPacket(req ArenaPanelRequest) (*Packet, error) {
	w := NewWriter()
	prevAct := req.PrevAct
	if prevAct == 0 {
		prevAct = ActionIDArenaGetTimes
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDArenaOpenPanel, w.Bytes()), nil
}

// ParseArenaPanelResult 从载荷反序列化本服竞技场面板回包。
//
// 线格式 (逆向 open_super_sport 并经线上 502 字节回包全量消费校验, 大端序):
//
//	Int32 myRank, Int16 winCount, Int32 fame, Int16 fameLevel,
//	Int16 remainChallengeTimes, Int16 remainBuyTimes, Int16 nextBuyPrice,
//	Int32 awardCoin, Int32 awardFame, Int32 awardTime, Int32 openTimer, Int32 cdTimer,
//	[opponents], [warReports]
func ParseArenaPanelResult(payload []byte) (*ArenaPanelResult, error) {
	r := NewReader(payload)
	readI32 := func(field string) (int32, error) {
		v, err := r.ReadInt32()
		if err != nil {
			return 0, fmt.Errorf("%w: failed to read %s: %v", ErrInvalidPayload, field, err)
		}
		return v, nil
	}
	readI16 := func(field string) (int16, error) {
		v, err := r.ReadInt16()
		if err != nil {
			return 0, fmt.Errorf("%w: failed to read %s: %v", ErrInvalidPayload, field, err)
		}
		return v, nil
	}

	res := &ArenaPanelResult{}
	var err error
	if res.MyRank, err = readI32("my_rank"); err != nil {
		return nil, err
	}
	if res.WinCount, err = readI16("win_count"); err != nil {
		return nil, err
	}
	if res.Fame, err = readI32("fame"); err != nil {
		return nil, err
	}
	if res.FameLevel, err = readI16("fame_level"); err != nil {
		return nil, err
	}
	if res.RemainChallengeTimes, err = readI16("remain_challenge_times"); err != nil {
		return nil, err
	}
	if res.RemainBuyTimes, err = readI16("remain_buy_times"); err != nil {
		return nil, err
	}
	if res.NextBuyPrice, err = readI16("next_buy_price"); err != nil {
		return nil, err
	}
	if res.AwardCoin, err = readI32("award_coin"); err != nil {
		return nil, err
	}
	if res.AwardFame, err = readI32("award_fame"); err != nil {
		return nil, err
	}
	if res.AwardTime, err = readI32("award_time"); err != nil {
		return nil, err
	}
	if res.OpenTimer, err = readI32("open_timer"); err != nil {
		return nil, err
	}
	if res.CdTimer, err = readI32("cd_timer"); err != nil {
		return nil, err
	}
	if res.Opponents, err = readArenaOpponents(r); err != nil {
		return nil, err
	}
	if res.WarReports, err = readArenaWarReports(r); err != nil {
		return nil, err
	}
	return res, nil
}

// BuildArenaPanelResultPacket 构造本服竞技场面板回包封包 (字段顺序与 ParseArenaPanelResult 对齐)。
func BuildArenaPanelResultPacket(res ArenaPanelResult) (*Packet, error) {
	w := NewWriter()
	w.WriteInt32(res.MyRank)
	w.WriteInt16(res.WinCount)
	w.WriteInt32(res.Fame)
	w.WriteInt16(res.FameLevel)
	w.WriteInt16(res.RemainChallengeTimes)
	w.WriteInt16(res.RemainBuyTimes)
	w.WriteInt16(res.NextBuyPrice)
	w.WriteInt32(res.AwardCoin)
	w.WriteInt32(res.AwardFame)
	w.WriteInt32(res.AwardTime)
	w.WriteInt32(res.OpenTimer)
	w.WriteInt32(res.CdTimer)
	writeArenaOpponents(w, res.Opponents)
	writeArenaWarReports(w, res.WarReports)
	return NewPacket(ActionIDArenaOpenPanel, w.Bytes()), nil
}

// writeArenaWarReports 写入 u16 数量前缀的历史战报列表。
func writeArenaWarReports(w *Writer, reports []ArenaWarReport) {
	w.WriteUint16(uint16(len(reports)))
	for _, rep := range reports {
		w.WriteInt32(rep.WarReportID)
		w.WriteUint8(rep.IsRead)
		w.WriteInt32(rep.AttackPlayerID)
		w.WriteString(rep.AttackPlayerName)
		w.WriteInt32(rep.DefensePlayerID)
		w.WriteString(rep.DefensePlayerName)
		w.WriteInt32(rep.WinPlayerID)
		w.WriteInt32(rep.OldRank)
		w.WriteInt32(rep.NewRank)
		w.WriteString(rep.Version)
		w.WriteInt32(rep.ReportTimer)
	}
}

// readArenaOpponents 读取 u16 数量前缀的对手列表。
func readArenaOpponents(r *Reader) ([]ArenaOpponent, error) {
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read opponent count: %v", ErrInvalidPayload, err)
	}
	opps := make([]ArenaOpponent, 0, count)
	for i := 0; i < int(count); i++ {
		var opp ArenaOpponent
		if opp.Rank, err = r.ReadInt32(); err != nil {
			return opps, fmt.Errorf("%w: failed to read opponent rank: %v", ErrInvalidPayload, err)
		}
		if opp.PlayerID, err = r.ReadInt32(); err != nil {
			return opps, fmt.Errorf("%w: failed to read opponent player_id: %v", ErrInvalidPayload, err)
		}
		if opp.RoleID, err = r.ReadInt32(); err != nil {
			return opps, fmt.Errorf("%w: failed to read opponent role_id: %v", ErrInvalidPayload, err)
		}
		if opp.Level, err = r.ReadInt16(); err != nil {
			return opps, fmt.Errorf("%w: failed to read opponent level: %v", ErrInvalidPayload, err)
		}
		if opp.Name, err = r.ReadString(); err != nil {
			return opps, fmt.Errorf("%w: failed to read opponent name: %v", ErrInvalidPayload, err)
		}
		if opp.AwardCoin, err = r.ReadInt32(); err != nil {
			return opps, fmt.Errorf("%w: failed to read opponent award_coin: %v", ErrInvalidPayload, err)
		}
		if opp.AwardFame, err = r.ReadInt32(); err != nil {
			return opps, fmt.Errorf("%w: failed to read opponent award_fame: %v", ErrInvalidPayload, err)
		}
		opps = append(opps, opp)
	}
	return opps, nil
}

// readArenaWarReports 读取 u16 数量前缀的历史战报列表。
func readArenaWarReports(r *Reader) ([]ArenaWarReport, error) {
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read war report count: %v", ErrInvalidPayload, err)
	}
	reports := make([]ArenaWarReport, 0, count)
	for i := 0; i < int(count); i++ {
		var rep ArenaWarReport
		if rep.WarReportID, err = r.ReadInt32(); err != nil {
			return reports, fmt.Errorf("%w: failed to read report id: %v", ErrInvalidPayload, err)
		}
		if rep.IsRead, err = r.ReadUint8(); err != nil {
			return reports, fmt.Errorf("%w: failed to read report is_read: %v", ErrInvalidPayload, err)
		}
		if rep.AttackPlayerID, err = r.ReadInt32(); err != nil {
			return reports, fmt.Errorf("%w: failed to read report attacker id: %v", ErrInvalidPayload, err)
		}
		if rep.AttackPlayerName, err = r.ReadString(); err != nil {
			return reports, fmt.Errorf("%w: failed to read report attacker name: %v", ErrInvalidPayload, err)
		}
		if rep.DefensePlayerID, err = r.ReadInt32(); err != nil {
			return reports, fmt.Errorf("%w: failed to read report defender id: %v", ErrInvalidPayload, err)
		}
		if rep.DefensePlayerName, err = r.ReadString(); err != nil {
			return reports, fmt.Errorf("%w: failed to read report defender name: %v", ErrInvalidPayload, err)
		}
		if rep.WinPlayerID, err = r.ReadInt32(); err != nil {
			return reports, fmt.Errorf("%w: failed to read report winner id: %v", ErrInvalidPayload, err)
		}
		if rep.OldRank, err = r.ReadInt32(); err != nil {
			return reports, fmt.Errorf("%w: failed to read report old_rank: %v", ErrInvalidPayload, err)
		}
		if rep.NewRank, err = r.ReadInt32(); err != nil {
			return reports, fmt.Errorf("%w: failed to read report new_rank: %v", ErrInvalidPayload, err)
		}
		if rep.Version, err = r.ReadString(); err != nil {
			return reports, fmt.Errorf("%w: failed to read report version: %v", ErrInvalidPayload, err)
		}
		if rep.ReportTimer, err = r.ReadInt32(); err != nil {
			return reports, fmt.Errorf("%w: failed to read report timer: %v", ErrInvalidPayload, err)
		}
		reports = append(reports, rep)
	}
	return reports, nil
}

// BuildArenaGetOpponentsResultPacket 构造竞技场对手列表回包封包。
func BuildArenaGetOpponentsResultPacket(opponents []ArenaOpponent) (*Packet, error) {
	w := NewWriter()
	writeArenaOpponents(w, opponents)
	return NewPacket(ActionIDArenaGetOpponents, w.Bytes()), nil
}

// writeArenaOpponents 写入 u16 数量前缀的对手列表。
func writeArenaOpponents(w *Writer, opponents []ArenaOpponent) {
	w.WriteUint16(uint16(len(opponents)))
	for _, opp := range opponents {
		w.WriteInt32(opp.Rank)
		w.WriteInt32(opp.PlayerID)
		w.WriteInt32(opp.RoleID)
		w.WriteInt16(opp.Level)
		w.WriteString(opp.Name)
		w.WriteInt32(opp.AwardCoin)
		w.WriteInt32(opp.AwardFame)
	}
}

// ParseArenaGetOpponentsResult 从载荷反序列化对手列表回包。
func ParseArenaGetOpponentsResult(payload []byte) (*ArenaGetOpponentsResult, error) {
	if len(payload) < 2 {
		return &ArenaGetOpponentsResult{}, nil
	}
	opps, err := readArenaOpponents(NewReader(payload))
	if err != nil {
		return nil, err
	}
	return &ArenaGetOpponentsResult{Opponents: opps}, nil
}

// BuildArenaChallengeResultPacket 构造竞技场挑战战报结算封包。
//
// 仅写出结算头与战报头 (布阵类型、胜利方)，不生成完整战斗过程；
// 主要用于进程内模拟服务端与编解码单测。
func BuildArenaChallengeResultPacket(res ArenaChallengeResult) (*Packet, error) {
	w := NewWriter()
	w.WriteUint8(res.Result)
	w.WriteInt16(res.RemainTimes)
	w.WriteInt32(res.CdTimer)
	if res.Result == ArenaResultSuccess {
		w.WriteUint16(1) // 结算块数量
		w.WriteInt32(res.AwardFame)
		w.WriteInt32(res.AwardCoin)
		w.WriteInt32(res.AwardGun)
		w.WriteInt32(res.ScrapID)
		w.WriteInt32(res.ReportID)
		w.WriteUint16(1) // 战报单元数量
		w.WriteUint8(res.DeployType)
		w.WriteInt32(res.WinPlayerID)
	}
	return NewPacket(ActionIDArenaChallenge, w.Bytes()), nil
}

// ParseArenaChallengeResult 从载荷反序列化挑战结算回包。
//
// 实测 (2026-11-01) 挑战被受理后回包为 52945 字节完整战报；本函数解析结算头、
// 奖励块与战报头，完整战斗过程保留在 WarReport 原始载荷中。
// 新名次不在本回包内，需重新打开面板读取 ArenaWarReport.OldRank/NewRank。
func ParseArenaChallengeResult(payload []byte) (*ArenaChallengeResult, error) {
	if len(payload) == 0 {
		return &ArenaChallengeResult{Success: true}, nil
	}
	r := NewReader(payload)
	res := &ArenaChallengeResult{WarReport: append([]byte(nil), payload...)}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read challenge result: %v", ErrInvalidPayload, err)
	}
	res.Success = res.Result == ArenaResultSuccess
	if res.RemainTimes, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: failed to read remain times: %v", ErrInvalidPayload, err)
	}
	if res.CdTimer, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read cd timer: %v", ErrInvalidPayload, err)
	}
	if !res.Success || r.Remaining() < 2 {
		return res, nil
	}

	// 结算块数组: 客户端只取第 0 个块。
	blockCount, err := r.ReadUint16()
	if err != nil {
		return res, nil
	}
	if blockCount == 0 || r.Remaining() < 20 {
		return res, nil
	}
	for _, field := range []*int32{&res.AwardFame, &res.AwardCoin, &res.AwardGun, &res.ScrapID, &res.ReportID} {
		if *field, err = r.ReadInt32(); err != nil {
			return res, nil
		}
	}

	// 战报单元数组: 每个单元 [Uint8 deployType, Int32 winPlayerID, 双方阵容, 出手序列, ...]。
	warCount, err := r.ReadUint16()
	if err != nil || warCount == 0 {
		return res, nil
	}
	if res.DeployType, err = r.ReadUint8(); err != nil {
		return res, nil
	}
	if res.WinPlayerID, err = r.ReadInt32(); err != nil {
		return res, nil
	}
	return res, nil
}
