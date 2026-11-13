// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现基于 haha.pcapng 逆向报文对齐的 16 项高频玩法的二进制请求封包构造与回包解析。
package protocol

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
)

// --------------------------------------------------------------------------------
// 1. 在线成长礼包 (在线时长福利)
// --------------------------------------------------------------------------------

// OnlineGiftItem 表示在线成长礼包中的一项奖励。
type OnlineGiftItem struct {
	Type   int32 // 奖励类型
	ItemID int32 // 道具/资源编号
	Value  int32 // 数量
}

// OnlineGiftCurrent 表示在线成长礼包当前状态。
type OnlineGiftCurrent struct {
	TimeStamp int32            // 0 表示暂无待领取礼包; 非 0 时客户端展示下一份礼包预览
	Gifts     []OnlineGiftItem // 待领取奖励列表
}

// BuildOnlineGiftCurrentPacket 构造查询在线成长礼包状态封包 (Module 0, Action 31)。
// 依据原版客户端 Mod_Player_Base.cinit: request 描述为空, 请求体为空。
func BuildOnlineGiftCurrentPacket() *Packet {
	return NewPacket(ActionIDPlayerGetCurrentOnlineGift, nil)
}

// ParseOnlineGiftCurrentResult 解析查询回包:
// int32 time_stamp + uint16 count + count*(int32 type,int32 item_id,int32 value)。
func ParseOnlineGiftCurrentResult(payload []byte) (*OnlineGiftCurrent, error) {
	decomp := DecompressPayloadIfNeeded(payload)
	if len(decomp) < 6 {
		return nil, fmt.Errorf("%w: payload too short for online gift state", ErrInvalidPayload)
	}
	r := NewReader(decomp)
	ts, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read time_stamp: %v", ErrInvalidPayload, err)
	}
	gifts, err := readOnlineGiftItems(r)
	if err != nil {
		return nil, err
	}
	return &OnlineGiftCurrent{TimeStamp: ts, Gifts: gifts}, nil
}

// OnlineGiftClaimResult 表示领取在线成长礼包回包。
type OnlineGiftClaimResult struct {
	Result        uint8            // 业务结果码: 97 成功 / 98 已领取 / 100 时间未到 / 101 背包已满
	Gifts         []OnlineGiftItem // 本次领取到的奖励列表
	NextTimeStamp int32            // 距离下一次礼包可领取的剩余秒数 (0 表示无后续礼包)
}

// BuildOnlineGiftClaimPacket 构造领取在线成长礼包封包 (Module 0, Action 30)。
// 依据原版客户端 Mod_Player_Base.cinit: request 描述为空, 请求体为空。
func BuildOnlineGiftClaimPacket() *Packet {
	return NewPacket(ActionIDPlayerGetOnlineGift, nil)
}

// ParseOnlineGiftClaimResult 解析领取回包:
// uint8 result + uint16 count + count*(int32 type,int32 item_id,int32 value) + int32 next_time_stamp。
func ParseOnlineGiftClaimResult(payload []byte) (*OnlineGiftClaimResult, error) {
	decomp := DecompressPayloadIfNeeded(payload)
	if len(decomp) < 3 {
		return nil, fmt.Errorf("%w: payload too short for online gift claim", ErrInvalidPayload)
	}
	r := NewReader(decomp)
	result, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read claim result: %v", ErrInvalidPayload, err)
	}
	gifts, err := readOnlineGiftItems(r)
	if err != nil {
		return nil, err
	}
	claim := &OnlineGiftClaimResult{Result: result, Gifts: gifts}
	// next_time_stamp 可能因服务端版本差异缺失, 缺失时保持 0
	if nextTS, err := r.ReadInt32(); err == nil {
		claim.NextTimeStamp = nextTS
	}
	return claim, nil
}

// readOnlineGiftItems 读取 uint16 count 及其后的 count 条 (type,item_id,value) 记录。
func readOnlineGiftItems(r *Reader) ([]OnlineGiftItem, error) {
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read gift count: %v", ErrInvalidPayload, err)
	}
	var gifts []OnlineGiftItem
	for i := 0; i < int(count); i++ {
		typ, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: truncated gift type at %d: %v", ErrInvalidPayload, i, err)
		}
		itemID, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: truncated gift item_id at %d: %v", ErrInvalidPayload, i, err)
		}
		value, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: truncated gift value at %d: %v", ErrInvalidPayload, i, err)
		}
		gifts = append(gifts, OnlineGiftItem{Type: typ, ItemID: itemID, Value: value})
	}
	return gifts, nil
}

// --------------------------------------------------------------------------------
// 1.1 序列号礼包领取 (haha.pcapng 实采协议, 非在线成长礼包)
// --------------------------------------------------------------------------------

// BuildClaimAwardPacket 构造领取指定序列号礼包封包 (Module 2, Action 29)。
//
// 官方抓包: [Int award_id][Uint32 PrevAct]；PrevAct 链由会话层在发包瞬间
// 用最近一次动作覆盖，因此这里标记 HasPrevAct。
func BuildClaimAwardPacket(awardSeqID uint32, prevAct uint32) *Packet {
	// 2026-11-14 实机：带 PrevAct 的 8 字节 2/29 请求对系统奖励被静默丢弃；
	// 与 35/0、25/0 一致，优先使用裸 [Int award_id] 载荷。prevAct 参数保留兼容。
	_ = prevAct
	w := NewWriter()
	w.WriteUint32(awardSeqID)
	return NewPacket(ActionIDItemClaimAward, w.Bytes())
}

// BuildSystemAwardListPacket 构造查询系统/活动奖励列表封包 (Module 2, Action 6)。
//
// 官方抓包请求为空载荷 + PrevAct 尾部。
func BuildSystemAwardListPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDSystemAwardList, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// SystemAward 表示一条待领取的系统/活动奖励。
type SystemAward struct {
	ID     uint32 // 奖励 ID，用于 2/29 领取
	Kind   int32  // 奖励类型/状态字段
	Text   string // 奖励描述 (UTF-8)
	Count  int32  // 奖励条目数
	Detail []SystemAwardItem
}

// SystemAwardItem 表示奖励明细中的一项。
type SystemAwardItem struct {
	Type  uint8 // 奖励类型
	Num   int32 // 数量
	Param int32 // 附加参数 (道具 ID 等)
}

// ParseSystemAwardListResult 解析 2/6 系统/活动奖励列表。
//
// 线格式: [Uint16 count] + count ×
// [Int32 id][Int32 kind][Uint16 text_len][text bytes]
// [Int32 item_count][item_count × (Uint8 type, Int32 num, Int32 param)]。
func ParseSystemAwardListResult(payload []byte) ([]SystemAward, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read system award count: %v", ErrInvalidPayload, err)
	}
	awards := make([]SystemAward, 0, count)
	for i := 0; i < int(count); i++ {
		var a SystemAward
		if a.ID, err = r.ReadUint32(); err != nil {
			return awards, nil
		}
		if a.Kind, err = r.ReadInt32(); err != nil {
			return awards, nil
		}
		textLen, err := r.ReadUint16()
		if err != nil {
			return awards, nil
		}
		text, err := r.ReadBytes(int(textLen))
		if err != nil {
			return awards, nil
		}
		a.Text = string(text)
		if a.Count, err = r.ReadInt32(); err != nil {
			return awards, nil
		}
		if a.Count < 0 || a.Count > 1000 {
			return awards, nil
		}
		for j := 0; j < int(a.Count); j++ {
			var it SystemAwardItem
			if it.Type, err = r.ReadUint8(); err != nil {
				return awards, nil
			}
			if it.Num, err = r.ReadInt32(); err != nil {
				return awards, nil
			}
			if it.Param, err = r.ReadInt32(); err != nil {
				return awards, nil
			}
			a.Detail = append(a.Detail, it)
		}
		awards = append(awards, a)
	}
	return awards, nil
}

// ParseClaimAwardResult 解析礼包领取结果。
func ParseClaimAwardResult(payload []byte) (bool, error) {
	if len(payload) == 0 {
		return true, nil
	}
	r := NewReader(payload)
	code, err := r.ReadUint8()
	if err != nil {
		return false, fmt.Errorf("%w: failed to read claim code: %v", ErrInvalidPayload, err)
	}
	// 结果码 0x14 (20) 或 0x00 表示成功
	return code == ItemActionSuccess || code == ItemSuccess, nil
}

// --------------------------------------------------------------------------------
// 2. 结缘宝箱：除了传说，其他都开
// --------------------------------------------------------------------------------

// JieYuanBoxItem 表示玩家背包持有的结缘宝箱条目。
type JieYuanBoxItem struct {
	ItemID int32 // 宝箱道具 ID (3937..3942)
	Count  int32 // 持有数量
}

// BuildJieYuanBoxListPacket 构造查询结缘宝箱列表封包 (Module 232, Action 30)。
func BuildJieYuanBoxListPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDJieYuanBoxList, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// ParseJieYuanBoxListResult 解析结缘宝箱列表回包。
func ParseJieYuanBoxListResult(payload []byte) ([]JieYuanBoxItem, error) {
	if len(payload) < 2 {
		return nil, nil
	}
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read box count: %v", ErrInvalidPayload, err)
	}
	var items []JieYuanBoxItem
	for i := 0; i < int(count); i++ {
		itemID, err := r.ReadInt32()
		if err != nil {
			break
		}
		itemCount, err := r.ReadInt32()
		if err != nil {
			break
		}
		items = append(items, JieYuanBoxItem{ItemID: itemID, Count: itemCount})
	}
	return items, nil
}

// FilterOpenableJieYuanBoxes 按照规则“除了传说，其他都开”过滤出允许开启的宝箱。
// 传说结缘宝箱 (ItemJieYuanBoxLegendary = 3942) 被严格剔除。
func FilterOpenableJieYuanBoxes(boxes []JieYuanBoxItem) []JieYuanBoxItem {
	var openable []JieYuanBoxItem
	for _, b := range boxes {
		if b.Count <= 0 {
			continue
		}
		// 严正过滤：跳过传说结缘宝箱
		if b.ItemID == ItemJieYuanBoxLegendary {
			continue
		}
		openable = append(openable, b)
	}
	return openable
}

// BuildJieYuanBoxOpenContextPacket 构造结缘宝箱开箱前置上下文协议包 (Module 528, Action 1)。
func BuildJieYuanBoxOpenContextPacket(prevAct uint32) *Packet {
	w := NewWriter()
	// 依据 haha.pcapng 帧实采: 07 04 + PrevAct
	w.WriteUint16(0x0704)
	if prevAct == 0 {
		prevAct = ActionIDJieYuanBoxList
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDJieYuanBoxOpenContext, w.Bytes())
}

// BuildJieYuanBoxOpenPacket 构造批量开启结缘宝箱请求封包 (Module 232, Action 34)。
//
// 抓包显示官方开箱请求为 [Int box_item_id, Int count, Uint32 PrevAct]；
// PrevAct 必须指向紧邻的 528/1 前置握手，否则网关静默丢弃开箱请求。
func BuildJieYuanBoxOpenPacket(itemID int32, openCount int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(itemID)
	w.WriteInt32(openCount)
	if prevAct == 0 {
		prevAct = ActionIDJieYuanBoxOpenContext
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDJieYuanBoxOpen, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// ParseJieYuanOpenContextResult 解析 528/1 前置上下文握手回包。
// 官方抓包回包载荷仅 1 字节 result_code（0x00 表示握手成功）。
func ParseJieYuanOpenContextResult(payload []byte) (uint8, error) {
	if len(payload) < 1 {
		return 0, fmt.Errorf("%w: empty jieyuan context response", ErrInvalidPayload)
	}
	return payload[0], nil
}

// ParseJieYuanBoxOpenResult 解析 232/34 批量开箱回包首字节 result_code。
//
// 官方抓包回包为整包 zlib 压缩，解压后结构为:
//
//	[Uint32 ActionID][UByte result_code][Uint16 reward_count][reward_count * 10B 奖励条目]
//
// 成功码为 5。调用方必须依据结果码分流，禁止仅凭“收到回包”判定开箱成功。
func ParseJieYuanBoxOpenResult(payload []byte) (uint8, error) {
	if len(payload) < 1 {
		return 0, fmt.Errorf("%w: empty jieyuan open response", ErrInvalidPayload)
	}
	return payload[0], nil
}

// --------------------------------------------------------------------------------
// 3. 领取日活跃奖励 (协议见 assistant.go)
// --------------------------------------------------------------------------------

// --------------------------------------------------------------------------------
// 4. 奇门遁甲
// --------------------------------------------------------------------------------

// BuildQiMenArrayInfoPacket 构造查询奇门法阵封包 (Module 486, Action 7)。
func BuildQiMenArrayInfoPacket(prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteUint32(0) // Type
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDQiMenArrayInfo, w.Bytes())
}

// BuildQiMenPuppetInfoPacket 构造查询阵眼傀儡属性封包 (Module 485, Action 0)。
func BuildQiMenPuppetInfoPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDQiMenArrayInfo
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDQiMenPuppetInfo, w.Bytes())
}

// BuildQiMenUpgradeStarPacket 构造奇门星图升级注灵封包 (Module 486, Action 1)。
func BuildQiMenUpgradeStarPacket(prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteUint32(0)
	if prevAct == 0 {
		prevAct = ActionIDQiMenPuppetInfo
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDQiMenUpgradeStar, w.Bytes())
}

// BuildQiMenFormationDeployPacket 构造奇门遁甲布阵生效封包 (Module 584, Action 1)。
func BuildQiMenFormationDeployPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDQiMenUpgradeStar
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDQiMenFormationDeploy, w.Bytes())
}

// BuildQiMenFormationConfirmPacket 构造奇门遁甲阵法确认封包 (Module 584, Action 3)。
func BuildQiMenFormationConfirmPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDQiMenFormationDeploy
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDQiMenFormationConfirm, w.Bytes())
}

// --------------------------------------------------------------------------------
// 5. 淬炼粗铁 (协议实现见 refine_pool.go)
// --------------------------------------------------------------------------------

// --------------------------------------------------------------------------------
// 6. 天神赐福
// --------------------------------------------------------------------------------

// --------------------------------------------------------------------------------
// 6. 天神赐福 (协议见 god_welfare.go)
// --------------------------------------------------------------------------------

// --------------------------------------------------------------------------------
// 7. 劫镖
// --------------------------------------------------------------------------------

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

// BuildXunXianGetProgressPacket 构造查询寻仙之路进度封包 (Module 466, Action 1)。
func BuildXunXianGetProgressPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDXunXianGetProgress, w.Bytes())
}

// BuildXunXianChallengePacket 构造挑战寻仙之路关卡节点封包 (Module 466, Action 3)。
func BuildXunXianChallengePacket(nodeID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(nodeID)
	if prevAct == 0 {
		prevAct = ActionIDXunXianGetProgress
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDXunXianChallenge, w.Bytes())
}

// BuildXunXianClaimChestPacket 构造领取寻仙之路通关宝箱封包 (Module 466, Action 13)。
func BuildXunXianClaimChestPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDXunXianChallenge
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDXunXianClaimChest, w.Bytes())
}

// --------------------------------------------------------------------------------
// 9. 锁妖塔
// --------------------------------------------------------------------------------

// 锁妖塔结果码 (Mod_RepressDemonTower_Base)。
const (
	// DemonTowerResultSuccess 成功
	DemonTowerResultSuccess uint8 = 0
	// DemonTowerResultFailed 失败
	DemonTowerResultFailed uint8 = 1
	// DemonTowerResultFunctionNoOpen 功能未开放
	DemonTowerResultFunctionNoOpen uint8 = 2
	// DemonTowerResultNotEnoughTicket 挑战次数不足
	DemonTowerResultNotEnoughTicket uint8 = 3
	// DemonTowerResultLayerError 层数错误
	DemonTowerResultLayerError uint8 = 4
	// DemonTowerResultItemPackFull 背包已满
	DemonTowerResultItemPackFull uint8 = 5
	// DemonTowerResultRaceStepError 赛季阶段错误
	DemonTowerResultRaceStepError uint8 = 6
	// DemonTowerResultNotEnoughIngot 元宝不足
	DemonTowerResultNotEnoughIngot uint8 = 7
	// DemonTowerResultBuyNumError 购买次数错误
	DemonTowerResultBuyNumError uint8 = 8
)

// DemonTowerStep 表示锁妖塔升阶条件条目 (step_list)。
type DemonTowerStep struct {
	// StepUpID 升阶条件 ID
	StepUpID int32
	// LayerNeed 需要通关层数
	LayerNeed int32
	// RoundNeed 需要总回合数
	RoundNeed int32
}

// DemonTowerLayer 表示锁妖塔已通关层条目 (layer_list)。
type DemonTowerLayer struct {
	// LayerID 层编号，也是 challenge 请求的目标 ID
	LayerID int32
	// FastestRound 该层最快通关回合数
	FastestRound int32
}

// DemonTowerPanel 表示锁妖塔主面板 panel_info 回包。
type DemonTowerPanel struct {
	// Result 业务结果码 (DemonTowerResult*)
	Result uint8
	// IsFirst 是否本赛季首次进入
	IsFirst int32
	// Season 赛季编号
	Season int32
	// LeftTime 本赛季剩余秒数
	LeftTime int32
	// NowStep 当前阶数
	NowStep int32
	// NowFinishLayer 当前已通关层数
	NowFinishLayer int32
	// NowTotalRound 当前累计回合数
	NowTotalRound int32
	// NextLayerLeftTime 下一层剩余时间
	NextLayerLeftTime int32
	// Rank 当前排名
	Rank int32
	// StepList 升阶条件列表
	StepList []DemonTowerStep
	// LayerList 已通关层列表
	LayerList []DemonTowerLayer
	// BuyTicketNum 可购买次数
	BuyTicketNum int32
	// NowTicketNum 当前剩余挑战次数
	NowTicketNum int32
	// RaceStep 赛季阶段编号
	RaceStep int32
	// Ranks 排名相关标记
	Ranks int32
}

// NextLayerID 返回当前可挑战的层 ID。
//
// 线上实测（2026-11-07 22:14，s0-niuchangsheng）：
//
//	now_finish_layer=40 时 challenge(40) 返回 SUCCESS(0)，
//	challenge(41) 返回 LAYER_ERROR(4)。
//
// 因此 now_finish_layer / layer_list 的最大值就是当前可挑战层，
// 不是“已通关层数 + 1”。无记录时从第 1 层开始。
func (p *DemonTowerPanel) NextLayerID() int32 {
	var current int32
	for _, l := range p.LayerList {
		if l.LayerID > current {
			current = l.LayerID
		}
	}
	if p.NowFinishLayer > current {
		current = p.NowFinishLayer
	}
	if current <= 0 {
		return 1
	}
	return current
}

// StepUpRequirement 返回当前阶升阶条件 (step_list 第一条)。
// 线上实测 2026-11-11：step_list=[{1,40,434}]，now_finish_layer=40，
// now_total_round=521 > 434，challenge(40) 返回 RACE_STEP_ERROR(6)，
// 即层数已满但回合数超标，无法升阶。
func (p *DemonTowerPanel) StepUpRequirement() (layerNeed, roundNeed int32, ok bool) {
	if p == nil || len(p.StepList) == 0 {
		return 0, 0, false
	}
	return p.StepList[0].LayerNeed, p.StepList[0].RoundNeed, true
}

// BuildDemonTowerPanelPacket 构造查询锁妖塔主面板封包 (Module 705, Action 0)。
//
// 官方 request 为空数组；实服验证空载荷可直接返回面板数据。
func BuildDemonTowerPanelPacket() *Packet {
	return NewPacket(ActionIDDemonTowerPanel, nil)
}

// BuildDemonTowerBaseInfoPacket 构造查询锁妖塔赛季开放状态封包 (Module 705, Action 8)。
func BuildDemonTowerBaseInfoPacket() *Packet {
	return NewPacket(ActionIDDemonTowerBaseInfo, nil)
}

// BuildDemonTowerRankPacket 构造查询锁妖塔排行榜封包 (Module 705, Action 5)。
func BuildDemonTowerRankPacket() *Packet {
	return NewPacket(ActionIDDemonTowerRank, nil)
}

// BuildDemonTowerChallengePacket 构造挑战锁妖塔指定层封包 (Module 705, Action 1)。
//
// request 仅含 1 个 IntUtil (目标层 ID)。注意不能追加 PrevAct 尾部：
// 会话层对 HasPrevAct 包会用最近动作号覆盖末尾 4 字节，从而破坏层号。
func BuildDemonTowerChallengePacket(layerID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(layerID)
	return NewPacket(ActionIDDemonTowerChallenge, w.Bytes())
}

// ParseDemonTowerBaseInfoResult 解析赛季开放状态回包 (Module 705, Action 8)。
// 回包为单个 Byte，1 表示处于赛季时间，0 表示赛季未开放。
func ParseDemonTowerBaseInfoResult(payload []byte) (bool, error) {
	if len(payload) == 0 {
		return false, fmt.Errorf("%w: demon tower base_info payload is empty", ErrInvalidPayload)
	}
	return payload[0] != 0, nil
}

// ParseDemonTowerRankSelfLayer 解析排行榜回包 (Module 705, Action 5) 的自身最高层。
//
// 回包前 5 个字段: [UByte result, Int self_layer, Int self_round, Int self_finish_ts, Int role_id, ...]。
// self_layer 为赛季累计通关层数 (跨阶次全局递增)，用于在 layer_list 为空时计算下一层目标。
func ParseDemonTowerRankSelfLayer(payload []byte) (uint8, int32, error) {
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return 0, 0, fmt.Errorf("%w: failed to read demon tower rank result: %v", ErrInvalidPayload, err)
	}
	if result != DemonTowerResultSuccess {
		return result, 0, nil
	}
	selfLayer, err := r.ReadInt32()
	if err != nil {
		return result, 0, fmt.Errorf("%w: failed to read demon tower rank self_layer: %v", ErrInvalidPayload, err)
	}
	return result, selfLayer, nil
}

// ParseDemonTowerPanelResult 解析锁妖塔主面板回包 (Module 705, Action 0)。
//
// 回包字段顺序对齐 com.datas.RepressDemonTowerData.panel_info:
// [UByte result, Int×8, [[Int,Int,Int]], [[Int,Int]], Int×4]。
func ParseDemonTowerPanelResult(payload []byte) (*DemonTowerPanel, error) {
	r := NewReader(payload)
	res := &DemonTowerPanel{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read demon tower result: %v", ErrInvalidPayload, err)
	}
	if res.Result != DemonTowerResultSuccess {
		return res, nil
	}

	fields := []*int32{
		&res.IsFirst, &res.Season, &res.LeftTime, &res.NowStep,
		&res.NowFinishLayer, &res.NowTotalRound, &res.NextLayerLeftTime, &res.Rank,
	}
	for i, dst := range fields {
		if *dst, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read demon tower field[%d]: %v", ErrInvalidPayload, i, err)
		}
	}

	stepCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read demon tower step count: %v", ErrInvalidPayload, err)
	}
	res.StepList = make([]DemonTowerStep, 0, stepCount)
	for i := 0; i < int(stepCount); i++ {
		var step DemonTowerStep
		if step.StepUpID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read demon tower step[%d].step_up_id: %v", ErrInvalidPayload, i, err)
		}
		if step.LayerNeed, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read demon tower step[%d].layer_need: %v", ErrInvalidPayload, i, err)
		}
		if step.RoundNeed, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read demon tower step[%d].round_need: %v", ErrInvalidPayload, i, err)
		}
		res.StepList = append(res.StepList, step)
	}

	layerCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read demon tower layer count: %v", ErrInvalidPayload, err)
	}
	res.LayerList = make([]DemonTowerLayer, 0, layerCount)
	for i := 0; i < int(layerCount); i++ {
		var layer DemonTowerLayer
		if layer.LayerID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read demon tower layer[%d].layer_id: %v", ErrInvalidPayload, i, err)
		}
		if layer.FastestRound, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read demon tower layer[%d].fastest_round: %v", ErrInvalidPayload, i, err)
		}
		res.LayerList = append(res.LayerList, layer)
	}

	tail := []*int32{&res.BuyTicketNum, &res.NowTicketNum, &res.RaceStep, &res.Ranks}
	for i, dst := range tail {
		if *dst, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read demon tower tail field[%d]: %v", ErrInvalidPayload, i, err)
		}
	}
	return res, nil
}

// ParseDemonTowerChallengeResult 解析挑战回包 (Module 705, Action 1) 的首字节结果码。
//
// 成功后服务端还会下发大型战报与奖励列表，自动挑战只需结果码，
// 无需完整解析战报结构。
func ParseDemonTowerChallengeResult(payload []byte) (uint8, error) {
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return 0, fmt.Errorf("%w: failed to read demon tower challenge result: %v", ErrInvalidPayload, err)
	}
	return result, nil
}

// --------------------------------------------------------------------------------
// 10. 山河游历
// --------------------------------------------------------------------------------

// BuildEraTravelGetStatePacket 构造查询山河游历状态封包 (Module 850, Action 4)。
func BuildEraTravelGetStatePacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDEraTravelGetState, w.Bytes())
}

// BuildEraTravelRollDicePacket 构造山河游历掷骰移动封包 (Module 850, Action 0)。
func BuildEraTravelRollDicePacket(step int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(step)
	if prevAct == 0 {
		prevAct = ActionIDEraTravelGetState
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDEraTravelRollDice, w.Bytes())
}

// --------------------------------------------------------------------------------
// 11. 灵道初解
// --------------------------------------------------------------------------------

// BuildLingDaoGetStatusPacket 构造查询灵道初解进度封包 (Module 658, Action 0)。
func BuildLingDaoGetStatusPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDLingDaoGetStatus, w.Bytes())
}

// BuildLingDaoOneKeyAwardPacket 构造灵道战斗通行证一键领取封包 (Module 711, Action 3)。
func BuildLingDaoOneKeyAwardPacket() *Packet {
	return NewPacket(ActionIDLingDaoOneKeyAward, nil)
}

// BuildLingDaoComprehendPacket 构造查询灵道战斗通行证面板封包 (Module 711, Action 0)。
func BuildLingDaoComprehendPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDLingDaoGetStatus
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDLingDaoComprehend, w.Bytes())
}

// --------------------------------------------------------------------------------
// 12. 山海幻兽
// --------------------------------------------------------------------------------

// BuildHuanshouPanelPacket 构造查询山海幻兽主面板封包 (Module 701, Action 0)。
func BuildHuanshouPanelPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDHuanshouPanel, w.Bytes())
}

// BuildHuanshouInvadeChallengePacket 构造挑战幻兽入侵副本封包 (Module 696, Action 2)。
// 依据 haha.pcapng 抓包事实: 载荷仅包含 PrevAct。
func BuildHuanshouInvadeChallengePacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDHuanshouPanel
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDHuanshouInvadeChallenge, w.Bytes())
}

// BuildHuanshouInvadeNodePacket 构造山海幻兽入侵逐关挑战扫荡封包 (Module 696, Action 4)。
func BuildHuanshouInvadeNodePacket(nodeID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(nodeID)
	if prevAct == 0 {
		prevAct = ActionIDHuanshouInvadeChallenge
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDHuanshouInvadeSettle, w.Bytes())
}

// BuildHuanshouInvadeSweepPacket 构造秒完成幻兽入侵扫荡封包 (Module 696, Action 3)。
func BuildHuanshouInvadeSweepPacket(prevAct uint32) *Packet {
	w := NewWriter()
	// 依据抓包实采: 00 0f cc af 06 + PrevAct
	w.WriteUint32(0x000FCCAF)
	w.WriteUint8(0x06)
	if prevAct == 0 {
		prevAct = ActionIDHuanshouInvadeChallenge
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDHuanshouInvadeSweep, w.Bytes())
}

// --------------------------------------------------------------------------------
// 14. 悟法领域
// --------------------------------------------------------------------------------

// BuildWuFaFieldInfoPacket 构造查询悟法领域面板封包 (Module 658, Action 0)。
//
// 对齐官方 Mod_WuFaField_Base.panel_info：request 为空，仅携带 PrevAct。
// 响应体由 ParseWuFaPanel 解析；官方协议中该回包没有 result_code。
func BuildWuFaFieldInfoPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDWuFaFieldInfo, w.Bytes())
}

// BuildWuFaDrawCardPacket 构造悟法领域感悟抽卡封包 (Module 658, Action 1)。
//
// 对齐官方 Mod_WuFaField_Base.draw：request=[Int field_type, UByte draw_type]，
// draw_type 取 WuFaDrawOne (0) 或 WuFaDrawTen (1)。
//
// 线上实采 (2026-11-03): draw 处理器对载荷做严格长度校验，末尾追加 4 字节 PrevAct
// 会被服务端静默丢弃且无任何回包；去掉 PrevAct 后同一载荷立即返回结果码 2。
// 因此本请求固定为 5 字节，禁止再拼 prevAct。
func BuildWuFaDrawCardPacket(fieldType int32, drawType uint8) *Packet {
	w := NewWriter()
	w.WriteInt32(fieldType)
	w.WriteUint8(drawType)
	return NewPacket(ActionIDWuFaDrawCard, w.Bytes())
}

// --------------------------------------------------------------------------------
// 15. 宗师修行 (协议常量与编解码见 master_practice.go)
// --------------------------------------------------------------------------------

// --------------------------------------------------------------------------------
// 16. 天帝宝库
// --------------------------------------------------------------------------------

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
func BuildTreasureHouseUseFreeFlagPacket(flag int32, prevAct uint32) *Packet {
	return BuildTreasureHouseConfirmPacket(flag, prevAct)
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

// Helper: DecompressPayloadIfNeeded 透明尝试解压 zlib 载荷
func DecompressPayloadIfNeeded(payload []byte) []byte {
	if len(payload) >= 2 && payload[0] == 0x78 {
		zr, err := zlib.NewReader(bytes.NewReader(payload))
		if err == nil {
			defer zr.Close()
			decomp, err := io.ReadAll(zr)
			if err == nil {
				return decomp
			}
		}
	}
	return payload
}
