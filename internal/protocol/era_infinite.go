// Package protocol 提供无量劫海/归墟 (Mod_EraInfiniteMission_Base, Module 853) 协议。
//
// 抓包基准: /tmp/wuliangjiehai.pcapng（2026-11-15，qq s0）：
//
//	0 get_now_season       [Int]
//	1 get_panel_info       面板
//	2 get_chapter_panel    [Int chapter_id] → 章节面板（含 now_level）
//	3 get_monster_team_info [Int now_level][Int team] → 怪物队伍
//	4 fight                [Int now_level] → [UByte result, fight_result_list, is_win, ...]
//
// 无量劫海 = 归墟 1/2/3/极境，每境 10 关；客户端按 now_level 逐关挑战。
package protocol

import "fmt"

const (
	// ActionIDEraInfiniteSeason 当前赛季 (853/0)
	ActionIDEraInfiniteSeason uint32 = 0x03550000
	// ActionIDEraInfinitePanel 面板 (853/1)
	ActionIDEraInfinitePanel uint32 = 0x03550001
	// ActionIDEraInfiniteChapter 章节面板 (853/2)
	ActionIDEraInfiniteChapter uint32 = 0x03550002
	// ActionIDEraInfiniteMonsterTeam 怪物队伍 (853/3)
	ActionIDEraInfiniteMonsterTeam uint32 = 0x03550003
	// ActionIDEraInfiniteFight 挑战 (853/4)
	ActionIDEraInfiniteFight uint32 = 0x03550004

	// ActionIDEraInfiniteDeployTypeSeq 布阵类型序列 (854/39)
	//
	// 官方客户端进入挑战前先发 854/39（空请求），返回 [UInt16 n, n×(Int type, Int deploy_id)]，
	// 再发 854/1 [Int deploy_id]，然后才 853/3 / 853/4。两包均无业务回包等待。
	ActionIDEraInfiniteDeployTypeSeq uint32 = 0x03560027
	// ActionIDEraInfiniteSuperDeployList 布阵列表 (854/1)
	ActionIDEraInfiniteSuperDeployList uint32 = 0x03560001
)

// 853 结果码（Mod_EraInfiniteMission_Base）。
const (
	EraInfiniteSuccess        uint8 = 0
	EraInfiniteFuncNotOpen    uint8 = 1
	EraInfiniteIsLock         uint8 = 2
	EraInfiniteAlreadyPass    uint8 = 3
	EraInfiniteEnded          uint8 = 4
	EraInfiniteFailed         uint8 = 5
	EraInfiniteWrongID        uint8 = 6
	EraInfinitePackageFull    uint8 = 7
	EraInfiniteNotEnoughItem  uint8 = 8
	EraInfiniteNotEnoughPoint uint8 = 9
	EraInfiniteMaxActive      uint8 = 10
)

// EraInfiniteChapter 表示章节面板关键字段。
type EraInfiniteChapter struct {
	Result    uint8
	NowLevel  int32
	ChapterID int32
}

// EraInfiniteTeamResult 表示 853/4 回包中单支队伍的结算。
// 无量劫海要求所有上阵队伍全部胜利（fight_result_list 逐队 is_win）。
type EraInfiniteTeamResult struct {
	Seq      int32
	IsWin    bool
	ReportID int32
	Version  string
}

// EraInfiniteFightResult 表示挑战结果。
type EraInfiniteFightResult struct {
	Result uint8
	IsWin  bool
	Teams  []EraInfiniteTeamResult
}

func eraInfinitePrevPacket(actionID uint32, ints ...int32) *Packet {
	w := NewWriter()
	for _, v := range ints {
		w.WriteInt32(v)
	}
	w.WriteUint32(0) // PrevAct 占位，由会话层覆盖
	pkt := NewPacket(actionID, w.Bytes())
	pkt.PrevActFromLastSent = true
	return pkt
}

// BuildEraInfiniteSeasonPacket 构造赛季查询 (853/0)。
func BuildEraInfiniteSeasonPacket() *Packet {
	return eraInfinitePrevPacket(ActionIDEraInfiniteSeason)
}

// BuildEraInfinitePanelPacket 构造面板查询 (853/1)。
func BuildEraInfinitePanelPacket() *Packet {
	return eraInfinitePrevPacket(ActionIDEraInfinitePanel)
}

// EraInfinitePanel 表示 853/1 面板信息。
//
// 字段顺序（Mod_EraInfiniteData.get_panel_info）:
// era_level, mission_level, rank_perc, rank, fight_end_ts, era_zone。
type EraInfinitePanel struct {
	EraLevel     int32
	MissionLevel int32
	RankPerc     int32
	Rank         int32
	FightEndTS   int32
	EraZone      int32
}

// ParseEraInfinitePanel 解析 853/1 面板信息（6 个 Int）。
func ParseEraInfinitePanel(payload []byte) (*EraInfinitePanel, error) {
	p := DecompressPayloadIfNeeded(payload)
	r := NewReader(p)
	out := &EraInfinitePanel{}
	for i, dst := range []*int32{&out.EraLevel, &out.MissionLevel, &out.RankPerc,
		&out.Rank, &out.FightEndTS, &out.EraZone} {
		v, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: era infinite panel[%d]: %v", ErrInvalidPayload, i, err)
		}
		*dst = v
	}
	return out, nil
}

// BuildEraInfiniteChapterPacket 构造章节面板 (853/2)。
func BuildEraInfiniteChapterPacket(chapterID int32) *Packet {
	return eraInfinitePrevPacket(ActionIDEraInfiniteChapter, chapterID)
}

// BuildEraInfiniteChapterPacketWithPrev 构造带显式 PrevAct 的章节面板 (853/2)。
//
// 章节扫描的两次请求之间可能间隔 4 秒超时，期间主服会发送 0/23 心跳；若继续用
// HasPrevAct（最后收到的包），下一章的尾链会变成心跳动作，服务端静默丢弃
// （2026-11-22 实测）。这里固定使用最后一条业务回包（LastAckActionID）作锚点。
func BuildEraInfiniteChapterPacketWithPrev(chapterID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(chapterID)
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDEraInfiniteChapter, w.Bytes())
}

// BuildEraInfiniteDeployTypeSeqPacket 构造布阵类型序列查询 (854/39)。
func BuildEraInfiniteDeployTypeSeqPacket() *Packet {
	return eraInfinitePrevPacket(ActionIDEraInfiniteDeployTypeSeq)
}

// BuildEraInfiniteSuperDeployListPacket 构造布阵列表请求 (854/1)。
func BuildEraInfiniteSuperDeployListPacket(deployID int32) *Packet {
	return eraInfinitePrevPacket(ActionIDEraInfiniteSuperDeployList, deployID)
}

// ParseEraInfiniteDeployTypeSeq 解析 854/39：UInt16 数量 + n×(Int type, Int deploy_id)，
// 返回每项 deploy_id。抓包示例：3 项 (1,90001)(2,90002)(3,90003)。
func ParseEraInfiniteDeployTypeSeq(payload []byte) ([]int32, error) {
	p := DecompressPayloadIfNeeded(payload)
	r := NewReader(p)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: era infinite deploy seq count: %v", ErrInvalidPayload, err)
	}
	out := make([]int32, 0, count)
	for i := 0; i < int(count); i++ {
		if _, err = r.ReadInt32(); err != nil { // type
			return nil, fmt.Errorf("%w: era infinite deploy seq[%d].type: %v", ErrInvalidPayload, i, err)
		}
		id, err := r.ReadInt32() // deploy_id
		if err != nil {
			return nil, fmt.Errorf("%w: era infinite deploy seq[%d].deploy_id: %v", ErrInvalidPayload, i, err)
		}
		out = append(out, id)
	}
	return out, nil
}

// BuildEraInfiniteDeployTypeSeqResultPacket 构造 854/39 回包，供离线 mock 与单测使用。
func BuildEraInfiniteDeployTypeSeqResultPacket(pairs [][2]int32) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(pairs)))
	for _, pair := range pairs {
		w.WriteInt32(pair[0])
		w.WriteInt32(pair[1])
	}
	return NewPacket(ActionIDEraInfiniteDeployTypeSeq, w.Bytes())
}

// BuildEraInfiniteMonsterTeamPacket 构造怪物队伍查询 (853/3)。
func BuildEraInfiniteMonsterTeamPacket(nowLevel, team int32) *Packet {
	return eraInfinitePrevPacket(ActionIDEraInfiniteMonsterTeam, nowLevel, team)
}

// BuildEraInfiniteFightPacket 构造挑战 (853/4)。
//
// 请求为 [Int now_level, [Int,Int] 列表]；抓包中列表为空（Short 0）。
func BuildEraInfiniteFightPacket(nowLevel int32) *Packet {
	w := NewWriter()
	w.WriteInt32(nowLevel)
	w.WriteUint16(0) // 空列表
	w.WriteUint32(0) // PrevAct 占位
	pkt := NewPacket(ActionIDEraInfiniteFight, w.Bytes())
	pkt.PrevActFromLastSent = true
	return pkt
}

// ParseEraInfiniteChapter 解析 853/2。
//
// 字段顺序: result, era_level, rule_list, now_level, lower_con, monster_list,
// best_player, fastest_player, rank_perc, rank, chapter_id, ...
func ParseEraInfiniteChapter(payload []byte) (*EraInfiniteChapter, error) {
	p := DecompressPayloadIfNeeded(payload)
	r := NewReader(p)
	out := &EraInfiniteChapter{}
	var err error
	if out.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: era infinite result: %v", ErrInvalidPayload, err)
	}
	// era_level
	if _, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: era infinite era_level: %v", ErrInvalidPayload, err)
	}
	// rule_list: Short count + Int 列表
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: era infinite rule list: %v", ErrInvalidPayload, err)
	}
	for i := 0; i < int(count); i++ {
		if _, err = r.ReadInt32(); err != nil {
			return nil, err
		}
	}
	if out.NowLevel, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: era infinite now_level: %v", ErrInvalidPayload, err)
	}
	// lower_con
	if _, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	// monster_list: Short count + count × [Int,Int]
	mcount, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	for i := 0; i < int(mcount); i++ {
		if _, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		if _, err = r.ReadInt32(); err != nil {
			return nil, err
		}
	}
	// 后续 best_player/fastest_player 为复杂嵌套结构，自动化只需要 now_level。
	return out, nil
}

// ParseEraInfiniteFightResult 解析 853/4 的 result 与 is_win。
func ParseEraInfiniteFightResult(payload []byte) (*EraInfiniteFightResult, error) {
	p := DecompressPayloadIfNeeded(payload)
	r := NewReader(p)
	out := &EraInfiniteFightResult{}
	var err error
	if out.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: era infinite fight result: %v", ErrInvalidPayload, err)
	}
	if out.Result != 0 {
		return out, nil
	}
	// fight_result_list: Short count + entries [Int seq, Int is_win, Int report_id, String version]
	count, err := r.ReadUint16()
	if err != nil {
		return out, nil
	}
	out.Teams = make([]EraInfiniteTeamResult, 0, count)
	for i := 0; i < int(count); i++ {
		var team EraInfiniteTeamResult
		if team.Seq, err = r.ReadInt32(); err != nil {
			return out, nil
		}
		win, err := r.ReadInt32()
		if err != nil {
			return out, nil
		}
		team.IsWin = win == 1
		if team.ReportID, err = r.ReadInt32(); err != nil {
			return out, nil
		}
		if team.Version, err = r.ReadString(); err != nil {
			return out, nil
		}
		out.Teams = append(out.Teams, team)
	}
	win, err := r.ReadInt32()
	if err != nil {
		return out, nil
	}
	out.IsWin = win == 1
	return out, nil
}

// EraInfiniteResultText 返回 853 结果码文本。
func EraInfiniteResultText(code uint8) string {
	switch code {
	case 0:
		return "成功"
	case 1:
		return "功能未开放"
	case 2:
		return "未解锁"
	case 3:
		return "已通关"
	case 4:
		return "已结束"
	case 5:
		return "失败"
	case 6:
		return "参数错误"
	case 7:
		return "背包已满"
	case 8:
		return "道具不足"
	case 9:
		return "积分不足"
	case 10:
		return "活跃上限"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// BuildEraInfinitePanelResultPacket 构造 853/1 回包，供离线 mock 与单测使用。
func BuildEraInfinitePanelResultPacket(eraLevel, missionLevel, rankPerc, rank, fightEndTS, eraZone int32) *Packet {
	w := NewWriter()
	for _, v := range []int32{eraLevel, missionLevel, rankPerc, rank, fightEndTS, eraZone} {
		w.WriteInt32(v)
	}
	return NewPacket(ActionIDEraInfinitePanel, w.Bytes())
}

// BuildEraInfiniteChapterResultPacket 构造章节面板回包，供离线 mock 与单测使用。
func BuildEraInfiniteChapterResultPacket(result uint8, nowLevel, chapterID int32) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	w.WriteInt32(110) // era_level
	w.WriteUint16(1).WriteInt32(1)
	w.WriteInt32(nowLevel)
	w.WriteInt32(1)
	w.WriteUint16(1).WriteInt32(1).WriteInt32(1)
	_ = chapterID
	return NewPacket(ActionIDEraInfiniteChapter, w.Bytes())
}

// BuildEraInfiniteFightResultPacket 构造挑战回包，供离线 mock 与单测使用。
func BuildEraInfiniteFightResultPacket(result uint8, isWin bool) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	w.WriteUint16(1)
	w.WriteInt32(1) // seq
	if isWin {
		w.WriteInt32(1)
	} else {
		w.WriteInt32(0)
	}
	w.WriteInt32(1)     // report_id
	w.WriteString("v1") // report_version
	if isWin {
		w.WriteInt32(1)
	} else {
		w.WriteInt32(0)
	}
	w.WriteUint16(0)
	return NewPacket(ActionIDEraInfiniteFight, w.Bytes())
}
