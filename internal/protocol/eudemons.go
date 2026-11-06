// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方幻兽主面板协议 (Mod_Eudemons_Base, Module 693 / 0x02B5)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_Eudemons_Base 与
// com.datas.EudemonsData：
//   - panel_info           Act 0 -> 0x02B50000  查询幻兽皮肤、经脉、技能与宝石
//   - activation_meridians Act 8 -> 0x02B50008  激活经脉
package protocol

import "fmt"

const (
	// ModuleEudemons 幻兽模块编号 (0x02B5)
	ModuleEudemons uint16 = 693
)

// 幻兽动作编号 (Mod_Eudemons_Base)。
const (
	// ActionIDEudemonsPanel 查询幻兽面板 (Action 0 -> 0x02B50000)
	ActionIDEudemonsPanel uint32 = 0x02B50000
	// ActionIDEudemonsActivateMeridians 激活幻兽经脉 (Action 8 -> 0x02B50008)
	ActionIDEudemonsActivateMeridians uint32 = 0x02B50008
)

// 幻兽结果码 (Mod_Eudemons_Base)。
const (
	// EudemonsResultSuccess 成功
	EudemonsResultSuccess uint8 = 0
	// EudemonsResultFailed 失败
	EudemonsResultFailed uint8 = 1
	// EudemonsResultFuncNotOpen 功能未开放
	EudemonsResultFuncNotOpen uint8 = 2
	// EudemonsResultMeridiansError 经脉编号错误
	EudemonsResultMeridiansError uint8 = 9
	// EudemonsResultMeridiansNotActive 经脉未激活前置
	EudemonsResultMeridiansNotActive uint8 = 10
	// EudemonsResultMeridiansActive 经脉已激活
	EudemonsResultMeridiansActive uint8 = 12
	// EudemonsResultGemStepNotEnough 宝石阶数不足
	EudemonsResultGemStepNotEnough uint8 = 13
)

// EudemonsMeridian 表示一条幻兽经脉。
type EudemonsMeridian struct {
	// ID 经脉编号
	ID int32
	// IsActive 是否已激活 (1 已激活)
	IsActive uint8
	// StoneSlots 经脉上镶嵌的宝石信息
	StoneSlots []int32
}

// EudemonsPanel 表示幻兽主面板。
type EudemonsPanel struct {
	// Result 面板结果码
	Result uint8
	// SkinID 当前皮肤编号
	SkinID int32
	// StoneMaxLevel 宝石最高阶数
	StoneMaxLevel int32
	// Meridians 经脉列表
	Meridians []EudemonsMeridian
	// BeastSkill 幻兽技能
	BeastSkill [][2]int32
	// PassiveSkill 被动技能
	PassiveSkill [][2]int32
	// GemExpList 宝石经验
	GemExpList [][2]int32
	// TotalStoneStep 宝石总阶数
	TotalStoneStep int32
}

// BuildEudemonsPanelPacket 构造查询幻兽面板封包 (0x02B50000)。
func BuildEudemonsPanelPacket() *Packet {
	return NewPacket(ActionIDEudemonsPanel, nil)
}

// BuildEudemonsActivateMeridiansPacket 构造激活经脉封包 (0x02B50008)。
func BuildEudemonsActivateMeridiansPacket(meridiansID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(meridiansID)
	return NewPacket(ActionIDEudemonsActivateMeridians, w.Bytes())
}

// ParseEudemonsPanelResult 解析幻兽主面板。
func ParseEudemonsPanelResult(payload []byte) (*EudemonsPanel, error) {
	r := NewReader(payload)
	p := &EudemonsPanel{}
	var err error
	if p.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read eudemons result: %v", ErrInvalidPayload, err)
	}
	if p.Result != EudemonsResultSuccess {
		return p, nil
	}
	if p.SkinID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read skin_id: %v", ErrInvalidPayload, err)
	}
	if p.StoneMaxLevel, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read stone_max_level: %v", ErrInvalidPayload, err)
	}
	meridianCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read meridians count: %v", ErrInvalidPayload, err)
	}
	p.Meridians = make([]EudemonsMeridian, 0, meridianCount)
	for i := 0; i < int(meridianCount); i++ {
		var m EudemonsMeridian
		if m.ID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read meridian[%d].id: %v", ErrInvalidPayload, i, err)
		}
		if m.IsActive, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read meridian[%d].is_active: %v", ErrInvalidPayload, i, err)
		}
		slotCount, err := r.ReadUint16()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read meridian[%d].stone count: %v", ErrInvalidPayload, i, err)
		}
		m.StoneSlots = make([]int32, 0, slotCount)
		for j := 0; j < int(slotCount); j++ {
			v, err := r.ReadInt32()
			if err != nil {
				return nil, fmt.Errorf("%w: failed to read meridian[%d].stone[%d]: %v", ErrInvalidPayload, i, j, err)
			}
			m.StoneSlots = append(m.StoneSlots, v)
		}
		p.Meridians = append(p.Meridians, m)
	}
	if err = readIntPairList(r, &p.BeastSkill, "beast_skill"); err != nil {
		return nil, err
	}
	if err = readIntPairList(r, &p.PassiveSkill, "passive_skill"); err != nil {
		return nil, err
	}
	if err = readIntPairList(r, &p.GemExpList, "gem_exp_list"); err != nil {
		return nil, err
	}
	if p.TotalStoneStep, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read total_stone_step: %v", ErrInvalidPayload, err)
	}
	return p, nil
}

func readIntPairList(r *Reader, dst *[][2]int32, name string) error {
	count, err := r.ReadUint16()
	if err != nil {
		return fmt.Errorf("%w: failed to read %s count: %v", ErrInvalidPayload, name, err)
	}
	*dst = make([][2]int32, 0, count)
	for i := 0; i < int(count); i++ {
		a, err := r.ReadInt32()
		if err != nil {
			return fmt.Errorf("%w: failed to read %s[%d].a: %v", ErrInvalidPayload, name, i, err)
		}
		b, err := r.ReadInt32()
		if err != nil {
			return fmt.Errorf("%w: failed to read %s[%d].b: %v", ErrInvalidPayload, name, i, err)
		}
		*dst = append(*dst, [2]int32{a, b})
	}
	return nil
}

// ParseEudemonsActionCode 解析单结果码动作。
func ParseEudemonsActionCode(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty eudemons action result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// BuildEudemonsPanelResultPacket 构造幻兽面板回包，供离线 mock 与单测使用。
func BuildEudemonsPanelResultPacket(p EudemonsPanel) *Packet {
	w := NewWriter()
	w.WriteUint8(p.Result)
	if p.Result == EudemonsResultSuccess {
		w.WriteInt32(p.SkinID)
		w.WriteInt32(p.StoneMaxLevel)
		w.WriteUint16(uint16(len(p.Meridians)))
		for _, m := range p.Meridians {
			w.WriteInt32(m.ID)
			w.WriteUint8(m.IsActive)
			w.WriteUint16(uint16(len(m.StoneSlots)))
			for _, v := range m.StoneSlots {
				w.WriteInt32(v)
			}
		}
		w.WriteUint16(uint16(len(p.BeastSkill)))
		for _, v := range p.BeastSkill {
			w.WriteInt32(v[0])
			w.WriteInt32(v[1])
		}
		w.WriteUint16(uint16(len(p.PassiveSkill)))
		for _, v := range p.PassiveSkill {
			w.WriteInt32(v[0])
			w.WriteInt32(v[1])
		}
		w.WriteUint16(uint16(len(p.GemExpList)))
		for _, v := range p.GemExpList {
			w.WriteInt32(v[0])
			w.WriteInt32(v[1])
		}
		w.WriteInt32(p.TotalStoneStep)
	}
	return NewPacket(ActionIDEudemonsPanel, w.Bytes())
}

// BuildEudemonsActionCodePacket 构造单结果码回包，供离线 mock 与单测使用。
func BuildEudemonsActionCodePacket(actionID uint32, code uint8) *Packet {
	return NewPacket(actionID, []byte{code})
}
