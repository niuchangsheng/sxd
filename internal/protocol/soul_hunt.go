// Package protocol 提供灵域猎妖 (Mod_SoulHunt_Base, Module 669 / 0x29D) 协议。
//
// 抓包基准: /tmp/henduo.pcapng（2026-11-14，qq s0）。
//
//	0 get_activity_state  [Int]
//	1 main_panel          [UByte result, Int×8, [Int,Int], Int×3]
//	3 challenge_panel     [Int step] → 关卡信息
//	5 start_challenge     [Int step] → 战报
//
// 所有请求尾部带 4 字节 PrevAct。
package protocol

import "fmt"

const (
	// ActionIDSoulHuntActivityState 活动状态 (669/0)
	ActionIDSoulHuntActivityState uint32 = 0x029D0000
	// ActionIDSoulHuntMainPanel 主面板 (669/1)
	ActionIDSoulHuntMainPanel uint32 = 0x029D0001
	// ActionIDSoulHuntChallengePanel 关卡面板 (669/3)
	ActionIDSoulHuntChallengePanel uint32 = 0x029D0003
	// ActionIDSoulHuntStartChallenge 发起挑战 (669/5)
	ActionIDSoulHuntStartChallenge uint32 = 0x029D0005
)

// SoulHunt 结果码。
const (
	SoulHuntSuccess         uint8 = 0
	SoulHuntFuncNotOpen     uint8 = 1
	SoulHuntFailed          uint8 = 2
	SoulHuntCantChallenge   uint8 = 3
	SoulHuntNoIngot         uint8 = 4
	SoulHuntNoChallengeTime uint8 = 5
	SoulHuntIsFighting      uint8 = 6
	SoulHuntNoPotential     uint8 = 7
	SoulHuntUpLevelLimit    uint8 = 8
)

// SoulHuntMainPanel 表示灵域猎妖主面板关键字段。
type SoulHuntMainPanel struct {
	Result uint8
	// Values 面板的前 8 个整型字段；抓包中第 2 个字段为当前关卡 (cur_step)。
	Values [8]int32
	// StepPairs 关卡列表 [Int,Int]（对应客户端 step_list）。
	StepPairs [][2]int32
	// Tail 列表之后的 3 个 Int，用于确认 is_can_challenge / 活动时间等字段。
	Tail [3]int32
}

// CurrentStep 返回当前关卡编号。
func (p *SoulHuntMainPanel) CurrentStep() int32 {
	return p.Values[1]
}

func soulHuntPrevPacket(actionID uint32, prefix []byte) *Packet {
	payload := make([]byte, 0, len(prefix)+4)
	payload = append(payload, prefix...)
	payload = append(payload, 0, 0, 0, 0)
	pkt := NewPacket(actionID, payload)
	pkt.HasPrevAct = true
	return pkt
}

// BuildSoulHuntActivityStatePacket 构造活动状态查询 (669/0)。
func BuildSoulHuntActivityStatePacket() *Packet {
	return soulHuntPrevPacket(ActionIDSoulHuntActivityState, nil)
}

// BuildSoulHuntMainPanelPacket 构造主面板查询 (669/1)。
func BuildSoulHuntMainPanelPacket() *Packet {
	return soulHuntPrevPacket(ActionIDSoulHuntMainPanel, nil)
}

// BuildSoulHuntChallengePanelPacket 构造关卡面板查询 (669/3)。
func BuildSoulHuntChallengePanelPacket(step int32) *Packet {
	w := NewWriter()
	w.WriteInt32(step)
	return soulHuntPrevPacket(ActionIDSoulHuntChallengePanel, w.Bytes())
}

// BuildSoulHuntStartChallengePacket 构造挑战请求 (669/5)。
func BuildSoulHuntStartChallengePacket(step int32) *Packet {
	w := NewWriter()
	w.WriteInt32(step)
	return soulHuntPrevPacket(ActionIDSoulHuntStartChallenge, w.Bytes())
}

// ParseSoulHuntMainPanel 解析 669/1 主面板。
func ParseSoulHuntMainPanel(payload []byte) (*SoulHuntMainPanel, error) {
	p := DecompressPayloadIfNeeded(payload)
	r := NewReader(p)
	out := &SoulHuntMainPanel{}
	var err error
	if out.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: soul hunt result: %v", ErrInvalidPayload, err)
	}
	for i := 0; i < len(out.Values); i++ {
		if out.Values[i], err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: soul hunt value[%d]: %v", ErrInvalidPayload, i, err)
		}
	}
	if n, e := r.ReadUint16(); e == nil {
		for i := 0; i < int(n); i++ {
			var pair [2]int32
			if pair[0], e = r.ReadInt32(); e != nil {
				break
			}
			if pair[1], e = r.ReadInt32(); e != nil {
				break
			}
			out.StepPairs = append(out.StepPairs, pair)
		}
	}
	for i := range out.Tail {
		if out.Tail[i], err = r.ReadInt32(); err != nil {
			break
		}
	}
	return out, nil
}

// ParseSoulHuntResult 解析单字节结果码。
func ParseSoulHuntResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty soul hunt result", ErrInvalidPayload)
	}
	return payload[0], nil
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
		return "无法挑战"
	case SoulHuntNoIngot:
		return "元宝不足"
	case SoulHuntNoChallengeTime:
		return "挑战次数不足"
	case SoulHuntIsFighting:
		return "正在战斗中"
	case SoulHuntNoPotential:
		return "潜力不足"
	case SoulHuntUpLevelLimit:
		return "升级上限"
	default:
		return "未知结果码"
	}
}

// BuildSoulHuntMainPanelResultPacket 构造主面板回包，供离线 mock 与单测使用。
func BuildSoulHuntMainPanelResultPacket(result uint8, values [8]int32) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	for _, v := range values {
		w.WriteInt32(v)
	}
	w.WriteUint16(0)
	w.WriteInt32(0)
	w.WriteInt32(0)
	w.WriteInt32(0)
	return NewPacket(ActionIDSoulHuntMainPanel, w.Bytes())
}
