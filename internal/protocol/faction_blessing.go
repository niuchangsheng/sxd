// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现帮派祭神 (Mod_Faction_Base 上香/祝福) 的协议常量、请求封包构造与回包解析。
// 协议描述符对齐 2026 客户端 Main.swf 中 Mod_Faction_Base 的 AS3 声明:
//   - faction_god_info:      module 10, action 22, request [], response [Int, Int, Int, Int, Int, Byte, [Int, String, Int, Int, Int]]
//   - incense:               module 10, action 23, request [Int], response [UByte]
//   - get_blessing_count:    module 10, action 24, request [], response [Int, Int]
//   - blessing_count_change: module 10, action 25, request [], response [UByte, Int]
package protocol

import "fmt"

// 帮派祭神供品编号 (对齐 FactionType.GodOfferings)。
const (
	// FactionOfferingWhiteSandalwood 白檀香 (免费，增加 10 次祝福)
	FactionOfferingWhiteSandalwood int32 = 1
	// FactionOfferingSuhe 苏合香 (消耗 30 元宝，增加 30 次祝福)
	FactionOfferingSuhe int32 = 2
	// FactionOfferingTianmu 天木香 (消耗 90 元宝，增加 100 次祝福)
	FactionOfferingTianmu int32 = 3
)

// FactionOffering 描述一种帮派祭神供品的消耗与收益。
type FactionOffering struct {
	// ID 供品编号 (1~3)
	ID int32
	// Name 供品名称
	Name string
	// IngotCost 单次上香消耗的元宝数量 (0 表示免费)
	IngotCost int32
	// GodExp 增加的神像灵气
	GodExp int32
	// Fame 增加的帮派声望
	Fame int32
	// BlessingCount 增加的祝福次数
	BlessingCount int32
}

// factionOfferings 供品静态数据，对齐 FactionType.GodOfferings:
// [名称, 元宝消耗, 神像灵气, 声望, 祝福次数, VIP 等级偏移, 阅历偏移]。
var factionOfferings = map[int32]FactionOffering{
	FactionOfferingWhiteSandalwood: {ID: FactionOfferingWhiteSandalwood, Name: "白檀香", IngotCost: 0, GodExp: 5, Fame: 10, BlessingCount: 10},
	FactionOfferingSuhe:            {ID: FactionOfferingSuhe, Name: "苏合香", IngotCost: 30, GodExp: 50, Fame: 100, BlessingCount: 30},
	FactionOfferingTianmu:          {ID: FactionOfferingTianmu, Name: "天木香", IngotCost: 90, GodExp: 1000, Fame: 1000, BlessingCount: 100},
}

// GetFactionOffering 按编号查询帮派祭神供品静态数据。
func GetFactionOffering(id int32) (FactionOffering, bool) {
	offering, ok := factionOfferings[id]
	return offering, ok
}

// FactionBlessingRecord 表示一条帮派祭神上香记录。
type FactionBlessingRecord struct {
	// PlayerID 上香玩家编号
	PlayerID int32
	// PlayerName 上香玩家昵称
	PlayerName string
	// IncenseID 供品编号
	IncenseID int32
	// Exp 本次贡献的神像灵气
	Exp int32
	// Time 上香时间 (Unix 秒)
	Time int32
}

// FactionGodInfo 表示帮派祭神神像信息 (Mod 10, Act 22 回包)。
type FactionGodInfo struct {
	// FactionGodLevel 神像等级
	FactionGodLevel int32
	// FactionGodExp 当前神像灵气
	FactionGodExp int32
	// RequireExp 升级所需神像灵气
	RequireExp int32
	// IncenseCount 今日已上香次数
	IncenseCount int32
	// TotalCount 今日可上香总次数
	TotalCount int32
	// IsCover 再次上香是否会覆盖当前剩余祝福次数 (1 表示会覆盖)
	IsCover int8
	// Records 最近上香记录
	Records []FactionBlessingRecord
}

// RemainingIncense 返回今日剩余可上香次数。
func (i *FactionGodInfo) RemainingIncense() int32 {
	if i == nil {
		return 0
	}
	remain := i.TotalCount - i.IncenseCount
	if remain < 0 {
		return 0
	}
	return remain
}

// FactionBlessingCount 表示当前祝福次数与祝福金币加成 (Mod 10, Act 24 回包)。
type FactionBlessingCount struct {
	// Count 当前剩余祝福次数
	Count int32
	// CoinAdd 祝福提供的金币加成
	CoinAdd int32
}

// BuildFactionGodInfoPacket 构造查询帮派祭神神像信息封包 (Mod 10, Act 22)。
func BuildFactionGodInfoPacket(prevAct uint32) *Packet {
	return newPrevActPacket(ActionIDFactionGodInfo, prevAct)
}

// ParseFactionGodInfoResult 解析帮派祭神神像信息回包:
// int32 faction_god_level + int32 faction_god_exp + int32 require_exp
// + int32 incense_count + int32 total_count + int8 is_cover
// + uint16 count + count*(int32 player_id + string player_name + int32 incense_id + int32 exp + int32 time)。
func ParseFactionGodInfoResult(payload []byte) (*FactionGodInfo, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))

	level, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read faction_god_level: %v", ErrInvalidPayload, err)
	}
	exp, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read faction_god_exp: %v", ErrInvalidPayload, err)
	}
	requireExp, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read require_exp: %v", ErrInvalidPayload, err)
	}
	incenseCount, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read incense_count: %v", ErrInvalidPayload, err)
	}
	totalCount, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read total_count: %v", ErrInvalidPayload, err)
	}
	isCover, err := r.ReadInt8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read is_cover: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read record count: %v", ErrInvalidPayload, err)
	}

	records := make([]FactionBlessingRecord, 0, count)
	for i := 0; i < int(count); i++ {
		playerID, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read record[%d] player_id: %v", ErrInvalidPayload, i, err)
		}
		playerName, err := r.ReadString()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read record[%d] player_name: %v", ErrInvalidPayload, i, err)
		}
		incenseID, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read record[%d] incense_id: %v", ErrInvalidPayload, i, err)
		}
		recordExp, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read record[%d] exp: %v", ErrInvalidPayload, i, err)
		}
		recordTime, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read record[%d] time: %v", ErrInvalidPayload, i, err)
		}
		records = append(records, FactionBlessingRecord{
			PlayerID:   playerID,
			PlayerName: playerName,
			IncenseID:  incenseID,
			Exp:        recordExp,
			Time:       recordTime,
		})
	}

	return &FactionGodInfo{
		FactionGodLevel: level,
		FactionGodExp:   exp,
		RequireExp:      requireExp,
		IncenseCount:    incenseCount,
		TotalCount:      totalCount,
		IsCover:         isCover,
		Records:         records,
	}, nil
}

// BuildFactionIncensePacket 构造帮派祭神上香封包 (Mod 10, Act 23)。
//
// 注意: 与同模块其他请求不同，2026 线上服务端要求 incense 请求载荷严格为
// 4 字节供品编号，末尾追加 prevAct 会被服务端静默丢弃且不回包。
// 该结论已通过 test/probes/probe_faction_blessing_live.go 实机验证:
// 4 字节载荷返回 0x21 (OFFERINGS_NO_EXIST)，8 字节载荷无响应。
func BuildFactionIncensePacket(offeringID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(offeringID)
	return NewPacket(ActionIDFactionIncense, w.Bytes())
}

// ParseFactionIncenseResult 解析上香回包: uint8 result。
func ParseFactionIncenseResult(payload []byte) (uint8, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	result, err := r.ReadUint8()
	if err != nil {
		return 0, fmt.Errorf("%w: failed to read incense result: %v", ErrInvalidPayload, err)
	}
	return result, nil
}

// BuildFactionGetBlessingCountPacket 构造查询帮派祭神当前祝福次数封包 (Mod 10, Act 24)。
func BuildFactionGetBlessingCountPacket(prevAct uint32) *Packet {
	return newPrevActPacket(ActionIDFactionGetBlessingCount, prevAct)
}

// ParseFactionBlessingCountResult 解析祝福次数回包: int32 count + int32 coin_add。
func ParseFactionBlessingCountResult(payload []byte) (*FactionBlessingCount, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))

	count, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read blessing count: %v", ErrInvalidPayload, err)
	}
	coinAdd, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read blessing coin_add: %v", ErrInvalidPayload, err)
	}
	return &FactionBlessingCount{Count: count, CoinAdd: coinAdd}, nil
}
