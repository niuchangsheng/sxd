// Package protocol 提供修真境界 (Mod_XiuzhenRealm_Base, Module 552 / 0x228) 协议。
//
// 抓包基准: /tmp/xiuzhen.pcapng（2026-11-22，qq s0）：
//
//	552/1 main_panel        []                      → 主面板
//	552/2 realm_panel       [Int type]              → 境界记录面板
//	552/3 get_award         [Int param]             → 单个境界奖励
//	552/4 get_step          []                      → 当前境界进度
//	552/5 get_extra_award   []                      → 一键领取礼包（含奖励列表）
//
// 动作号按 1 基排列（抓包中 552/1、552/4、552/5 均为空请求）。
// 552/5 回包: UByte result + [UInt16 n, n×(Int item_id, Int num)] + 额外字段。
// 抓包奖励: 8808 青铜宝箱 ×5、5852 一品神器包 ×1。
package protocol

import "fmt"

const (
	// ActionIDXiuZhenRealmMain 主面板 (552/1)
	ActionIDXiuZhenRealmMain uint32 = 0x02280001
	// ActionIDXiuZhenRealmRealm 境界记录面板 (552/2)
	ActionIDXiuZhenRealmRealm uint32 = 0x02280002
	// ActionIDXiuZhenRealmAward 单境界奖励 (552/3)
	ActionIDXiuZhenRealmAward uint32 = 0x02280003
	// ActionIDXiuZhenRealmStep 当前境界进度 (552/4)
	ActionIDXiuZhenRealmStep uint32 = 0x02280004
	// ActionIDXiuZhenRealmExtraAward 一键领取礼包 (552/5)
	ActionIDXiuZhenRealmExtraAward uint32 = 0x02280005
)

// 552 结果码（Mod_XiuzhenRealm_Base）。
const (
	XiuZhenRealmSuccess      uint8 = 0
	XiuZhenRealmIsGet        uint8 = 1
	XiuZhenRealmLessPoint    uint8 = 2
	XiuZhenRealmCondLimit    uint8 = 3
	XiuZhenRealmItemPackFull uint8 = 4
)

// XiuZhenRealmReward 表示礼包中的一项奖励。
type XiuZhenRealmReward struct {
	ItemID int32
	Num    int32
}

func xiuZhenRealmPacket(actionID uint32, args ...int32) *Packet {
	w := NewWriter()
	for _, v := range args {
		w.WriteInt32(v)
	}
	w.WriteUint32(0) // PrevAct 占位，由会话层覆盖
	pkt := NewPacket(actionID, w.Bytes())
	pkt.PrevActFromLastSent = true
	return pkt
}

// BuildXiuZhenRealmMainPacket 构造主面板查询 (552/1)。
func BuildXiuZhenRealmMainPacket() *Packet {
	return xiuZhenRealmPacket(ActionIDXiuZhenRealmMain)
}

// BuildXiuZhenRealmExtraAwardPacket 构造一键领取礼包 (552/5)。
func BuildXiuZhenRealmExtraAwardPacket() *Packet {
	return xiuZhenRealmPacket(ActionIDXiuZhenRealmExtraAward)
}

// XiuZhenRealmResult 表示 552/5 回包结果。
type XiuZhenRealmResult struct {
	Result  uint8
	Rewards []XiuZhenRealmReward
	// RawTail 保留奖励列表之后的原始字节，便于排查版本差异。
	RawTail []byte
}

// ParseXiuZhenRealmResult 解析 552/5：[UByte result, [UInt16 n, n×(Int,Int)], ...]。
func ParseXiuZhenRealmResult(payload []byte) (*XiuZhenRealmResult, error) {
	p := DecompressPayloadIfNeeded(payload)
	r := NewReader(p)
	out := &XiuZhenRealmResult{}
	var err error
	if out.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: xiuzhen realm result: %v", ErrInvalidPayload, err)
	}
	if out.Result != XiuZhenRealmSuccess {
		return out, nil
	}
	count, err := r.ReadUint16()
	if err != nil {
		return out, nil
	}
	for i := 0; i < int(count); i++ {
		var it XiuZhenRealmReward
		if it.ItemID, err = r.ReadInt32(); err != nil {
			return out, nil
		}
		if it.Num, err = r.ReadInt32(); err != nil {
			return out, nil
		}
		out.Rewards = append(out.Rewards, it)
	}
	if r.Remaining() > 0 {
		out.RawTail = append([]byte(nil), p[len(p)-r.Remaining():]...)
	}
	return out, nil
}

// ParseXiuZhenRealmMainResult 解析 552/1 首字节结果码。
func ParseXiuZhenRealmMainResult(payload []byte) (uint8, error) {
	p := DecompressPayloadIfNeeded(payload)
	if len(p) == 0 {
		return 0, fmt.Errorf("%w: empty xiuzhen realm main result", ErrInvalidPayload)
	}
	return p[0], nil
}

// XiuZhenRealmResultText 返回 552 结果码文本。
func XiuZhenRealmResultText(code uint8) string {
	switch code {
	case XiuZhenRealmSuccess:
		return "成功"
	case XiuZhenRealmIsGet:
		return "已领取"
	case XiuZhenRealmLessPoint:
		return "境界点不足"
	case XiuZhenRealmCondLimit:
		return "条件未满足"
	case XiuZhenRealmItemPackFull:
		return "背包已满"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// BuildXiuZhenRealmResultPacket 构造 552/5 回包，供离线 mock 与单测使用。
func BuildXiuZhenRealmResultPacket(result uint8, rewards []XiuZhenRealmReward) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	w.WriteUint16(uint16(len(rewards)))
	for _, it := range rewards {
		w.WriteInt32(it.ItemID)
		w.WriteInt32(it.Num)
	}
	return NewPacket(ActionIDXiuZhenRealmExtraAward, w.Bytes())
}
