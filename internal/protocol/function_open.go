package protocol

import "fmt"

// 功能开放状态查询 (Mod_Player_Base.check_function_open, Module 0, Action 69)。
//
// 服务端对未解锁模块通常静默丢弃业务请求; 该接口用于在发起业务前探测功能是否开放,
// 避免每个调度周期都等待业务请求超时。
const (
	// ActionIDPlayerCheckFunctionOpen 查询指定功能是否开放 (Module 0, Action 69 -> 0x00000045)
	ActionIDPlayerCheckFunctionOpen uint32 = 0x00000045

	// PlayerFunctionOpenTrue 功能已开放 (Mod_Player_Base.TRUE)
	PlayerFunctionOpenTrue uint8 = 119
	// PlayerFunctionOpenFalse 功能未开放 (Mod_Player_Base.FALSE)
	PlayerFunctionOpenFalse uint8 = 120

	// FunctionIDRefinePool 粗铁淬炼池功能编号 (FunctionTypeData: RefinePool)
	FunctionIDRefinePool int32 = 163
	// FunctionIDWorshipMars 尊像膜拜功能编号 (FunctionTypeData: WorshipMars)
	FunctionIDWorshipMars int32 = 55
	// FunctionIDWuFaField 悟法领域功能编号 (FunctionTypeData: WuFaField)
	FunctionIDWuFaField int32 = 245
	// FunctionIDFateHunt 猎命功能编号 (FunctionTypeData: Fate)
	FunctionIDFateHunt int32 = 24
	// FunctionIDZodiac 十二生肖功能编号 (FunctionTypeData: Zodiac)
	FunctionIDZodiac int32 = 58
	// FunctionIDBeelzebubTrials 魔王试炼功能编号 (FunctionTypeData: BeelzebubTrials)
	FunctionIDBeelzebubTrials int32 = 68
	// FunctionIDMiracleFighters 奇门遁甲功能编号 (FunctionTypeData: MiracleFighters)
	FunctionIDMiracleFighters int32 = 152
	// FunctionIDRepressDemonTower 锁妖塔功能编号 (FunctionTypeData: RepressDemonTower)
	FunctionIDRepressDemonTower int32 = 257
	// FunctionIDShanhaiWorld 山海九界功能编号 (FunctionTypeData: ShanhaiWorld，通天塔等玩法归属该功能)
	FunctionIDShanhaiWorld int32 = 243
	// FunctionIDEudemons 山海幻兽功能编号 (FunctionTypeData: Eudemons，含多宝山/异界入侵)
	FunctionIDEudemons int32 = 256
	// FunctionIDDestinySword 本命飞剑功能编号 (FunctionTypeData: DestinySword)
	FunctionIDDestinySword int32 = 289
	// FunctionIDLinkFate 结缘功能编号 (FunctionTypeData: LinkFate)
	FunctionIDLinkFate int32 = 140
	// FunctionIDPartnerLink 天缘/灵结功能编号 (FunctionTypeData: PartnerLink)
	FunctionIDPartnerLink int32 = 166
	// FunctionIDSaintArea 圣域功能编号 (FunctionTypeData: SaintArea)
	FunctionIDSaintArea int32 = 165
	// FunctionIDFaction 帮派功能编号 (FunctionTypeData: Faction)
	FunctionIDFaction int32 = 119
	// FunctionIDTreasureHouse 天帝宝库功能编号 (FunctionTypeData: TreasureHouse)
	FunctionIDTreasureHouse int32 = 231
	// FunctionIDStTrials 秘境探宝功能编号 (FunctionTypeData: StTrials)
	FunctionIDStTrials int32 = 128
	// FunctionIDStPracticeRoom 仙界练功房功能编号 (FunctionTypeData: StPracticeRoom)
	FunctionIDStPracticeRoom int32 = 94
	// FunctionIDTravelEvent 仙旅奇缘功能编号 (FunctionTypeData: TravelEvent)
	FunctionIDTravelEvent int32 = 34
	// FunctionIDRollCake 吉星高照功能编号 (FunctionTypeData: RollCake)
	FunctionIDRollCake int32 = 54
	// FunctionIDFindImmortal 喜从天降功能编号 (FunctionTypeData: FindImmortal)
	FunctionIDFindImmortal int32 = 70
	// FunctionIDFishing 钓鱼功能编号 (FunctionTypeData: Fishing)
	FunctionIDFishing int32 = 134
)

// BuildCheckFunctionOpenPacket 构造功能开放状态查询封包。
//
// 线格式: Int32 function_id, Uint32 prev_act。
func BuildCheckFunctionOpenPacket(functionID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(functionID)
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDPlayerCheckFunctionOpen, w.Bytes())
	// 心跳会推进服务端 PrevAct 链, 发包时由会话层用真实最近动作号覆盖尾部。
	pkt.HasPrevAct = true
	return pkt
}

// ParseCheckFunctionOpenResponse 解析功能开放状态查询回包。
func ParseCheckFunctionOpenResponse(payload []byte) (uint8, error) {
	r := NewReader(payload)
	flag, err := r.ReadUint8()
	if err != nil {
		return 0, fmt.Errorf("check function open: read flag: %w", err)
	}
	return flag, nil
}
