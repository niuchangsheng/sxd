// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方「多宝山」协议 (Mod_MysticalTreasure_Base, Module 740 / 0x02E4)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_MysticalTreasure_Base、
// com.datas.MysticalTreasureData 与 com.views.mystical_treasure.MysticalTreasureView：
//   - get_panel_info  Act 1 -> 0x02E40001  查询活动进度、免费挖掘次数与已开格位
//   - get_mission_time Act 2 -> 0x02E40002  Int mission_id，领取计时任务
//   - dig_treasure    Act 3 -> 0x02E40003  使用免费次数挖掘一格
//
// 自动化仅使用 dig_num 标记的免费挖掘次数，不消耗元宝。
package protocol

import "fmt"

const (
	// ModuleMysticalTreasure 多宝山模块编号 (0x02E4)
	ModuleMysticalTreasure uint16 = 740
)

// 多宝山动作编号 (Mod_MysticalTreasure_Base)。
const (
	// ActionIDMysticalTreasurePanel 查询多宝山面板 (Action 1 -> 0x02E40001)
	ActionIDMysticalTreasurePanel uint32 = 0x02E40001
	// ActionIDMysticalTreasureMissionTime 领取计时任务 (Action 2 -> 0x02E40002)
	ActionIDMysticalTreasureMissionTime uint32 = 0x02E40002
	// ActionIDMysticalTreasureDig 挖掘一格宝藏 (Action 3 -> 0x02E40003)
	ActionIDMysticalTreasureDig uint32 = 0x02E40003
)

// 多宝山结果码 (Mod_MysticalTreasure_Base)。
const (
	// MysticalTreasureResultSuccess 成功
	MysticalTreasureResultSuccess uint8 = 0
	// MysticalTreasureResultFuncNotOpen 功能未开放
	MysticalTreasureResultFuncNotOpen uint8 = 1
	// MysticalTreasureResultCannotGet 暂不可领取
	MysticalTreasureResultCannotGet uint8 = 2
	// MysticalTreasureResultNotEnoughTime 时间不足
	MysticalTreasureResultNotEnoughTime uint8 = 3
	// MysticalTreasureResultFailed 失败
	MysticalTreasureResultFailed uint8 = 4
	// MysticalTreasureResultPackageFull 背包已满
	MysticalTreasureResultPackageFull uint8 = 5
	// MysticalTreasureResultFatePackageFull 命格背包已满
	MysticalTreasureResultFatePackageFull uint8 = 6
)

// MysticalTreasureGrid 表示一个已开启的宝库格位。
type MysticalTreasureGrid struct {
	// GridID 格位编号
	GridID int32
	// AwardID 奖励编号
	AwardID int32
}

// MysticalTreasurePanel 表示多宝山面板。
type MysticalTreasurePanel struct {
	// Activity 活动进度
	Activity int32
	// CostIngot 累计消耗元宝
	CostIngot int32
	// TodayIngot 今日消耗元宝
	TodayIngot int32
	// Mission1Num 任务 1 完成次数
	Mission1Num int32
	// Mission2Num 任务 2 完成次数
	Mission2Num int32
	// TodayMission3 今日任务 3 完成次数
	TodayMission3 int32
	// TodayMission4 今日任务 4 完成次数
	TodayMission4 int32
	// DigNum 剩余免费挖掘次数
	DigNum int32
	// OpenGrids 已开启格位
	OpenGrids []MysticalTreasureGrid
	// BigAwardIDs 已获得的大奖编号
	BigAwardIDs []int32
}

// MysticalTreasureDigResult 表示一次挖掘结果。
type MysticalTreasureDigResult struct {
	// Result 结果码
	Result uint8
	// OpenGridID 本次开启的格位
	OpenGridID int32
	// AwardID 本次获得的奖励编号
	AwardID int32
	// BigAwardIDs 本次触发的大奖编号
	BigAwardIDs []int32
}

// BuildMysticalTreasurePanelPacket 构造查询多宝山面板封包 (0x02E40001)。
func BuildMysticalTreasurePanelPacket() *Packet {
	return NewPacket(ActionIDMysticalTreasurePanel, nil)
}

// BuildMysticalTreasureMissionTimePacket 构造领取计时任务封包 (0x02E40002)。
func BuildMysticalTreasureMissionTimePacket(missionID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(missionID)
	return NewPacket(ActionIDMysticalTreasureMissionTime, w.Bytes())
}

// BuildMysticalTreasureDigPacket 构造挖掘封包 (0x02E40003)。
func BuildMysticalTreasureDigPacket() *Packet {
	return NewPacket(ActionIDMysticalTreasureDig, nil)
}

// ParseMysticalTreasurePanelResult 解析多宝山面板。
func ParseMysticalTreasurePanelResult(payload []byte) (*MysticalTreasurePanel, error) {
	r := NewReader(payload)
	p := &MysticalTreasurePanel{}
	var err error
	for i, field := range []*int32{
		&p.Activity, &p.CostIngot, &p.TodayIngot, &p.Mission1Num,
		&p.Mission2Num, &p.TodayMission3, &p.TodayMission4, &p.DigNum,
	} {
		if *field, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read mystical treasure field %d: %v", ErrInvalidPayload, i, err)
		}
	}
	gridCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read grid count: %v", ErrInvalidPayload, err)
	}
	p.OpenGrids = make([]MysticalTreasureGrid, 0, gridCount)
	for i := 0; i < int(gridCount); i++ {
		var g MysticalTreasureGrid
		if g.GridID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read grid[%d].grid_id: %v", ErrInvalidPayload, i, err)
		}
		if g.AwardID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read grid[%d].award_id: %v", ErrInvalidPayload, i, err)
		}
		p.OpenGrids = append(p.OpenGrids, g)
	}
	awardCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read big award count: %v", ErrInvalidPayload, err)
	}
	p.BigAwardIDs = make([]int32, 0, awardCount)
	for i := 0; i < int(awardCount); i++ {
		id, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read big award[%d]: %v", ErrInvalidPayload, i, err)
		}
		p.BigAwardIDs = append(p.BigAwardIDs, id)
	}
	return p, nil
}

// ParseMysticalTreasureActionResult 解析仅返回单结果码的动作 (get_mission_time)。
func ParseMysticalTreasureActionResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty mystical treasure action result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// ParseMysticalTreasureDigResult 解析挖掘回包。
func ParseMysticalTreasureDigResult(payload []byte) (*MysticalTreasureDigResult, error) {
	r := NewReader(payload)
	res := &MysticalTreasureDigResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read dig result: %v", ErrInvalidPayload, err)
	}
	if res.Result != MysticalTreasureResultSuccess {
		return res, nil
	}
	if res.OpenGridID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read open_grid_id: %v", ErrInvalidPayload, err)
	}
	if res.AwardID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read award_id: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read big award count: %v", ErrInvalidPayload, err)
	}
	for i := 0; i < int(count); i++ {
		id, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read big award[%d]: %v", ErrInvalidPayload, i, err)
		}
		res.BigAwardIDs = append(res.BigAwardIDs, id)
	}
	return res, nil
}

// BuildMysticalTreasurePanelResultPacket 构造面板回包，供离线 mock 与单测使用。
func BuildMysticalTreasurePanelResultPacket(p MysticalTreasurePanel) *Packet {
	w := NewWriter()
	for _, v := range []int32{
		p.Activity, p.CostIngot, p.TodayIngot, p.Mission1Num,
		p.Mission2Num, p.TodayMission3, p.TodayMission4, p.DigNum,
	} {
		w.WriteInt32(v)
	}
	w.WriteUint16(uint16(len(p.OpenGrids)))
	for _, g := range p.OpenGrids {
		w.WriteInt32(g.GridID)
		w.WriteInt32(g.AwardID)
	}
	w.WriteUint16(uint16(len(p.BigAwardIDs)))
	for _, id := range p.BigAwardIDs {
		w.WriteInt32(id)
	}
	return NewPacket(ActionIDMysticalTreasurePanel, w.Bytes())
}

// BuildMysticalTreasureDigResultPacket 构造挖掘回包，供离线 mock 与单测使用。
func BuildMysticalTreasureDigResultPacket(res MysticalTreasureDigResult) *Packet {
	w := NewWriter()
	w.WriteUint8(res.Result)
	if res.Result == MysticalTreasureResultSuccess {
		w.WriteInt32(res.OpenGridID)
		w.WriteInt32(res.AwardID)
		w.WriteUint16(uint16(len(res.BigAwardIDs)))
		for _, id := range res.BigAwardIDs {
			w.WriteInt32(id)
		}
	}
	return NewPacket(ActionIDMysticalTreasureDig, w.Bytes())
}

// BuildMysticalTreasureActionResultPacket 构造单结果码回包，供离线 mock 与单测使用。
func BuildMysticalTreasureActionResultPacket(actionID uint32, code uint8) *Packet {
	return NewPacket(actionID, []byte{code})
}
