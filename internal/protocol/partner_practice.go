// Package protocol 提供幻境修炼 (Mod_PartnerPractice_Base, Module 306) 协议编解码。
package protocol

import (
	"fmt"
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
	ActionIDPartnerPracticeRoleAttr   uint32 = 0x0132000A
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

// BuildPartnerPracticeRoleWarAttrPacket 构造伙伴战斗属性查询 (306/10)。
func BuildPartnerPracticeRoleWarAttrPacket(roleID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(roleID)
	return NewPacket(ActionIDPartnerPracticeRoleAttr, w.Bytes())
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
