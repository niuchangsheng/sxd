// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方点金石协议 (Mod_GoldenTouchStone_Base, Module 551 / 0x227)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_GoldenTouchStone_Base：
// 先查询状态 (初始化/分享/可点化)，再进行点化或分享求助。
package protocol

import "fmt"

const (
	// ActionIDGoldenTouchStoneStatus 查询点金石状态 (Module 551, Action 0 -> 0x02270000)
	ActionIDGoldenTouchStoneStatus uint32 = 0x02270000
	// ActionIDGoldenTouchStoneShare 查询分享面板 (Module 551, Action 1 -> 0x02270001)
	ActionIDGoldenTouchStoneShare uint32 = 0x02270001
	// ActionIDGoldenTouchStoneTouchPanel 查询点化面板 (Module 551, Action 2 -> 0x02270002)
	ActionIDGoldenTouchStoneTouchPanel uint32 = 0x02270002
	// ActionIDGoldenTouchStoneInit 初始化点金石 (Module 551, Action 3 -> 0x02270003)
	ActionIDGoldenTouchStoneInit uint32 = 0x02270003
	// ActionIDGoldenTouchStoneTouch 点化金石 (Module 551, Action 6 -> 0x02270006)
	ActionIDGoldenTouchStoneTouch uint32 = 0x02270006
)

// 点金石状态与结果码。
const (
	// GoldenTouchStoneStatusInit 待初始化
	GoldenTouchStoneStatusInit uint8 = 0
	// GoldenTouchStoneStatusShare 待分享
	GoldenTouchStoneStatusShare uint8 = 1
	// GoldenTouchStoneStatusTouch 可点化
	GoldenTouchStoneStatusTouch uint8 = 2
	// GoldenTouchStoneResultSuccess 成功
	GoldenTouchStoneResultSuccess uint8 = 3
	// GoldenTouchStoneResultFail 失败
	GoldenTouchStoneResultFail uint8 = 4
	// GoldenTouchStoneResultNotEnoughHelp 帮助人数不足
	GoldenTouchStoneResultNotEnoughHelp uint8 = 5
	// GoldenTouchStoneResultHasHelp 已帮助过
	GoldenTouchStoneResultHasHelp uint8 = 6
	// GoldenTouchStoneResultNotEnoughCount 次数不足
	GoldenTouchStoneResultNotEnoughCount uint8 = 7
)

// GoldenTouchStonePanel 表示点化面板信息。
//
// 线格式: Int level, Int exp, Int this_week_luck_value, Int cur_activity,
// Int next_week_luck_value, Int today_touch_num。
type GoldenTouchStonePanel struct {
	// Level 点金石等级
	Level int32
	// Exp 当前经验
	Exp int32
	// ThisWeekLuckValue 本周幸运值
	ThisWeekLuckValue int32
	// CurActivity 当前活跃值
	CurActivity int32
	// NextWeekLuckValue 下周幸运值
	NextWeekLuckValue int32
	// TodayTouchNum 今日已点化次数
	TodayTouchNum int32
}

// BuildGoldenTouchStoneStatusPacket 构造查询状态封包 (0x02270000)。
func BuildGoldenTouchStoneStatusPacket() *Packet {
	return NewPacket(ActionIDGoldenTouchStoneStatus, nil)
}

// BuildGoldenTouchStoneTouchPanelPacket 构造查询点化面板封包 (0x02270002)。
func BuildGoldenTouchStoneTouchPanelPacket() *Packet {
	return NewPacket(ActionIDGoldenTouchStoneTouchPanel, nil)
}

// BuildGoldenTouchStoneInitPacket 构造初始化封包 (0x02270003)。
func BuildGoldenTouchStoneInitPacket() *Packet {
	return NewPacket(ActionIDGoldenTouchStoneInit, nil)
}

// BuildGoldenTouchStoneTouchPacket 构造点化封包 (0x02270006)。
func BuildGoldenTouchStoneTouchPacket() *Packet {
	return NewPacket(ActionIDGoldenTouchStoneTouch, nil)
}

// ParseGoldenTouchStoneStatusResult 解析状态 (UByte status)。
func ParseGoldenTouchStoneStatusResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty golden touch stone status", ErrInvalidPayload)
	}
	return payload[0], nil
}

// ParseGoldenTouchStoneTouchPanelResult 解析点化面板。
func ParseGoldenTouchStoneTouchPanelResult(payload []byte) (*GoldenTouchStonePanel, error) {
	r := NewReader(payload)
	res := &GoldenTouchStonePanel{}
	var err error
	if res.Level, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read stone level: %v", ErrInvalidPayload, err)
	}
	if res.Exp, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read stone exp: %v", ErrInvalidPayload, err)
	}
	if res.ThisWeekLuckValue, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read stone this_week_luck_value: %v", ErrInvalidPayload, err)
	}
	if res.CurActivity, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read stone cur_activity: %v", ErrInvalidPayload, err)
	}
	if res.NextWeekLuckValue, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read stone next_week_luck_value: %v", ErrInvalidPayload, err)
	}
	if res.TodayTouchNum, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read stone today_touch_num: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// ParseGoldenTouchStoneTouchResult 解析点化结果: UByte result, Int exp, Int luck。
func ParseGoldenTouchStoneTouchResult(payload []byte) (result uint8, exp int32, luck int32, err error) {
	r := NewReader(payload)
	if result, err = r.ReadUint8(); err != nil {
		return 0, 0, 0, fmt.Errorf("%w: failed to read stone touch result: %v", ErrInvalidPayload, err)
	}
	if exp, err = r.ReadInt32(); err != nil {
		return result, 0, 0, fmt.Errorf("%w: failed to read stone touch exp: %v", ErrInvalidPayload, err)
	}
	if luck, err = r.ReadInt32(); err != nil {
		return result, exp, 0, fmt.Errorf("%w: failed to read stone touch luck: %v", ErrInvalidPayload, err)
	}
	return result, exp, luck, nil
}

// BuildGoldenTouchStoneStatusResultPacket 构造状态回包，供离线 mock 与单测使用。
func BuildGoldenTouchStoneStatusResultPacket(status uint8) *Packet {
	return NewPacket(ActionIDGoldenTouchStoneStatus, []byte{status})
}

// BuildGoldenTouchStoneTouchPanelResultPacket 构造点化面板回包，供离线 mock 与单测使用。
func BuildGoldenTouchStoneTouchPanelResultPacket(panel GoldenTouchStonePanel) *Packet {
	w := NewWriter()
	w.WriteInt32(panel.Level)
	w.WriteInt32(panel.Exp)
	w.WriteInt32(panel.ThisWeekLuckValue)
	w.WriteInt32(panel.CurActivity)
	w.WriteInt32(panel.NextWeekLuckValue)
	w.WriteInt32(panel.TodayTouchNum)
	return NewPacket(ActionIDGoldenTouchStoneTouchPanel, w.Bytes())
}

// BuildGoldenTouchStoneTouchResultPacket 构造点化回包，供离线 mock 与单测使用。
func BuildGoldenTouchStoneTouchResultPacket(result uint8, exp, luck int32) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	w.WriteInt32(exp)
	w.WriteInt32(luck)
	return NewPacket(ActionIDGoldenTouchStoneTouch, w.Bytes())
}
