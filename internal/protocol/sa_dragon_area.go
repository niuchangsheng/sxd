// Package protocol 提供神仙道二进制协议封包编解码与响应解析能力。
//
// 本文件实现圣域「无尽龙域 / 龙域战场」(Mod_SaDragonArea_Base, Module 362) 的
// 请求封包构建、状态码语义映射与响应解码。
//
// 官方 Main.swf (com.protocols.Mod_SaDragonArea_Base / com.datas.SaDragonAreaData):
//   - Module 362 (0x016A), 走圣域独立连接 (DataBase.SOCKETSAINTAREA):
//     * Action 0: get_dragon_area_info (request=[])
//     * Action 1: get_top_10           (request=[])
//     * Action 2: fight                (request=[])
//     * Action 4: quick_fight          (request=[], 免费扫荡至 quick_seq)
package protocol

import (
	"fmt"
)

// ModuleSaDragonArea 圣域无尽龙域/龙域战场模块号 (Mod_SaDragonArea_Base)。
const ModuleSaDragonArea uint32 = 362

// 圣域无尽龙域/龙域战场协议动作号。
const (
	// ActionIDSaDragonAreaInfo 获取当前关卡、可扫荡层数与通关状态 (362/0)
	ActionIDSaDragonAreaInfo uint32 = (ModuleSaDragonArea << 16) | 0
	// ActionIDSaDragonAreaTop10 获取龙域排行榜前十 (362/1)
	ActionIDSaDragonAreaTop10 uint32 = (ModuleSaDragonArea << 16) | 1
	// ActionIDSaDragonAreaFight 发起当前层挑战战斗 (362/2)
	ActionIDSaDragonAreaFight uint32 = (ModuleSaDragonArea << 16) | 2
	// ActionIDSaDragonAreaQuickFight 一键免费扫荡至 quick_seq (362/4)
	ActionIDSaDragonAreaQuickFight uint32 = (ModuleSaDragonArea << 16) | 4
)

// Mod_SaDragonArea_Base 结果码常量。
const (
	SaDragonAreaResultSuccess        uint8 = 0 // SUCCESS
	SaDragonAreaResultFunctionNoOpen uint8 = 1 // FUNCTION_NO_OPEN
	SaDragonAreaResultCompleteAll    uint8 = 2 // COMPLETE_ALL
	SaDragonAreaResultSaintAreaClose uint8 = 3 // SAINT_AREA_CLOSE
	SaDragonAreaResultCD             uint8 = 4 // CD
	SaDragonAreaResultFailed         uint8 = 5 // FAILD
)

// SaDragonAreaResultText 返回无尽龙域结果码的可读说明。
func SaDragonAreaResultText(code uint8) string {
	switch code {
	case SaDragonAreaResultSuccess:
		return "成功"
	case SaDragonAreaResultFunctionNoOpen:
		return "功能未开放"
	case SaDragonAreaResultCompleteAll:
		return "已全部通关"
	case SaDragonAreaResultSaintAreaClose:
		return "圣域已关闭"
	case SaDragonAreaResultCD:
		return "战斗冷却中"
	case SaDragonAreaResultFailed:
		return "挑战失败"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// SaDragonAreaAward 表示龙域关卡或扫荡奖励条目。
type SaDragonAreaAward struct {
	ItemID int32
	Count  int32
}

// SaDragonAreaInfo 表示 362/0 get_dragon_area_info 的面板快照。
type SaDragonAreaInfo struct {
	Seq                int32
	Awards             []SaDragonAreaAward
	CompleteCount      int16
	DecreaseDifficulty int8
	FirstNickName      string
	FirstWarReportID   int32
	FirstVersion       string
	BestNickName       string
	BestWarReportID    int32
	BestVersion        string
	SpecialPattern     int32
	MonsterTeamID      int32
	FiveElemPropID     int32
	LastWeekSeq        int32
	QuickSeq           int32
	IsDragonClear      int32
}

// SaDragonAreaSweepEntry 表示 362/4 quick_fight 返回的单层扫荡奖励。
type SaDragonAreaSweepEntry struct {
	Seq    int32
	Awards []SaDragonAreaAward
}

// BuildSaDragonAreaInfoPacket 构造 362/0 get_dragon_area_info 请求包 (request=[])。
func BuildSaDragonAreaInfoPacket() *Packet {
	return NewPacket(ActionIDSaDragonAreaInfo, nil)
}

// BuildSaDragonAreaFightPacket 构造 362/2 fight 挑战请求包 (request=[])。
func BuildSaDragonAreaFightPacket() *Packet {
	return NewPacket(ActionIDSaDragonAreaFight, nil)
}

// BuildSaDragonAreaQuickFightPacket 构造 362/4 quick_fight 免费一键扫荡请求包 (request=[])。
func BuildSaDragonAreaQuickFightPacket() *Packet {
	return NewPacket(ActionIDSaDragonAreaQuickFight, nil)
}

// ParseSaDragonAreaInfo 解析 362/0 get_dragon_area_info 响应。
func ParseSaDragonAreaInfo(payload []byte) (*SaDragonAreaInfo, error) {
	if len(payload) == 0 {
		return nil, ErrPayloadTruncated
	}
	r := NewReader(payload)
	info := &SaDragonAreaInfo{}
	var err error

	if info.Seq, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("protocol: parse sa_dragon_area seq: %w", err)
	}
	awardCnt, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("protocol: parse sa_dragon_area award_list count: %w", err)
	}
	if awardCnt > 0 {
		info.Awards = make([]SaDragonAreaAward, 0, awardCnt)
		for i := 0; i < int(awardCnt); i++ {
			itemID, err := r.ReadInt32()
			if err != nil {
				return nil, fmt.Errorf("protocol: parse sa_dragon_area award[%d].item_id: %w", i, err)
			}
			cnt, err := r.ReadInt32()
			if err != nil {
				return nil, fmt.Errorf("protocol: parse sa_dragon_area award[%d].count: %w", i, err)
			}
			info.Awards = append(info.Awards, SaDragonAreaAward{ItemID: itemID, Count: cnt})
		}
	}
	if info.CompleteCount, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("protocol: parse sa_dragon_area complete_count: %w", err)
	}
	if info.DecreaseDifficulty, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("protocol: parse sa_dragon_area decrease_difficulty: %w", err)
	}
	if info.FirstNickName, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("protocol: parse sa_dragon_area first_nickname: %w", err)
	}
	if info.FirstWarReportID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("protocol: parse sa_dragon_area first_war_report_id: %w", err)
	}
	if info.FirstVersion, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("protocol: parse sa_dragon_area first_version: %w", err)
	}
	if info.BestNickName, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("protocol: parse sa_dragon_area best_nickname: %w", err)
	}
	if info.BestWarReportID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("protocol: parse sa_dragon_area best_war_report_id: %w", err)
	}
	if info.BestVersion, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("protocol: parse sa_dragon_area best_version: %w", err)
	}
	if info.SpecialPattern, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("protocol: parse sa_dragon_area special_pattern: %w", err)
	}
	if info.MonsterTeamID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("protocol: parse sa_dragon_area monster_team_id: %w", err)
	}

	// round_dragonball_list: [Int8 location, Int32 dragonball, Int8 level]
	dbCnt, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("protocol: parse sa_dragon_area dragonball count: %w", err)
	}
	for i := 0; i < int(dbCnt); i++ {
		if _, err = r.ReadInt8(); err != nil {
			return nil, fmt.Errorf("protocol: parse sa_dragon_area dragonball[%d].location: %w", i, err)
		}
		if _, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("protocol: parse sa_dragon_area dragonball[%d].id: %w", i, err)
		}
		if _, err = r.ReadInt8(); err != nil {
			return nil, fmt.Errorf("protocol: parse sa_dragon_area dragonball[%d].level: %w", i, err)
		}
	}

	// monster_list: 7 x Int32
	monCnt, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("protocol: parse sa_dragon_area monster count: %w", err)
	}
	for i := 0; i < int(monCnt); i++ {
		for j := 0; j < 7; j++ {
			if _, err = r.ReadInt32(); err != nil {
				return nil, fmt.Errorf("protocol: parse sa_dragon_area monster[%d][%d]: %w", i, j, err)
			}
		}
	}

	if info.FiveElemPropID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("protocol: parse sa_dragon_area five_elements_property_id: %w", err)
	}
	if info.LastWeekSeq, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("protocol: parse sa_dragon_area last_week_seq: %w", err)
	}
	if info.QuickSeq, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("protocol: parse sa_dragon_area quick_seq: %w", err)
	}
	if info.IsDragonClear, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("protocol: parse sa_dragon_area is_dragon_clear: %w", err)
	}
	return info, nil
}

// ParseSaDragonAreaFightResult 解析 362/2 fight 响应的首字节结果码。
func ParseSaDragonAreaFightResult(payload []byte) (uint8, error) {
	if len(payload) < 1 {
		return 0, ErrPayloadTruncated
	}
	return payload[0], nil
}

// ParseSaDragonAreaQuickFightResult 解析 362/4 quick_fight 扫荡奖励列表。
func ParseSaDragonAreaQuickFightResult(payload []byte) ([]SaDragonAreaSweepEntry, error) {
	if len(payload) < 2 {
		return nil, ErrPayloadTruncated
	}
	r := NewReader(payload)
	cnt, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("protocol: parse sa_dragon_area quick_fight count: %w", err)
	}
	entries := make([]SaDragonAreaSweepEntry, 0, cnt)
	for i := 0; i < int(cnt); i++ {
		seq, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("protocol: parse sa_dragon_area quick_fight[%d].seq: %w", i, err)
		}
		aCnt, err := r.ReadUint16()
		if err != nil {
			return nil, fmt.Errorf("protocol: parse sa_dragon_area quick_fight[%d].awards count: %w", i, err)
		}
		awards := make([]SaDragonAreaAward, 0, aCnt)
		for j := 0; j < int(aCnt); j++ {
			itemID, err := r.ReadInt32()
			if err != nil {
				return nil, fmt.Errorf("protocol: parse sa_dragon_area quick_fight[%d].award[%d].item_id: %w", i, j, err)
			}
			num, err := r.ReadInt32()
			if err != nil {
				return nil, fmt.Errorf("protocol: parse sa_dragon_area quick_fight[%d].award[%d].count: %w", i, j, err)
			}
			awards = append(awards, SaDragonAreaAward{ItemID: itemID, Count: num})
		}
		entries = append(entries, SaDragonAreaSweepEntry{Seq: seq, Awards: awards})
	}
	return entries, nil
}
