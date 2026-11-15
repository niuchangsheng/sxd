// Package protocol 提供无尽宝箱 (Mod_EndlessBox_Base, Module 690 / 0x2B2) 协议。
//
// 只在「宝箱狂欢周」开启；690/4 activity_panel 返回活动起止时间：
//
//	1 panel_info  [Int score, [Int type, Int num, Int has_buy_num] 列表, Int cost_id, Int charge_ingot_num]
//	2 open_box    [Int box_type, Int open_type] → [UByte result, [Int,Int]]
//	3 gain_box_award     [UByte result]
//	4 activity_panel     [Int start_time, Int end_time, Int flag, [Int]]
//	5 buy_box     [Int]（元宝购买，自动化禁止调用）
//	6 gain_all_box_award [UByte result, [Int,Int]]
package protocol

import "fmt"

const (
	// ActionIDEndlessBoxPanel 箱子面板 (690/1)
	ActionIDEndlessBoxPanel uint32 = 0x02B20001
	// ActionIDEndlessBoxOpen 开箱 (690/2)
	ActionIDEndlessBoxOpen uint32 = 0x02B20002
	// ActionIDEndlessBoxGainAward 领取单箱奖励 (690/3)
	ActionIDEndlessBoxGainAward uint32 = 0x02B20003
	// ActionIDEndlessBoxActivity 活动面板 (690/4)
	ActionIDEndlessBoxActivity uint32 = 0x02B20004
	// ActionIDEndlessBoxBuy 购买箱子 (690/5)（禁止调用）
	ActionIDEndlessBoxBuy uint32 = 0x02B20005
	// ActionIDEndlessBoxGainAll 领取全部奖励 (690/6)
	ActionIDEndlessBoxGainAll uint32 = 0x02B20006
)

// 690 结果码。
const (
	EndlessBoxSuccess      uint8 = 0
	EndlessBoxNotOpen      uint8 = 1
	EndlessBoxNotEnoughBox uint8 = 2
	EndlessBoxPackFull     uint8 = 3
	EndlessBoxFailure      uint8 = 4
	EndlessBoxNoScore      uint8 = 5
	EndlessBoxNoIngot      uint8 = 6
	EndlessBoxLimitNum     uint8 = 7
)

// EndlessBoxEntry 表示一种箱子。
type EndlessBoxEntry struct {
	Type      int32
	Num       int32
	HasBuyNum int32
}

// EndlessBoxPanel 表示箱子面板。
type EndlessBoxPanel struct {
	Score  int32
	Boxes  []EndlessBoxEntry
	CostID int32
}

// EndlessBoxActivity 表示宝箱狂欢周活动窗口。
type EndlessBoxActivity struct {
	StartTime int32
	EndTime   int32
	Flag      int32
}

// BuildEndlessBoxPanelPacket 构造箱子面板查询 (690/1)。
func BuildEndlessBoxPanelPacket() *Packet {
	return NewPacket(ActionIDEndlessBoxPanel, nil)
}

// BuildEndlessBoxOpenPacket 构造开箱请求 (690/2)。
func BuildEndlessBoxOpenPacket(boxType, openType int32) *Packet {
	w := NewWriter()
	w.WriteInt32(boxType)
	w.WriteInt32(openType)
	return NewPacket(ActionIDEndlessBoxOpen, w.Bytes())
}

// BuildEndlessBoxGainAllPacket 构造领取全部奖励 (690/6)。
func BuildEndlessBoxGainAllPacket() *Packet {
	return NewPacket(ActionIDEndlessBoxGainAll, nil)
}

// BuildEndlessBoxActivityPacket 构造活动面板查询 (690/4)。
func BuildEndlessBoxActivityPacket() *Packet {
	return NewPacket(ActionIDEndlessBoxActivity, nil)
}

// ParseEndlessBoxActivity 解析 690/4。
func ParseEndlessBoxActivity(payload []byte) (*EndlessBoxActivity, error) {
	p := DecompressPayloadIfNeeded(payload)
	r := NewReader(p)
	out := &EndlessBoxActivity{}
	var err error
	if out.StartTime, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: endless box start: %v", ErrInvalidPayload, err)
	}
	if out.EndTime, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: endless box end: %v", ErrInvalidPayload, err)
	}
	if out.Flag, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: endless box flag: %v", ErrInvalidPayload, err)
	}
	return out, nil
}

// ParseEndlessBoxPanel 解析 690/1。
func ParseEndlessBoxPanel(payload []byte) (*EndlessBoxPanel, error) {
	p := DecompressPayloadIfNeeded(payload)
	r := NewReader(p)
	out := &EndlessBoxPanel{}
	var err error
	if out.Score, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: endless box score: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: endless box count: %v", ErrInvalidPayload, err)
	}
	for i := 0; i < int(count); i++ {
		var b EndlessBoxEntry
		if b.Type, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		if b.Num, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		if b.HasBuyNum, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		out.Boxes = append(out.Boxes, b)
	}
	if out.CostID, err = r.ReadInt32(); err != nil {
		return nil, err
	}
	return out, nil
}

// ParseEndlessBoxResult 解析单字节结果码。
func ParseEndlessBoxResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty endless box result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// EndlessBoxResultText 返回 690 结果码文本。
func EndlessBoxResultText(code uint8) string {
	switch code {
	case EndlessBoxSuccess:
		return "成功"
	case EndlessBoxNotOpen:
		return "未开放"
	case EndlessBoxNotEnoughBox:
		return "宝箱不足"
	case EndlessBoxPackFull:
		return "背包已满"
	case EndlessBoxFailure:
		return "失败"
	case EndlessBoxNoScore:
		return "积分不足"
	case EndlessBoxNoIngot:
		return "元宝不足"
	case EndlessBoxLimitNum:
		return "次数上限"
	default:
		return "未知结果码"
	}
}

// BuildEndlessBoxPanelResultPacket 构造面板回包，供离线 mock 与单测使用。
func BuildEndlessBoxPanelResultPacket(score int32, boxes []EndlessBoxEntry, costID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(score)
	w.WriteUint16(uint16(len(boxes)))
	for _, b := range boxes {
		w.WriteInt32(b.Type).WriteInt32(b.Num).WriteInt32(b.HasBuyNum)
	}
	w.WriteInt32(costID)
	return NewPacket(ActionIDEndlessBoxPanel, w.Bytes())
}

// BuildEndlessBoxActivityResultPacket 构造活动面板回包，供离线 mock 与单测使用。
func BuildEndlessBoxActivityResultPacket(start, end, flag int32) *Packet {
	w := NewWriter()
	w.WriteInt32(start).WriteInt32(end).WriteInt32(flag)
	w.WriteUint16(0)
	return NewPacket(ActionIDEndlessBoxActivity, w.Bytes())
}
