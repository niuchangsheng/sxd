// Package protocol 提供跨服劫镖/商队 (Caravan) 协议编解码。
package protocol

import (
	"fmt"
)

// BuildCaravanGetListPacket 构造“开始勘察/搜索劫镖目标”封包 (Module 314, Action 1)。
//
// 官方 Mod_RobMoney_Base.search: request=[]，载荷仅 PrevAct。
// 旧命名 GetList 为误读，保函数名以兼容现有调用方。
func BuildCaravanGetListPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDCaravanGetList, w.Bytes())
}

// BuildCaravanSearchPacket 构造开始勘察封包，语义同 BuildCaravanGetListPacket。
func BuildCaravanSearchPacket(prevAct uint32) *Packet {
	return BuildCaravanGetListPacket(prevAct)
}

// BuildCaravanGetDetailPacket 构造查询指定劫镖目标详情封包 (Module 314, Action 0)。
func BuildCaravanGetDetailPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDCaravanGetList
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDCaravanGetDetail, w.Bytes())
}

// BuildCaravanOpenPanelPacket 构造打开劫镖面板封包 (Module 314, Action 12)。
func BuildCaravanOpenPanelPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDCaravanGetDetail
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDCaravanOpenPanel, w.Bytes())
}

// BuildCaravanRobPacket 构造发起劫镖封包 (Module 314, Action 2)。
//
// 官方 Mod_RobMoney_Base.rob: request=[]，线上实测载荷仅需 PrevAct。
func BuildCaravanRobPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDCaravanGetList
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDCaravanRob, w.Bytes())
}

// 兼容旧命名。
func BuildCaravanHijackPacket(prevAct uint32) *Packet { return BuildCaravanOpenPanelPacket(prevAct) }
func BuildCaravanSettlePacket(prevAct uint32) *Packet { return BuildCaravanRobPacket(prevAct) }

// 劫镖结果码 (Mod_RobMoney_Base)。
const (
	// CaravanResultSuccess 成功
	CaravanResultSuccess uint8 = 5
	// CaravanResultFunctionNoOpen 功能未开放
	CaravanResultFunctionNoOpen uint8 = 6
	// CaravanResultNotActivityTime 不在活动时间
	CaravanResultNotActivityTime uint8 = 7
	// CaravanResultNotEnoughTimes 劫镖次数不足
	CaravanResultNotEnoughTimes uint8 = 8
	// CaravanResultNotEnoughIngot 元宝不足
	CaravanResultNotEnoughIngot uint8 = 9
	// CaravanResultHaveApply 已申请
	CaravanResultHaveApply uint8 = 10
	// CaravanResultFailure 失败
	CaravanResultFailure uint8 = 11
	// CaravanResultNotEnoughBuyTimes 购买勘察次数不足
	CaravanResultNotEnoughBuyTimes uint8 = 12
)

// CaravanSearchResult 表示“开始勘察”的结果。
//
// 官方 search 回包: [[Int target_id], UByte result]。
type CaravanSearchResult struct {
	// Targets 勘察到的目标编号列表
	Targets []int32
	// Result 结果码 (CaravanResult*)
	Result uint8
}

// ParseCaravanSearchResult 解析开始勘察回包。
func ParseCaravanSearchResult(payload []byte) (*CaravanSearchResult, error) {
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("劫镖勘察目标数量读取失败: %w", err)
	}
	res := &CaravanSearchResult{Targets: make([]int32, 0, count)}
	for i := 0; i < int(count); i++ {
		id, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("劫镖勘察目标[%d]读取失败: %w", i, err)
		}
		res.Targets = append(res.Targets, id)
	}
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("劫镖勘察结果码读取失败: %w", err)
	}
	return res, nil
}

// CaravanRobResult 表示一次劫镖的结果。
type CaravanRobResult struct {
	// Result 结果码
	Result uint8
	// Award 奖励标识 [类型, 数值]
	Award [2]int32
}

// ParseCaravanRobResult 解析劫镖回包 [UByte result, [Int, Int]]。
func ParseCaravanRobResult(payload []byte) (*CaravanRobResult, error) {
	r := NewReader(payload)
	res := &CaravanRobResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("劫镖结果码读取失败: %w", err)
	}
	if res.Result == CaravanResultSuccess {
		if res.Award[0], err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("劫镖奖励类型读取失败: %w", err)
		}
		if res.Award[1], err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("劫镖奖励数值读取失败: %w", err)
		}
	}
	return res, nil
}

// BuildCaravanSearchResultPacket 构造开始勘察回包，供离线 mock 与单测使用。
func BuildCaravanSearchResultPacket(targets []int32, result uint8) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(targets)))
	for _, id := range targets {
		w.WriteInt32(id)
	}
	w.WriteUint8(result)
	return NewPacket(ActionIDCaravanGetList, w.Bytes())
}

// ParseCaravanInfoResultCode 解析劫镖信息面板首字节状态码。
//
// 该字段不是通用结果码，而是目标搜索状态：
// 0=NOT_SEARCH (尚未搜索到目标)，1=SEARCHED (已搜索到可劫目标)。
func ParseCaravanInfoResultCode(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty caravan info", ErrInvalidPayload)
	}
	return payload[0], nil
}

// 劫镖搜索状态。
const (
	// CaravanStateNotSearch 尚未搜索目标
	CaravanStateNotSearch uint8 = 0
	// CaravanStateSearched 已搜索到目标
	CaravanStateSearched uint8 = 1
)

// CaravanResultText 返回劫镖结果码的可读文本。
func CaravanResultText(code uint8) string {
	switch code {
	case CaravanResultSuccess:
		return "成功"
	case CaravanResultFunctionNoOpen:
		return "功能未开放"
	case CaravanResultNotActivityTime:
		return "不在活动时间"
	case CaravanResultNotEnoughTimes:
		return "劫镖次数不足"
	case CaravanResultNotEnoughIngot:
		return "元宝不足"
	case CaravanResultHaveApply:
		return "已申请"
	case CaravanResultFailure:
		return "失败"
	case CaravanResultNotEnoughBuyTimes:
		return "勘察次数不足"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// BuildCaravanRobResultPacket 构造劫镖回包，供离线 mock 与单测使用。
func BuildCaravanRobResultPacket(result uint8, award [2]int32) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	if result == CaravanResultSuccess {
		w.WriteInt32(award[0])
		w.WriteInt32(award[1])
	}
	return NewPacket(ActionIDCaravanRob, w.Bytes())
}

// BuildCaravanInfoResultPacket 构造劫镖面板最小回包，供离线 mock 与单测使用。
func BuildCaravanInfoResultPacket(result uint8) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	w.WriteInt32(0)
	w.WriteInt32(0)
	w.WriteInt32(0)
	w.WriteInt32(0)
	w.WriteUint16(0)
	w.WriteString("")
	w.WriteInt32(0)
	w.WriteInt32(0)
	return NewPacket(ActionIDCaravanGetDetail, w.Bytes())
}

// --------------------------------------------------------------------------------
// 8. 寻仙之路
// --------------------------------------------------------------------------------
