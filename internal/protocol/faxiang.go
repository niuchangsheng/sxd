// Package protocol 提供主角法相 (Mod_MainRoleFaXiang_Base, Module 518) 协议编解码。
//
// 协议基准: 2026-11-21 官方客户端抓包 /tmp/faxiang3.pcapng
// (10.0.0.13:61029 ↔ 81.70.56.121:8200)。法相页面「造物佛手」与「守护魔兽」
// 均走 Module 518，而不是旧实现的 Mod_ExtremeTrial (721)：
//
//	518/11 [空 + PrevAct]                  extra_fo_info        造物佛手面板
//	518/12 [空 + PrevAct]                  extra_mo_info        守护魔兽面板
//	518/13 [空 + PrevAct]                  extra_pray           祈愿（随机 3 个炼化奖励）
//	518/14 [空 + PrevAct]                  extra_refresh_pray   元宝重随，自动化禁用
//	518/15 [Int award_id][PrevAct]         extra_choose_pray_award 选择奖励并开始炼化
//	518/16 [空 + PrevAct]                  extra_mo_challenge   守护魔兽挑战（战报）
//	518/17 [空 + PrevAct]                  gain_pray_award      领取炼化完成奖励
//
// 回包布局（抓包实测）:
//
//	518/11 [Int level][Int exp][Short n][n×Int award_id][Int cur_award_id][Int refine_finish_time]
//	518/12 [Int level][Int exp][Int next_can_fight_time][Int stunt][Int supernatural][Int pattern]
//	518/13 [UByte result][Short n][n×Int award_id]
//	518/15 [UByte result][Int refine_finish_time]
//	518/16 [UByte result][Int kill_num][战报...]
//	518/17 [UByte result]
package protocol

import "fmt"

const (
	// ActionIDFaxiangGetPanelInfo 法相主面板 (518/0)
	ActionIDFaxiangGetPanelInfo uint32 = 0x02060000
	// ActionIDFaxiangExtraFoInfo 造物佛手面板 (518/11)
	ActionIDFaxiangExtraFoInfo uint32 = 0x0206000B
	// ActionIDFaxiangExtraMoInfo 守护魔兽面板 (518/12)
	ActionIDFaxiangExtraMoInfo uint32 = 0x0206000C
	// ActionIDFaxiangExtraPray 祈愿随机三种炼化奖励 (518/13)
	ActionIDFaxiangExtraPray uint32 = 0x0206000D
	// ActionIDFaxiangExtraRefreshPray 元宝重随炼化奖励 (518/14)，自动化禁止调用
	ActionIDFaxiangExtraRefreshPray uint32 = 0x0206000E
	// ActionIDFaxiangExtraChoosePrayAward 选择奖励并开始炼化 (518/15)
	ActionIDFaxiangExtraChoosePrayAward uint32 = 0x0206000F
	// ActionIDFaxiangExtraMoChallenge 守护魔兽挑战 (518/16)
	ActionIDFaxiangExtraMoChallenge uint32 = 0x02060010
	// ActionIDFaxiangGainPrayAward 领取炼化完成奖励 (518/17)
	ActionIDFaxiangGainPrayAward uint32 = 0x02060011
)

// 法相模块结果码 (Mod_MainRoleFaXiang_Base 常量)。
const (
	FaxiangSuccess          uint8 = 0  // SUCCESS
	FaxiangTaskNotComplete  uint8 = 1  // TASK_NOT_COMPLETE
	FaxiangFunctionNoOpen   uint8 = 2  // FUNCTION_NO_OPEN
	FaxiangHasFinish        uint8 = 3  // HAS_FINISH
	FaxiangItemNotEnough    uint8 = 4  // ITEM_NOT_ENOUGH
	FaxiangRefining         uint8 = 5  // REFINING
	FaxiangFailed           uint8 = 6  // FAILD
	FaxiangConditionLimit   uint8 = 7  // CONDITION_LIMIT
	FaxiangTaskNotFinishAll uint8 = 8  // TASK_NOT_FINISH_ALL
	FaxiangFailCD           uint8 = 9  // FAIL_CD
	FaxiangFighten          uint8 = 10 // FIGHTEN
	FaxiangNotEnoughMove    uint8 = 11 // NOT_ENOUGH_MOVE_COUNT
	FaxiangPointError       uint8 = 12 // POINT_ERROR
	FaxiangCanNotFight      uint8 = 13 // CAN_NOT_FIGHT
	FaxiangNotMonster       uint8 = 14 // NOT_MONSTER
	FaxiangNotEnoughItem    uint8 = 15 // NOT_ENOUGH_ITEM
	FaxiangHasUsed          uint8 = 16 // HAS_USED
	FaxiangBuilding         uint8 = 17 // BUILDING
	FaxiangMinLevelLimit    uint8 = 18 // MIN_LEVEL_LIMIT
	FaxiangErrorPos         uint8 = 19 // ERROR_POS
	FaxiangNotEnoughRes     uint8 = 20 // NOT_ENOUGH_RESOURCE
	FaxiangNotFighted       uint8 = 21 // NOT_FIGHTED
	FaxiangHasChallenge     uint8 = 22 // HAS_CHALLENGE
	FaxiangErrorRoleJob     uint8 = 23 // ERROR_ROLE_JOB
	FaxiangCanNotPray       uint8 = 24 // CAN_NOT_PRAY
	FaxiangNotEnoughIngot   uint8 = 25 // NOT_ENOUGH_INGOT
	FaxiangErrorPrayAwardID uint8 = 26 // ERROR_PRAY_AWARD_ID
	FaxiangNotEnoughDaoYuan uint8 = 27 // NOT_ENOUGH_DAO_YUAN
	FaxiangTimeLimit        uint8 = 28 // TIME_LIMIT
	FaxiangNotPrayAward     uint8 = 29 // NOT_PRAY_AWARD
	FaxiangPackageFull      uint8 = 30 // PACKAGE_FULL
)

// FaxiangExtraFoInfo 造物佛手面板 (518/11)。
type FaxiangExtraFoInfo struct {
	// Level 造物佛手熟练度等级
	Level int32
	// CurExp 当前熟练度经验
	CurExp int32
	// AwardList 已祈愿但尚未选择/炼化的奖励 ID 列表
	AwardList []int32
	// CurPrayAwardID 当前正在炼化的奖励 ID，0 表示没有
	CurPrayAwardID int32
	// RefineFinishTime 当前炼化完成时间（服务器时间戳）
	RefineFinishTime int32
}

// FaxiangExtraMoInfo 守护魔兽面板 (518/12)。
type FaxiangExtraMoInfo struct {
	// Level 守护魔兽等级
	Level int32
	// CurExp 当前熟练度经验
	CurExp int32
	// NextCanFightTime 下次可挑战时间（服务器时间戳），0 表示可挑战
	NextCanFightTime int32
	// RandomStuntID 魔兽绝技 ID
	RandomStuntID int32
	// RandomSupernaturalID 魔兽神通 ID
	RandomSupernaturalID int32
	// RandomSpecialPattern int32 魔兽特殊阵法 ID
	RandomSpecialPattern int32
}

// FaxiangPrayOptions 祈愿结果 (518/13)。
type FaxiangPrayOptions struct {
	// Result 结果码
	Result uint8
	// AwardIDs 随机出的可炼化奖励 ID 列表
	AwardIDs []int32
}

// FaxiangChoosePrayAwardResult 选择炼化奖励结果 (518/15)。
type FaxiangChoosePrayAwardResult struct {
	// Result 结果码
	Result uint8
	// RefineFinishTime 本次炼化完成时间（服务器时间戳）
	RefineFinishTime int32
}

// FaxiangMoChallengeResult 守护魔兽挑战结果 (518/16)。
type FaxiangMoChallengeResult struct {
	// Result 结果码
	Result uint8
	// KillNum 击杀数/熟练度收益
	KillNum int32
}

// newFaxiangPrevActPacket 构造带 4 字节 PrevAct 占位的 518 请求。
// 官方客户端所有 Module 518 请求末尾都携带 PrevAct，会话发送层会填值。
func newFaxiangPrevActPacket(actionID uint32, payload []byte) *Packet {
	body := make([]byte, len(payload)+4)
	copy(body, payload)
	pkt := NewPacket(actionID, body)
	pkt.HasPrevAct = true
	return pkt
}

// BuildFaxiangGetPanelInfoPacket 构造法相主面板封包 (518/0)。
// 官方客户端先请求主面板，再按 isFoOk/isMoOk 请求 518/11、518/12。
func BuildFaxiangGetPanelInfoPacket() *Packet {
	return newFaxiangPrevActPacket(ActionIDFaxiangGetPanelInfo, nil)
}

// BuildFaxiangExtraFoInfoPacket 构造造物佛手面板封包 (518/11)。
func BuildFaxiangExtraFoInfoPacket() *Packet {
	return newFaxiangPrevActPacket(ActionIDFaxiangExtraFoInfo, nil)
}

// BuildFaxiangExtraMoInfoPacket 构造守护魔兽面板封包 (518/12)。
func BuildFaxiangExtraMoInfoPacket() *Packet {
	return newFaxiangPrevActPacket(ActionIDFaxiangExtraMoInfo, nil)
}

// BuildFaxiangExtraPrayPacket 构造祈愿封包 (518/13)。
func BuildFaxiangExtraPrayPacket() *Packet {
	return newFaxiangPrevActPacket(ActionIDFaxiangExtraPray, nil)
}

// BuildFaxiangExtraChoosePrayAwardPacket 构造选择奖励并开始炼化封包 (518/15)。
func BuildFaxiangExtraChoosePrayAwardPacket(awardID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(awardID)
	return newFaxiangPrevActPacket(ActionIDFaxiangExtraChoosePrayAward, w.Bytes())
}

// BuildFaxiangExtraMoChallengePacket 构造守护魔兽挑战封包 (518/16)。
func BuildFaxiangExtraMoChallengePacket() *Packet {
	return newFaxiangPrevActPacket(ActionIDFaxiangExtraMoChallenge, nil)
}

// BuildFaxiangGainPrayAwardPacket 构造领取炼化完成奖励封包 (518/17)。
func BuildFaxiangGainPrayAwardPacket() *Packet {
	return newFaxiangPrevActPacket(ActionIDFaxiangGainPrayAward, nil)
}

// ParseFaxiangExtraFoInfo 解析 518/11 造物佛手面板。
func ParseFaxiangExtraFoInfo(payload []byte) (*FaxiangExtraFoInfo, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	info := &FaxiangExtraFoInfo{}
	var err error
	if info.Level, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("法相造物佛手等级读取失败: %w", err)
	}
	if info.CurExp, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("法相造物佛手熟练度读取失败: %w", err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("法相造物佛手奖励数量读取失败: %w", err)
	}
	info.AwardList = make([]int32, 0, count)
	for i := 0; i < int(count); i++ {
		id, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("法相造物佛手奖励[%d]读取失败: %w", i, err)
		}
		info.AwardList = append(info.AwardList, id)
	}
	if info.CurPrayAwardID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("法相造物佛手当前炼化奖励读取失败: %w", err)
	}
	if info.RefineFinishTime, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("法相造物佛手炼化完成时间读取失败: %w", err)
	}
	return info, nil
}

// ParseFaxiangExtraMoInfo 解析 518/12 守护魔兽面板。
func ParseFaxiangExtraMoInfo(payload []byte) (*FaxiangExtraMoInfo, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	info := &FaxiangExtraMoInfo{}
	fields := []struct {
		name string
		dst  *int32
	}{
		{"等级", &info.Level},
		{"熟练度", &info.CurExp},
		{"下次挑战时间", &info.NextCanFightTime},
		{"绝技", &info.RandomStuntID},
		{"神通", &info.RandomSupernaturalID},
		{"特殊阵法", &info.RandomSpecialPattern},
	}
	for _, f := range fields {
		v, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("法相守护魔兽%s读取失败: %w", f.name, err)
		}
		*f.dst = v
	}
	return info, nil
}

// ParseFaxiangExtraPrayResult 解析 518/13 祈愿结果。
func ParseFaxiangExtraPrayResult(payload []byte) (*FaxiangPrayOptions, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	result, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("法相祈愿结果码读取失败: %w", err)
	}
	out := &FaxiangPrayOptions{Result: result}
	if result != FaxiangSuccess {
		return out, nil
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("法相祈愿奖励数量读取失败: %w", err)
	}
	out.AwardIDs = make([]int32, 0, count)
	for i := 0; i < int(count); i++ {
		id, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("法相祈愿奖励[%d]读取失败: %w", i, err)
		}
		out.AwardIDs = append(out.AwardIDs, id)
	}
	return out, nil
}

// ParseFaxiangChoosePrayAwardResult 解析 518/15 选择炼化奖励结果。
func ParseFaxiangChoosePrayAwardResult(payload []byte) (*FaxiangChoosePrayAwardResult, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	result, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("法相选择炼化奖励结果码读取失败: %w", err)
	}
	out := &FaxiangChoosePrayAwardResult{Result: result}
	if result != FaxiangSuccess {
		return out, nil
	}
	if out.RefineFinishTime, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("法相炼化完成时间读取失败: %w", err)
	}
	return out, nil
}

// ParseFaxiangGainPrayAwardResult 解析 518/17 领取炼化奖励结果码。
func ParseFaxiangGainPrayAwardResult(payload []byte) (uint8, error) {
	if len(payload) < 1 {
		return 0, fmt.Errorf("法相领取炼化奖励结果码为空")
	}
	return payload[0], nil
}

// ParseFaxiangExtraMoChallengeResult 解析 518/16 守护魔兽挑战结果。
// 回包在结果码与击杀数之后是一份完整战报，本函数只读取到 kill_num。
func ParseFaxiangExtraMoChallengeResult(payload []byte) (*FaxiangMoChallengeResult, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	result, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("法相守护魔兽挑战结果码读取失败: %w", err)
	}
	out := &FaxiangMoChallengeResult{Result: result}
	if result != FaxiangSuccess {
		return out, nil
	}
	if r.Remaining() < 4 {
		return nil, fmt.Errorf("法相守护魔兽挑战击杀数缺失")
	}
	if out.KillNum, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("法相守护魔兽挑战击杀数读取失败: %w", err)
	}
	return out, nil
}

// FaxiangResultText 返回法相结果码文本。
func FaxiangResultText(code uint8) string {
	switch code {
	case FaxiangSuccess:
		return "成功"
	case FaxiangTaskNotComplete:
		return "任务未完成"
	case FaxiangFunctionNoOpen:
		return "功能未开放"
	case FaxiangHasFinish:
		return "已完成"
	case FaxiangItemNotEnough:
		return "物品不足"
	case FaxiangRefining:
		return "炼化中"
	case FaxiangFailed:
		return "失败"
	case FaxiangConditionLimit:
		return "条件不满足"
	case FaxiangTaskNotFinishAll:
		return "任务未全部完成"
	case FaxiangFailCD:
		return "冷却中"
	case FaxiangNotEnoughMove:
		return "移动次数不足"
	case FaxiangPointError:
		return "坐标错误"
	case FaxiangCanNotFight:
		return "不可挑战"
	case FaxiangNotMonster:
		return "不是魔兽"
	case FaxiangNotEnoughItem:
		return "道具不足"
	case FaxiangHasUsed:
		return "已使用"
	case FaxiangBuilding:
		return "建造中"
	case FaxiangMinLevelLimit:
		return "等级不足"
	case FaxiangErrorPos:
		return "位置错误"
	case FaxiangNotEnoughRes:
		return "资源不足"
	case FaxiangNotFighted:
		return "尚未挑战"
	case FaxiangHasChallenge:
		return "已挑战/冷却中"
	case FaxiangErrorRoleJob:
		return "职业不符"
	case FaxiangCanNotPray:
		return "当前不可祈愿"
	case FaxiangNotEnoughIngot:
		return "元宝不足"
	case FaxiangErrorPrayAwardID:
		return "奖励 ID 错误"
	case FaxiangNotEnoughDaoYuan:
		return "道缘不足"
	case FaxiangTimeLimit:
		return "时间未到"
	case FaxiangNotPrayAward:
		return "炼化尚未完成"
	case FaxiangPackageFull:
		return "背包已满"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}
