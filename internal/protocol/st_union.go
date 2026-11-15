// Package protocol 提供仙盟上香与仙盟魔神挑战协议。
//
// 抓包基准: /tmp/xianmeng.pcapng（2026-11-15，qq s0，仙界跨服会话）：
//
//	仙盟上香 (Mod_StUnionActivity_Base, Module 179):
//	  10 get_st_union_god_info 面板
//	  11 st_union_god_incense [Int offer_id] → [UByte result, Short, Short, Int]
//
//	仙盟魔神挑战 (Mod_StUnionTask_Base, Module 337):
//	  0 get_challenge_info 面板
//	  1 fight 挑战（无参数）→ [UByte result, 战报]
//	  2 add_star / 3 buy_ticket（元宝购买，自动化禁止调用）
package protocol

import "fmt"

const (
	// ActionIDStUnionTreeInfo 仙盟许愿树面板 (179/0)
	ActionIDStUnionTreeInfo uint32 = 0x00B30000
	// ActionIDStUnionGodInfo 仙盟神像面板 (179/10)
	ActionIDStUnionGodInfo uint32 = 0x00B3000A
	// ActionIDStUnionGodIncense 仙盟上香 (179/11)
	ActionIDStUnionGodIncense uint32 = 0x00B3000B
	// ActionIDStUnionTaskInfo 仙盟魔神挑战面板 (337/0)
	ActionIDStUnionTaskInfo uint32 = 0x01510000
	// ActionIDStUnionTaskFight 仙盟魔神挑战 (337/1)
	ActionIDStUnionTaskFight uint32 = 0x01510001
	// ActionIDStUnionTaskBuyTicket 购买挑战券 (337/3)
	ActionIDStUnionTaskBuyTicket uint32 = 0x01510003
)

// 179 上香结果码（节选）。
const (
	StUnionIncenseSuccess    uint8 = 2
	StUnionIncenseHadGift    uint8 = 3
	StUnionIncenseNoCoin     uint8 = 17
	StUnionIncenseNoIngot    uint8 = 18
	StUnionIncenseNoVip      uint8 = 19
	StUnionIncenseLevelLimit uint8 = 20
	StUnionIncenseTodayLimit uint8 = 22
)

// 337 魔神挑战结果码。
const (
	StUnionTaskSuccess       uint8 = 0
	StUnionTaskNotInUnion    uint8 = 1
	StUnionTaskNoTicket      uint8 = 2
	StUnionTaskTimesUsed     uint8 = 3
	StUnionTaskPackFull      uint8 = 4
	StUnionTaskFailure       uint8 = 5
	StUnionTaskMaxLimit      uint8 = 6
	StUnionTaskNoIngot       uint8 = 7
	StUnionTaskBuyTimesLimit uint8 = 8
	StUnionTaskNoContribute  uint8 = 9
)

func stUnionPrevPacket(actionID uint32, prefix []byte) *Packet {
	payload := make([]byte, 0, len(prefix)+4)
	payload = append(payload, prefix...)
	payload = append(payload, 0, 0, 0, 0)
	pkt := NewPacket(actionID, payload)
	pkt.HasPrevAct = true
	return pkt
}

// BuildStUnionTreeInfoPacket 构造仙盟许愿树面板 (179/0)。
func BuildStUnionTreeInfoPacket() *Packet {
	return stUnionPrevPacket(ActionIDStUnionTreeInfo, nil)
}

// BuildStUnionGodInfoPacket 构造仙盟神像面板 (179/10)。
//
// 显式固定 PrevAct=179/0 且不置 HasPrevAct：与结缘宝箱 232/34 同样的教训，
// 若期间插入心跳/推送，会话层用「最后收包」覆盖会破坏上下文令牌，导致后续
// 179/11 上香被静默丢弃。
func BuildStUnionGodInfoPacket() *Packet {
	w := NewWriter()
	w.WriteUint32(ActionIDStUnionTreeInfo)
	return NewPacket(ActionIDStUnionGodInfo, w.Bytes())
}

// BuildStUnionIncensePacket 构造上香请求 (179/11)。
//
// offerID：抓包中为 1（铜钱档）。自动化只使用该档，禁止调用元宝档。
// PrevAct 显式固定为紧邻的 179/10（同上，避免被心跳/推送覆盖）。
func BuildStUnionIncensePacket(offerID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(offerID)
	w.WriteUint32(ActionIDStUnionGodInfo)
	return NewPacket(ActionIDStUnionGodIncense, w.Bytes())
}

// BuildStUnionTaskInfoPacket 构造魔神挑战面板 (337/0)。
func BuildStUnionTaskInfoPacket() *Packet {
	return stUnionPrevPacket(ActionIDStUnionTaskInfo, nil)
}

// BuildStUnionTaskFightPacket 构造魔神挑战请求 (337/1)。
func BuildStUnionTaskFightPacket() *Packet {
	return stUnionPrevPacket(ActionIDStUnionTaskFight, nil)
}

// BuildStUnionTaskBuyTicketPacket 构造购买挑战券请求 (337/3)。
func BuildStUnionTaskBuyTicketPacket() *Packet {
	return stUnionPrevPacket(ActionIDStUnionTaskBuyTicket, nil)
}

// StUnionIncenseResult 表示上香回包。
type StUnionIncenseResult struct {
	Result uint8
	A      uint16
	B      uint16
	C      int32
}

// ParseStUnionIncenseResult 解析 179/11。
func ParseStUnionIncenseResult(payload []byte) (*StUnionIncenseResult, error) {
	p := DecompressPayloadIfNeeded(payload)
	r := NewReader(p)
	out := &StUnionIncenseResult{}
	var err error
	if out.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: st union incense result: %v", ErrInvalidPayload, err)
	}
	if out.A, err = r.ReadUint16(); err != nil {
		return nil, fmt.Errorf("%w: st union incense a: %v", ErrInvalidPayload, err)
	}
	if out.B, err = r.ReadUint16(); err != nil {
		return nil, fmt.Errorf("%w: st union incense b: %v", ErrInvalidPayload, err)
	}
	if out.C, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: st union incense c: %v", ErrInvalidPayload, err)
	}
	return out, nil
}

// StUnionTaskInfo 表示 337/0 魔神挑战面板的前 4 个整型字段。
//
// 官方 schema 为 [Int,Int,Int,Int,[Int×4],Int,Short,[Int,Int],Int]；
// 具体语义待实机对照（预计包含剩余次数/挑战券数量），先原样打印便于判断。
type StUnionTaskInfo struct {
	A, B, C, D int32
}

// ParseStUnionTaskInfo 解析 337/0 面板的前 4 个 Int。
func ParseStUnionTaskInfo(payload []byte) (*StUnionTaskInfo, error) {
	p := DecompressPayloadIfNeeded(payload)
	r := NewReader(p)
	out := &StUnionTaskInfo{}
	for i, dst := range []*int32{&out.A, &out.B, &out.C, &out.D} {
		v, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: st union task info[%d]: %v", ErrInvalidPayload, i, err)
		}
		*dst = v
	}
	return out, nil
}

// ParseStUnionResult 解析单字节结果码。
func ParseStUnionResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty st union result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// StUnionIncenseResultText 返回上香结果码文本。
func StUnionIncenseResultText(code uint8) string {
	switch code {
	case StUnionIncenseSuccess:
		return "成功"
	case StUnionIncenseHadGift:
		return "已领过奖励"
	case StUnionIncenseNoCoin:
		return "铜钱不足"
	case StUnionIncenseNoIngot:
		return "元宝不足"
	case StUnionIncenseNoVip:
		return "VIP 不足"
	case StUnionIncenseLevelLimit:
		return "仙盟等级不足"
	case StUnionIncenseTodayLimit:
		return "今日已达上限"
	default:
		return "未知结果码"
	}
}

// StUnionTaskResultText 返回魔神挑战结果码文本。
func StUnionTaskResultText(code uint8) string {
	switch code {
	case StUnionTaskSuccess:
		return "成功"
	case StUnionTaskNotInUnion:
		return "未加入仙盟"
	case StUnionTaskNoTicket:
		return "挑战券不足"
	case StUnionTaskTimesUsed:
		return "挑战次数已用完"
	case StUnionTaskPackFull:
		return "背包已满"
	case StUnionTaskFailure:
		return "挑战失败"
	case StUnionTaskMaxLimit:
		return "已达上限"
	case StUnionTaskNoIngot:
		return "元宝不足"
	case StUnionTaskBuyTimesLimit:
		return "购买次数上限"
	case StUnionTaskNoContribute:
		return "贡献不足"
	default:
		return "未知结果码"
	}
}

// BuildStUnionIncenseResultPacket 构造上香回包，供离线 mock 与单测使用。
func BuildStUnionIncenseResultPacket(result uint8, a, b uint16, c int32) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	w.WriteUint16(a)
	w.WriteUint16(b)
	w.WriteInt32(c)
	return NewPacket(ActionIDStUnionGodIncense, w.Bytes())
}
