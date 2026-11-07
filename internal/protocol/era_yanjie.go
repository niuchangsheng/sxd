// Package protocol 提供纪元湮劫 / 无尽洞天 (Mod_EraInfiniteMission_Base, Module 853) 协议编解码。
//
// @author Ateng
// @since 2026-11-07
package protocol

import (
	"fmt"
)

// 纪元湮劫动作编号 (Mod_EraInfiniteMission_Base)。
const (
	// ActionIDEraYanjieSeason 查询当前赛季 (Action 0 -> 0x03550000)
	ActionIDEraYanjieSeason uint32 = 0x03550000
	// ActionIDEraYanjiePanel 查询主面板 (Action 1 -> 0x03550001)
	ActionIDEraYanjiePanel uint32 = 0x03550001
	// ActionIDEraYanjieChapter 查询章节面板 (Action 2 -> 0x03550002)
	ActionIDEraYanjieChapter uint32 = 0x03550002
	// ActionIDEraYanjieMonsterTeam 查询关卡敌方队伍 (Action 3 -> 0x03550003)
	ActionIDEraYanjieMonsterTeam uint32 = 0x03550003
	// ActionIDEraYanjieFight 发起关卡战斗 (Action 4 -> 0x03550004)
	ActionIDEraYanjieFight uint32 = 0x03550004
)

// 纪元湮劫战斗结果码 (Mod_EraInfiniteMission_Base)。
const (
	// EraYanjieResultSuccess 成功
	EraYanjieResultSuccess uint8 = 0
	// EraYanjieResultNotOpen 功能未开放
	EraYanjieResultNotOpen uint8 = 1
	// EraYanjieResultLocked 关卡未解锁
	EraYanjieResultLocked uint8 = 2
	// EraYanjieResultAlreadyPass 已通关
	EraYanjieResultAlreadyPass uint8 = 3
	// EraYanjieResultAlreadyEnd 活动已结束
	EraYanjieResultAlreadyEnd uint8 = 4
	// EraYanjieResultFailed 失败
	EraYanjieResultFailed uint8 = 5
	// EraYanjieResultWrongID 关卡编号错误
	EraYanjieResultWrongID uint8 = 6
	// EraYanjieResultPackFull 背包已满
	EraYanjieResultPackFull uint8 = 7
)

// EraYanjiePanel 代表纪元湮劫主面板。
//
// 线格式 (get_panel_info):
// Int32 era_level, Int32 mission_level, Int32 rank_perc, Int32 rank,
// Int32 fight_end_ts, Int32 era_zone。
type EraYanjiePanel struct {
	// EraLevel 纪元等级
	EraLevel int32
	// MissionLevel 当前已通关的关卡进度
	MissionLevel int32
	// RankPerc 百分比排名
	RankPerc int32
	// Rank 名次
	Rank int32
	// FightEndTS 最近战斗结束时间
	FightEndTS int32
	// EraZone 纪元战区
	EraZone int32
}

// EraYanjieBattle 代表关卡战斗中的单场战报。
type EraYanjieBattle struct {
	// Seq 战斗序号
	Seq int32
	// IsWin 是否胜利 (1 胜利)
	IsWin int32
	// WarReportID 战报编号
	WarReportID int32
	// ReportVersion 战报版本
	ReportVersion string
}

// EraYanjieFightResult 代表关卡战斗结算。
//
// 线格式 (fight):
// UByte result, [Int32 seq, Int32 is_win, Int32 war_report_id, String report_version] battle_list,
// Int32 is_win, [Int32 seq, Int32 item_id] use_item_list。
type EraYanjieFightResult struct {
	// Result 业务结果码 (EraYanjieResultSuccess 等)
	Result uint8
	// Battles 逐场战报
	Battles []EraYanjieBattle
	// IsWin 整体是否胜利 (1 胜利)
	IsWin int32
}

// BuildEraYanjieSeasonPacket 构造查询当前赛季封包。
func BuildEraYanjieSeasonPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDEraYanjieSeason, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// BuildEraYanjiePanelPacket 构造查询主面板封包。
func BuildEraYanjiePanelPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDEraYanjieSeason
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDEraYanjiePanel, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// BuildEraYanjieMonsterTeamPacket 构造查询关卡敌方队伍封包。
func BuildEraYanjieMonsterTeamPacket(level int32, deployType int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(level)
	w.WriteInt32(deployType)
	if prevAct == 0 {
		prevAct = ActionIDEraYanjiePanel
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDEraYanjieMonsterTeam, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// BuildEraYanjieFightPacket 构造关卡战斗封包。
//
// 官方请求为 [Int level, [Int deploy_type, Int seq]]，seq 从 1 开始。
func BuildEraYanjieFightPacket(level int32, deployType int32, seq int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(level)
	w.WriteInt32(deployType)
	w.WriteInt32(seq)
	if prevAct == 0 {
		prevAct = ActionIDEraYanjieMonsterTeam
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDEraYanjieFight, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// ParseEraYanjiePanelResult 解析纪元湮劫主面板。
func ParseEraYanjiePanelResult(payload []byte) (*EraYanjiePanel, error) {
	if len(payload) < 24 {
		return nil, fmt.Errorf("%w: payload too short for era yanjie panel (%dB)", ErrInvalidPayload, len(payload))
	}
	r := NewReader(payload)
	fields := make([]int32, 6)
	for i := range fields {
		v, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read era yanjie field #%d: %v", ErrInvalidPayload, i, err)
		}
		fields[i] = v
	}
	return &EraYanjiePanel{
		EraLevel:     fields[0],
		MissionLevel: fields[1],
		RankPerc:     fields[2],
		Rank:         fields[3],
		FightEndTS:   fields[4],
		EraZone:      fields[5],
	}, nil
}

// ParseEraYanjieMonsterTeamResult 解析敌方队伍查询结果码。
func ParseEraYanjieMonsterTeamResult(payload []byte) (uint8, error) {
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return 0, fmt.Errorf("%w: failed to read era yanjie monster result: %v", ErrInvalidPayload, err)
	}
	return result, nil
}

// ParseEraYanjieFightResult 解析关卡战斗结算。
func ParseEraYanjieFightResult(payload []byte) (*EraYanjieFightResult, error) {
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read era yanjie fight result: %v", ErrInvalidPayload, err)
	}
	res := &EraYanjieFightResult{Result: result}
	count, err := r.ReadUint16()
	if err != nil {
		return res, nil
	}
	for i := 0; i < int(count); i++ {
		var b EraYanjieBattle
		if b.Seq, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read battle[%d].seq: %v", ErrInvalidPayload, i, err)
		}
		if b.IsWin, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read battle[%d].is_win: %v", ErrInvalidPayload, i, err)
		}
		if b.WarReportID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read battle[%d].war_report_id: %v", ErrInvalidPayload, i, err)
		}
		if b.ReportVersion, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("%w: failed to read battle[%d].report_version: %v", ErrInvalidPayload, i, err)
		}
		res.Battles = append(res.Battles, b)
	}
	if res.IsWin, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read era yanjie is_win: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// BuildEraYanjiePanelResultPacket 构造主面板回包 (测试与 Mock 服务端使用)。
func BuildEraYanjiePanelResultPacket(panel EraYanjiePanel) *Packet {
	w := NewWriter()
	w.WriteInt32(panel.EraLevel)
	w.WriteInt32(panel.MissionLevel)
	w.WriteInt32(panel.RankPerc)
	w.WriteInt32(panel.Rank)
	w.WriteInt32(panel.FightEndTS)
	w.WriteInt32(panel.EraZone)
	return NewPacket(ActionIDEraYanjiePanel, w.Bytes())
}

// BuildEraYanjieFightResultPacket 构造战斗回包 (测试与 Mock 服务端使用)。
func BuildEraYanjieFightResultPacket(res EraYanjieFightResult) *Packet {
	w := NewWriter()
	w.WriteUint8(res.Result)
	w.WriteUint16(uint16(len(res.Battles)))
	for _, b := range res.Battles {
		w.WriteInt32(b.Seq)
		w.WriteInt32(b.IsWin)
		w.WriteInt32(b.WarReportID)
		w.WriteString(b.ReportVersion)
	}
	w.WriteInt32(res.IsWin)
	w.WriteUint16(0)
	return NewPacket(ActionIDEraYanjieFight, w.Bytes())
}