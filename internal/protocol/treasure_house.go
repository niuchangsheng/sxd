// Package protocol 提供天帝宝库 (Mod_TreasureHouse_Base, Module 594) 协议编解码。
package protocol

import (
	"encoding/binary"
	"fmt"
)

// TreasureAwardSelect 自选奖品设定条目。
type TreasureAwardSelect struct {
	TierID int32
	ItemID int32
}

// newTreasureHousePrevActPacket 构造带 PrevAct 尾部的天帝宝库请求封包。
//
// 官方 DataBase.netCall 在每次请求末尾自动追加 4 字节前序 Module+Action；
// Go 会话层通过 HasPrevAct 标记，在真正发包前用最近一次实际发送的 ActionID
// 覆盖末尾 4 字节。天帝宝库的 set_use_free_flag (Act 4) 会被服务端严格校验
// PrevAct 链：交互选奖期间心跳持续推进该链，若沿用调用方缓存的旧 PrevAct，
// 服务端会静默丢弃该请求（无回包）。因此所有 594 请求都必须走动态 PrevAct。
func newTreasureHousePrevActPacket(actionID uint32, payload []byte) *Packet {
	pkt := NewPacket(actionID, payload)
	pkt.HasPrevAct = true
	return pkt
}

// BuildTreasureHouseInfoPacket 构造查询天帝宝库钥匙与信息封包 (Module 594, Action 2)。
func BuildTreasureHouseInfoPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return newTreasureHousePrevActPacket(ActionIDTreasureHouseInfo, w.Bytes())
}

// BuildTreasureHousePoolPacket 构造拉取天帝宝库自选奖池封包 (Module 594, Action 9)。
func BuildTreasureHousePoolPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTreasureHouseInfo
	}
	w.WriteUint32(prevAct)
	return newTreasureHousePrevActPacket(ActionIDTreasureHousePool, w.Bytes())
}

// TreasureHousePool 表示天帝宝库三档候选奖池。
type TreasureHousePool struct {
	TotalCount int
	Tier1Items []int32 // 一档候选道具
	Tier2Items []int32 // 二档候选道具
	Tier3Items []int32 // 三档候选道具
}

// ParseTreasureHousePoolResult 解析天帝宝库拉取奖池回包 (Module 594, Action 9)。
func ParseTreasureHousePoolResult(payload []byte) (*TreasureHousePool, error) {
	decomp := DecompressPayloadIfNeeded(payload)
	if len(decomp) < 6 {
		return nil, fmt.Errorf("%w: payload too short for pool", ErrInvalidPayload)
	}
	r := NewReader(decomp)
	// 若前4字节为 ActionID 则跳过
	if len(decomp) >= 4 && binary.BigEndian.Uint32(decomp[:4]) == ActionIDTreasureHousePool {
		_, _ = r.ReadUint32()
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read pool count: %v", ErrInvalidPayload, err)
	}
	pool := &TreasureHousePool{TotalCount: int(count)}
	for i := 0; i < int(count); i++ {
		itemID, err := r.ReadInt32()
		if err != nil {
			break
		}
		_, _ = r.ReadInt32() // param
		if i < 15 {
			pool.Tier3Items = append(pool.Tier3Items, itemID)
		} else if i < 30 {
			pool.Tier2Items = append(pool.Tier2Items, itemID)
		} else {
			pool.Tier1Items = append(pool.Tier1Items, itemID)
		}
	}
	return pool, nil
}

// 天帝宝库业务结果码 (对齐原版 com.protocols.Mod_TreasureHouse_Base)。
const (
	TreasureHouseResultSuccess        uint8 = 0  // 成功
	TreasureHouseResultFailed         uint8 = 1  // 通用失败
	TreasureHouseResultNoFreeItem     uint8 = 2  // 无免费次数/免费道具
	TreasureHouseResultNoIngot        uint8 = 3  // 元宝不足
	TreasureHouseResultNoEmptyGrid    uint8 = 4  // 无空格
	TreasureHouseResultItemPackFull   uint8 = 5  // 背包已满
	TreasureHouseResultDragonballFull uint8 = 6  // 龙珠包已满
	TreasureHouseResultLinkfateFull   uint8 = 7  // 结缘包已满
	TreasureHouseResultMaxTimes       uint8 = 8  // 达到最大次数
	TreasureHouseResultNotSelectAward uint8 = 9  // 未选择大奖
	TreasureHouseResultArrayChipFull  uint8 = 10 // 阵符包已满
	TreasureHouseResultTimesLimit     uint8 = 11 // 次数上限
	TreasureHouseResultNoItem         uint8 = 12 // 无可用道具
)

// TreasureHouseResultName 返回结果码的中文语义，便于日志与熔断分支定位。
func TreasureHouseResultName(code uint8) string {
	switch code {
	case TreasureHouseResultSuccess:
		return "成功"
	case TreasureHouseResultFailed:
		return "通用失败"
	case TreasureHouseResultNoFreeItem:
		return "无免费次数"
	case TreasureHouseResultNoIngot:
		return "元宝不足"
	case TreasureHouseResultNoEmptyGrid:
		return "无空格"
	case TreasureHouseResultItemPackFull:
		return "背包已满"
	case TreasureHouseResultDragonballFull:
		return "龙珠包已满"
	case TreasureHouseResultLinkfateFull:
		return "结缘包已满"
	case TreasureHouseResultMaxTimes:
		return "达到最大次数"
	case TreasureHouseResultNotSelectAward:
		return "未选择大奖"
	case TreasureHouseResultArrayChipFull:
		return "阵符包已满"
	case TreasureHouseResultTimesLimit:
		return "次数上限"
	case TreasureHouseResultNoItem:
		return "无可用道具"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// TreasureHouseIngotCost 表示元宝抽奖的阶梯价格。
type TreasureHouseIngotCost struct {
	Min  int32
	Max  int32
	Cost int32
}

// TreasureHouseMainPanel 表示天帝宝库主面板 (Module 594, Action 1)。
type TreasureHouseMainPanel struct {
	Result                     uint8
	WishList                   []TreasureAwardSelect // 当前三档自选大奖 (quality, award_id)
	FreeItemNum                int32                 // 当前持有的免费钥匙数量
	UseFreeFlag                int32                 // 是否优先使用免费次数
	FreeNeedTimes              int32                 // 免费次数累计所需次数
	FreeItemAccu               int32                 // 免费道具累计进度
	UseIngotTimes              int32                 // 已用元宝抽奖次数
	IngotCostList              []TreasureHouseIngotCost
	LeftCanGetFree             int32 // 今日仍可领取的免费次数
	TotalTimes                 int32 // 累计抽奖次数
	NewAroundShopPrivilegeFlag uint8 // 新周边商店特权标记
}

// ParseTreasureHouseMainPanelResult 解析主面板回包:
//
//	UByte result_code
//	[Int quality, Int award_id] wish_list
//	Int free_item_num, Int use_free_flag, Int free_need_times, Int free_item_accu, Int use_ingot_times
//	[Int min, Int max, Int cost] ingot_cost_list
//	Int left_can_get_free, Int total_times, UByte is_new_around_shop_privilege_flag
func ParseTreasureHouseMainPanelResult(payload []byte) (*TreasureHouseMainPanel, error) {
	decomp := DecompressPayloadIfNeeded(payload)
	r := NewReader(decomp)
	result, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: 缺少 result_code", ErrInvalidPayload)
	}
	panel := &TreasureHouseMainPanel{Result: result}
	if result != TreasureHouseResultSuccess {
		return panel, nil
	}

	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: 缺少 wish_list 数量", ErrInvalidPayload)
	}
	for i := 0; i < int(count); i++ {
		quality, qErr := r.ReadInt32()
		awardID, aErr := r.ReadInt32()
		if qErr != nil || aErr != nil {
			return nil, fmt.Errorf("%w: wish_list 第 %d 项截断", ErrInvalidPayload, i)
		}
		panel.WishList = append(panel.WishList, TreasureAwardSelect{TierID: quality, ItemID: awardID})
	}

	fields := []*int32{
		&panel.FreeItemNum, &panel.UseFreeFlag, &panel.FreeNeedTimes,
		&panel.FreeItemAccu, &panel.UseIngotTimes,
	}
	for i, dst := range fields {
		v, fErr := r.ReadInt32()
		if fErr != nil {
			return nil, fmt.Errorf("%w: 主面板第 %d 个整型字段截断", ErrInvalidPayload, i)
		}
		*dst = v
	}

	costCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: 缺少 ingot_cost_list 数量", ErrInvalidPayload)
	}
	for i := 0; i < int(costCount); i++ {
		min, e1 := r.ReadInt32()
		max, e2 := r.ReadInt32()
		cost, e3 := r.ReadInt32()
		if e1 != nil || e2 != nil || e3 != nil {
			return nil, fmt.Errorf("%w: ingot_cost_list 第 %d 项截断", ErrInvalidPayload, i)
		}
		panel.IngotCostList = append(panel.IngotCostList, TreasureHouseIngotCost{Min: min, Max: max, Cost: cost})
	}

	if v, vErr := r.ReadInt32(); vErr == nil {
		panel.LeftCanGetFree = v
	}
	if v, vErr := r.ReadInt32(); vErr == nil {
		panel.TotalTimes = v
	}
	if v, vErr := r.ReadUint8(); vErr == nil {
		panel.NewAroundShopPrivilegeFlag = v
	}
	return panel, nil
}

// TreasureHouseAwardCandidate 表示一档候选大奖条目。
type TreasureHouseAwardCandidate struct {
	AwardID  int32
	OpenFlag int32 // 1 表示当前可选，0 表示未解锁
}

// TreasureHouseQualityPanel 表示单个档位的自选候选信息。
type TreasureHouseQualityPanel struct {
	Quality      int32
	RefreshTimes int32
	RefreshIngot int32
	RefreshLimit int32
	Awards       []TreasureHouseAwardCandidate
}

// TreasureHouseSelectPanel 表示自选面板 (Module 594, Action 2)。
type TreasureHouseSelectPanel struct {
	Result     uint8
	Qualities  []TreasureHouseQualityPanel
	NotifyFlag int32
}

// ParseTreasureHouseSelectPanelResult 解析自选面板回包:
//
//	UByte result_code
//	[Int quality, Int refresh_times, Int refresh_ingot, Int refresh_limit,
//	 [Int award_id, Int open_flag]] quality_award_list
//	Int notify_flag
func ParseTreasureHouseSelectPanelResult(payload []byte) (*TreasureHouseSelectPanel, error) {
	decomp := DecompressPayloadIfNeeded(payload)
	r := NewReader(decomp)
	result, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: 缺少 result_code", ErrInvalidPayload)
	}
	panel := &TreasureHouseSelectPanel{Result: result}
	if result != TreasureHouseResultSuccess {
		return panel, nil
	}

	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: 缺少 quality_award_list 数量", ErrInvalidPayload)
	}
	for i := 0; i < int(count); i++ {
		quality, e1 := r.ReadInt32()
		refreshTimes, e2 := r.ReadInt32()
		refreshIngot, e3 := r.ReadInt32()
		refreshLimit, e4 := r.ReadInt32()
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
			return nil, fmt.Errorf("%w: quality_award_list 第 %d 项字段截断", ErrInvalidPayload, i)
		}
		awardCount, e5 := r.ReadUint16()
		if e5 != nil {
			return nil, fmt.Errorf("%w: quality_award_list 第 %d 项候选数量截断", ErrInvalidPayload, i)
		}
		qp := TreasureHouseQualityPanel{
			Quality:      quality,
			RefreshTimes: refreshTimes,
			RefreshIngot: refreshIngot,
			RefreshLimit: refreshLimit,
		}
		for j := 0; j < int(awardCount); j++ {
			awardID, e6 := r.ReadInt32()
			openFlag, e7 := r.ReadInt32()
			if e6 != nil || e7 != nil {
				return nil, fmt.Errorf("%w: quality_award_list 第 %d 项候选 %d 截断", ErrInvalidPayload, i, j)
			}
			qp.Awards = append(qp.Awards, TreasureHouseAwardCandidate{AwardID: awardID, OpenFlag: openFlag})
		}
		panel.Qualities = append(panel.Qualities, qp)
	}
	if v, vErr := r.ReadInt32(); vErr == nil {
		panel.NotifyFlag = v
	}
	return panel, nil
}

// ParseTreasureHouseResultCode 解析仅含单字节 result_code 的回包 (set_award / set_use_free_flag / get_free_item 等)。
func ParseTreasureHouseResultCode(payload []byte) (uint8, error) {
	decomp := DecompressPayloadIfNeeded(payload)
	if len(decomp) < 1 {
		return 0, fmt.Errorf("%w: 空回包", ErrInvalidPayload)
	}
	return decomp[0], nil
}

// TreasureHouseLotteryResult 表示单次抽奖回包 (Module 594, Action 6)。
type TreasureHouseLotteryResult struct {
	Result  uint8
	AwardID int32 // 中奖条目 ID，失败时可能缺省为 0
}

// ParseTreasureHouseLotteryResult 解析抽奖回包: UByte result_code + Int award_id。
func ParseTreasureHouseLotteryResult(payload []byte) (*TreasureHouseLotteryResult, error) {
	decomp := DecompressPayloadIfNeeded(payload)
	r := NewReader(decomp)
	result, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: 缺少 result_code", ErrInvalidPayload)
	}
	lr := &TreasureHouseLotteryResult{Result: result}
	if awardID, aErr := r.ReadInt32(); aErr == nil {
		lr.AwardID = awardID
	}
	return lr, nil
}

// BuildTreasureHouseMainPanelPacket 构造查询主面板封包 (Module 594, Action 1)。
func BuildTreasureHouseMainPanelPacket(prevAct uint32) *Packet {
	return BuildTreasureHouseRefreshPacket(prevAct)
}

// BuildTreasureHouseUseFreeFlagPacket 构造设置优先使用免费次数封包 (Module 594, Action 4)。
//
// Main.swf 协议定义 request 为 [Int flag]。DataBase.netCall 再在包尾追加 4 字节
// PrevAct，因此正确载荷是 8 字节。2026-11-14 的 8 字节尝试失败是当时 PrevAct
// 链使用了「最后收包」锚点所致（与 357/0、853 同一根因）；链修复后此处恢复
// 官方 [Int flag][4B PrevAct] 形态。
func BuildTreasureHouseUseFreeFlagPacket(flag int32, prevAct uint32) *Packet {
	_ = prevAct
	w := NewWriter()
	w.WriteInt32(flag)
	w.WriteUint32(0) // PrevAct 占位，由会话层覆盖
	pkt := NewPacket(ActionIDTreasureHouseSetUseFreeFlag, w.Bytes())
	pkt.PrevActFromLastSent = true
	return pkt
}

// BuildTreasureHouseGetFreeItemPacket 构造领取免费钥匙封包 (Module 594, Action 8)。
func BuildTreasureHouseGetFreeItemPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTreasureHouseGetFreeItem
	}
	w.WriteUint32(prevAct)
	return newTreasureHousePrevActPacket(ActionIDTreasureHouseGetFreeItem, w.Bytes())
}

// BuildTreasureHouseSelectPacket 构造设定自选大奖封包 (Module 594, Action 3)。
func BuildTreasureHouseSelectPacket(selections []TreasureAwardSelect, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(selections)))
	for _, s := range selections {
		w.WriteInt32(s.TierID)
		w.WriteInt32(s.ItemID)
	}
	if prevAct == 0 {
		prevAct = ActionIDTreasureHousePool
	}
	w.WriteUint32(prevAct)
	return newTreasureHousePrevActPacket(ActionIDTreasureHouseSelect, w.Bytes())
}

// BuildTreasureHouseConfirmPacket 构造锁定确认自选大奖封包 (Module 594, Action 4)。
func BuildTreasureHouseConfirmPacket(confirmID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(confirmID)
	if prevAct == 0 {
		prevAct = ActionIDTreasureHouseSelect
	}
	w.WriteUint32(prevAct)
	return newTreasureHousePrevActPacket(ActionIDTreasureHouseConfirm, w.Bytes())
}

// BuildTreasureHouseRefreshPacket 构造刷新天帝宝库状态封包 (Module 594, Action 1)。
func BuildTreasureHouseRefreshPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTreasureHouseConfirm
	}
	w.WriteUint32(prevAct)
	return newTreasureHousePrevActPacket(ActionIDTreasureHouseRefresh, w.Bytes())
}

// BuildTreasureHouseDrawPacket 构造开启天帝宝库抽奖封包 (Module 594, Action 6)。
func BuildTreasureHouseDrawPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTreasureHouseRefresh
	}
	w.WriteUint32(prevAct)
	return newTreasureHousePrevActPacket(ActionIDTreasureHouseDraw, w.Bytes())
}
