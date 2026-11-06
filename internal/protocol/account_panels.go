// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现对账号养成进度探测所需的只读面板编解码：
// 法阵 (486)、龙珠 (111)、血脉 (116)、藏经阁 (85)、丹魂 (347)、
// 圣器 (280)、兵法 (299)、混沌虚空 (124)。
package protocol

import "fmt"

// ---- 通用读取辅助 ----

func readInt32s(r *Reader, n int) ([]int32, error) {
	values := make([]int32, 0, n)
	for i := 0; i < n; i++ {
		v, err := r.ReadInt32()
		if err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, nil
}

func skipInt32s(r *Reader, n int) error {
	for i := 0; i < n; i++ {
		if _, err := r.ReadInt32(); err != nil {
			return err
		}
	}
	return nil
}

// ---- 法阵 (Mod_MagicArray_Base, Module 486) ----

// MagicArrayCostItem 法阵注灵材料。
type MagicArrayCostItem struct {
	ItemID  int32
	ItemNum int32
	AddExp  int32
}

// MagicArrayPanel 法阵面板关键进度。
type MagicArrayPanel struct {
	Level     int32
	Exp       int32
	MaxExp    int32 // 升到下一级所需总经验
	CostItems []MagicArrayCostItem
}

// BuildMagicArrayPanelPacket 构造法阵主面板查询封包 (Module 486, Action 1)。
func BuildMagicArrayPanelPacket(prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(0)
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(0x01E60001, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// ParseMagicArrayPanelResponse 解析法阵主面板 (Module 486, Action 1)。
func ParseMagicArrayPanelResponse(payload []byte) (*MagicArrayPanel, error) {
	r := NewReader(payload)
	if err := skipInt32s(r, 2); err != nil { // total_step, add_rate
		return nil, fmt.Errorf("read magic array header: %w", err)
	}

	skillCount, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	if err := skipInt32s(r, int(skillCount)); err != nil {
		return nil, err
	}

	arrayCount, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	for i := 0; i < int(arrayCount); i++ {
		if err := skipInt32s(r, 2); err != nil { // array_id, is_unlock
			return nil, err
		}
		gridCount, err := r.ReadUint16()
		if err != nil {
			return nil, err
		}
		for j := 0; j < int(gridCount); j++ {
			if err := skipInt32s(r, 5); err != nil { // grid_id + 4 attr fields
				return nil, err
			}
			if _, err := r.ReadString(); err != nil { // puppet/role name
				return nil, err
			}
			if err := skipInt32s(r, 3); err != nil {
				return nil, err
			}
		}
		if err := skipInt32s(r, 3); err != nil { // have_chips, need_chips, slot_id
			return nil, err
		}
	}

	panel := &MagicArrayPanel{}
	if panel.Level, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	if panel.Exp, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	if panel.MaxExp, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	if _, err = r.ReadInt32(); err != nil { // critical
		return nil, err
	}

	attrCount, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	if err := skipInt32s(r, int(attrCount)*3); err != nil {
		return nil, err
	}

	costCount, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	for i := 0; i < int(costCount); i++ {
		values, err := readInt32s(r, 4) // item_id, item_num, add_exp, extra
		if err != nil {
			return nil, err
		}
		panel.CostItems = append(panel.CostItems, MagicArrayCostItem{
			ItemID: values[0], ItemNum: values[1], AddExp: values[2],
		})
	}
	return panel, nil
}

// ---- 龙珠 (Mod_Dragonball_Base, Module 111) ----

// DragonBall 单颗龙珠进度。
type DragonBall struct {
	ItemID int16
	Level  uint8
	Exp    int32
	Grade  int32
}

// DragonBallPanel 龙珠面板关键进度。
type DragonBallPanel struct {
	Balls         []DragonBall
	UnlockGrid    uint8
	LongZhuScrap  int32
	ShenLongScrap int32
}

// BuildDragonBallListPacket 构造龙珠列表查询封包 (Module 111, Action 3)。
func BuildDragonBallListPacket(prevAct uint32) *Packet {
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w := NewWriter()
	w.WriteUint32(prevAct)
	pkt := NewPacket(0x006F0003, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// ParseDragonBallListResponse 解析龙珠列表 (Module 111, Action 3)。
func ParseDragonBallListResponse(payload []byte) (*DragonBallPanel, error) {
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	panel := &DragonBallPanel{Balls: make([]DragonBall, 0, count)}
	for i := 0; i < int(count); i++ {
		if _, err := r.ReadInt32(); err != nil { // player_dragonball_id
			return nil, err
		}
		itemID, err := r.ReadInt16()
		if err != nil {
			return nil, err
		}
		level, err := r.ReadUint8()
		if err != nil {
			return nil, err
		}
		if _, err := r.ReadUint8(); err != nil { // read_level
			return nil, err
		}
		exp, err := r.ReadInt32()
		if err != nil {
			return nil, err
		}
		if _, err := r.ReadBytes(4); err != nil { // merge counts
			return nil, err
		}
		if _, err := r.ReadInt32(); err != nil { // is_locked
			return nil, err
		}
		grade, err := r.ReadInt32()
		if err != nil {
			return nil, err
		}
		if _, err := r.ReadInt16(); err != nil { // position
			return nil, err
		}
		panel.Balls = append(panel.Balls, DragonBall{ItemID: itemID, Level: level, Exp: exp, Grade: grade})
	}
	if panel.UnlockGrid, err = r.ReadUint8(); err != nil {
		return nil, err
	}
	if panel.LongZhuScrap, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	if panel.ShenLongScrap, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	_ = skipInt32s(r, 4) // costs / ingot / package count
	return panel, nil
}

// ---- 血脉 (Mod_Bloodline_Base, Module 116) ----

// Bloodline 单条血脉进度。
type Bloodline struct {
	ID            int32
	Level         int32
	Exp           int32
	MainRoleLevel int32
}

// BloodlinePanel 血脉面板关键进度。
type BloodlinePanel struct {
	Bloodlines []Bloodline
}

// BuildBloodlineInfoPacket 构造血脉面板查询封包 (Module 116, Action 0)。
func BuildBloodlineInfoPacket(prevAct uint32) *Packet {
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w := NewWriter()
	w.WriteUint32(prevAct)
	pkt := NewPacket(0x00740000, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// ParseBloodlineInfoResponse 解析血脉面板 (Module 116, Action 0)。
func ParseBloodlineInfoResponse(payload []byte) (*BloodlinePanel, error) {
	r := NewReader(payload)
	if _, err := r.ReadUint8(); err != nil { // isWakeUp
		return nil, err
	}
	roleCount, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	for i := 0; i < int(roleCount); i++ {
		if err := skipInt32s(r, 3); err != nil { // playerRoleId, roleId, level
			return nil, err
		}
		if _, err := r.ReadUint8(); err != nil {
			return nil, err
		}
		if _, err := r.ReadInt16(); err != nil {
			return nil, err
		}
		if _, err := r.ReadUint8(); err != nil {
			return nil, err
		}
	}
	if err := skipInt32s(r, 2); err != nil { // min, cream
		return nil, err
	}
	bloodCount, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	panel := &BloodlinePanel{Bloodlines: make([]Bloodline, 0, bloodCount)}
	for i := 0; i < int(bloodCount); i++ {
		id, err := r.ReadInt32()
		if err != nil {
			return nil, err
		}
		level, err := r.ReadUint8()
		if err != nil {
			return nil, err
		}
		values, err := readInt32s(r, 5) // exp, potential, equip_role, qingLongPoint, qingLongRate
		if err != nil {
			return nil, err
		}
		if _, err := r.ReadUint8(); err != nil { // qingLongLock
			return nil, err
		}
		if err := skipInt32s(r, 2); err != nil { // baiHuPoint, baiHuRate
			return nil, err
		}
		if _, err := r.ReadUint8(); err != nil {
			return nil, err
		}
		if err := skipInt32s(r, 2); err != nil { // zhuQue
			return nil, err
		}
		if _, err := r.ReadUint8(); err != nil {
			return nil, err
		}
		if err := skipInt32s(r, 2); err != nil { // xuanWu
			return nil, err
		}
		if _, err := r.ReadUint8(); err != nil {
			return nil, err
		}
		rank, err := r.ReadInt32()
		if err != nil {
			return nil, err
		}
		if _, err := r.ReadBytes(3); err != nil { // isActivity, isLevelActivity, hasLimit
			return nil, err
		}
		mainRoleLevel, err := readBloodlineTail(r)
		if err != nil {
			return nil, err
		}
		_ = rank
		panel.Bloodlines = append(panel.Bloodlines, Bloodline{
			ID: id, Level: int32(level), Exp: values[0], MainRoleLevel: mainRoleLevel,
		})
	}
	// 主角色血脉等级位于每条记录尾部；解析函数已消费，这里从记录原始字段补读。
	return panel, nil
}

// readBloodlineTail 消费血脉记录剩余字段并返回 main_role_bloodline_level:
// qingRate2, qingLock2, baiRate2, baiLock2, zhuRate2, zhuLock2, xuanRate2, xuanLock2,
// main_role_level, twoStageLv, twoStageSkill, thirdLater
func readBloodlineTail(r *Reader) (int32, error) {
	for i := 0; i < 4; i++ {
		if _, err := r.ReadInt32(); err != nil {
			return 0, err
		}
		if _, err := r.ReadUint8(); err != nil {
			return 0, err
		}
	}
	values, err := readInt32s(r, 3)
	if err != nil {
		return 0, err
	}
	if _, err := r.ReadUint8(); err != nil {
		return 0, err
	}
	return values[0], nil
}

// ---- 藏经阁 (Mod_Library_Base, Module 85) ----

// LibraryJob 单个职业的藏经阁等级。
type LibraryJob struct {
	JobID uint8
	Level int16
}

// LibraryPanel 藏经阁面板关键进度。
type LibraryPanel struct {
	Jobs []LibraryJob
}

// BuildLibraryInfoPacket 构造藏经阁面板查询封包 (Module 85, Action 1)。
func BuildLibraryInfoPacket(prevAct uint32) *Packet {
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w := NewWriter()
	w.WriteUint32(prevAct)
	pkt := NewPacket(0x00550001, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// ParseLibraryInfoResponse 解析藏经阁面板 (Module 85, Action 1)。
func ParseLibraryInfoResponse(payload []byte) (*LibraryPanel, error) {
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	panel := &LibraryPanel{Jobs: make([]LibraryJob, 0, count)}
	for i := 0; i < int(count); i++ {
		jobID, err := r.ReadUint8()
		if err != nil {
			return nil, err
		}
		level, err := r.ReadInt16()
		if err != nil {
			return nil, err
		}
		panel.Jobs = append(panel.Jobs, LibraryJob{JobID: jobID, Level: level})
	}
	return panel, nil
}

// ---- 丹魂 (Mod_ElixirSoul_Base, Module 347) ----

// ElixirSoulSlot 单个丹魂位。
type ElixirSoulSlot struct {
	ID    uint8
	Level uint8
}

// ElixirSoulPanel 丹魂面板关键进度。
type ElixirSoulPanel struct {
	Slots        []ElixirSoulSlot
	NormalElixir int16
	HighElixir   int16
	SkillLevel   uint8
	StatePoint   int32
}

// BuildElixirSoulRoleInfoPacket 构造丹魂角色面板查询封包 (Module 347, Action 0)。
func BuildElixirSoulRoleInfoPacket(playerRoleID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(playerRoleID)
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(0x015B0000, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// ParseElixirSoulRoleInfoResponse 解析丹魂角色面板 (Module 347, Action 0)。
func ParseElixirSoulRoleInfoResponse(payload []byte) (*ElixirSoulPanel, error) {
	r := NewReader(payload)
	if _, err := r.ReadUint8(); err != nil { // result
		return nil, err
	}
	if _, err := r.ReadInt32(); err != nil { // player_role_id
		return nil, err
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	panel := &ElixirSoulPanel{Slots: make([]ElixirSoulSlot, 0, count)}
	for i := 0; i < int(count); i++ {
		id, err := r.ReadUint8()
		if err != nil {
			return nil, err
		}
		level, err := r.ReadUint8()
		if err != nil {
			return nil, err
		}
		panel.Slots = append(panel.Slots, ElixirSoulSlot{ID: id, Level: level})
	}
	if panel.NormalElixir, err = r.ReadInt16(); err != nil {
		return nil, err
	}
	if panel.HighElixir, err = r.ReadInt16(); err != nil {
		return nil, err
	}
	if panel.SkillLevel, err = r.ReadUint8(); err != nil {
		return nil, err
	}
	if panel.StatePoint, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	return panel, nil
}

// ---- 圣器 (Mod_SantWeapon_Base, Module 280) ----

// SantWeapon 单件圣器进度。
type SantWeapon struct {
	ItemID    int32
	Level     int32
	StarLevel int32
	LingCai   int
}

// SantWeaponRole 单个角色的圣器配置。
type SantWeaponRole struct {
	PlayerRoleID int32
	RoleID       int32
	Level        int32
	Weapons      []SantWeapon
}

// SantWeaponPanel 圣器面板关键进度。
type SantWeaponPanel struct {
	Roles   []SantWeaponRole
	XuanTie int32
	JingHua int32
}

// BuildSantWeaponInfoPacket 构造圣器面板查询封包 (Module 280, Action 0)。
func BuildSantWeaponInfoPacket(prevAct uint32) *Packet {
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w := NewWriter()
	w.WriteUint32(prevAct)
	pkt := NewPacket(0x01180000, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

func parseSantWeapons(r *Reader) ([]SantWeapon, error) {
	count, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	weapons := make([]SantWeapon, 0, count)
	for i := 0; i < int(count); i++ {
		if _, err := r.ReadInt32(); err != nil { // player_sant_weapon_id
			return nil, err
		}
		itemID, err := r.ReadInt32()
		if err != nil {
			return nil, err
		}
		level, err := r.ReadInt32()
		if err != nil {
			return nil, err
		}
		if err := skipInt32s(r, 3); err != nil { // player_role_id, grid_id, break_rate
			return nil, err
		}
		star, err := r.ReadInt32()
		if err != nil {
			return nil, err
		}
		lingCount, err := r.ReadUint16()
		if err != nil {
			return nil, err
		}
		for j := 0; j < int(lingCount); j++ {
			if _, err := r.ReadUint8(); err != nil {
				return nil, err
			}
			if _, err := r.ReadInt32(); err != nil {
				return nil, err
			}
		}
		weapons = append(weapons, SantWeapon{ItemID: itemID, Level: level, StarLevel: star, LingCai: int(lingCount)})
	}
	return weapons, nil
}

// ParseSantWeaponInfoResponse 解析圣器面板 (Module 280, Action 0)。
func ParseSantWeaponInfoResponse(payload []byte) (*SantWeaponPanel, error) {
	r := NewReader(payload)
	roleCount, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	panel := &SantWeaponPanel{Roles: make([]SantWeaponRole, 0, roleCount)}
	for i := 0; i < int(roleCount); i++ {
		playerRoleID, err := r.ReadInt32()
		if err != nil {
			return nil, err
		}
		roleID, err := r.ReadInt32()
		if err != nil {
			return nil, err
		}
		level, err := r.ReadInt32()
		if err != nil {
			return nil, err
		}
		if _, err := r.ReadInt32(); err != nil { // suit_equip_id
			return nil, err
		}
		weapons, err := parseSantWeapons(r)
		if err != nil {
			return nil, err
		}
		panel.Roles = append(panel.Roles, SantWeaponRole{
			PlayerRoleID: playerRoleID, RoleID: roleID, Level: level, Weapons: weapons,
		})
	}
	if _, err := parseSantWeapons(r); err != nil { // package list
		return nil, err
	}
	if _, err := r.ReadUint8(); err != nil { // unlock_part
		return nil, err
	}
	partCount, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	for i := 0; i < int(partCount); i++ {
		if _, err := r.ReadBytes(2); err != nil {
			return nil, err
		}
	}
	if panel.XuanTie, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	if panel.JingHua, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	return panel, nil
}

// ---- 兵法 (Mod_Tactics_Base, Module 299) ----

// TacticsPanel 兵法面板关键进度。
type TacticsPanel struct {
	Level        int32
	Exp          int32
	NextLevelExp int32
}

// tacticsWantExp 兵法升到「当前等级+1」所需总经验 (对齐 TacticsTypeData.tacticsLevelList)。
var tacticsWantExp = []int32{
	1500, 3000, 4500, 6000, 8000, 10000, 12000, 14000, 16000, 18000, 20000, 23000,
	26000, 29000, 32000, 35000, 40000, 45000, 50000, 55000, 60000, 65000, 70000, 75000,
	80000, 88000, 96000, 104000, 112000, 120000, 128000, 136000, 144000, 152000, 160000, 168000,
	176000, 184000, 192000, 200000, 210000, 220000, 230000, 240000, 250000, 260000, 270000, 280000,
	290000, 300000, 310000, 320000, 330000, 340000, 350000, 360000, 370000, 380000, 390000, 400000,
	410000, 420000, 430000, 440000, 450000, 460000, 470000, 480000, 490000, 500000, 510000, 520000,
	530000, 540000, 550000, 560000, 570000, 580000, 590000, 600000, 615000, 630000, 645000, 660000,
	675000, 690000, 705000, 720000, 735000, 750000, 765000, 780000, 795000, 810000, 825000, 840000,
	855000, 870000, 885000, 900000, 920000, 940000, 960000, 980000, 1000000, 1020000, 1040000, 1060000,
	1080000, 1100000, 1120000, 1140000, 1160000, 1180000, 1200000, 1220000, 1240000, 1260000, 1280000, 0,
}

// BuildTacticsInfoPacket 构造兵法面板查询封包 (Module 299, Action 0)。
func BuildTacticsInfoPacket(prevAct uint32) *Packet {
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w := NewWriter()
	w.WriteUint32(prevAct)
	pkt := NewPacket(0x012B0000, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// ParseTacticsInfoResponse 解析兵法面板 (Module 299, Action 0)。
func ParseTacticsInfoResponse(payload []byte) (*TacticsPanel, error) {
	r := NewReader(payload)
	level, err := r.ReadInt32()
	if err != nil {
		return nil, err
	}
	exp, err := r.ReadInt32()
	if err != nil {
		return nil, err
	}
	panel := &TacticsPanel{Level: level, Exp: exp}
	if level >= 1 && int(level) <= len(tacticsWantExp) {
		panel.NextLevelExp = tacticsWantExp[level-1]
	}
	return panel, nil
}

// ---- 混沌虚空 (Mod_ChaosEquipment_Base, Module 124) ----

// ChaosMonster 混沌魔兽进度。
type ChaosMonster struct {
	ItemID    int16
	Potential uint8
	Break1    uint8
	Break2    uint8
	EatCount  int32
}

// ChaosItem 混沌道具进度。
type ChaosItem struct {
	ItemID   int16
	AgeLevel int16
}

// ChaosPanel 混沌虚空关键进度。
type ChaosPanel struct {
	LingYe   int32
	Monsters []ChaosMonster
	Items    []ChaosItem
}

// BuildChaosPackPacket 构造混沌虚空背包查询封包 (Module 124, Action 0)。
func BuildChaosPackPacket(prevAct uint32) *Packet {
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w := NewWriter()
	w.WriteUint32(prevAct)
	pkt := NewPacket(0x007C0000, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// BuildChaosRoleMonsterPacket 构造角色混沌魔兽查询封包 (Module 124, Action 1)。
func BuildChaosRoleMonsterPacket(playerRoleID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(playerRoleID)
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(0x007C0001, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

func parseChaosMonsters(r *Reader) ([]ChaosMonster, error) {
	count, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	monsters := make([]ChaosMonster, 0, count)
	for i := 0; i < int(count); i++ {
		if _, err := r.ReadInt32(); err != nil { // player_chaos_monster_id
			return nil, err
		}
		itemID, err := r.ReadInt16()
		if err != nil {
			return nil, err
		}
		if _, err := r.ReadInt16(); err != nil { // grid_id
			return nil, err
		}
		potential, err := r.ReadUint8()
		if err != nil {
			return nil, err
		}
		break1, err := r.ReadUint8()
		if err != nil {
			return nil, err
		}
		break2, err := r.ReadUint8()
		if err != nil {
			return nil, err
		}
		if err := skipInt32s(r, 3); err != nil { // number, price, player_role_id
			return nil, err
		}
		eatCount, err := r.ReadInt32()
		if err != nil {
			return nil, err
		}
		if err := skipInt32s(r, 2); err != nil { // three_percent, four_percent
			return nil, err
		}
		// fusion list
		if _, err := parseChaosFusionList(r); err != nil {
			return nil, err
		}
		// red attr list [Int, Int]
		if _, err := parseChaosPairList(r); err != nil {
			return nil, err
		}
		// skill list [Int, Byte]
		if _, err := parseChaosSkillList(r); err != nil {
			return nil, err
		}
		// item info [Int, Short, Short, Short, Int, Int, Int]
		if err := skipChaosItemInfoList(r); err != nil {
			return nil, err
		}
		monsters = append(monsters, ChaosMonster{
			ItemID: itemID, Potential: potential, Break1: break1, Break2: break2, EatCount: eatCount,
		})
	}
	return monsters, nil
}

func parseChaosFusionList(r *Reader) (int, error) {
	count, err := r.ReadUint16()
	if err != nil {
		return 0, err
	}
	for i := 0; i < int(count); i++ {
		if _, err := r.ReadInt32(); err != nil { // player_chaos_monster_id
			return 0, err
		}
		if _, err := r.ReadInt32(); err != nil { // item_id
			return 0, err
		}
		if _, err := r.ReadUint8(); err != nil { // break_count_1
			return 0, err
		}
		if _, err := r.ReadUint8(); err != nil { // break_count_2
			return 0, err
		}
		if err := skipInt32s(r, 3); err != nil { // three%, four%, att_add%
			return 0, err
		}
		if _, err := r.ReadUint8(); err != nil { // fusion_pos
			return 0, err
		}
		if _, err := r.ReadUint8(); err != nil { // eat_count
			return 0, err
		}
		if _, err := r.ReadInt16(); err != nil { // grid_id
			return 0, err
		}
	}
	return int(count), nil
}

func parseChaosPairList(r *Reader) (int, error) {
	count, err := r.ReadUint16()
	if err != nil {
		return 0, err
	}
	if err := skipInt32s(r, int(count)*2); err != nil {
		return 0, err
	}
	return int(count), nil
}

func parseChaosSkillList(r *Reader) (int, error) {
	count, err := r.ReadUint16()
	if err != nil {
		return 0, err
	}
	for i := 0; i < int(count); i++ {
		if _, err := r.ReadInt32(); err != nil {
			return 0, err
		}
		if _, err := r.ReadUint8(); err != nil {
			return 0, err
		}
	}
	return int(count), nil
}

func skipChaosItemInfoList(r *Reader) error {
	count, err := r.ReadUint16()
	if err != nil {
		return err
	}
	for i := 0; i < int(count); i++ {
		if _, err := r.ReadInt32(); err != nil {
			return err
		}
		if _, err := r.ReadInt16(); err != nil {
			return err
		}
		if _, err := r.ReadInt16(); err != nil {
			return err
		}
		if _, err := r.ReadInt16(); err != nil {
			return err
		}
		if err := skipInt32s(r, 3); err != nil {
			return err
		}
	}
	return nil
}

// ParseChaosPackResponse 解析混沌虚空背包 (Module 124, Action 0)。
func ParseChaosPackResponse(payload []byte) (*ChaosPanel, error) {
	r := NewReader(payload)
	lingYe, err := r.ReadInt32()
	if err != nil {
		return nil, err
	}
	monsters, err := parseChaosMonsters(r)
	if err != nil {
		return nil, err
	}
	itemCount, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	panel := &ChaosPanel{LingYe: lingYe, Monsters: monsters}
	for i := 0; i < int(itemCount); i++ {
		if _, err := r.ReadInt32(); err != nil { // player_chaos_item_id
			return nil, err
		}
		itemID, err := r.ReadInt16()
		if err != nil {
			return nil, err
		}
		if _, err := r.ReadInt16(); err != nil { // grid_id
			return nil, err
		}
		ageLevel, err := r.ReadInt16()
		if err != nil {
			return nil, err
		}
		if err := skipInt32s(r, 3); err != nil { // number, price, player_chaos_monster_id
			return nil, err
		}
		panel.Items = append(panel.Items, ChaosItem{ItemID: itemID, AgeLevel: ageLevel})
	}
	return panel, nil
}

// ParseChaosRoleMonsterResponse 解析角色混沌魔兽列表 (Module 124, Action 1)。
func ParseChaosRoleMonsterResponse(payload []byte) ([]ChaosMonster, error) {
	return parseChaosMonsters(NewReader(payload))
}
