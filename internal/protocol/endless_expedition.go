// Package protocol 提供无尽洞天 (Mod_EndlessExpedition_Base, Module 797 / 0x31D) 协议。
//
// 抓包基准: /tmp/henduo.pcapng（2026-11-14，qq s0）：
//
//	20 get_activity_state 活动状态 [Int]
//	21 endless_panel      面板 [UByte result, Int×11, [Int], Int×4]，第 2 个 Int 为当前层
//	22 get_monster_team_info [Int layer]
//	23 endless_challenge     [Int layer] → 战报
//	32 get_ban_list          [Int layer]
package protocol

import "fmt"

const (
	// ActionIDEndlessActivityState 活动状态 (797/20)
	ActionIDEndlessActivityState uint32 = 0x031D0014
	// ActionIDEndlessPanel 无尽洞天面板 (797/21)
	ActionIDEndlessPanel uint32 = 0x031D0015
	// ActionIDEndlessMonsterTeamInfo 怪物队伍信息 (797/22)
	ActionIDEndlessMonsterTeamInfo uint32 = 0x031D0016
	// ActionIDEndlessChallenge 挑战当前层 (797/23)
	ActionIDEndlessChallenge uint32 = 0x031D0017
	// ActionIDEndlessBanList 禁选列表 (797/32)
	ActionIDEndlessBanList uint32 = 0x031D0020
)

// 797 结果码。
const (
	EndlessSuccess      uint8 = 0
	EndlessFailed       uint8 = 1
	EndlessNotSatisfied uint8 = 2
	EndlessNoPower      uint8 = 5
	EndlessItemPackFull uint8 = 6
	EndlessNoAward      uint8 = 7
	EndlessNotPass      uint8 = 8
	EndlessIsGet        uint8 = 9
	EndlessFinalMission uint8 = 10
	EndlessNotOpen      uint8 = 11
	EndlessWarCD        uint8 = 13
)

// EndlessPanel 表示无尽洞天面板关键字段。
type EndlessPanel struct {
	Result uint8
	// Values 面板前 11 个 Int；抓包中第 2 个 Int 为当前层。
	Values [11]int32
}

// Layer 返回当前层编号。
func (p *EndlessPanel) Layer() int32 { return p.Values[1] }

func endlessPrevPacket(actionID uint32, layer int32, hasLayer bool) *Packet {
	payload := make([]byte, 0, 8)
	if hasLayer {
		payload = append(payload, byte(layer>>24), byte(layer>>16), byte(layer>>8), byte(layer))
	}
	payload = append(payload, 0, 0, 0, 0)
	pkt := NewPacket(actionID, payload)
	pkt.HasPrevAct = true
	return pkt
}

// BuildEndlessActivityStatePacket 构造活动状态查询 (797/20)。
func BuildEndlessActivityStatePacket() *Packet {
	return endlessPrevPacket(ActionIDEndlessActivityState, 0, false)
}

// BuildEndlessPanelPacket 构造面板查询 (797/21)。
func BuildEndlessPanelPacket() *Packet {
	return endlessPrevPacket(ActionIDEndlessPanel, 0, false)
}

// BuildEndlessBanListPacket 构造禁选列表 (797/32)。
func BuildEndlessBanListPacket(layer int32) *Packet {
	return endlessPrevPacket(ActionIDEndlessBanList, layer, true)
}

// BuildEndlessMonsterTeamInfoPacket 构造怪物队伍信息 (797/22)。
func BuildEndlessMonsterTeamInfoPacket(layer int32) *Packet {
	return endlessPrevPacket(ActionIDEndlessMonsterTeamInfo, layer, true)
}

// BuildEndlessChallengePacket 构造挑战请求 (797/23)。
func BuildEndlessChallengePacket(layer int32) *Packet {
	return endlessPrevPacket(ActionIDEndlessChallenge, layer, true)
}

// ParseEndlessPanel 解析 797/21。
func ParseEndlessPanel(payload []byte) (*EndlessPanel, error) {
	p := DecompressPayloadIfNeeded(payload)
	r := NewReader(p)
	out := &EndlessPanel{}
	var err error
	if out.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: endless result: %v", ErrInvalidPayload, err)
	}
	for i := 0; i < len(out.Values); i++ {
		if out.Values[i], err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: endless value[%d]: %v", ErrInvalidPayload, i, err)
		}
	}
	return out, nil
}

// ParseEndlessResult 解析单字节结果码。
func ParseEndlessResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty endless result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// ParseEndlessActivityState 解析 797/20 的 Int。
func ParseEndlessActivityState(payload []byte) (int32, error) {
	p := DecompressPayloadIfNeeded(payload)
	if len(p) < 4 {
		return 0, fmt.Errorf("%w: endless activity state too short", ErrInvalidPayload)
	}
	return int32(uint32(p[0])<<24 | uint32(p[1])<<16 | uint32(p[2])<<8 | uint32(p[3])), nil
}

// EndlessResultText 返回 797 结果码文本。
func EndlessResultText(code uint8) string {
	switch code {
	case EndlessSuccess:
		return "成功"
	case EndlessFailed:
		return "失败"
	case EndlessNotSatisfied:
		return "条件不满足"
	case EndlessNoPower:
		return "体力不足"
	case EndlessItemPackFull:
		return "背包已满"
	case EndlessNoAward:
		return "暂无奖励"
	case EndlessNotPass:
		return "未通关"
	case EndlessIsGet:
		return "已领取"
	case EndlessFinalMission:
		return "已是最终层"
	case EndlessNotOpen:
		return "未开放"
	case EndlessWarCD:
		return "战斗冷却中"
	default:
		return "未知结果码"
	}
}

// BuildEndlessPanelResultPacket 构造面板回包，供离线 mock 与单测使用。
func BuildEndlessPanelResultPacket(result uint8, values [11]int32) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	for _, v := range values {
		w.WriteInt32(v)
	}
	w.WriteUint16(0)
	for i := 0; i < 4; i++ {
		w.WriteInt32(0)
	}
	return NewPacket(ActionIDEndlessPanel, w.Bytes())
}
