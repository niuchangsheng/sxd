// Package protocol 提供扩充玩法协议：百炼乾坤一/二、灵域猎妖、幻境修炼与纪元造化秘境。
//
// 协议依据 docs/protocol/actions.json：
//
//	Mod_BaiLianQianKun_Base     (272) get_info 0 / challenge 1 / one_key_challenge 2
//	Mod_BaiLianQianKunTwo_Base  (320) get_info 0 / challenge 1 / one_key_challenge 2
//	Mod_SoulHunt_Base           (669) get_activity_state 0 / main_panel 1 / challenge_panel 3 /
//	                                  start_challenge 5 / task_panel 9 / get_task_award 10
//	Mod_PartnerPractice_Base    (306) get_partner_practice_info 0 / select_partner 1 /
//	                                  start_practice 2 / stop_practice 4 / fight 9
//	Mod_EraCreationRealm_Base   (866) get_panel_info 1 / explore 6 / fight_event 7 /
//	                                  fight 10 / fight_award_panel 11 / choose_fight_award 12
package protocol

import "fmt"

// 百炼乾坤动作 ID。
const (
	ActionIDBaiLianOneInfo  uint32 = 0x01100000
	ActionIDBaiLianOneFight uint32 = 0x01100001
	ActionIDBaiLianOneKey   uint32 = 0x01100002
	ActionIDBaiLianTwoInfo  uint32 = 0x01400000
	ActionIDBaiLianTwoFight uint32 = 0x01400001
	ActionIDBaiLianTwoKey   uint32 = 0x01400002
)

// 百炼乾坤一结果码 (Module 272)。
const (
	BaiLianOneHasGet      uint8 = 0
	BaiLianOneUnGet       uint8 = 1
	BaiLianOneCompleted   uint8 = 2
	BaiLianOneUncompleted uint8 = 3
	BaiLianOneActivate    uint8 = 4
	BaiLianOneNonActivate uint8 = 5
	BaiLianOneWin         uint8 = 6
	BaiLianOneLose        uint8 = 7
	BaiLianOneTimeCD      uint8 = 8
	BaiLianOneFailed      uint8 = 9
	BaiLianOneSuccess     uint8 = 10
	BaiLianOneFighting    uint8 = 11
	BaiLianOneLock        uint8 = 12
	BaiLianOnePass        uint8 = 13
)

// 百炼乾坤二结果码 (Module 320)。
const (
	BaiLianTwoHasGet     uint8 = 0
	BaiLianTwoUnGet      uint8 = 1
	BaiLianTwoCompleted  uint8 = 2
	BaiLianTwoUncomplete uint8 = 3
	BaiLianTwoActivate   uint8 = 4
	BaiLianTwoNonActive  uint8 = 5
	BaiLianTwoSuccess    uint8 = 6
	BaiLianTwoFuncUnopen uint8 = 7
	BaiLianTwoWin        uint8 = 8
	BaiLianTwoLose       uint8 = 9
	BaiLianTwoTimeCD     uint8 = 10
	BaiLianTwoFailed     uint8 = 11
	BaiLianTwoFighting   uint8 = 12
	BaiLianTwoLock       uint8 = 13
	BaiLianTwoPass       uint8 = 14
)

// 灵域猎妖动作 ID。
const (
	ActionIDSoulHuntState          uint32 = 0x029D0000
	ActionIDSoulHuntMainPanel      uint32 = 0x029D0001
	ActionIDSoulHuntChallengePanel uint32 = 0x029D0003
	ActionIDSoulHuntStartChallenge uint32 = 0x029D0005
	ActionIDSoulHuntTaskPanel      uint32 = 0x029D0009
	ActionIDSoulHuntTaskAward      uint32 = 0x029D000A
)

// 灵域猎妖结果码 (Module 669)。
const (
	SoulHuntSuccess       uint8 = 0
	SoulHuntFuncNotOpen   uint8 = 1
	SoulHuntFailed        uint8 = 2
	SoulHuntCantChallenge uint8 = 3
	SoulHuntNoIngot       uint8 = 4
	SoulHuntNoTime        uint8 = 5
	SoulHuntIsFighting    uint8 = 6
	SoulHuntNoPotential   uint8 = 7
	SoulHuntUpLevelLimit  uint8 = 8
)

// 幻境修炼动作 ID。
const (
	ActionIDPartnerPracticeInfo       uint32 = 0x01320000
	ActionIDPartnerPracticeSelect     uint32 = 0x01320001
	ActionIDPartnerPracticeStart      uint32 = 0x01320002
	ActionIDPartnerPracticeSpeed      uint32 = 0x01320003
	ActionIDPartnerPracticeStop       uint32 = 0x01320004
	ActionIDPartnerPracticeEnterFight uint32 = 0x01320005
	ActionIDPartnerPracticeCallMirror uint32 = 0x01320006
	ActionIDPartnerPracticeFight      uint32 = 0x01320009
)

// 幻境修炼结果码 (Module 306)。
const (
	PartnerPracticeSuccess       uint8 = 5
	PartnerPracticeNotInDeploy   uint8 = 6
	PartnerPracticeFinished      uint8 = 7
	PartnerPracticeFuncNoOpen    uint8 = 8
	PartnerPracticeSame          uint8 = 9
	PartnerPracticeFailure       uint8 = 10
	PartnerPracticeOtherPractice uint8 = 11
	PartnerPracticeNotPartner    uint8 = 12
	PartnerPracticeNoIngot       uint8 = 13
	PartnerPracticeTimesLimit    uint8 = 14
	PartnerPracticeLimit         uint8 = 15
	PartnerPracticeMaxLimit      uint8 = 16
	PartnerPracticeCDTime        uint8 = 21
	PartnerPracticeTimeLimit     uint8 = 25
)

// 纪元造化秘境动作 ID。
const (
	ActionIDEraCreationSeason     uint32 = 0x03620000
	ActionIDEraCreationPanel      uint32 = 0x03620001
	ActionIDEraCreationExplore    uint32 = 0x03620006
	ActionIDEraCreationFightEvent uint32 = 0x03620007
	ActionIDEraCreationFight      uint32 = 0x0362000A
	ActionIDEraCreationAwardPanel uint32 = 0x0362000B
	ActionIDEraCreationChoose     uint32 = 0x0362000C
)

// 纪元造化秘境结果码 (Module 866)。
const (
	EraCreationSuccess       uint8 = 0
	EraCreationFuncNotOpen   uint8 = 1
	EraCreationFailed        uint8 = 2
	EraCreationWrongID       uint8 = 3
	EraCreationNoIngot       uint8 = 4
	EraCreationNoChargeIngot uint8 = 5
	EraCreationAlreadyActive uint8 = 6
	EraCreationNoPrivilege   uint8 = 7
	EraCreationHaveAward     uint8 = 8
	EraCreationMaxFightEvent uint8 = 9
	EraCreationNoExploreNum  uint8 = 10
	EraCreationWrongDeploy   uint8 = 11
	EraCreationNoFightAward  uint8 = 12
)

// BuildBaiLianInfoPacket 构造百炼乾坤面板查询封包。
func BuildBaiLianInfoPacket(two bool) *Packet {
	if two {
		return NewPacket(ActionIDBaiLianTwoInfo, nil)
	}
	return NewPacket(ActionIDBaiLianOneInfo, nil)
}

// BuildBaiLianOneKeyPacket 构造百炼乾坤一键挑战封包。
func BuildBaiLianOneKeyPacket(two bool) *Packet {
	if two {
		return NewPacket(ActionIDBaiLianTwoKey, nil)
	}
	return NewPacket(ActionIDBaiLianOneKey, nil)
}

// BaiLianInfo 表示百炼乾坤面板的可读字段。
type BaiLianInfo struct {
	// Result 320 首字节状态；272 无独立结果码，保留面板首字节。
	Result uint8
	// Level 当前关卡。
	Level int32
	// Fields 其余状态字段（按协议顺序）。
	Fields []int32
	// Aux1 面板最后一个 Int 前的第一个 Short 字段（语义未确认，仅用于日志）。
	Aux1 int32
}

// ParseBaiLianInfo 解析百炼乾坤面板。two=true 时按 320 布局（首字节为状态）。
func ParseBaiLianInfo(payload []byte, two bool) (*BaiLianInfo, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	info := &BaiLianInfo{}
	if two {
		res, err := r.ReadUint8()
		if err != nil {
			return nil, fmt.Errorf("百炼乾坤二状态读取失败: %w", err)
		}
		info.Result = res
		level, err := r.ReadInt8()
		if err != nil {
			return nil, fmt.Errorf("百炼乾坤二关卡读取失败: %w", err)
		}
		info.Level = int32(level)
	} else {
		level, err := r.ReadInt8()
		if err != nil {
			return nil, fmt.Errorf("百炼乾坤一关卡读取失败: %w", err)
		}
		info.Level = int32(level)
	}
	for i := 0; i < 3; i++ {
		v, err := r.ReadUint8()
		if err != nil {
			return nil, fmt.Errorf("百炼乾坤状态字段[%d]读取失败: %w", i, err)
		}
		info.Fields = append(info.Fields, int32(v))
	}
	score, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("百炼乾坤积分读取失败: %w", err)
	}
	info.Fields = append(info.Fields, score)
	short1, err := r.ReadInt16()
	if err != nil {
		return nil, fmt.Errorf("百炼乾坤字段读取失败: %w", err)
	}
	short2, err := r.ReadInt16()
	if err != nil {
		return nil, fmt.Errorf("百炼乾坤次数读取失败: %w", err)
	}
	info.Fields = append(info.Fields, int32(short1), int32(short2))
	info.Aux1 = int32(short1)
	return info, nil
}

// ParseActionFirstByte 解析以 UByte 结果码开头的回包。
func ParseActionFirstByte(payload []byte, name string) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%s结果码为空", name)
	}
	return payload[0], nil
}

// SoulHuntMainPanel 表示灵域猎妖主面板的关键字段。
type SoulHuntMainPanel struct {
	Result uint8
	Ints   []int32
	Levels []int32
	Times  []int32
}

// ParseSoulHuntMainPanel 解析灵域猎妖主面板 (669/1)。
//
// 布局：[UByte result, Int x8, [Int,Int], Int,Int,Int]。
func ParseSoulHuntMainPanel(payload []byte) (*SoulHuntMainPanel, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	p := &SoulHuntMainPanel{}
	res, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("灵域猎妖结果码读取失败: %w", err)
	}
	p.Result = res
	for i := 0; i < 8; i++ {
		v, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("灵域猎妖面板字段[%d]读取失败: %w", i, err)
		}
		p.Ints = append(p.Ints, v)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("灵域猎妖关卡数量读取失败: %w", err)
	}
	for i := 0; i < int(count); i++ {
		id, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("灵域猎妖关卡[%d]读取失败: %w", i, err)
		}
		if _, err := r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("灵域猎妖关卡[%d]时间读取失败: %w", i, err)
		}
		p.Levels = append(p.Levels, id)
	}
	for i := 0; i < 3 && r.Remaining() >= 4; i++ {
		v, err := r.ReadInt32()
		if err != nil {
			break
		}
		p.Times = append(p.Times, v)
	}
	return p, nil
}

// SoulHuntEvent 表示灵域猎妖可挑战事件。
type SoulHuntEvent struct {
	Result uint8
	IDs    []int32
}

// BuildSoulHuntChallengePanelPacket 构造关卡面板查询封包。
func BuildSoulHuntChallengePanelPacket(levelID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(levelID)
	return NewPacket(ActionIDSoulHuntChallengePanel, w.Bytes())
}

// BuildSoulHuntStartChallengePacket 构造开始挑战封包。
func BuildSoulHuntStartChallengePacket(levelID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(levelID)
	return NewPacket(ActionIDSoulHuntStartChallenge, w.Bytes())
}

// BuildSoulHuntTaskAwardPacket 构造领取任务奖励封包。
func BuildSoulHuntTaskAwardPacket(taskID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(taskID)
	return NewPacket(ActionIDSoulHuntTaskAward, w.Bytes())
}

// ParseSoulHuntChallengeResult 解析挑战关卡回包首字节。
func ParseSoulHuntChallengeResult(payload []byte) (uint8, error) {
	return ParseActionFirstByte(payload, "灵域猎妖挑战")
}

// SoulHuntResultText 返回灵域猎妖结果码文本。
func SoulHuntResultText(code uint8) string {
	switch code {
	case SoulHuntSuccess:
		return "成功"
	case SoulHuntFuncNotOpen:
		return "功能未开放"
	case SoulHuntFailed:
		return "失败"
	case SoulHuntCantChallenge:
		return "当前不可挑战"
	case SoulHuntNoIngot:
		return "元宝不足"
	case SoulHuntNoTime:
		return "次数不足"
	case SoulHuntIsFighting:
		return "正在战斗中"
	case SoulHuntNoPotential:
		return "潜能不足"
	case SoulHuntUpLevelLimit:
		return "等级不足"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// BuildPartnerPracticeInfoPacket 构造幻境修炼信息查询封包。
func BuildPartnerPracticeInfoPacket() *Packet {
	return NewPacket(ActionIDPartnerPracticeInfo, nil)
}

// BuildPartnerPracticeStartPacket 构造开始修炼封包。
func BuildPartnerPracticeStartPacket() *Packet {
	return NewPacket(ActionIDPartnerPracticeStart, nil)
}

// BuildPartnerPracticeStopPacket 构造停止/结算修炼封包。
func BuildPartnerPracticeStopPacket() *Packet {
	return NewPacket(ActionIDPartnerPracticeStop, nil)
}

// BuildPartnerPracticeEnterFightPacket 构造进入修炼战斗封包 (306/5)。
func BuildPartnerPracticeEnterFightPacket() *Packet {
	return NewPacket(ActionIDPartnerPracticeEnterFight, nil)
}

// BuildPartnerPracticeCallMirrorPacket 构造召唤镜像封包 (306/6)。
func BuildPartnerPracticeCallMirrorPacket() *Packet {
	return NewPacket(ActionIDPartnerPracticeCallMirror, nil)
}

// BuildPartnerPracticeFightPacket 构造修炼战斗封包 (306/9)。
func BuildPartnerPracticeFightPacket() *Packet {
	return NewPacket(ActionIDPartnerPracticeFight, nil)
}

// BuildPartnerPracticeSelectPacket 构造选择修炼伙伴封包。
func BuildPartnerPracticeSelectPacket(roleID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(roleID)
	return NewPacket(ActionIDPartnerPracticeSelect, w.Bytes())
}

// PartnerPracticeInfo 表示幻境修炼面板关键字段。
type PartnerPracticeInfo struct {
	// PartnerRoleID 当前选中的伙伴角色 ID。
	PartnerRoleID int32
	// Result 结果码。
	Result uint8
}

// ParsePartnerPracticeInfo 解析幻境修炼信息，仅取首个 Int（当前伙伴）。
func ParsePartnerPracticeInfo(payload []byte) (*PartnerPracticeInfo, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	id, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("幻境修炼伙伴读取失败: %w", err)
	}
	info := &PartnerPracticeInfo{PartnerRoleID: id}
	if r.Remaining() > 0 {
		if res, rErr := r.ReadUint8(); rErr == nil {
			info.Result = res
		}
	}
	return info, nil
}

// ParsePartnerPracticeResult 解析修炼动作回包首字节。
func ParsePartnerPracticeResult(payload []byte) (uint8, error) {
	return ParseActionFirstByte(payload, "幻境修炼")
}

// PartnerPracticeResultText 返回幻境修炼结果码文本。
func PartnerPracticeResultText(code uint8) string {
	switch code {
	case PartnerPracticeSuccess:
		return "成功"
	case PartnerPracticeNotInDeploy:
		return "伙伴未上阵"
	case PartnerPracticeFinished:
		return "修炼已完成"
	case PartnerPracticeFuncNoOpen:
		return "功能未开放"
	case PartnerPracticeSame:
		return "已是当前伙伴"
	case PartnerPracticeFailure:
		return "失败"
	case PartnerPracticeOtherPractice:
		return "其他伙伴修炼中"
	case PartnerPracticeNotPartner:
		return "不是伙伴"
	case PartnerPracticeNoIngot:
		return "元宝不足"
	case PartnerPracticeTimesLimit:
		return "次数不足"
	case PartnerPracticeLimit, PartnerPracticeMaxLimit:
		return "已达上限"
	case PartnerPracticeCDTime:
		return "冷却中"
	case PartnerPracticeTimeLimit:
		return "时间限制"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// BuildEraCreationPanelPacket 构造纪元造化秘境面板查询封包。
func BuildEraCreationPanelPacket() *Packet {
	return NewPacket(ActionIDEraCreationPanel, nil)
}

// BuildEraCreationExplorePacket 构造探索封包。
func BuildEraCreationExplorePacket() *Packet {
	return NewPacket(ActionIDEraCreationExplore, nil)
}

// BuildEraCreationFightEventPacket 构造事件查询封包。
func BuildEraCreationFightEventPacket() *Packet {
	return NewPacket(ActionIDEraCreationFightEvent, nil)
}

// BuildEraCreationFightPacket 构造挑战事件封包。
func BuildEraCreationFightPacket(eventID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(eventID)
	return NewPacket(ActionIDEraCreationFight, w.Bytes())
}

// BuildEraCreationAwardPanelPacket 构造战斗奖励面板查询封包。
func BuildEraCreationAwardPanelPacket() *Packet {
	return NewPacket(ActionIDEraCreationAwardPanel, nil)
}

// BuildEraCreationChooseAwardPacket 构造领取战斗奖励封包。
func BuildEraCreationChooseAwardPacket() *Packet {
	return NewPacket(ActionIDEraCreationChoose, nil)
}

// EraCreationExploreResult 表示探索回包。
type EraCreationExploreResult struct {
	Result uint8
	Values []int32
}

// ParseEraCreationExplore 解析探索回包 [UByte result, Int x4]。
func ParseEraCreationExplore(payload []byte) (*EraCreationExploreResult, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	res, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("造化秘境探索结果码读取失败: %w", err)
	}
	out := &EraCreationExploreResult{Result: res}
	for i := 0; i < 4 && r.Remaining() >= 4; i++ {
		v, err := r.ReadInt32()
		if err != nil {
			break
		}
		out.Values = append(out.Values, v)
	}
	return out, nil
}

// EraCreationEventList 表示 fight_event 下发的可挑战事件。
type EraCreationEventList struct {
	Result uint8
	IDs    []int32
}

// ParseEraCreationFightEvent 解析 fight_event 回包 [UByte result, [Int x4]]。
func ParseEraCreationFightEvent(payload []byte) (*EraCreationEventList, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	res, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("造化秘境事件结果码读取失败: %w", err)
	}
	out := &EraCreationEventList{Result: res}
	if r.Remaining() < 2 {
		return out, nil
	}
	count, err := r.ReadUint16()
	if err != nil {
		return out, nil
	}
	for i := 0; i < int(count) && r.Remaining() >= 4; i++ {
		id, err := r.ReadInt32()
		if err != nil {
			break
		}
		out.IDs = append(out.IDs, id)
	}
	return out, nil
}

// BuildEraCreationSeasonPacket 构造赛季查询封包。
func BuildEraCreationSeasonPacket() *Packet {
	return NewPacket(ActionIDEraCreationSeason, nil)
}

// EraCreationResultText 返回造化秘境结果码文本。
func EraCreationResultText(code uint8) string {
	switch code {
	case EraCreationSuccess:
		return "成功"
	case EraCreationFuncNotOpen:
		return "功能未开放"
	case EraCreationFailed:
		return "失败"
	case EraCreationWrongID:
		return "事件编号错误"
	case EraCreationNoIngot:
		return "元宝不足"
	case EraCreationNoChargeIngot:
		return "充值元宝不足"
	case EraCreationAlreadyActive:
		return "已激活"
	case EraCreationNoPrivilege:
		return "无特权"
	case EraCreationHaveAward:
		return "有奖励待领取"
	case EraCreationMaxFightEvent:
		return "战斗事件已达上限"
	case EraCreationNoExploreNum:
		return "探索次数不足"
	case EraCreationWrongDeploy:
		return "阵容错误"
	case EraCreationNoFightAward:
		return "暂无战斗奖励"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// 神仙卡牌收藏集动作 ID (Mod_CultivationCardGame_Base, Module 859)。
const (
	ActionIDCultivationCardPanel  uint32 = 0x035B0000
	ActionIDCultivationGroupAward uint32 = 0x035B0002
	ActionIDCultivationBagInfo    uint32 = 0x035B0003
	ActionIDCultivationOpenBag    uint32 = 0x035B0004
	ActionIDCultivationTaskInfo   uint32 = 0x035B0005
	ActionIDCultivationTaskAward  uint32 = 0x035B0006
	ActionIDCultivationShopInfo   uint32 = 0x035B0007
	ActionIDCultivationBuyShop    uint32 = 0x035B0008
)

// 神仙卡牌结果码。
const (
	CultivationCardSuccess       uint8 = 0
	CultivationCardFailed        uint8 = 1
	CultivationCardActNotOpen    uint8 = 2
	CultivationCardCardLock      uint8 = 3
	CultivationCardNotComplete   uint8 = 9
	CultivationCardHadGet        uint8 = 10
	CultivationCardItemPackFull  uint8 = 12
	CultivationCardNotEnoughItem uint8 = 13
	CultivationCardNotEnoughRes  uint8 = 14
	CultivationCardBuyLimit      uint8 = 15
	CultivationCardCondError     uint8 = 30
)

// CultivationCardTask 表示觅寻仙踪任务条目。
type CultivationCardTask struct {
	TaskID   int32
	Progress int32
	Flag     uint8
}

// CultivationCardBag 表示卡包条目。
type CultivationCardBag struct {
	ItemID int32
	Count  int32
}

// CultivationCardShopItem 表示卡包商城条目。
type CultivationCardShopItem struct {
	ItemID int32
	Price  int32
}

// BuildCultivationCardPanelPacket 构造收藏集面板查询封包。
func BuildCultivationCardPanelPacket() *Packet {
	return NewPacket(ActionIDCultivationCardPanel, nil)
}

// BuildCultivationCardTaskInfoPacket 构造任务信息查询封包。
func BuildCultivationCardTaskInfoPacket(groupID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(groupID)
	return NewPacket(ActionIDCultivationTaskInfo, w.Bytes())
}

// BuildCultivationCardTaskAwardPacket 构造任务奖励领取封包。
func BuildCultivationCardTaskAwardPacket(taskID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(taskID)
	return NewPacket(ActionIDCultivationTaskAward, w.Bytes())
}

// BuildCultivationCardBagInfoPacket 构造卡包查询封包。
func BuildCultivationCardBagInfoPacket() *Packet {
	return NewPacket(ActionIDCultivationBagInfo, nil)
}

// BuildCultivationCardOpenBagPacket 构造开卡包封包。
func BuildCultivationCardOpenBagPacket(itemID, count int32) *Packet {
	w := NewWriter()
	w.WriteInt32(itemID)
	w.WriteInt32(count)
	return NewPacket(ActionIDCultivationOpenBag, w.Bytes())
}

// BuildCultivationCardShopInfoPacket 构造卡包商城查询封包。
func BuildCultivationCardShopInfoPacket() *Packet {
	return NewPacket(ActionIDCultivationShopInfo, nil)
}

// BuildCultivationCardBuyShopPacket 构造卡包商城购买封包。
func BuildCultivationCardBuyShopPacket(itemID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(itemID)
	return NewPacket(ActionIDCultivationBuyShop, w.Bytes())
}

// BuildCultivationCardGroupAwardPacket 构造套装奖励领取封包。
func BuildCultivationCardGroupAwardPacket(groupID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(groupID)
	return NewPacket(ActionIDCultivationGroupAward, w.Bytes())
}

// ParseCultivationCardTasks 解析任务列表 [Int group, [Int task, Int progress, Byte flag]]。
func ParseCultivationCardTasks(payload []byte) (int32, []CultivationCardTask, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	group, err := r.ReadInt32()
	if err != nil {
		return 0, nil, fmt.Errorf("卡牌任务组读取失败: %w", err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return group, nil, nil
	}
	tasks := make([]CultivationCardTask, 0, count)
	for i := 0; i < int(count); i++ {
		var task CultivationCardTask
		if task.TaskID, err = r.ReadInt32(); err != nil {
			return group, tasks, fmt.Errorf("卡牌任务[%d] id 读取失败: %w", i, err)
		}
		if task.Progress, err = r.ReadInt32(); err != nil {
			return group, tasks, fmt.Errorf("卡牌任务[%d] 进度读取失败: %w", i, err)
		}
		if task.Flag, err = r.ReadUint8(); err != nil {
			return group, tasks, fmt.Errorf("卡牌任务[%d] 状态读取失败: %w", i, err)
		}
		tasks = append(tasks, task)
	}
	return group, tasks, nil
}

// ParseCultivationCardBags 解析卡包列表 [[Int item, Int count]]。
func ParseCultivationCardBags(payload []byte) ([]CultivationCardBag, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	count, err := r.ReadUint16()
	if err != nil {
		return nil, nil
	}
	bags := make([]CultivationCardBag, 0, count)
	for i := 0; i < int(count); i++ {
		var bag CultivationCardBag
		if bag.ItemID, err = r.ReadInt32(); err != nil {
			return bags, fmt.Errorf("卡包[%d] id 读取失败: %w", i, err)
		}
		if bag.Count, err = r.ReadInt32(); err != nil {
			return bags, fmt.Errorf("卡包[%d] 数量读取失败: %w", i, err)
		}
		bags = append(bags, bag)
	}
	return bags, nil
}

// ParseCultivationCardShop 解析卡包商城 [[Int slot, Int price], Int extra]。
//
// 实测尾随 Int 不是货币类型（不同角色分别为 0/398），仅作活动积分记录；
// 卡包商城按既有玩法约定使用铜钱结算。
func ParseCultivationCardShop(payload []byte) ([]CultivationCardShopItem, int32, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	count, err := r.ReadUint16()
	if err != nil {
		return nil, 0, fmt.Errorf("卡包商城数量读取失败: %w", err)
	}
	items := make([]CultivationCardShopItem, 0, count)
	for i := 0; i < int(count); i++ {
		var item CultivationCardShopItem
		if item.ItemID, err = r.ReadInt32(); err != nil {
			return items, 0, fmt.Errorf("卡包商城[%d] id 读取失败: %w", i, err)
		}
		if item.Price, err = r.ReadInt32(); err != nil {
			return items, 0, fmt.Errorf("卡包商城[%d] 价格读取失败: %w", i, err)
		}
		items = append(items, item)
	}
	extra := int32(0)
	if r.Remaining() >= 4 {
		extra, _ = r.ReadInt32()
	}
	return items, extra, nil
}

// CultivationCardResultText 返回卡牌结果码文本。
func CultivationCardResultText(code uint8) string {
	switch code {
	case CultivationCardSuccess:
		return "成功"
	case CultivationCardFailed:
		return "失败"
	case CultivationCardActNotOpen:
		return "活动未开放"
	case CultivationCardCardLock:
		return "卡牌锁定"
	case CultivationCardNotComplete:
		return "未完成"
	case CultivationCardHadGet:
		return "已领取"
	case CultivationCardItemPackFull:
		return "背包已满"
	case CultivationCardNotEnoughItem:
		return "道具不足"
	case CultivationCardNotEnoughRes:
		return "资源不足"
	case CultivationCardBuyLimit:
		return "购买上限"
	case CultivationCardCondError:
		return "条件不满足"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// 仙界远征任务动作 ID (Mod_ExpeditionTask_Base, Module 430)。
const (
	ActionIDExpeditionInfo  uint32 = 0x01AE0000
	ActionIDExpeditionStart uint32 = 0x01AE0001
)

// 远征任务结果码。
const (
	ExpeditionSuccess      uint8 = 0
	ExpeditionAlreadyStart uint8 = 1
	ExpeditionOverGet      uint8 = 2
	ExpeditionFailed       uint8 = 3
)

// ExpeditionTask 表示远征任务条目。
type ExpeditionTask struct {
	MissionID int32
	State     int32
	Field2    int32
	Field3    int32
	Rewards   []EraRebirthReward
}

// BuildExpeditionInfoPacket 构造远征任务面板查询封包。
func BuildExpeditionInfoPacket() *Packet {
	return NewPacket(ActionIDExpeditionInfo, nil)
}

// BuildExpeditionStartPacket 构造开始远征任务封包。
func BuildExpeditionStartPacket(missionID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(missionID)
	return NewPacket(ActionIDExpeditionStart, w.Bytes())
}

// ParseExpeditionInfo 解析远征任务面板 [Long, Int, [Int,Int,Int,Int,[Int,Int]], Int, ...]。
func ParseExpeditionInfo(payload []byte) ([]ExpeditionTask, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	if _, err := r.ReadInt64(); err != nil {
		return nil, fmt.Errorf("远征任务时间读取失败: %w", err)
	}
	if _, err := r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("远征任务积分读取失败: %w", err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("远征任务数量读取失败: %w", err)
	}
	tasks := make([]ExpeditionTask, 0, count)
	for i := 0; i < int(count); i++ {
		var t ExpeditionTask
		if t.MissionID, err = r.ReadInt32(); err != nil {
			return tasks, fmt.Errorf("远征任务[%d] id 读取失败: %w", i, err)
		}
		if t.State, err = r.ReadInt32(); err != nil {
			return tasks, fmt.Errorf("远征任务[%d] 状态读取失败: %w", i, err)
		}
		if t.Field2, err = r.ReadInt32(); err != nil {
			return tasks, fmt.Errorf("远征任务[%d] 字段读取失败: %w", i, err)
		}
		if t.Field3, err = r.ReadInt32(); err != nil {
			return tasks, fmt.Errorf("远征任务[%d] 字段读取失败: %w", i, err)
		}
		rewardCount, err := r.ReadUint16()
		if err != nil {
			return tasks, fmt.Errorf("远征任务[%d] 奖励数量读取失败: %w", i, err)
		}
		for j := 0; j < int(rewardCount); j++ {
			var reward EraRebirthReward
			if reward.ItemID, err = r.ReadInt32(); err != nil {
				return tasks, fmt.Errorf("远征任务[%d] 奖励[%d]读取失败: %w", i, j, err)
			}
			num, nErr := r.ReadInt32()
			if nErr != nil {
				return tasks, fmt.Errorf("远征任务[%d] 奖励[%d]数量读取失败: %w", i, j, nErr)
			}
			reward.Num = int64(num)
			t.Rewards = append(t.Rewards, reward)
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

// ParseExpeditionResult 解析远征任务动作回包首字节。
func ParseExpeditionResult(payload []byte) (uint8, error) {
	return ParseActionFirstByte(payload, "远征任务")
}

// ExpeditionResultText 返回远征任务结果码文本。
func ExpeditionResultText(code uint8) string {
	switch code {
	case ExpeditionSuccess:
		return "成功"
	case ExpeditionAlreadyStart:
		return "已开始"
	case ExpeditionOverGet:
		return "已领取"
	case ExpeditionFailed:
		return "失败"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// 全网幻魔塔动作 ID (Mod_SaWorldTower_Base, Module 521)。
const (
	ActionIDSaWorldTowerOpen   uint32 = 0x02090000
	ActionIDSaWorldTowerPanel  uint32 = 0x02090001
	ActionIDSaWorldTowerDeploy uint32 = 0x02090002
	ActionIDSaWorldTowerFight  uint32 = 0x02090003
)

// 全网幻魔塔结果码。
const (
	SaWorldTowerSuccess    uint8 = 0
	SaWorldTowerFuncNoOpen uint8 = 1
	SaWorldTowerNoTime     uint8 = 2
	SaWorldTowerCDTime     uint8 = 3
	SaWorldTowerErrorID    uint8 = 4
	SaWorldTowerFailure    uint8 = 5
	SaWorldTowerNoIngot    uint8 = 6
)

// BuildSaWorldTowerOpenPacket 构造开放状态查询封包。
func BuildSaWorldTowerOpenPacket() *Packet {
	return NewPacket(ActionIDSaWorldTowerOpen, nil)
}

// BuildSaWorldTowerPanelPacket 构造幻魔塔面板查询封包。
func BuildSaWorldTowerPanelPacket() *Packet {
	return NewPacket(ActionIDSaWorldTowerPanel, nil)
}

// BuildSaWorldTowerDeployPacket 构造幻魔塔阵容查询封包。
func BuildSaWorldTowerDeployPacket(towerID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(towerID)
	return NewPacket(ActionIDSaWorldTowerDeploy, w.Bytes())
}

// BuildSaWorldTowerFightPacket 构造幻魔塔挑战封包。
func BuildSaWorldTowerFightPacket(towerID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(towerID)
	return NewPacket(ActionIDSaWorldTowerFight, w.Bytes())
}

// SaWorldTowerResultText 返回全网幻魔塔结果码文本。
func SaWorldTowerResultText(code uint8) string {
	switch code {
	case SaWorldTowerSuccess:
		return "成功"
	case SaWorldTowerFuncNoOpen:
		return "功能未开放"
	case SaWorldTowerNoTime:
		return "次数不足"
	case SaWorldTowerCDTime:
		return "冷却中"
	case SaWorldTowerErrorID:
		return "编号错误"
	case SaWorldTowerFailure:
		return "失败"
	case SaWorldTowerNoIngot:
		return "元宝不足"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// 奇缘宝藏动作 ID (Mod_FortuneBox_Base, Module 808)。
const (
	ActionIDFortuneBoxOpen    uint32 = 0x03280000
	ActionIDFortuneBoxPanel   uint32 = 0x03280001
	ActionIDFortuneBoxOpenBox uint32 = 0x03280003
	ActionIDFortuneBoxChoose  uint32 = 0x03280004
)

// 奇缘宝藏结果码。
const (
	FortuneBoxSuccess       uint8 = 0
	FortuneBoxFuncNoOpen    uint8 = 1
	FortuneBoxAlreadyBuy    uint8 = 2
	FortuneBoxNoChargeIngot uint8 = 3
	FortuneBoxNoIngot       uint8 = 4
	FortuneBoxPackageFull   uint8 = 5
	FortuneBoxFailed        uint8 = 6
	FortuneBoxMaxChooseNum  uint8 = 7
	FortuneBoxNoScore       uint8 = 8
	FortuneBoxCanNotChoose  uint8 = 9
	FortuneBoxWrongID       uint8 = 10
)

// BuildFortuneBoxOpenPacket 构造奇缘宝藏开放状态查询封包。
func BuildFortuneBoxOpenPacket() *Packet {
	return NewPacket(ActionIDFortuneBoxOpen, nil)
}

// BuildFortuneBoxPanelPacket 构造奇缘宝藏面板查询封包。
func BuildFortuneBoxPanelPacket() *Packet {
	return NewPacket(ActionIDFortuneBoxPanel, nil)
}

// BuildFortuneBoxOpenBoxPacket 构造开启宝箱封包。
func BuildFortuneBoxOpenBoxPacket(boxID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(boxID)
	return NewPacket(ActionIDFortuneBoxOpenBox, w.Bytes())
}

// BuildFortuneBoxChoosePacket 构造选择宝箱奖励封包。
func BuildFortuneBoxChoosePacket(boxID, itemID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(boxID)
	w.WriteInt32(itemID)
	return NewPacket(ActionIDFortuneBoxChoose, w.Bytes())
}

// FortuneBoxResultText 返回奇缘宝藏结果码文本。
func FortuneBoxResultText(code uint8) string {
	switch code {
	case FortuneBoxSuccess:
		return "成功"
	case FortuneBoxFuncNoOpen:
		return "功能未开放"
	case FortuneBoxAlreadyBuy:
		return "已购买"
	case FortuneBoxNoChargeIngot:
		return "充值元宝不足"
	case FortuneBoxNoIngot:
		return "元宝不足"
	case FortuneBoxPackageFull:
		return "背包已满"
	case FortuneBoxFailed:
		return "失败"
	case FortuneBoxMaxChooseNum:
		return "选择已达上限"
	case FortuneBoxNoScore:
		return "积分不足"
	case FortuneBoxCanNotChoose:
		return "不可选择"
	case FortuneBoxWrongID:
		return "编号错误"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// 宠物仙域游历动作 ID (Mod_FairylandDispatch_Base, Module 714)。
const (
	ActionIDFairylandMainPanel uint32 = 0x02CA0001
	ActionIDFairylandTaskPanel uint32 = 0x02CA0002
	ActionIDFairylandDispatch  uint32 = 0x02CA0006
	ActionIDFairylandTaskAward uint32 = 0x02CA000A
)

// 宠物仙域结果码。
const (
	FairylandSuccess        uint8 = 0
	FairylandFailed         uint8 = 1
	FairylandNotSame        uint8 = 2
	FairylandIsLocked       uint8 = 3
	FairylandTopLevel       uint8 = 4
	FairylandLessResource   uint8 = 5
	FairylandEmptyGrid      uint8 = 6
	FairylandLessEnergy     uint8 = 7
	FairylandLessGrid       uint8 = 8
	FairylandLessScore      uint8 = 9
	FairylandExchangeLimit  uint8 = 10
	FairylandItemPackFull   uint8 = 11
	FairylandIsGet          uint8 = 12
	FairylandLessCharge     uint8 = 13
	FairylandNoIngot        uint8 = 14
	FairylandBuyLimit       uint8 = 15
	FairylandLessUnlockNum  uint8 = 16
	FairylandIsActived      uint8 = 17
	FairylandTimesLimited   uint8 = 18
	FairylandOrderCompleted uint8 = 19
)

// BuildFairylandMainPanelPacket 构造宠物仙域主面板查询封包。
func BuildFairylandMainPanelPacket() *Packet {
	return NewPacket(ActionIDFairylandMainPanel, nil)
}

// BuildFairylandTaskPanelPacket 构造宠物仙域任务面板查询封包。
func BuildFairylandTaskPanelPacket() *Packet {
	return NewPacket(ActionIDFairylandTaskPanel, nil)
}

// BuildFairylandDispatchPacket 构造宠物游历派遣封包。
func BuildFairylandDispatchPacket(orderID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(orderID)
	return NewPacket(ActionIDFairylandDispatch, w.Bytes())
}

// BuildFairylandTaskAwardPacket 构造宠物仙域任务奖励领取封包。
func BuildFairylandTaskAwardPacket(taskID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(taskID)
	return NewPacket(ActionIDFairylandTaskAward, w.Bytes())
}

// FairylandResultText 返回宠物仙域结果码文本。
func FairylandResultText(code uint8) string {
	switch code {
	case FairylandSuccess:
		return "成功"
	case FairylandFailed:
		return "失败"
	case FairylandNotSame:
		return "不匹配"
	case FairylandIsLocked:
		return "未解锁"
	case FairylandTopLevel:
		return "已满级"
	case FairylandLessResource:
		return "资源不足"
	case FairylandEmptyGrid:
		return "空位不足"
	case FairylandLessEnergy:
		return "体力不足"
	case FairylandLessGrid:
		return "格子不足"
	case FairylandLessScore:
		return "积分不足"
	case FairylandExchangeLimit:
		return "兑换上限"
	case FairylandItemPackFull:
		return "背包已满"
	case FairylandIsGet:
		return "已领取"
	case FairylandNoIngot:
		return "元宝不足"
	case FairylandTimesLimited:
		return "次数上限"
	case FairylandOrderCompleted:
		return "订单已完成"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// 三界修炼动作 ID (Mod_ThreeWorld_Base 529 / Mod_HumanWorld_Base 531)。
const (
	ActionIDThreeWorldStatus     uint32 = 0x02110001
	ActionIDThreeWorldResetTime  uint32 = 0x02110006
	ActionIDHumanWorldPractice   uint32 = 0x0213000C
	ActionIDHumanWorldStart      uint32 = 0x0213000D
	ActionIDHumanWorldStop       uint32 = 0x0213000E
	ActionIDHumanWorldAccelerate uint32 = 0x0213000F
)

// 三界修炼状态。
const (
	ThreeWorldSuccess  uint8 = 0
	ThreeWorldFailed   uint8 = 1
	ThreeWorldClose    uint8 = 2
	ThreeWorldReset    uint8 = 3
	ThreeWorldIdle     uint8 = 4
	ThreeWorldPractice uint8 = 5
	ThreeWorldComplete uint8 = 6
	ThreeWorldWait     uint8 = 7
	ThreeWorldOpen     uint8 = 8
)

// ThreeWorldStatus 表示三界修炼状态回包。
type ThreeWorldStatus struct {
	State uint8
	Sub   uint8
	V1    int32
	V2    int32
	V3    int32
}

// BuildThreeWorldStatusPacket 构造三界状态查询封包。
func BuildThreeWorldStatusPacket() *Packet {
	return NewPacket(ActionIDThreeWorldStatus, nil)
}

// ParseThreeWorldStatus 解析 [UByte, UByte, Int, Int, Int]。
func ParseThreeWorldStatus(payload []byte) (*ThreeWorldStatus, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	st := &ThreeWorldStatus{}
	var err error
	if st.State, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("三界状态读取失败: %w", err)
	}
	if st.Sub, err = r.ReadUint8(); err != nil {
		return st, nil
	}
	st.V1, _ = r.ReadInt32()
	st.V2, _ = r.ReadInt32()
	st.V3, _ = r.ReadInt32()
	return st, nil
}

// BuildHumanWorldPracticePacket 构造人界修炼信息查询封包。
func BuildHumanWorldPracticePacket() *Packet {
	return NewPacket(ActionIDHumanWorldPractice, nil)
}

// BuildHumanWorldStartPacket 构造人界开始修炼封包。
func BuildHumanWorldStartPacket() *Packet {
	return NewPacket(ActionIDHumanWorldStart, nil)
}

// BuildHumanWorldStopPacket 构造人界停止修炼封包。
func BuildHumanWorldStopPacket() *Packet {
	return NewPacket(ActionIDHumanWorldStop, nil)
}

// ParseHumanWorldPractice 解析 [UByte result, Int x7]，返回结果码与字段。
func ParseHumanWorldPractice(payload []byte) (uint8, []int32, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	res, err := r.ReadUint8()
	if err != nil {
		return 0, nil, fmt.Errorf("人界修炼结果码读取失败: %w", err)
	}
	vals := make([]int32, 0, 7)
	for i := 0; i < 7 && r.Remaining() >= 4; i++ {
		v, rErr := r.ReadInt32()
		if rErr != nil {
			break
		}
		vals = append(vals, v)
	}
	return res, vals, nil
}

// ThreeWorldResultText 返回三界/人界结果码文本。
func ThreeWorldResultText(code uint8) string {
	switch code {
	case 0:
		return "成功"
	case 1:
		return "失败"
	case 2:
		return "已关闭"
	case 5:
		return "修炼中"
	case 6:
		return "已完成"
	case 7:
		return "等待中"
	default:
		return fmt.Sprintf("状态(%d)", code)
	}
}
