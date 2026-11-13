// Package protocol 提供旧版药园（伙伴/摇钱树）协议构造与解析。
//
// 2026-11-14 实机：低等级账号 (ncs s1, 94 级) 的新农场种子商店 buy_new_herbs
// 全形态静默丢弃，但旧版 13/1 伙伴列表、13/12 摇钱树次数可用；摇钱树次数
// 每几十秒免费恢复一次，可无元宝种植。
package protocol

import "fmt"

// 旧版药园动作编号 (Mod_Farm_Base, Module 13)。
const (
	// ActionIDFarmRoleList 伙伴列表 13/1。
	ActionIDFarmRoleList uint32 = 0x000D0001
	// ActionIDFarmPlantHerbs 旧版播种 13/5。
	ActionIDFarmPlantHerbs uint32 = 0x000D0005
	// ActionIDFarmCheckRoleState 播种前伙伴状态检查 13/8。
	ActionIDFarmCheckRoleState uint32 = 0x000D0008
	// ActionIDFarmCoinTreeInfo 摇钱树剩余次数/恢复时间 13/12。
	ActionIDFarmCoinTreeInfo uint32 = 0x000D000C
)

// 旧版播种类型与结果码。
const (
	// FarmHerbTypeExp 伙伴经验药草 (需要伙伴等级达标)。
	FarmHerbTypeExp uint8 = 2
	// FarmHerbTypeCoin 摇钱树 (消耗免费恢复的摇钱树次数)。
	FarmHerbTypeCoin uint8 = 3
	// FarmHerbTypeXianling 仙灵树。
	FarmHerbTypeXianling uint8 = 4
	// FarmCheckRoleStatePass check_role_state 通过。
	FarmCheckRoleStatePass uint8 = 21
	// FarmPlantSuccess 旧版播种成功。
	FarmPlantSuccess uint8 = 8
)

// FarmCoinTreeInfo 摇钱树次数信息。
type FarmCoinTreeInfo struct {
	// Count 当前可免费种植次数
	Count int32
	// RemainBuyTimes 今日剩余可购买次数 (购买消耗元宝，自动化不使用)
	RemainBuyTimes int32
	// Ingot 购买单价 (元宝，自动化不使用)
	Ingot int32
	// RecoverCD 下一次免费恢复的剩余秒数
	RecoverCD int32
}

// BuildFarmRoleListPacket 构造查询旧版伙伴列表封包 (13/1)。
func BuildFarmRoleListPacket() *Packet {
	return NewPacket(ActionIDFarmRoleList, nil)
}

// ParseFarmRoleIDs 解析旧版伙伴列表回包，仅提取 role_id。
//
// 记录线格式: [Int32 roleID][String name][String sign][Int32 level]
// [Int64 exp][Int64 maxExp][Int32 a][Int32 b][Int32 c][Uint8 flag]。
func ParseFarmRoleIDs(payload []byte) ([]int32, error) {
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("read farm role count: %w", err)
	}
	out := make([]int32, 0, count)
	for i := 0; i < int(count); i++ {
		roleID, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("read farm role #%d id: %w", i, err)
		}
		if _, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("read farm role #%d name: %w", i, err)
		}
		if _, err = r.ReadString(); err != nil {
			return nil, fmt.Errorf("read farm role #%d sign: %w", i, err)
		}
		if _, err = r.ReadInt32(); err != nil { // level
			return nil, fmt.Errorf("read farm role #%d level: %w", i, err)
		}
		if _, err = r.ReadInt64(); err != nil { // exp
			return nil, fmt.Errorf("read farm role #%d exp: %w", i, err)
		}
		if _, err = r.ReadInt64(); err != nil { // max exp
			return nil, fmt.Errorf("read farm role #%d max exp: %w", i, err)
		}
		for f := 0; f < 3; f++ {
			if _, err = r.ReadInt32(); err != nil {
				return nil, fmt.Errorf("read farm role #%d field: %w", i, err)
			}
		}
		if _, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("read farm role #%d flag: %w", i, err)
		}
		out = append(out, roleID)
	}
	return out, nil
}

// BuildFarmCoinTreeInfoPacket 构造查询摇钱树次数封包 (13/12)。
func BuildFarmCoinTreeInfoPacket() *Packet {
	return NewPacket(ActionIDFarmCoinTreeInfo, nil)
}

// ParseFarmCoinTreeInfo 解析摇钱树次数回包 (4 × Int32)。
func ParseFarmCoinTreeInfo(payload []byte) (*FarmCoinTreeInfo, error) {
	r := NewReader(payload)
	count, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("read coin tree count: %w", err)
	}
	remain, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("read coin tree remain buy: %w", err)
	}
	ingot, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("read coin tree ingot: %w", err)
	}
	cd, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("read coin tree recover cd: %w", err)
	}
	return &FarmCoinTreeInfo{Count: count, RemainBuyTimes: remain, Ingot: ingot, RecoverCD: cd}, nil
}

// BuildFarmCheckRoleStatePacket 构造伙伴播种资格检查封包 (13/8)。
func BuildFarmCheckRoleStatePacket(roleID int32, herbType uint8) *Packet {
	w := NewWriter()
	w.WriteInt32(roleID)
	w.WriteUint8(herbType)
	return NewPacket(ActionIDFarmCheckRoleState, w.Bytes())
}

// ParseFarmCheckRoleStateResult 解析伙伴播种资格结果。
func ParseFarmCheckRoleStateResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("empty farm check role state result")
	}
	return payload[0], nil
}

// BuildFarmPlantHerbsPacket 构造旧版播种封包 (13/5)。
func BuildFarmPlantHerbsPacket(landID, roleID int32, herbType uint8, star int8) *Packet {
	w := NewWriter()
	w.WriteInt32(landID)
	w.WriteInt32(roleID)
	w.WriteUint8(herbType)
	w.WriteInt8(star)
	return NewPacket(ActionIDFarmPlantHerbs, w.Bytes())
}

// ParseFarmPlantHerbsResult 解析旧版播种结果 (首字节)。
func ParseFarmPlantHerbsResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("empty farm plant herbs result")
	}
	return payload[0], nil
}