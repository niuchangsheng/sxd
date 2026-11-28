// 本文件实现宠物仙域协议编解码 (Mod_PetImmortalDomain_Base, Module 755)。
package protocol

import "fmt"

// 宠物仙域模块与动作 ID (Mod_PetImmortalDomain_Base, Module 755 / 0x02F3)。
const (
	ModulePetImmortalDomain uint16 = 755

	ActionIDPetImmortalGetState             uint32 = 0x02F30000 // 755/0 查询开放状态
	ActionIDPetImmortalSectDomainList       uint32 = 0x02F30001 // 755/1 宗门仙域列表
	ActionIDPetImmortalSectDomainTravelInfo uint32 = 0x02F30002 // 755/2 单个宗门仙域游历槽位详情
	ActionIDPetImmortalShareDomainInfo      uint32 = 0x02F30003 // 755/3 共享仙域详情
	ActionIDPetImmortalPetTravelInfo        uint32 = 0x02F3000D // 755/13 单只宠物游历收益详情
	ActionIDPetImmortalMyPetList            uint32 = 0x02F3000E // 755/14 我的宠物游历状态列表
	ActionIDPetImmortalPetTravel            uint32 = 0x02F3000F // 755/15 派遣宠物游历
	ActionIDPetImmortalRecallPet            uint32 = 0x02F30010 // 755/16 召回游历宠物
)

// 宠物仙域状态与结果码 (Mod_PetImmortalDomain_Base)。
const (
	PetImmortalOpen               uint8 = 0
	PetImmortalClose              uint8 = 1
	PetImmortalSuccess            uint8 = 2
	PetImmortalFailed             uint8 = 3
	PetImmortalSectNotInvolved    uint8 = 4
	PetImmortalFunNotOpen         uint8 = 5
	PetImmortalNoSect             uint8 = 6
	PetImmortalDidError           uint8 = 7
	PetImmortalNotMyDomain        uint8 = 8
	PetImmortalCDTime             uint8 = 9
	PetImmortalWaring             uint8 = 13
	PetImmortalPosIDError         uint8 = 19
	PetImmortalNoPetTravel        uint8 = 20
	PetImmortalNotMyPet           uint8 = 21
	PetImmortalPosNotEmpty        uint8 = 22
	PetImmortalDomainTravelLimit  uint8 = 23
	PetImmortalTravelTotalLimit   uint8 = 24
	PetImmortalTravelTimeLimit    uint8 = 25
	PetImmortalTraveling          uint8 = 26
	PetImmortalPetError           uint8 = 27
	PetImmortalNoPet              uint8 = 28
	PetImmortalNotMySpecialDomain uint8 = 29
	PetImmortalDomainSectChange   uint8 = 40
)

// PetImmortalDomainResultText 返回宠物仙域结果码的人类可读文本。
func PetImmortalDomainResultText(code uint8) string {
	switch code {
	case PetImmortalOpen:
		return "已开放"
	case PetImmortalClose:
		return "未开放"
	case PetImmortalSuccess:
		return "成功"
	case PetImmortalFailed:
		return "失败"
	case PetImmortalSectNotInvolved:
		return "宗门未参与"
	case PetImmortalFunNotOpen:
		return "功能未开启"
	case PetImmortalNoSect:
		return "没有宗门"
	case PetImmortalDidError:
		return "仙域ID错误"
	case PetImmortalNotMyDomain:
		return "非本宗门仙域"
	case PetImmortalCDTime:
		return "冷却中"
	case PetImmortalWaring:
		return "战斗中"
	case PetImmortalPosIDError:
		return "槽位ID错误"
	case PetImmortalNoPetTravel:
		return "无宠物游历"
	case PetImmortalNotMyPet:
		return "非本人宠物"
	case PetImmortalPosNotEmpty:
		return "位置已有宠物"
	case PetImmortalDomainTravelLimit:
		return "已有宠物在当前仙域游历"
	case PetImmortalTravelTotalLimit:
		return "同时游历的宠物已达2只上限"
	case PetImmortalTravelTimeLimit:
		return "宠物游历时间已达本周上限"
	case PetImmortalTraveling:
		return "宠物游历中"
	case PetImmortalPetError:
		return "宠物错误"
	case PetImmortalNoPet:
		return "未拥有该宠物"
	case PetImmortalNotMySpecialDomain:
		return "非本宗门特殊仙域"
	case PetImmortalDomainSectChange:
		return "仙域归属宗门已变更"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// BuildPetImmortalGetStatePacket 构造 755/0 查询开放状态封包。
func BuildPetImmortalGetStatePacket() *Packet {
	return NewPacket(ActionIDPetImmortalGetState, nil)
}

// BuildPetImmortalSectDomainListPacket 构造 755/1 宗门仙域列表查询封包。
func BuildPetImmortalSectDomainListPacket() *Packet {
	return NewPacket(ActionIDPetImmortalSectDomainList, nil)
}

// BuildPetImmortalSectDomainTravelInfoPacket 构造 755/2 单个宗门仙域游历槽位查询封包。
func BuildPetImmortalSectDomainTravelInfoPacket(domainID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(domainID)
	return NewPacket(ActionIDPetImmortalSectDomainTravelInfo, w.Bytes())
}

// BuildPetImmortalShareDomainInfoPacket 构造 755/3 共享仙域详情查询封包。
func BuildPetImmortalShareDomainInfoPacket() *Packet {
	return NewPacket(ActionIDPetImmortalShareDomainInfo, nil)
}

// BuildPetImmortalMyPetListPacket 构造 755/14 我的宠物游历状态列表查询封包。
func BuildPetImmortalMyPetListPacket() *Packet {
	return NewPacket(ActionIDPetImmortalMyPetList, nil)
}

// BuildPetImmortalPetTravelPacket 构造 755/15 派遣宠物游历封包。
// 共享仙域时 domainID=0, posID=0；宗门仙域时为 sect_domains[i].id 与空槽位 pos_id。
func BuildPetImmortalPetTravelPacket(domainID, posID, petID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(domainID)
	w.WriteInt32(posID)
	w.WriteInt32(petID)
	return NewPacket(ActionIDPetImmortalPetTravel, w.Bytes())
}

// BuildPetImmortalRecallPetPacket 构造 755/16 召回游历宠物封包。
func BuildPetImmortalRecallPetPacket(domainID, posID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(domainID)
	w.WriteInt32(posID)
	return NewPacket(ActionIDPetImmortalRecallPet, w.Bytes())
}

// PetImmortalSectDomain 单个宗门仙域摘要 (755/1 sect_domains 条目)。
type PetImmortalSectDomain struct {
	ID              int32
	DomainID        int32
	ProtectTS       int32
	SectID          int32
	AttackNum       int32
	DefenseNum      int32
	ScorePro        int32
	LuckPro         int32
	ExtraScorePro   int32
	ExtraLuckPro    int32
	IsTravel        bool
	NowTravelNum    int32
	SpecialTS       int32
	IsSectAlliance  int32
}

// PetImmortalSectDomainList 755/1 宗门仙域列表响应。
type PetImmortalSectDomainList struct {
	Result   uint8
	MySectID int32
	Domains  []PetImmortalSectDomain
}

// ParsePetImmortalSectDomainList 解析 755/1 响应载荷。
func ParsePetImmortalSectDomainList(payload []byte) (*PetImmortalSectDomainList, error) {
	if len(payload) < 1 {
		return nil, fmt.Errorf("protocol: 宠物仙域列表回包为空")
	}
	r := NewReader(payload)
	res, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}
	out := &PetImmortalSectDomainList{Result: res}
	if res != PetImmortalSuccess {
		return out, nil
	}
	if out.MySectID, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	domCnt, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	out.Domains = make([]PetImmortalSectDomain, 0, domCnt)
	for i := 0; i < int(domCnt); i++ {
		var d PetImmortalSectDomain
		if d.ID, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		if d.DomainID, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		if d.ProtectTS, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		if d.SectID, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		if d.AttackNum, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		if d.DefenseNum, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		if d.ScorePro, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		if d.LuckPro, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		if d.ExtraScorePro, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		if d.ExtraLuckPro, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		isTravel, err := r.ReadUint8()
		if err != nil {
			return nil, err
		}
		d.IsTravel = isTravel != 0
		if d.NowTravelNum, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		if d.SpecialTS, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		if d.IsSectAlliance, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		out.Domains = append(out.Domains, d)
	}
	return out, nil
}

// PetImmortalDomainPet 单个宗门仙域槽位上的宠物信息 (755/2 pets 条目)。
type PetImmortalDomainPet struct {
	PetID      int32
	ProtectTS  int32
	TravelTS   int32
	Nickname   string
	ServerName string
	AppID      int32
	SectID     int32
	PosID      int32
	WarPower   int64
	SectName   string
}

// PetImmortalSectDomainTravelInfo 755/2 宗门仙域游历槽位详情响应。
type PetImmortalSectDomainTravelInfo struct {
	Result             uint8
	MyAppID            int32
	NowSectID          int32
	Pets               []PetImmortalDomainPet
	CanTravelOtherSect bool
	MySectID           int32
	ExpelLowerTS       int32
	ExpelLowerLv       int32
	SpecialTS          int32
	IsSectAlliance     int32
	PosNum             int32
	AllianceRank       int32
}

// ParsePetImmortalSectDomainTravelInfo 解析 755/2 响应载荷。
func ParsePetImmortalSectDomainTravelInfo(payload []byte) (*PetImmortalSectDomainTravelInfo, error) {
	if len(payload) < 1 {
		return nil, fmt.Errorf("protocol: 宠物仙域槽位详情回包为空")
	}
	r := NewReader(payload)
	res, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}
	out := &PetImmortalSectDomainTravelInfo{Result: res}
	if res != PetImmortalSuccess {
		return out, nil
	}
	if out.MyAppID, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	if out.NowSectID, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	petCnt, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	out.Pets = make([]PetImmortalDomainPet, 0, petCnt)
	for i := 0; i < int(petCnt); i++ {
		var p PetImmortalDomainPet
		if p.PetID, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		if p.ProtectTS, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		if p.TravelTS, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		if p.Nickname, err = r.ReadString(); err != nil {
			return nil, err
		}
		if p.ServerName, err = r.ReadString(); err != nil {
			return nil, err
		}
		if p.AppID, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		if p.SectID, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		if p.PosID, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		if p.WarPower, err = r.ReadInt64(); err != nil {
			return nil, err
		}
		if p.SectName, err = r.ReadString(); err != nil {
			return nil, err
		}
		out.Pets = append(out.Pets, p)
	}
	canOther, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}
	out.CanTravelOtherSect = canOther != 0
	if out.MySectID, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	if out.ExpelLowerTS, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	if out.ExpelLowerLv, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	if out.SpecialTS, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	if out.IsSectAlliance, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	if out.PosNum, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	if out.AllianceRank, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	return out, nil
}

// PetImmortalMyPet 我的单只宠物游历状态 (755/14 条目)。
type PetImmortalMyPet struct {
	PetID     int32
	TravelTS  int32 // 本周已游历秒数
	IsTravel  bool  // 当前是否游历中
	LuckTimes int32
	LuckLimit int32
	IsHave    bool // 是否拥有该宠物
}

// PetImmortalMyPetList 755/14 我的宠物游历状态列表响应。
type PetImmortalMyPetList struct {
	Pets     []PetImmortalMyPet
	IsUnread bool
}

// ParsePetImmortalMyPetList 解析 755/14 响应载荷。
func ParsePetImmortalMyPetList(payload []byte) (*PetImmortalMyPetList, error) {
	if len(payload) < 3 {
		return nil, fmt.Errorf("protocol: 宠物游历列表回包过短 (%d bytes)", len(payload))
	}
	r := NewReader(payload)
	cnt, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	out := &PetImmortalMyPetList{
		Pets: make([]PetImmortalMyPet, 0, cnt),
	}
	for i := 0; i < int(cnt); i++ {
		var p PetImmortalMyPet
		if p.PetID, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		if p.TravelTS, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		isTravel, err := r.ReadUint8()
		if err != nil {
			return nil, err
		}
		p.IsTravel = isTravel != 0
		if p.LuckTimes, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		if p.LuckLimit, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		isHave, err := r.ReadUint32()
		if err != nil {
			return nil, err
		}
		p.IsHave = isHave != 0
		out.Pets = append(out.Pets, p)
	}
	unread, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}
	out.IsUnread = unread != 0
	return out, nil
}
