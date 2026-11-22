// Package protocol 提供伏魔塔 (Mod_DemonTower_Base) 协议编解码。
package protocol

import (
	"fmt"
)

// 锁妖塔结果码 (Mod_RepressDemonTower_Base)。
const (
	// DemonTowerResultSuccess 成功
	DemonTowerResultSuccess uint8 = 0
	// DemonTowerResultFailed 失败
	DemonTowerResultFailed uint8 = 1
	// DemonTowerResultFunctionNoOpen 功能未开放
	DemonTowerResultFunctionNoOpen uint8 = 2
	// DemonTowerResultNotEnoughTicket 挑战次数不足
	DemonTowerResultNotEnoughTicket uint8 = 3
	// DemonTowerResultLayerError 层数错误
	DemonTowerResultLayerError uint8 = 4
	// DemonTowerResultItemPackFull 背包已满
	DemonTowerResultItemPackFull uint8 = 5
	// DemonTowerResultRaceStepError 赛季阶段错误
	DemonTowerResultRaceStepError uint8 = 6
	// DemonTowerResultNotEnoughIngot 元宝不足
	DemonTowerResultNotEnoughIngot uint8 = 7
	// DemonTowerResultBuyNumError 购买次数错误
	DemonTowerResultBuyNumError uint8 = 8
)

// DemonTowerStep 表示锁妖塔升阶条件条目 (step_list)。
type DemonTowerStep struct {
	// StepUpID 升阶条件 ID
	StepUpID int32
	// LayerNeed 需要通关层数
	LayerNeed int32
	// RoundNeed 需要总回合数
	RoundNeed int32
}

// DemonTowerLayer 表示锁妖塔已通关层条目 (layer_list)。
type DemonTowerLayer struct {
	// LayerID 层编号，也是 challenge 请求的目标 ID
	LayerID int32
	// FastestRound 该层最快通关回合数
	FastestRound int32
}

// DemonTowerPanel 表示锁妖塔主面板 panel_info 回包。
type DemonTowerPanel struct {
	// Result 业务结果码 (DemonTowerResult*)
	Result uint8
	// IsFirst 是否本赛季首次进入
	IsFirst int32
	// Season 赛季编号
	Season int32
	// LeftTime 本赛季剩余秒数
	LeftTime int32
	// NowStep 当前阶数
	NowStep int32
	// NowFinishLayer 当前已通关层数
	NowFinishLayer int32
	// NowTotalRound 当前累计回合数
	NowTotalRound int32
	// NextLayerLeftTime 下一层剩余时间
	NextLayerLeftTime int32
	// Rank 当前排名
	Rank int32
	// StepList 升阶条件列表
	StepList []DemonTowerStep
	// LayerList 已通关层列表
	LayerList []DemonTowerLayer
	// BuyTicketNum 可购买次数
	BuyTicketNum int32
	// NowTicketNum 当前剩余挑战次数
	NowTicketNum int32
	// RaceStep 赛季阶段编号
	RaceStep int32
	// Ranks 排名相关标记
	Ranks int32
}

// NextLayerID 返回当前可挑战的层 ID。
//
// 线上实测（2026-11-07 22:14，s0-niuchangsheng）：
//
//	now_finish_layer=40 时 challenge(40) 返回 SUCCESS(0)，
//	challenge(41) 返回 LAYER_ERROR(4)。
//
// 因此 now_finish_layer / layer_list 的最大值就是当前可挑战层，
// 不是“已通关层数 + 1”。无记录时从第 1 层开始。
func (p *DemonTowerPanel) NextLayerID() int32 {
	var current int32
	for _, l := range p.LayerList {
		if l.LayerID > current {
			current = l.LayerID
		}
	}
	if p.NowFinishLayer > current {
		current = p.NowFinishLayer
	}
	if current <= 0 {
		return 1
	}
	return current
}

// StepUpRequirement 返回当前阶升阶条件 (step_list 第一条)。
// 线上实测 2026-11-11：step_list=[{1,40,434}]，now_finish_layer=40，
// now_total_round=521 > 434，challenge(40) 返回 RACE_STEP_ERROR(6)，
// 即层数已满但回合数超标，无法升阶。
func (p *DemonTowerPanel) StepUpRequirement() (layerNeed, roundNeed int32, ok bool) {
	if p == nil || len(p.StepList) == 0 {
		return 0, 0, false
	}
	return p.StepList[0].LayerNeed, p.StepList[0].RoundNeed, true
}

// BuildDemonTowerPanelPacket 构造查询锁妖塔主面板封包 (Module 705, Action 0)。
//
// 官方 request 为空数组；实服验证空载荷可直接返回面板数据。
func BuildDemonTowerPanelPacket() *Packet {
	return NewPacket(ActionIDDemonTowerPanel, nil)
}

// BuildDemonTowerBaseInfoPacket 构造查询锁妖塔赛季开放状态封包 (Module 705, Action 8)。
func BuildDemonTowerBaseInfoPacket() *Packet {
	return NewPacket(ActionIDDemonTowerBaseInfo, nil)
}

// BuildDemonTowerRankPacket 构造查询锁妖塔排行榜封包 (Module 705, Action 5)。
func BuildDemonTowerRankPacket() *Packet {
	return NewPacket(ActionIDDemonTowerRank, nil)
}

// BuildDemonTowerChallengePacket 构造挑战锁妖塔指定层封包 (Module 705, Action 1)。
//
// request 仅含 1 个 IntUtil (目标层 ID)。注意不能追加 PrevAct 尾部：
// 会话层对 HasPrevAct 包会用最近动作号覆盖末尾 4 字节，从而破坏层号。
func BuildDemonTowerChallengePacket(layerID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(layerID)
	return NewPacket(ActionIDDemonTowerChallenge, w.Bytes())
}

// ParseDemonTowerBaseInfoResult 解析赛季开放状态回包 (Module 705, Action 8)。
// 回包为单个 Byte，1 表示处于赛季时间，0 表示赛季未开放。
func ParseDemonTowerBaseInfoResult(payload []byte) (bool, error) {
	if len(payload) == 0 {
		return false, fmt.Errorf("%w: demon tower base_info payload is empty", ErrInvalidPayload)
	}
	return payload[0] != 0, nil
}

// ParseDemonTowerRankSelfLayer 解析排行榜回包 (Module 705, Action 5) 的自身最高层。
//
// 回包前 5 个字段: [UByte result, Int self_layer, Int self_round, Int self_finish_ts, Int role_id, ...]。
// self_layer 为赛季累计通关层数 (跨阶次全局递增)，用于在 layer_list 为空时计算下一层目标。
func ParseDemonTowerRankSelfLayer(payload []byte) (uint8, int32, error) {
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return 0, 0, fmt.Errorf("%w: failed to read demon tower rank result: %v", ErrInvalidPayload, err)
	}
	if result != DemonTowerResultSuccess {
		return result, 0, nil
	}
	selfLayer, err := r.ReadInt32()
	if err != nil {
		return result, 0, fmt.Errorf("%w: failed to read demon tower rank self_layer: %v", ErrInvalidPayload, err)
	}
	return result, selfLayer, nil
}

// ParseDemonTowerPanelResult 解析锁妖塔主面板回包 (Module 705, Action 0)。
//
// 回包字段顺序对齐 com.datas.RepressDemonTowerData.panel_info:
// [UByte result, Int×8, [[Int,Int,Int]], [[Int,Int]], Int×4]。
func ParseDemonTowerPanelResult(payload []byte) (*DemonTowerPanel, error) {
	r := NewReader(payload)
	res := &DemonTowerPanel{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read demon tower result: %v", ErrInvalidPayload, err)
	}
	if res.Result != DemonTowerResultSuccess {
		return res, nil
	}

	fields := []*int32{
		&res.IsFirst, &res.Season, &res.LeftTime, &res.NowStep,
		&res.NowFinishLayer, &res.NowTotalRound, &res.NextLayerLeftTime, &res.Rank,
	}
	for i, dst := range fields {
		if *dst, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read demon tower field[%d]: %v", ErrInvalidPayload, i, err)
		}
	}

	stepCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read demon tower step count: %v", ErrInvalidPayload, err)
	}
	res.StepList = make([]DemonTowerStep, 0, stepCount)
	for i := 0; i < int(stepCount); i++ {
		var step DemonTowerStep
		if step.StepUpID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read demon tower step[%d].step_up_id: %v", ErrInvalidPayload, i, err)
		}
		if step.LayerNeed, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read demon tower step[%d].layer_need: %v", ErrInvalidPayload, i, err)
		}
		if step.RoundNeed, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read demon tower step[%d].round_need: %v", ErrInvalidPayload, i, err)
		}
		res.StepList = append(res.StepList, step)
	}

	layerCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read demon tower layer count: %v", ErrInvalidPayload, err)
	}
	res.LayerList = make([]DemonTowerLayer, 0, layerCount)
	for i := 0; i < int(layerCount); i++ {
		var layer DemonTowerLayer
		if layer.LayerID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read demon tower layer[%d].layer_id: %v", ErrInvalidPayload, i, err)
		}
		if layer.FastestRound, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read demon tower layer[%d].fastest_round: %v", ErrInvalidPayload, i, err)
		}
		res.LayerList = append(res.LayerList, layer)
	}

	tail := []*int32{&res.BuyTicketNum, &res.NowTicketNum, &res.RaceStep, &res.Ranks}
	for i, dst := range tail {
		if *dst, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read demon tower tail field[%d]: %v", ErrInvalidPayload, i, err)
		}
	}
	return res, nil
}

// ParseDemonTowerChallengeResult 解析挑战回包 (Module 705, Action 1) 的首字节结果码。
//
// 成功后服务端还会下发大型战报与奖励列表，自动挑战只需结果码，
// 无需完整解析战报结构。
func ParseDemonTowerChallengeResult(payload []byte) (uint8, error) {
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return 0, fmt.Errorf("%w: failed to read demon tower challenge result: %v", ErrInvalidPayload, err)
	}
	return result, nil
}

// --------------------------------------------------------------------------------
// 10. 山河游历
// --------------------------------------------------------------------------------
