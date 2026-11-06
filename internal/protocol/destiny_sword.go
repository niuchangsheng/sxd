// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方「飞剑」协议 (Mod_DestinySword_Base, Module 842 / 0x034A)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_DestinySword_Base 与
// com.datas.DestinySwordData：
//   - panel_info           Act 1  -> 0x034A0001  查询角色飞剑与资源
//   - sword_panel_info     Act 2  -> 0x034A0002  查询全部飞剑与剑意
//   - one_click_temper_panel Act 15 -> 0x034A000F 查询一键淬炼消耗
//   - one_click_temper     Act 16 -> 0x034A0010 执行一键淬炼
package protocol

import "fmt"

const (
	// ModuleDestinySword 飞剑模块编号 (0x034A)
	ModuleDestinySword uint16 = 842
)

// 飞剑动作编号 (Mod_DestinySword_Base)。
const (
	// ActionIDDestinySwordPanel 查询飞剑面板 (Action 1 -> 0x034A0001)
	ActionIDDestinySwordPanel uint32 = 0x034A0001
	// ActionIDDestinySwordSwordPanel 查询全部飞剑 (Action 2 -> 0x034A0002)
	ActionIDDestinySwordSwordPanel uint32 = 0x034A0002
	// ActionIDDestinySwordOneClickTemperPanel 查询一键淬炼消耗 (Action 15 -> 0x034A000F)
	ActionIDDestinySwordOneClickTemperPanel uint32 = 0x034A000F
	// ActionIDDestinySwordOneClickTemper 执行一键淬炼 (Action 16 -> 0x034A0010)
	ActionIDDestinySwordOneClickTemper uint32 = 0x034A0010
)

// 飞剑结果码 (Mod_DestinySword_Base)。
const (
	// DestinySwordResultSuccess 成功
	DestinySwordResultSuccess uint8 = 0
	// DestinySwordResultFuncNotOpen 功能未开放
	DestinySwordResultFuncNotOpen uint8 = 1
	// DestinySwordResultWrongID 编号错误
	DestinySwordResultWrongID uint8 = 2
	// DestinySwordResultNotGet 未获得
	DestinySwordResultNotGet uint8 = 3
	// DestinySwordResultNoIntent 没有剑意
	DestinySwordResultNoIntent uint8 = 11
	// DestinySwordResultNotEnoughItem 道具不足
	DestinySwordResultNotEnoughItem uint8 = 10
	// DestinySwordResultNotEnoughNum 数量不足
	DestinySwordResultNotEnoughNum uint8 = 16
	// DestinySwordResultMaxIntentLevel 剑意已达最高级
	DestinySwordResultMaxIntentLevel uint8 = 17
	// DestinySwordResultNotEnoughIntent 剑意不足
	DestinySwordResultNotEnoughIntent uint8 = 20
	// DestinySwordResultNotEnoughLevel 等级不足
	DestinySwordResultNotEnoughLevel uint8 = 22
)

// DestinySwordIntent 表示装备在飞剑上的一条剑意。
type DestinySwordIntent struct {
	// PlayerIntentID 玩家剑意实例 ID
	PlayerIntentID int32
	// IntentID 剑意类型 ID
	IntentID int32
	// IntentLevel 剑意等级
	IntentLevel int32
}

// DestinySwordEquip 表示一把飞剑。
type DestinySwordEquip struct {
	// SwordID 飞剑编号
	SwordID int32
	// IsUnlock 是否解锁
	IsUnlock int32
	// Level 飞剑等级
	Level int32
	// Intents 已装备剑意
	Intents []DestinySwordIntent
	// RoleID 装备角色编号
	RoleID int32
	// NodeLevel 神通节点等级
	NodeLevel int32
	// SupernaturalRoleID 神通角色编号
	SupernaturalRoleID int32
}

// DestinySwordRole 表示一个装备飞剑的角色。
type DestinySwordRole struct {
	// PlayerRoleID 玩家角色实例 ID
	PlayerRoleID int32
	// IsMainRole 是否主角
	IsMainRole uint8
	// RoleID 角色编号
	RoleID int32
	// RoleName 角色名
	RoleName string
	// RoleLevel 角色等级
	RoleLevel int32
	// IsDeploy 是否上阵
	IsDeploy uint8
	// EquipList 已装备飞剑
	EquipList []DestinySwordEquip
}

// DestinySwordPanel 表示飞剑主面板。
type DestinySwordPanel struct {
	// Roles 角色与飞剑列表
	Roles []DestinySwordRole
	// NowDeployType 当前出战类型
	NowDeployType int32
	// ItemNum 飞剑打造材料数量
	ItemNum int32
	// JianyiScrap 剑意碎片数量
	JianyiScrap int32
}

// DestinySwordTemperCost 表示一键淬炼的单档消耗。
type DestinySwordTemperCost struct {
	// IntentLevel 剑意等级
	IntentLevel int32
	// CostNum 消耗数量
	CostNum int32
}

// DestinySwordTemperPanel 表示一键淬炼消耗面板。
type DestinySwordTemperPanel struct {
	// Result 结果码
	Result uint8
	// Costs 各等级剑意消耗
	Costs []DestinySwordTemperCost
	// NeedLevel1Num 还需要的 1 级剑意数量
	NeedLevel1Num int32
	// NeedTotalLevel1Num 总共需要的 1 级剑意数量
	NeedTotalLevel1Num int32
	// NeedJianyiScrapNum 需要剑意碎片数量
	NeedJianyiScrapNum int32
}

// BuildDestinySwordPanelPacket 构造查询飞剑面板封包 (0x034A0001)。
func BuildDestinySwordPanelPacket() *Packet {
	return NewPacket(ActionIDDestinySwordPanel, nil)
}

// BuildDestinySwordSwordPanelPacket 构造查询全部飞剑封包 (0x034A0002)。
func BuildDestinySwordSwordPanelPacket() *Packet {
	return NewPacket(ActionIDDestinySwordSwordPanel, nil)
}

// BuildDestinySwordOneClickTemperPanelPacket 构造查询一键淬炼消耗封包 (0x034A000F)。
func BuildDestinySwordOneClickTemperPanelPacket(playerIntentID, upToLevel int32) *Packet {
	w := NewWriter()
	w.WriteInt32(playerIntentID)
	w.WriteInt32(upToLevel)
	return NewPacket(ActionIDDestinySwordOneClickTemperPanel, w.Bytes())
}

// BuildDestinySwordOneClickTemperPacket 构造执行一键淬炼封包 (0x034A0010)。
func BuildDestinySwordOneClickTemperPacket(playerIntentID, upToLevel int32) *Packet {
	w := NewWriter()
	w.WriteInt32(playerIntentID)
	w.WriteInt32(upToLevel)
	return NewPacket(ActionIDDestinySwordOneClickTemper, w.Bytes())
}

// ParseDestinySwordPanelResult 解析飞剑主面板。
func ParseDestinySwordPanelResult(payload []byte) (*DestinySwordPanel, error) {
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read sword role count: %v", ErrInvalidPayload, err)
	}
	panel := &DestinySwordPanel{Roles: make([]DestinySwordRole, 0, count)}
	for i := 0; i < int(count); i++ {
		var role DestinySwordRole
		if err = readDestinySwordRole(r, &role); err != nil {
			return nil, fmt.Errorf("%w: role[%d]: %v", ErrInvalidPayload, i, err)
		}
		panel.Roles = append(panel.Roles, role)
	}
	if panel.NowDeployType, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read now_deploy_type: %v", ErrInvalidPayload, err)
	}
	if panel.ItemNum, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read item_num: %v", ErrInvalidPayload, err)
	}
	if panel.JianyiScrap, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read jianyi_scrap: %v", ErrInvalidPayload, err)
	}
	return panel, nil
}

func readDestinySwordRole(r *Reader, role *DestinySwordRole) error {
	var err error
	if role.PlayerRoleID, err = r.ReadInt32(); err != nil {
		return err
	}
	if role.IsMainRole, err = r.ReadUint8(); err != nil {
		return err
	}
	if role.RoleID, err = r.ReadInt32(); err != nil {
		return err
	}
	if role.RoleName, err = r.ReadString(); err != nil {
		return err
	}
	if role.RoleLevel, err = r.ReadInt32(); err != nil {
		return err
	}
	if role.IsDeploy, err = r.ReadUint8(); err != nil {
		return err
	}
	count, err := r.ReadUint16()
	if err != nil {
		return err
	}
	role.EquipList = make([]DestinySwordEquip, 0, count)
	for i := 0; i < int(count); i++ {
		var sword DestinySwordEquip
		if err := readDestinySwordEquip(r, &sword); err != nil {
			return err
		}
		role.EquipList = append(role.EquipList, sword)
	}
	return nil
}

func readDestinySwordEquip(r *Reader, sword *DestinySwordEquip) error {
	var err error
	if sword.SwordID, err = r.ReadInt32(); err != nil {
		return err
	}
	if sword.IsUnlock, err = r.ReadInt32(); err != nil {
		return err
	}
	if sword.Level, err = r.ReadInt32(); err != nil {
		return err
	}
	intentCount, err := r.ReadUint16()
	if err != nil {
		return err
	}
	sword.Intents = make([]DestinySwordIntent, 0, intentCount)
	for i := 0; i < int(intentCount); i++ {
		var intent DestinySwordIntent
		if intent.PlayerIntentID, err = r.ReadInt32(); err != nil {
			return err
		}
		if intent.IntentID, err = r.ReadInt32(); err != nil {
			return err
		}
		if intent.IntentLevel, err = r.ReadInt32(); err != nil {
			return err
		}
		sword.Intents = append(sword.Intents, intent)
	}
	attrCount, err := r.ReadUint16()
	if err != nil {
		return err
	}
	for i := 0; i < int(attrCount); i++ {
		if _, err = r.ReadInt32(); err != nil {
			return err
		}
		if _, err = r.ReadInt32(); err != nil {
			return err
		}
	}
	if sword.RoleID, err = r.ReadInt32(); err != nil {
		return err
	}
	if sword.NodeLevel, err = r.ReadInt32(); err != nil {
		return err
	}
	if sword.SupernaturalRoleID, err = r.ReadInt32(); err != nil {
		return err
	}
	return nil
}

// ParseDestinySwordSwordPanelResult 解析全部飞剑列表。
func ParseDestinySwordSwordPanelResult(payload []byte) ([]DestinySwordEquip, error) {
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read sword count: %v", ErrInvalidPayload, err)
	}
	swords := make([]DestinySwordEquip, 0, count)
	for i := 0; i < int(count); i++ {
		var sword DestinySwordEquip
		if err := readDestinySwordEquip(r, &sword); err != nil {
			return nil, fmt.Errorf("%w: sword[%d]: %v", ErrInvalidPayload, i, err)
		}
		swords = append(swords, sword)
	}
	return swords, nil
}

// ParseDestinySwordTemperPanelResult 解析一键淬炼消耗面板。
func ParseDestinySwordTemperPanelResult(payload []byte) (*DestinySwordTemperPanel, error) {
	r := NewReader(payload)
	res := &DestinySwordTemperPanel{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read temper panel result: %v", ErrInvalidPayload, err)
	}
	if res.Result != DestinySwordResultSuccess {
		return res, nil
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read temper cost count: %v", ErrInvalidPayload, err)
	}
	res.Costs = make([]DestinySwordTemperCost, 0, count)
	for i := 0; i < int(count); i++ {
		var c DestinySwordTemperCost
		if c.IntentLevel, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read cost[%d].level: %v", ErrInvalidPayload, i, err)
		}
		if c.CostNum, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read cost[%d].num: %v", ErrInvalidPayload, i, err)
		}
		res.Costs = append(res.Costs, c)
	}
	if res.NeedLevel1Num, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read need_level_1_num: %v", ErrInvalidPayload, err)
	}
	if res.NeedTotalLevel1Num, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read need_total_level_1_num: %v", ErrInvalidPayload, err)
	}
	if res.NeedJianyiScrapNum, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read need_jianyi_scrap_num: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// ParseDestinySwordActionCode 解析单结果码动作 (one_click_temper)。
func ParseDestinySwordActionCode(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty destiny sword action result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// BuildDestinySwordPanelResultPacket 构造飞剑面板回包，供离线 mock 与单测使用。
func BuildDestinySwordPanelResultPacket(p DestinySwordPanel) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(p.Roles)))
	for _, role := range p.Roles {
		w.WriteInt32(role.PlayerRoleID)
		w.WriteUint8(role.IsMainRole)
		w.WriteInt32(role.RoleID)
		w.WriteString(role.RoleName)
		w.WriteInt32(role.RoleLevel)
		w.WriteUint8(role.IsDeploy)
		w.WriteUint16(uint16(len(role.EquipList)))
		for _, sword := range role.EquipList {
			writeDestinySwordEquip(w, sword)
		}
	}
	w.WriteInt32(p.NowDeployType)
	w.WriteInt32(p.ItemNum)
	w.WriteInt32(p.JianyiScrap)
	return NewPacket(ActionIDDestinySwordPanel, w.Bytes())
}

func writeDestinySwordEquip(w *Writer, sword DestinySwordEquip) {
	w.WriteInt32(sword.SwordID)
	w.WriteInt32(sword.IsUnlock)
	w.WriteInt32(sword.Level)
	w.WriteUint16(uint16(len(sword.Intents)))
	for _, intent := range sword.Intents {
		w.WriteInt32(intent.PlayerIntentID)
		w.WriteInt32(intent.IntentID)
		w.WriteInt32(intent.IntentLevel)
	}
	w.WriteUint16(0) // attr_list
	w.WriteInt32(sword.RoleID)
	w.WriteInt32(sword.NodeLevel)
	w.WriteInt32(sword.SupernaturalRoleID)
}

// BuildDestinySwordTemperPanelResultPacket 构造淬炼面板回包，供离线 mock 与单测使用。
func BuildDestinySwordTemperPanelResultPacket(p DestinySwordTemperPanel) *Packet {
	w := NewWriter()
	w.WriteUint8(p.Result)
	if p.Result == DestinySwordResultSuccess {
		w.WriteUint16(uint16(len(p.Costs)))
		for _, c := range p.Costs {
			w.WriteInt32(c.IntentLevel)
			w.WriteInt32(c.CostNum)
		}
		w.WriteInt32(p.NeedLevel1Num)
		w.WriteInt32(p.NeedTotalLevel1Num)
		w.WriteInt32(p.NeedJianyiScrapNum)
	}
	return NewPacket(ActionIDDestinySwordOneClickTemperPanel, w.Bytes())
}

// BuildDestinySwordActionCodePacket 构造单结果码回包，供离线 mock 与单测使用。
func BuildDestinySwordActionCodePacket(actionID uint32, code uint8) *Packet {
	return NewPacket(actionID, []byte{code})
}
