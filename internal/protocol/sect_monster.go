// Package protocol 提供宗门灵兽 (Mod_SectMonster_Base, Module 576) 协议编解码。
//
// 线上实测: 该模块必须通过**仙盟城镇会话**发送，且请求**不能携带 PrevAct 尾部**
// （4 字节 [sect_id] 或 8 字节 [monster_type, feed_type] 原样发送）。
// 携带 PrevAct 时服务端静默丢弃。
//
// @author Ateng
// @since 2026-11-07
package protocol

import (
	"fmt"
)

// 宗门灵兽动作编号 (Mod_SectMonster_Base)。
const (
	// ActionIDSectMonsterEggPanel 灵兽蛋面板 (Action 0 -> 0x02400000)
	ActionIDSectMonsterEggPanel uint32 = 0x02400000
	// ActionIDSectMonsterList 宗门灵兽列表 (Action 1 -> 0x02400001)
	ActionIDSectMonsterList uint32 = 0x02400001
	// ActionIDSectMonsterFeed 喂养灵兽 (Action 3 -> 0x02400003)
	ActionIDSectMonsterFeed uint32 = 0x02400003
)

// 宗门灵兽喂养结果码 (Mod_SectMonster_Base)。
const (
	// SectMonsterResultSuccess 成功
	SectMonsterResultSuccess uint8 = 0
	// SectMonsterResultHasPushed 今日已推送过魂
	SectMonsterResultHasPushed uint8 = 1
	// SectMonsterResultJobLimit 职业限制
	SectMonsterResultJobLimit uint8 = 2
	// SectMonsterResultFullMemberTimeLimit 满员时间限制
	SectMonsterResultFullMemberTimeLimit uint8 = 3
	// SectMonsterResultFailed 失败
	SectMonsterResultFailed uint8 = 4
	// SectMonsterResultFeedNumLimit 喂养次数不足/已达上限
	SectMonsterResultFeedNumLimit uint8 = 5
	// SectMonsterResultNotHaveMonsterType 无此灵兽类型
	SectMonsterResultNotHaveMonsterType uint8 = 6
	// SectMonsterResultPackFull 背包已满
	SectMonsterResultPackFull uint8 = 7
	// SectMonsterResultNotEnoughIngot 元宝不足 (高级喂养档)
	SectMonsterResultNotEnoughIngot uint8 = 8
)

// SectMonster 代表宗门中的一只可喂养灵兽。
//
// 条目线格式为 [Int32 monster_type, Int32 level, Int32 flag, Int32 exp_cap]。
type SectMonster struct {
	// Type 灵兽类型
	Type int32
	// Level 灵兽等级
	Level int32
	// Flag 状态标记
	Flag int32
	// ExpCap 经验上限/累计值
	ExpCap int32
}

// SectMonsterCommon 代表通用灵兽类型条目 [Int32 type, Int32 level, Int32 num]。
type SectMonsterCommon struct {
	// Type 灵兽类型
	Type int32
	// Level 灵兽等级
	Level int32
	// Num 数量/喂养值
	Num int32
}

// SectMonsterList 代表宗门灵兽列表。
//
// 线格式 (get_monster_list):
// [Int32 monster_type, Int32 level, Int32 flag, Int32 exp_cap] monster_list,
// Int32 free_feed_num, Int32 high_feed_num, Int32 job_id, Int32 week_add_monster_exp,
// [Int32 type, Int32 level, Int32 num] common_monster_list。
type SectMonsterList struct {
	// Monsters 宗门内已存在的灵兽
	Monsters []SectMonster
	// FreeFeedNum 已使用的免费喂养次数
	FreeFeedNum int32
	// HighFeedNum 已使用的高级喂养次数
	HighFeedNum int32
	// JobID 职业限制
	JobID int32
	// WeekAddMonsterExp 本周累计增加经验
	WeekAddMonsterExp int32
	// CommonMonsters 通用灵兽类型列表
	CommonMonsters []SectMonsterCommon
}

// SectMonsterFeedAward 代表喂养奖励中的单件道具。
type SectMonsterFeedAward struct {
	// ItemID 道具编号
	ItemID int32
	// ItemNum 道具数量
	ItemNum int32
}

// SectMonsterFeedResult 代表宗门灵兽喂养结果。
type SectMonsterFeedResult struct {
	// Result 业务结果码 (SectMonsterResultSuccess 等)
	Result uint8
	// Awards 喂养奖励
	Awards []SectMonsterFeedAward
}

// BuildSectMonsterListPacket 构造宗门灵兽列表封包。
//
// 请求为 4 字节 [sect_id]，不带 PrevAct。
func BuildSectMonsterListPacket(sectID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(sectID)
	return NewPacket(ActionIDSectMonsterList, w.Bytes())
}

// BuildSectMonsterEggPanelPacket 构造灵兽蛋面板封包 (4 字节 sect_id)。
func BuildSectMonsterEggPanelPacket(sectID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(sectID)
	return NewPacket(ActionIDSectMonsterEggPanel, w.Bytes())
}

// BuildSectMonsterFeedPacket 构造宗门灵兽喂养封包。
//
// feedType=0 为免费喂养；1 为高级喂养（自动化永不使用）。
func BuildSectMonsterFeedPacket(monsterType int32, feedType int32) *Packet {
	w := NewWriter()
	w.WriteInt32(monsterType)
	w.WriteInt32(feedType)
	return NewPacket(ActionIDSectMonsterFeed, w.Bytes())
}

// ParseSectMonsterListResult 解析宗门灵兽列表。
func ParseSectMonsterListResult(payload []byte) (*SectMonsterList, error) {
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read sect monster count: %v", ErrInvalidPayload, err)
	}
	res := &SectMonsterList{Monsters: make([]SectMonster, 0, count)}
	for i := 0; i < int(count); i++ {
		var m SectMonster
		if m.Type, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read monster[%d].type: %v", ErrInvalidPayload, i, err)
		}
		if m.Level, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read monster[%d].level: %v", ErrInvalidPayload, i, err)
		}
		if m.Flag, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read monster[%d].flag: %v", ErrInvalidPayload, i, err)
		}
		if m.ExpCap, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read monster[%d].exp_cap: %v", ErrInvalidPayload, i, err)
		}
		res.Monsters = append(res.Monsters, m)
	}
	for _, field := range []*int32{&res.FreeFeedNum, &res.HighFeedNum, &res.JobID, &res.WeekAddMonsterExp} {
		if *field, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read sect monster scalar: %v", ErrInvalidPayload, err)
		}
	}
	commonCount, err := r.ReadUint16()
	if err != nil {
		return res, nil
	}
	for i := 0; i < int(commonCount); i++ {
		var c SectMonsterCommon
		if c.Type, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read common[%d].type: %v", ErrInvalidPayload, i, err)
		}
		if c.Level, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read common[%d].level: %v", ErrInvalidPayload, i, err)
		}
		if c.Num, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read common[%d].num: %v", ErrInvalidPayload, i, err)
		}
		res.CommonMonsters = append(res.CommonMonsters, c)
	}
	return res, nil
}

// ParseSectMonsterFeedResult 解析宗门灵兽喂养结果。
func ParseSectMonsterFeedResult(payload []byte) (*SectMonsterFeedResult, error) {
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read sect monster feed result: %v", ErrInvalidPayload, err)
	}
	res := &SectMonsterFeedResult{Result: result}
	count, err := r.ReadUint16()
	if err != nil {
		return res, nil
	}
	for i := 0; i < int(count); i++ {
		itemID, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read feed award[%d].item_id: %v", ErrInvalidPayload, i, err)
		}
		num, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read feed award[%d].item_num: %v", ErrInvalidPayload, i, err)
		}
		res.Awards = append(res.Awards, SectMonsterFeedAward{ItemID: itemID, ItemNum: num})
	}
	return res, nil
}

// BuildSectMonsterListResultPacket 构造宗门灵兽列表回包 (测试与 Mock 服务端使用)。
func BuildSectMonsterListResultPacket(res SectMonsterList) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(res.Monsters)))
	for _, m := range res.Monsters {
		w.WriteInt32(m.Type)
		w.WriteInt32(m.Level)
		w.WriteInt32(m.Flag)
		w.WriteInt32(m.ExpCap)
	}
	w.WriteInt32(res.FreeFeedNum)
	w.WriteInt32(res.HighFeedNum)
	w.WriteInt32(res.JobID)
	w.WriteInt32(res.WeekAddMonsterExp)
	w.WriteUint16(uint16(len(res.CommonMonsters)))
	for _, c := range res.CommonMonsters {
		w.WriteInt32(c.Type)
		w.WriteInt32(c.Level)
		w.WriteInt32(c.Num)
	}
	return NewPacket(ActionIDSectMonsterList, w.Bytes())
}

// BuildSectMonsterFeedResultPacket 构造喂养回包 (测试与 Mock 服务端使用)。
func BuildSectMonsterFeedResultPacket(res SectMonsterFeedResult) *Packet {
	w := NewWriter()
	w.WriteUint8(res.Result)
	w.WriteUint16(uint16(len(res.Awards)))
	for _, a := range res.Awards {
		w.WriteInt32(a.ItemID)
		w.WriteInt32(a.ItemNum)
	}
	return NewPacket(ActionIDSectMonsterFeed, w.Bytes())
}