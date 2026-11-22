// Package protocol 提供纪元造化秘境 (Mod_EraCreationRealm_Base, Module 866) 协议编解码。
package protocol

import (
	"fmt"
)

// 纪元造化秘境动作 ID。
const (
	ActionIDEraCreationSeason     uint32 = 0x03620000
	ActionIDEraCreationPanel      uint32 = 0x03620001
	ActionIDEraCreationExplore    uint32 = 0x03620006
	ActionIDEraCreationFightEvent uint32 = 0x03620007
	// ActionIDEraCreationFightDeploy 战斗布阵面板 (866/8, request=[])。
	// 官方 flow: fight_event(7) → fight_deploy(8) → deploy(9) → fight(10)；
	// 缺少布阵时 fight(10) 会被服务端静默丢弃。
	ActionIDEraCreationFightDeploy uint32 = 0x03620008
	// ActionIDEraCreationDeploy 布阵（866/9, request=[Int deploy_id, Int entity_id, UByte type]）。
	ActionIDEraCreationDeploy     uint32 = 0x03620009
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

// eraCreationPrevPacket 构造带 4 字节 PrevAct 占位的 866 请求；
// 抓包中官方客户端的 866 请求均带 PrevAct 尾部。
func eraCreationPrevPacket(actionID uint32, prefix []byte) *Packet {
	payload := make([]byte, 0, len(prefix)+4)
	payload = append(payload, prefix...)
	payload = append(payload, 0, 0, 0, 0)
	pkt := NewPacket(actionID, payload)
	pkt.HasPrevAct = true
	return pkt
}

// BuildEraCreationPanelPacket 构造纪元造化秘境面板查询封包。
func BuildEraCreationPanelPacket() *Packet {
	return eraCreationPrevPacket(ActionIDEraCreationPanel, nil)
}

// BuildEraCreationExplorePacket 构造探索封包。
func BuildEraCreationExplorePacket() *Packet {
	return eraCreationPrevPacket(ActionIDEraCreationExplore, nil)
}

// BuildEraCreationFightEventPacket 构造事件查询封包。
func BuildEraCreationFightEventPacket() *Packet {
	return eraCreationPrevPacket(ActionIDEraCreationFightEvent, nil)
}

// BuildEraCreationFightDeployPacket 构造战斗布阵面板查询封包 (866/8)。
func BuildEraCreationFightDeployPacket() *Packet {
	return eraCreationPrevPacket(ActionIDEraCreationFightDeploy, nil)
}

// BuildEraCreationDeployPacket 构造布阵封包 (866/9)。
// 官方 CommonDeployView 以 [deploy_id, entity_id, 0] 调用。
func BuildEraCreationDeployPacket(deployID, entityID int32, typ uint8) *Packet {
	w := NewWriter()
	w.WriteInt32(deployID)
	w.WriteInt32(entityID)
	w.WriteUint8(typ)
	return eraCreationPrevPacket(ActionIDEraCreationDeploy, w.Bytes())
}

// EraCreationDeployPanel 表示 866/8 布阵面板。
type EraCreationDeployPanel struct {
	DeployID int32
	Slots    [][2]int32
}

// ParseEraCreationDeployPanel 解析 866/8：result + deploy_id + [count][(pos, entity_id)]。
func ParseEraCreationDeployPanel(payload []byte) (*EraCreationDeployPanel, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	code, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: era creation deploy panel", ErrInvalidPayload)
	}
	if code != EraCreationSuccess {
		return nil, fmt.Errorf("era creation deploy panel result=%d", code)
	}
	out := &EraCreationDeployPanel{}
	if out.DeployID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: deploy id", ErrInvalidPayload)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: deploy slot count", ErrInvalidPayload)
	}
	for i := 0; i < int(count); i++ {
		var a, b int32
		if a, err = r.ReadInt32(); err != nil {
			return out, nil
		}
		if b, err = r.ReadInt32(); err != nil {
			return out, nil
		}
		out.Slots = append(out.Slots, [2]int32{a, b})
	}
	return out, nil
}

// BuildEraCreationFightPacket 构造挑战事件封包。
func BuildEraCreationFightPacket(eventID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(eventID)
	return eraCreationPrevPacket(ActionIDEraCreationFight, w.Bytes())
}

// BuildEraCreationAwardPanelPacket 构造战斗奖励面板查询封包。
func BuildEraCreationAwardPanelPacket() *Packet {
	return eraCreationPrevPacket(ActionIDEraCreationAwardPanel, nil)
}

// BuildEraCreationChooseAwardPacket 构造领取战斗奖励封包。
func BuildEraCreationChooseAwardPacket() *Packet {
	return eraCreationPrevPacket(ActionIDEraCreationChoose, nil)
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
	for i := 0; i < int(count); i++ {
		id, err := r.ReadInt32()
		if err != nil {
			break
		}
		out.IDs = append(out.IDs, id)
		// 每个事件为 4 个 Int，读取剩余 3 个字段保持对齐。
		for j := 0; j < 3; j++ {
			if _, err := r.ReadInt32(); err != nil {
				return out, nil
			}
		}
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
