// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方「NPC 结交」协议 (Mod_NpcFriendship_Base, Module 436 / 0x01B4)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_NpcFriendship_Base、
// com.datas.NpcFriendShipData 与 com.assist.server.NpcFriendShipType：
//   - get_info              Act 1 -> 0x01B40001  查询全部 NPC 好感度与任务状态
//   - get_give_present_info Act 2 -> 0x01B40002  查询今日送礼次数上限
//   - give_present          Act 3 -> 0x01B40003  [Int npc_id, Int present_id]
//   - make_friend           Act 4 -> 0x01B40004  [Int npc_id]
//   - gain_mission_award    Act 7 -> 0x01B40007  [Int npc_id]
//
// 礼物 1=伴手礼 (消耗铜钱系道具)，2=同心结，3=金兰花 (元宝)。自动化只使用礼物 1。
package protocol

import "fmt"

const (
	// ModuleNpcFriendship NPC 结交模块编号 (0x01B4)
	ModuleNpcFriendship uint16 = 436
)

// NPC 结交动作编号 (Mod_NpcFriendship_Base)。
const (
	// ActionIDNpcFriendshipInfo 查询 NPC 结交信息 (Action 1 -> 0x01B40001)
	ActionIDNpcFriendshipInfo uint32 = 0x01B40001
	// ActionIDNpcFriendshipPresentInfo 查询送礼信息 (Action 2 -> 0x01B40002)
	ActionIDNpcFriendshipPresentInfo uint32 = 0x01B40002
	// ActionIDNpcFriendshipGivePresent 赠送礼物 (Action 3 -> 0x01B40003)
	ActionIDNpcFriendshipGivePresent uint32 = 0x01B40003
	// ActionIDNpcFriendshipMakeFriend 结交 NPC (Action 4 -> 0x01B40004)
	ActionIDNpcFriendshipMakeFriend uint32 = 0x01B40004
	// ActionIDNpcFriendshipTakeMission 接取结交任务 (Action 6 -> 0x01B40006)
	ActionIDNpcFriendshipTakeMission uint32 = 0x01B40006
	// ActionIDNpcFriendshipGainMissionAward 领取结交任务奖励 (Action 7 -> 0x01B40007)
	ActionIDNpcFriendshipGainMissionAward uint32 = 0x01B40007
	// ActionIDNpcFriendshipAnswerQuestion 回答 NPC 每日问答 (Action 8 -> 0x01B40008)
	ActionIDNpcFriendshipAnswerQuestion uint32 = 0x01B40008
	// ActionIDNpcFriendshipCompleteMission 完成结交任务 (Action 9 -> 0x01B40009)
	ActionIDNpcFriendshipCompleteMission uint32 = 0x01B40009
	// ActionIDNpcFriendshipFingerGuess 猜拳 (Action 10 -> 0x01B4000A)
	ActionIDNpcFriendshipFingerGuess uint32 = 0x01B4000A
	// ActionIDNpcFriendshipDiceGuess 掷骰子 (Action 12 -> 0x01B4000C)
	ActionIDNpcFriendshipDiceGuess uint32 = 0x01B4000C
)

// NPC 结交礼物编号 (NpcFriendShipType.PresentList)。
const (
	// NpcFriendshipPresentSouvenir 伴手礼 (铜钱系道具，自动化唯一使用)
	NpcFriendshipPresentSouvenir int32 = 1
	// NpcFriendshipPresentKnot 同心结
	NpcFriendshipPresentKnot int32 = 2
	// NpcFriendshipPresentOrchid 金兰花 (消耗元宝，自动化永不使用)
	NpcFriendshipPresentOrchid int32 = 3
)

// NpcFriendShipMakeFriendMinExp 结义所需的最低好感度经验。
//
// 客户端 NpcFriendShipType.LevelList:
//
//	Lv4 患难与共 min_exp=671  max_exp=1070
//	Lv5 义结金兰 min_exp=1070 max_exp=99999
//
// 服务端仅在 Lv5(义结金兰) 时允许 make_friend；好感度不足返回 EXP_NO_ENOUGH(6)。
// 由于 1070 在 LevelList 中同时命中 Lv4，自动化使用严格大于 1070 的阈值，
// 即好感度达到 1071 后再发起结义。
const NpcFriendShipMakeFriendMinExp int32 = 1071

// NPC 结交结果码 (Mod_NpcFriendship_Base)。
const (
	// NpcFriendshipResultSuccess 成功
	NpcFriendshipResultSuccess uint8 = 0
	// NpcFriendshipResultCountLimit 次数上限
	NpcFriendshipResultCountLimit uint8 = 1
	// NpcFriendshipResultNoEnough 物品不足
	NpcFriendshipResultNoEnough uint8 = 2
	// NpcFriendshipResultExpMax 好感度已满级
	NpcFriendshipResultExpMax uint8 = 3
	// NpcFriendshipResultFailure 失败
	NpcFriendshipResultFailure uint8 = 4
	// NpcFriendshipResultMakeFriendCountLimit 结交人数上限
	NpcFriendshipResultMakeFriendCountLimit uint8 = 5
	// NpcFriendshipResultExpNoEnough 好感度不足
	NpcFriendshipResultExpNoEnough uint8 = 6
	// NpcFriendshipResultNotFriend 尚未结交
	NpcFriendshipResultNotFriend uint8 = 7
	// NpcFriendshipResultMaxCountLimit 已达上限
	NpcFriendshipResultMaxCountLimit uint8 = 8
	// NpcFriendshipResultAlreadyTake 已领取
	NpcFriendshipResultAlreadyTake uint8 = 9
	// NpcFriendshipResultHasNotComplete 任务未完成
	NpcFriendshipResultHasNotComplete uint8 = 10
	// NpcFriendshipResultHasAlreadyReceive 已领取过
	NpcFriendshipResultHasAlreadyReceive uint8 = 11
	// NpcFriendshipResultPackFull 背包已满
	NpcFriendshipResultPackFull uint8 = 12
	// NpcFriendshipResultErrorQuestID 问题/任务编号错误
	NpcFriendshipResultErrorQuestID uint8 = 16
	// NpcFriendshipResultHasAlreadyComplete 任务已完成
	NpcFriendshipResultHasAlreadyComplete uint8 = 17
	// NpcFriendshipResultCompleteNoEnough 任务进度/交付物不足
	NpcFriendshipResultCompleteNoEnough uint8 = 18
	// NpcFriendshipResultNpcError NPC 状态错误
	NpcFriendshipResultNpcError uint8 = 19
	// NpcFriendshipResultAnswerA 回答 A
	NpcFriendshipResultAnswerA uint8 = 13
	// NpcFriendshipResultAnswerB 回答 B
	NpcFriendshipResultAnswerB uint8 = 14
	// NpcFriendshipResultAnswerC 回答 C
	NpcFriendshipResultAnswerC uint8 = 15
	// NpcFriendshipResultWin 猜拳/掷骰子胜利
	NpcFriendshipResultWin uint8 = 20
	// NpcFriendshipResultLose 猜拳/掷骰子失败
	NpcFriendshipResultLose uint8 = 21
	// NpcFriendshipResultTie 猜拳/掷骰子平局
	NpcFriendshipResultTie uint8 = 22
)

// NpcFriendshipInfo 表示一个 NPC 的结交状态。
type NpcFriendshipInfo struct {
	// NpcID NPC 编号
	NpcID int32
	// CurExp 当前好感度经验
	CurExp int32
	// IsFriend 是否已结交 (1 已结交)
	IsFriend uint8
	// MissionType 结交任务类型
	MissionType int32
	// HasTakeMission 是否已接取任务
	HasTakeMission uint8
	// IsComplete 任务是否完成
	IsComplete uint8
	// IsReceive 任务奖励是否已领取
	IsReceive uint8
	// QuestionID 问答题编号
	QuestionID int32
	// ProgressArgs 任务进度参数
	ProgressArgs int32
	// TownNpcID 城镇 NPC 编号
	TownNpcID int32
	// MissionItemID 任务物品编号
	MissionItemID int32
}

// NpcFriendshipSnapshot 表示 NPC 结交总览。
type NpcFriendshipSnapshot struct {
	// Npcs 全部 NPC 结交状态
	Npcs []NpcFriendshipInfo
	// FriendCount 已结交数量
	FriendCount int32
	// MissionCount 任务数量
	MissionCount int32
}

// NpcFriendshipPresentInfo 表示送礼面板。
type NpcFriendshipPresentInfo struct {
	// UsedCounts present_id -> 今日已使用次数
	UsedCounts map[int32]int32
	// MaxGivePresent 今日最多赠送次数
	MaxGivePresent uint8
}

// NpcFriendshipGiveResult 表示送礼结果。
type NpcFriendshipGiveResult struct {
	// Result 结果码
	Result uint8
	// NewExp 送礼后的好感度经验
	NewExp int32
	// NpcID NPC 编号
	NpcID int32
}

// NpcFriendshipFriendResult 表示结交结果。
type NpcFriendshipFriendResult struct {
	// Result 结果码
	Result uint8
	// IsFriend 结交后的状态
	IsFriend uint8
	// NpcID NPC 编号
	NpcID int32
}

// BuildNpcFriendshipInfoPacket 构造查询 NPC 结交信息封包 (0x01B40001)。
func BuildNpcFriendshipInfoPacket() *Packet {
	return NewPacket(ActionIDNpcFriendshipInfo, nil)
}

// BuildNpcFriendshipPresentInfoPacket 构造查询送礼信息封包 (0x01B40002)。
func BuildNpcFriendshipPresentInfoPacket() *Packet {
	return NewPacket(ActionIDNpcFriendshipPresentInfo, nil)
}

// BuildNpcFriendshipGivePresentPacket 构造送礼封包 (0x01B40003)。
//
// 官方客户端调用顺序为 [present_id, npc_id] (见 NpcFriendShipPresentView.onBuy)，
// 与字段语义容易混淆，务必保持 present_id 在前。
func BuildNpcFriendshipGivePresentPacket(presentID, npcID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(presentID)
	w.WriteInt32(npcID)
	return NewPacket(ActionIDNpcFriendshipGivePresent, w.Bytes())
}

// BuildNpcFriendshipMakeFriendPacket 构造结交封包 (0x01B40004)。
func BuildNpcFriendshipMakeFriendPacket(npcID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(npcID)
	return NewPacket(ActionIDNpcFriendshipMakeFriend, w.Bytes())
}

// BuildNpcFriendshipGainMissionAwardPacket 构造领取任务奖励封包 (0x01B40007)。
func BuildNpcFriendshipGainMissionAwardPacket(npcID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(npcID)
	return NewPacket(ActionIDNpcFriendshipGainMissionAward, w.Bytes())
}

// BuildNpcFriendshipTakeMissionPacket 构造接取结交任务封包 (0x01B40006)。
func BuildNpcFriendshipTakeMissionPacket(npcID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(npcID)
	return NewPacket(ActionIDNpcFriendshipTakeMission, w.Bytes())
}

// BuildNpcFriendshipAnswerQuestionPacket 构造回答 NPC 问题封包 (0x01B40008)。
//
// 线格式: Int32 question_id, Int32 npc_id, UByte answer。
// 2026-11-08 线上探针确认参数顺序为 question_id 在前；反过来发送会返回
// ERROR_QUEST_ID(16)。answer 使用 ANSWER_A(13)/ANSWER_B(14)/ANSWER_C(15)。
func BuildNpcFriendshipAnswerQuestionPacket(questionID, npcID int32, answer uint8) *Packet {
	w := NewWriter()
	w.WriteInt32(questionID)
	w.WriteInt32(npcID)
	w.WriteUint8(answer)
	return NewPacket(ActionIDNpcFriendshipAnswerQuestion, w.Bytes())
}

// BuildNpcFriendshipCompleteMissionPacket 构造完成结交任务封包 (0x01B40009)。
//
// 线格式: Int32 npc_id, Int32 mission_type, Int32 town_npc_id。
func BuildNpcFriendshipCompleteMissionPacket(npcID, missionType, townNPCID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(npcID)
	w.WriteInt32(missionType)
	w.WriteInt32(townNPCID)
	return NewPacket(ActionIDNpcFriendshipCompleteMission, w.Bytes())
}

// BuildNpcFriendshipFingerGuessPacket 构造猜拳封包 (0x01B4000A)。
//
// 线格式: Int32 npc_id, Int32 quest_id(mission_type), Int32 town_npc_id, Int32 my_guess。
// my_guess: 0/1/2 分别对应石头/剪刀/布。
func BuildNpcFriendshipFingerGuessPacket(npcID, questID, townNPCID, myGuess int32) *Packet {
	w := NewWriter()
	w.WriteInt32(npcID)
	w.WriteInt32(questID)
	w.WriteInt32(townNPCID)
	w.WriteInt32(myGuess)
	return NewPacket(ActionIDNpcFriendshipFingerGuess, w.Bytes())
}

// BuildNpcFriendshipDiceGuessPacket 构造掷骰子封包 (0x01B4000C)。
//
// 线格式: Int32 npc_id, Int32 quest_id(mission_type), Int32 town_npc_id。
func BuildNpcFriendshipDiceGuessPacket(npcID, questID, townNPCID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(npcID)
	w.WriteInt32(questID)
	w.WriteInt32(townNPCID)
	return NewPacket(ActionIDNpcFriendshipDiceGuess, w.Bytes())
}

// NpcFriendshipTakeMissionResult 表示接取任务结果。
type NpcFriendshipTakeMissionResult struct {
	// Result 结果码
	Result uint8
	// HasTakeMission 接取后的状态 (1 已接取)
	HasTakeMission uint8
	// NpcID NPC 编号
	NpcID int32
	// MissionCount 当前进行中的任务数
	MissionCount int32
}

// NpcFriendshipAnswerResult 表示回答 NPC 问题结果。
type NpcFriendshipAnswerResult struct {
	// Result 结果码
	Result uint8
	// NewExp 回答后的好感度经验
	NewExp int32
	// NpcID NPC 编号
	NpcID int32
}

// NpcFriendshipCompleteResult 表示完成结交任务结果。
type NpcFriendshipCompleteResult struct {
	// Result 结果码
	Result uint8
	// NpcID NPC 编号
	NpcID int32
}

// NpcFriendshipGuessResult 表示猜拳/掷骰子结果。
type NpcFriendshipGuessResult struct {
	// Result WIN(20) / LOSE(21) / TIE(22)
	Result uint8
	// MyGuess 我方出拳/点数 (服务端回显)
	MyGuess int32
	// NpcGuess NPC 出拳/点数
	NpcGuess int32
}

// NpcFriendshipMissionAwardResult 表示领取任务奖励结果。
type NpcFriendshipMissionAwardResult struct {
	// Result 结果码
	Result uint8
	// IsReceive 奖励领取状态
	IsReceive uint8
	// NewExp 领取后的好感度经验
	NewExp int32
	// NpcID NPC 编号
	NpcID int32
}

// ParseNpcFriendshipTakeMissionResult 解析接取任务回包。
func ParseNpcFriendshipTakeMissionResult(payload []byte) (*NpcFriendshipTakeMissionResult, error) {
	r := NewReader(payload)
	res := &NpcFriendshipTakeMissionResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read take mission result: %v", ErrInvalidPayload, err)
	}
	if res.HasTakeMission, err = r.ReadUint8(); err != nil {
		return res, nil
	}
	if res.NpcID, err = r.ReadInt32(); err != nil {
		return res, nil
	}
	if res.MissionCount, err = r.ReadInt32(); err != nil {
		return res, nil
	}
	return res, nil
}

// ParseNpcFriendshipAnswerResult 解析回答 NPC 问题回包。
func ParseNpcFriendshipAnswerResult(payload []byte) (*NpcFriendshipAnswerResult, error) {
	r := NewReader(payload)
	res := &NpcFriendshipAnswerResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read answer result: %v", ErrInvalidPayload, err)
	}
	if res.NewExp, err = r.ReadInt32(); err != nil {
		return res, nil
	}
	if res.NpcID, err = r.ReadInt32(); err != nil {
		return res, nil
	}
	return res, nil
}

// ParseNpcFriendshipCompleteResult 解析完成结交任务回包。
func ParseNpcFriendshipCompleteResult(payload []byte) (*NpcFriendshipCompleteResult, error) {
	r := NewReader(payload)
	res := &NpcFriendshipCompleteResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read complete mission result: %v", ErrInvalidPayload, err)
	}
	if res.NpcID, err = r.ReadInt32(); err != nil {
		return res, nil
	}
	return res, nil
}

// ParseNpcFriendshipGuessResult 解析猜拳/掷骰子回包。
func ParseNpcFriendshipGuessResult(payload []byte) (*NpcFriendshipGuessResult, error) {
	r := NewReader(payload)
	res := &NpcFriendshipGuessResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read guess result: %v", ErrInvalidPayload, err)
	}
	if res.MyGuess, err = r.ReadInt32(); err != nil {
		return res, nil
	}
	if res.NpcGuess, err = r.ReadInt32(); err != nil {
		return res, nil
	}
	return res, nil
}

// ParseNpcFriendshipMissionAwardResult 解析领取任务奖励回包。
func ParseNpcFriendshipMissionAwardResult(payload []byte) (*NpcFriendshipMissionAwardResult, error) {
	r := NewReader(payload)
	res := &NpcFriendshipMissionAwardResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read mission award result: %v", ErrInvalidPayload, err)
	}
	if res.IsReceive, err = r.ReadUint8(); err != nil {
		return res, nil
	}
	if res.NewExp, err = r.ReadInt32(); err != nil {
		return res, nil
	}
	if res.NpcID, err = r.ReadInt32(); err != nil {
		return res, nil
	}
	return res, nil
}

// NpcFriendshipGuessResultText 返回猜拳/掷骰子结果的可读文本。
func NpcFriendshipGuessResultText(code uint8) string {
	switch code {
	case NpcFriendshipResultWin:
		return "胜利"
	case NpcFriendshipResultLose:
		return "失败"
	case NpcFriendshipResultTie:
		return "平局"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// ParseNpcFriendshipInfoResult 解析 NPC 结交总览。
func ParseNpcFriendshipInfoResult(payload []byte) (*NpcFriendshipSnapshot, error) {
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read npc count: %v", ErrInvalidPayload, err)
	}
	snap := &NpcFriendshipSnapshot{Npcs: make([]NpcFriendshipInfo, 0, count)}
	for i := 0; i < int(count); i++ {
		var n NpcFriendshipInfo
		for _, field := range []*int32{&n.NpcID, &n.CurExp} {
			if *field, err = r.ReadInt32(); err != nil {
				return nil, fmt.Errorf("%w: failed to read npc[%d]: %v", ErrInvalidPayload, i, err)
			}
		}
		if n.IsFriend, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read npc[%d].is_friend: %v", ErrInvalidPayload, i, err)
		}
		if n.MissionType, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read npc[%d].mission_type: %v", ErrInvalidPayload, i, err)
		}
		for _, field := range []*uint8{&n.HasTakeMission, &n.IsComplete, &n.IsReceive} {
			if *field, err = r.ReadUint8(); err != nil {
				return nil, fmt.Errorf("%w: failed to read npc[%d] state: %v", ErrInvalidPayload, i, err)
			}
		}
		for _, field := range []*int32{&n.QuestionID, &n.ProgressArgs, &n.TownNpcID, &n.MissionItemID} {
			if *field, err = r.ReadInt32(); err != nil {
				return nil, fmt.Errorf("%w: failed to read npc[%d] tail: %v", ErrInvalidPayload, i, err)
			}
		}
		snap.Npcs = append(snap.Npcs, n)
	}
	if snap.FriendCount, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read friend_count: %v", ErrInvalidPayload, err)
	}
	if snap.MissionCount, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read mission_count: %v", ErrInvalidPayload, err)
	}
	return snap, nil
}

// ParseNpcFriendshipPresentInfoResult 解析送礼面板。
func ParseNpcFriendshipPresentInfoResult(payload []byte) (map[int32]int32, uint8, error) {
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, 0, fmt.Errorf("%w: failed to read present count: %v", ErrInvalidPayload, err)
	}
	used := make(map[int32]int32, count)
	for i := 0; i < int(count); i++ {
		presentID, err := r.ReadInt32()
		if err != nil {
			return nil, 0, fmt.Errorf("%w: failed to read present[%d].id: %v", ErrInvalidPayload, i, err)
		}
		useCount, err := r.ReadInt32()
		if err != nil {
			return nil, 0, fmt.Errorf("%w: failed to read present[%d].count: %v", ErrInvalidPayload, i, err)
		}
		used[presentID] = useCount
	}
	maxGive, err := r.ReadUint8()
	if err != nil {
		return nil, 0, fmt.Errorf("%w: failed to read max_give_present: %v", ErrInvalidPayload, err)
	}
	return used, maxGive, nil
}

// ParseNpcFriendshipGiveResult 解析送礼结果。
func ParseNpcFriendshipGiveResult(payload []byte) (*NpcFriendshipGiveResult, error) {
	r := NewReader(payload)
	res := &NpcFriendshipGiveResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read give result: %v", ErrInvalidPayload, err)
	}
	if res.Result != NpcFriendshipResultSuccess {
		return res, nil
	}
	if res.NewExp, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read give new_exp: %v", ErrInvalidPayload, err)
	}
	if res.NpcID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read give npc_id: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// ParseNpcFriendshipFriendResult 解析结交结果。
func ParseNpcFriendshipFriendResult(payload []byte) (*NpcFriendshipFriendResult, error) {
	r := NewReader(payload)
	res := &NpcFriendshipFriendResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read make friend result: %v", ErrInvalidPayload, err)
	}
	if res.Result != NpcFriendshipResultSuccess {
		return res, nil
	}
	if res.IsFriend, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read make friend is_friend: %v", ErrInvalidPayload, err)
	}
	if res.NpcID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read make friend npc_id: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// BuildNpcFriendshipInfoResultPacket 构造 NPC 结交信息回包，供离线 mock 与单测使用。
func BuildNpcFriendshipInfoResultPacket(snap NpcFriendshipSnapshot) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(snap.Npcs)))
	for _, n := range snap.Npcs {
		w.WriteInt32(n.NpcID)
		w.WriteInt32(n.CurExp)
		w.WriteUint8(n.IsFriend)
		w.WriteInt32(n.MissionType)
		w.WriteUint8(n.HasTakeMission)
		w.WriteUint8(n.IsComplete)
		w.WriteUint8(n.IsReceive)
		w.WriteInt32(n.QuestionID)
		w.WriteInt32(n.ProgressArgs)
		w.WriteInt32(n.TownNpcID)
		w.WriteInt32(n.MissionItemID)
	}
	w.WriteInt32(snap.FriendCount)
	w.WriteInt32(snap.MissionCount)
	return NewPacket(ActionIDNpcFriendshipInfo, w.Bytes())
}

// BuildNpcFriendshipPresentInfoResultPacket 构造送礼信息回包，供离线 mock 与单测使用。
func BuildNpcFriendshipPresentInfoResultPacket(used map[int32]int32, maxGive uint8) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(used)))
	for id, count := range used {
		w.WriteInt32(id)
		w.WriteInt32(count)
	}
	w.WriteUint8(maxGive)
	return NewPacket(ActionIDNpcFriendshipPresentInfo, w.Bytes())
}

// BuildNpcFriendshipGiveResultPacket 构造送礼回包，供离线 mock 与单测使用。
func BuildNpcFriendshipGiveResultPacket(res NpcFriendshipGiveResult) *Packet {
	w := NewWriter()
	w.WriteUint8(res.Result)
	if res.Result == NpcFriendshipResultSuccess {
		w.WriteInt32(res.NewExp)
		w.WriteInt32(res.NpcID)
	}
	return NewPacket(ActionIDNpcFriendshipGivePresent, w.Bytes())
}

// BuildNpcFriendshipFriendResultPacket 构造结交回包，供离线 mock 与单测使用。
func BuildNpcFriendshipFriendResultPacket(res NpcFriendshipFriendResult) *Packet {
	w := NewWriter()
	w.WriteUint8(res.Result)
	if res.Result == NpcFriendshipResultSuccess {
		w.WriteUint8(res.IsFriend)
		w.WriteInt32(res.NpcID)
	}
	return NewPacket(ActionIDNpcFriendshipMakeFriend, w.Bytes())
}

// BuildNpcFriendshipActionCodePacket 构造单结果码回包，供离线 mock 与单测使用。
func BuildNpcFriendshipActionCodePacket(actionID uint32, code uint8) *Packet {
	return NewPacket(actionID, []byte{code})
}

// BuildNpcFriendshipTakeMissionResultPacket 构造接取任务回包，供离线 mock 与单测使用。
func BuildNpcFriendshipTakeMissionResultPacket(res NpcFriendshipTakeMissionResult) *Packet {
	w := NewWriter()
	w.WriteUint8(res.Result)
	w.WriteUint8(res.HasTakeMission)
	w.WriteInt32(res.NpcID)
	w.WriteInt32(res.MissionCount)
	return NewPacket(ActionIDNpcFriendshipTakeMission, w.Bytes())
}

// BuildNpcFriendshipAnswerResultPacket 构造回答问答回包，供离线 mock 与单测使用。
func BuildNpcFriendshipAnswerResultPacket(res NpcFriendshipAnswerResult) *Packet {
	w := NewWriter()
	w.WriteUint8(res.Result)
	w.WriteInt32(res.NewExp)
	w.WriteInt32(res.NpcID)
	return NewPacket(ActionIDNpcFriendshipAnswerQuestion, w.Bytes())
}

// BuildNpcFriendshipCompleteResultPacket 构造完成任务回包，供离线 mock 与单测使用。
func BuildNpcFriendshipCompleteResultPacket(res NpcFriendshipCompleteResult) *Packet {
	w := NewWriter()
	w.WriteUint8(res.Result)
	w.WriteInt32(res.NpcID)
	return NewPacket(ActionIDNpcFriendshipCompleteMission, w.Bytes())
}

// BuildNpcFriendshipGuessResultPacket 构造猜拳/掷骰子回包，供离线 mock 与单测使用。
func BuildNpcFriendshipGuessResultPacket(actionID uint32, res NpcFriendshipGuessResult) *Packet {
	w := NewWriter()
	w.WriteUint8(res.Result)
	w.WriteInt32(res.MyGuess)
	w.WriteInt32(res.NpcGuess)
	return NewPacket(actionID, w.Bytes())
}

// BuildNpcFriendshipMissionAwardResultPacket 构造领取任务奖励回包，供离线 mock 与单测使用。
func BuildNpcFriendshipMissionAwardResultPacket(res NpcFriendshipMissionAwardResult) *Packet {
	w := NewWriter()
	w.WriteUint8(res.Result)
	w.WriteUint8(res.IsReceive)
	w.WriteInt32(res.NewExp)
	w.WriteInt32(res.NpcID)
	return NewPacket(ActionIDNpcFriendshipGainMissionAward, w.Bytes())
}
