// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方世界 Boss 协议 (Mod_WorldBoss_Base, Module 24 / 0x18)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_WorldBoss_Base 与
// com.datas.WorldBossData：列表返回各 Boss 的开战时间与状态，
// 开战中可对指定 bossId 发起战斗。自动化只做免费参战，
// 永不调用 clear_death_cd / reset_death_cd / strengthen_combat (消耗元宝)。
package protocol

import "fmt"

const (
	// ActionIDWorldBossList 查询世界 Boss 列表 (Module 24, Action 0 -> 0x00180000)
	ActionIDWorldBossList uint32 = 0x00180000
	// ActionIDWorldBossData 查询当前世界 Boss 数据 (Module 24, Action 1 -> 0x00180001)
	ActionIDWorldBossData uint32 = 0x00180001
	// ActionIDWorldBossFight 发起世界 Boss 战斗 (Module 24, Action 2 -> 0x00180002)
	ActionIDWorldBossFight uint32 = 0x00180002
	// ActionIDWorldBossClearDeathCD 清除死亡冷却 (Module 24, Action 3；消耗元宝，自动化不调用)
	ActionIDWorldBossClearDeathCD uint32 = 0x00180003
	// ActionIDWorldBossStrengthenCombat 鼓舞强化 (Module 24, Action 7；消耗元宝，自动化不调用)
	ActionIDWorldBossStrengthenCombat uint32 = 0x00180007
)

// Mod_WorldBoss_Base 结果码。
const (
	// WorldBossResultSuccess 战斗受理成功
	WorldBossResultSuccess uint8 = 0
	// WorldBossResultFailed 战斗失败
	WorldBossResultFailed uint8 = 1
	// WorldBossResultNotBegin 未到开战时间
	WorldBossResultNotBegin uint8 = 2
	// WorldBossResultMissionOver 活动已结束
	WorldBossResultMissionOver uint8 = 3
	// WorldBossResultBossDead Boss 已被击杀
	WorldBossResultBossDead uint8 = 4
	// WorldBossResultDeathStatus 角色处于死亡状态
	WorldBossResultDeathStatus uint8 = 5
	// WorldBossResultNotEnoughIngot 元宝不足
	WorldBossResultNotEnoughIngot uint8 = 6
	// WorldBossResultMaxTimes 今日参战次数已满
	WorldBossResultMaxTimes uint8 = 7
	// WorldBossResultNotEnoughSkill 挑战所需技能点不足
	WorldBossResultNotEnoughSkill uint8 = 8
	// WorldBossResultCombatLimit 战力不足
	WorldBossResultCombatLimit uint8 = 9
)

// 世界 Boss 状态 (com.assist.view.info.BossInfo)。
const (
	// WorldBossStateWillOpen 未到开战时间
	WorldBossStateWillOpen uint8 = 0
	// WorldBossStateOpen 开战中
	WorldBossStateOpen uint8 = 1
	// WorldBossStateOver 已结束
	WorldBossStateOver uint8 = 2
	// WorldBossStateKilled 已被击杀
	WorldBossStateKilled uint8 = 3
)

// WorldBossEntry 表示列表中的一个世界 Boss。
//
// 线格式 (Mod_WorldBoss_Base.get_world_boss_list response 元素):
// Byte boss_id, Byte map_id, Int level, Byte state, Byte can_set,
// Int start_time, Int end_time, Byte can_auto, Byte auto,
// Int robot_coin, Int robot_fame。
type WorldBossEntry struct {
	// BossID Boss 编号 (1 擎天木 / 2 赤炎兽 / 3 刀疤兔 / 10 白泽 / 11 青龙)
	BossID uint8
	// MapID 所在地图编号
	MapID uint8
	// Level Boss 等级
	Level int32
	// State 状态 (0 未开始 / 1 开战中 / 2 已结束 / 3 已击杀)
	State uint8
	// CanSet 是否可设置
	CanSet uint8
	// StartTime 开战时间 (服务器时间戳/时分秒，以服务端语义为准)
	StartTime int32
	// EndTime 结束时间
	EndTime int32
	// CanAuto 是否可自动参战
	CanAuto uint8
	// Auto 自动参战开关
	Auto uint8
	// RobotCoin 机器人铜钱奖励
	RobotCoin int32
	// RobotFame 机器人声望奖励
	RobotFame int32
}

// BuildWorldBossListPacket 构造查询世界 Boss 列表封包 (0x00180000)。
//
// 请求载荷为单字节 0 (官方 ActivityWindowView.get_world_boss_list 传 [0])；
// 实测空载荷会被服务端静默丢弃。
func BuildWorldBossListPacket() *Packet {
	return NewPacket(ActionIDWorldBossList, []byte{0})
}

// BuildWorldBossFightPacket 构造对指定 Boss 发起战斗封包 (0x00180002)。
func BuildWorldBossFightPacket(bossID uint8) *Packet {
	return NewPacket(ActionIDWorldBossFight, []byte{bossID})
}

// ParseWorldBossListResult 解析世界 Boss 列表回包。
func ParseWorldBossListResult(payload []byte) ([]WorldBossEntry, error) {
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read world boss count: %v", ErrInvalidPayload, err)
	}
	out := make([]WorldBossEntry, 0, count)
	for i := 0; i < int(count); i++ {
		var e WorldBossEntry
		if e.BossID, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read world boss[%d].boss_id: %v", ErrInvalidPayload, i, err)
		}
		if e.MapID, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read world boss[%d].map_id: %v", ErrInvalidPayload, i, err)
		}
		if e.Level, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read world boss[%d].level: %v", ErrInvalidPayload, i, err)
		}
		if e.State, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read world boss[%d].state: %v", ErrInvalidPayload, i, err)
		}
		if e.CanSet, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read world boss[%d].can_set: %v", ErrInvalidPayload, i, err)
		}
		if e.StartTime, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read world boss[%d].start_time: %v", ErrInvalidPayload, i, err)
		}
		if e.EndTime, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read world boss[%d].end_time: %v", ErrInvalidPayload, i, err)
		}
		if e.CanAuto, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read world boss[%d].can_auto: %v", ErrInvalidPayload, i, err)
		}
		if e.Auto, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read world boss[%d].auto: %v", ErrInvalidPayload, i, err)
		}
		if e.RobotCoin, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read world boss[%d].robot_coin: %v", ErrInvalidPayload, i, err)
		}
		if e.RobotFame, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read world boss[%d].robot_fame: %v", ErrInvalidPayload, i, err)
		}
		out = append(out, e)
	}
	return out, nil
}

// ParseWorldBossFightResult 解析世界 Boss 战斗回包的首个结果码。
//
// 回包随后携带完整战斗数据，自动化只消费结果码，避免解析大段战报。
func ParseWorldBossFightResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty world boss fight result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// BuildWorldBossListResultPacket 构造世界 Boss 列表回包，供离线 mock 与单测使用。
func BuildWorldBossListResultPacket(entries []WorldBossEntry) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(entries)))
	for _, e := range entries {
		w.WriteUint8(e.BossID)
		w.WriteUint8(e.MapID)
		w.WriteInt32(e.Level)
		w.WriteUint8(e.State)
		w.WriteUint8(e.CanSet)
		w.WriteInt32(e.StartTime)
		w.WriteInt32(e.EndTime)
		w.WriteUint8(e.CanAuto)
		w.WriteUint8(e.Auto)
		w.WriteInt32(e.RobotCoin)
		w.WriteInt32(e.RobotFame)
	}
	return NewPacket(ActionIDWorldBossList, w.Bytes())
}

// BuildWorldBossFightResultPacket 构造世界 Boss 战斗回包，供离线 mock 与单测使用。
func BuildWorldBossFightResultPacket(result uint8) *Packet {
	return NewPacket(ActionIDWorldBossFight, []byte{result})
}
