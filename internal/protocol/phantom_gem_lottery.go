// Package protocol 实现神仙道二进制网络协议编解码。
//
// 本文件实现「山海幻兽·多宝山」(Mod_PhantomGemLottery_Base, Module 694 / 0x02B6) 协议编解码。
// 依据官方 Main.swf (com.protocols.Mod_PhantomGemLottery_Base / com.datas.PhantomGemLotteryData)、
// phantom_gem_lottery.swf (Mc_PhantomGemLottery) 与真实抓包 duobaoshan.pcapng 对齐：
//   - 694/0 panel_info (0x02B60000): 请求为空 + 4B PrevAct；回包为四座宝山详情、福馈列表及 7 个尾部 Int32。
//   - 694/1 find (0x02B60001): 请求为 [Int8 mountain_id, Int8 find_type] + 4B PrevAct；
//     回包为 [UByte result, [Int32 item_id, Int32 item_num, Int8 is_high], [Int32 item_id, Int32 item_num], Int32 player_rest_time]。
//   - 694/2 get_requite_award (0x02B60002): 请求为 [Int32, Int32] + 4B PrevAct；回包为 [UByte result]。
//
// 四座宝山编号 (phantom_gem_lottery.swf mc_pool1~mc_pool4 + PhantomGemLotteryData.MOUNTAIN_LINGYU_INDEX=4)：
//   - 1: 沧溟 (沧溟宝山)
//   - 2: 玄晖 (玄晖宝山)
//   - 3: 翠青 (翠青宝山)
//   - 4: 灵玉 (灵玉宝山，默认)
package protocol

import (
	"fmt"
	"strings"
)

const (
	// ModulePhantomGemLottery 山海幻兽·多宝山模块编号 (0x02B6)。
	ModulePhantomGemLottery uint16 = 694

	// ActionIDPhantomGemLotteryPanel 查询多宝山面板 (694/0 -> 0x02B60000)。
	ActionIDPhantomGemLotteryPanel uint32 = 0x02B60000
	// ActionIDPhantomGemLotteryFind 执行多宝山寻宝 (694/1 -> 0x02B60001)。
	ActionIDPhantomGemLotteryFind uint32 = 0x02B60001
	// ActionIDPhantomGemLotteryGetRequiteAward 领取多宝山福馈奖励 (694/2 -> 0x02B60002)。
	ActionIDPhantomGemLotteryGetRequiteAward uint32 = 0x02B60002
)

// 四座宝山编号常量。
const (
	PhantomGemMountainCangMing int8 = 1 // 沧溟宝山
	PhantomGemMountainXuanHui  int8 = 2 // 玄晖宝山
	PhantomGemMountainCuiQing  int8 = 3 // 翠青宝山
	PhantomGemMountainLingYu   int8 = 4 // 灵玉宝山 (默认)
)

// 多宝山寻宝类型常量。
const (
	// PhantomGemFindTypeNormal 普通寻宝（消耗道源，每日共享 1 次 + 宗门 buff 额外次数）。
	PhantomGemFindTypeNormal int8 = 0
	// PhantomGemFindTypeHigh 高级寻宝（消耗元宝/寻宝司南，自动化不调用）。
	PhantomGemFindTypeHigh int8 = 1
)

// 多宝山结果码 (Mod_PhantomGemLottery_Base)。
const (
	PhantomGemLotteryResultSuccess              uint8 = 0 // 成功
	PhantomGemLotteryResultFailed               uint8 = 1 // 失败
	PhantomGemLotteryResultNotEnoughTimes       uint8 = 2 // 次数不足
	PhantomGemLotteryResultNotEnoughIngot       uint8 = 3 // 元宝不足
	PhantomGemLotteryResultNotEnoughDaoyuan     uint8 = 4 // 道缘不足
	PhantomGemLotteryResultEudemonsFunNotOpened uint8 = 5 // 山海幻兽功能未开启
	PhantomGemLotteryResultItemPackFull         uint8 = 6 // 背包满
)

// PhantomGemLotteryPoolItem 表示宝山奖池中的单条概率配置项。
type PhantomGemLotteryPoolItem struct {
	ItemID  int32
	ItemNum int32
	Prob    int32
	IsHigh  int8
}

// PhantomGemLotteryMountain 表示单座宝山的状态信息。
type PhantomGemLotteryMountain struct {
	MountainID             int8
	NormalItems            []PhantomGemLotteryPoolItem
	RemainNormalTimes      int32
	HighItems              []PhantomGemLotteryPoolItem
	RemainHighTimes        int32
	RemainExtraNormalTimes int32
}

// TotalRemainNormalTimes 返回该宝山当前剩余普通寻宝总次数（基础次数 + 宗门 buff 额外次数）。
func (m PhantomGemLotteryMountain) TotalRemainNormalTimes() int32 {
	total := m.RemainNormalTimes + m.RemainExtraNormalTimes
	if total < 0 {
		return 0
	}
	return total
}

// PhantomGemLotteryPanel 表示 694/0 panel_info 响应面板。
type PhantomGemLotteryPanel struct {
	Mountains                    []PhantomGemLotteryMountain
	RequiteCount                 int
	RequiteValue                 int32
	TicketNum                    int32
	IsLuckyDay                   int32
	PlayerRestTime               int32
	DiscountNum                  int32
	SectBuffExpireTime           int32
	IsCloudTreasuryPrivilegeFlag int32
}

// MountainByID 按编号查找宝山记录。
func (p *PhantomGemLotteryPanel) MountainByID(id int8) (PhantomGemLotteryMountain, bool) {
	if p == nil {
		return PhantomGemLotteryMountain{}, false
	}
	for _, m := range p.Mountains {
		if m.MountainID == id {
			return m, true
		}
	}
	return PhantomGemLotteryMountain{}, false
}

// PhantomGemLotteryAward 表示 694/1 find 回包中的单条寻宝奖励。
type PhantomGemLotteryAward struct {
	ItemID  int32
	ItemNum int32
	IsHigh  int8
}

// PhantomGemLotteryLuckyItem 表示 694/1 find 回包中的幸运日/额外掉落道具。
type PhantomGemLotteryLuckyItem struct {
	ItemID  int32
	ItemNum int32
}

// PhantomGemLotteryFindResult 表示 694/1 find 响应结果。
type PhantomGemLotteryFindResult struct {
	Result         uint8
	Awards         []PhantomGemLotteryAward
	LuckyItems     []PhantomGemLotteryLuckyItem
	PlayerRestTime int32
}

// ParsePhantomGemMountain 将配置中的宝山名称/拼音/编号解析为协议 mountain_id。
// 空字符串默认返回灵玉 (4)。
func ParsePhantomGemMountain(raw string) (int8, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case "", "灵玉", "灵玉宝山", "lingyu", "ling_yu", "4":
		return PhantomGemMountainLingYu, true
	case "沧溟", "沧溟宝山", "cangming", "cang_ming", "1":
		return PhantomGemMountainCangMing, true
	case "玄晖", "玄晖宝山", "xuanhui", "xuan_hui", "2":
		return PhantomGemMountainXuanHui, true
	case "翠青", "翠青宝山", "cuiqing", "cui_qing", "3":
		return PhantomGemMountainCuiQing, true
	default:
		return 0, false
	}
}

// PhantomGemMountainName 返回宝山编号对应的中文名称。
func PhantomGemMountainName(id int8) string {
	switch id {
	case PhantomGemMountainCangMing:
		return "沧溟"
	case PhantomGemMountainXuanHui:
		return "玄晖"
	case PhantomGemMountainCuiQing:
		return "翠青"
	case PhantomGemMountainLingYu:
		return "灵玉"
	default:
		return fmt.Sprintf("未知宝山(%d)", id)
	}
}

// PhantomGemLotteryResultName 返回结果码对应的中文描述。
func PhantomGemLotteryResultName(code uint8) string {
	switch code {
	case PhantomGemLotteryResultSuccess:
		return "成功"
	case PhantomGemLotteryResultFailed:
		return "失败"
	case PhantomGemLotteryResultNotEnoughTimes:
		return "次数不足"
	case PhantomGemLotteryResultNotEnoughIngot:
		return "元宝不足"
	case PhantomGemLotteryResultNotEnoughDaoyuan:
		return "道缘不足"
	case PhantomGemLotteryResultEudemonsFunNotOpened:
		return "山海幻兽功能未开启"
	case PhantomGemLotteryResultItemPackFull:
		return "背包满"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// BuildPhantomGemLotteryPanelPacket 构造查询多宝山面板封包 (694/0, 携带自动 PrevAct)。
func BuildPhantomGemLotteryPanelPacket() *Packet {
	return NewPacketWithAutoPrevAct(ActionIDPhantomGemLotteryPanel, nil)
}

// BuildPhantomGemLotteryFindPacket 构造多宝山寻宝封包 (694/1, 携带自动 PrevAct)。
// 线格式: [Int8 mountain_id, Int8 find_type, Uint32 prevAct]。
func BuildPhantomGemLotteryFindPacket(mountainID int8, findType int8) *Packet {
	w := NewWriter()
	w.WriteInt8(mountainID)
	w.WriteInt8(findType)
	return NewPacketWithAutoPrevAct(ActionIDPhantomGemLotteryFind, w.Bytes())
}

// ParsePhantomGemLotteryPanelResult 解析 694/0 panel_info 回包。
func ParsePhantomGemLotteryPanelResult(payload []byte) (*PhantomGemLotteryPanel, error) {
	r := NewReader(payload)
	mCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: phantom_gem_lottery panel mountain count: %v", ErrInvalidPayload, err)
	}

	panel := &PhantomGemLotteryPanel{
		Mountains: make([]PhantomGemLotteryMountain, 0, mCount),
	}

	readPoolItems := func(label string) ([]PhantomGemLotteryPoolItem, error) {
		cnt, err := r.ReadUint16()
		if err != nil {
			return nil, fmt.Errorf("%w: phantom_gem_lottery %s count: %v", ErrInvalidPayload, label, err)
		}
		items := make([]PhantomGemLotteryPoolItem, 0, cnt)
		for i := 0; i < int(cnt); i++ {
			itemID, err := r.ReadInt32()
			if err != nil {
				return nil, fmt.Errorf("%w: phantom_gem_lottery %s[%d].item_id: %v", ErrInvalidPayload, label, i, err)
			}
			itemNum, err := r.ReadInt32()
			if err != nil {
				return nil, fmt.Errorf("%w: phantom_gem_lottery %s[%d].item_num: %v", ErrInvalidPayload, label, i, err)
			}
			prob, err := r.ReadInt32()
			if err != nil {
				return nil, fmt.Errorf("%w: phantom_gem_lottery %s[%d].prob: %v", ErrInvalidPayload, label, i, err)
			}
			isHigh, err := r.ReadInt8()
			if err != nil {
				return nil, fmt.Errorf("%w: phantom_gem_lottery %s[%d].is_high: %v", ErrInvalidPayload, label, i, err)
			}
			items = append(items, PhantomGemLotteryPoolItem{
				ItemID:  itemID,
				ItemNum: itemNum,
				Prob:    prob,
				IsHigh:  isHigh,
			})
		}
		return items, nil
	}

	for i := 0; i < int(mCount); i++ {
		var m PhantomGemLotteryMountain
		m.MountainID, err = r.ReadInt8()
		if err != nil {
			return nil, fmt.Errorf("%w: phantom_gem_lottery mountain[%d].id: %v", ErrInvalidPayload, i, err)
		}
		m.NormalItems, err = readPoolItems("normal_list")
		if err != nil {
			return nil, err
		}
		m.RemainNormalTimes, err = r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: phantom_gem_lottery mountain[%d].remain_normal_times: %v", ErrInvalidPayload, i, err)
		}
		m.HighItems, err = readPoolItems("high_list")
		if err != nil {
			return nil, err
		}
		m.RemainHighTimes, err = r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: phantom_gem_lottery mountain[%d].remain_high_times: %v", ErrInvalidPayload, i, err)
		}
		m.RemainExtraNormalTimes, err = r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: phantom_gem_lottery mountain[%d].remain_extra_normal_times: %v", ErrInvalidPayload, i, err)
		}
		panel.Mountains = append(panel.Mountains, m)
	}

	// requite_list: [Int32 requite_id, Int32 need_requite_value, [Int32, Int32, Int32, Int32], Int32 get_requite_award_id]
	reqCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: phantom_gem_lottery requite_list count: %v", ErrInvalidPayload, err)
	}
	panel.RequiteCount = int(reqCount)
	for i := 0; i < int(reqCount); i++ {
		if _, err := r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: phantom_gem_lottery requite[%d].id: %v", ErrInvalidPayload, i, err)
		}
		if _, err := r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: phantom_gem_lottery requite[%d].need_value: %v", ErrInvalidPayload, i, err)
		}
		subCount, err := r.ReadUint16()
		if err != nil {
			return nil, fmt.Errorf("%w: phantom_gem_lottery requite[%d].award_list count: %v", ErrInvalidPayload, i, err)
		}
		for j := 0; j < int(subCount); j++ {
			for k := 0; k < 4; k++ {
				if _, err := r.ReadInt32(); err != nil {
					return nil, fmt.Errorf("%w: phantom_gem_lottery requite[%d].award[%d]: %v", ErrInvalidPayload, i, j, err)
				}
			}
		}
		if _, err := r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: phantom_gem_lottery requite[%d].get_award_id: %v", ErrInvalidPayload, i, err)
		}
	}

	// 尾部 7 个 Int32 字段。
	if panel.RequiteValue, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: phantom_gem_lottery requite_value: %v", ErrInvalidPayload, err)
	}
	if panel.TicketNum, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: phantom_gem_lottery ticket_num: %v", ErrInvalidPayload, err)
	}
	if panel.IsLuckyDay, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: phantom_gem_lottery is_lucky_day: %v", ErrInvalidPayload, err)
	}
	if panel.PlayerRestTime, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: phantom_gem_lottery player_rest_time: %v", ErrInvalidPayload, err)
	}
	if panel.DiscountNum, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: phantom_gem_lottery discount_num: %v", ErrInvalidPayload, err)
	}
	if panel.SectBuffExpireTime, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: phantom_gem_lottery sect_buff_expire_time: %v", ErrInvalidPayload, err)
	}
	if panel.IsCloudTreasuryPrivilegeFlag, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: phantom_gem_lottery cloud_treasury_privilege: %v", ErrInvalidPayload, err)
	}

	return panel, nil
}

// ParsePhantomGemLotteryFindResult 解析 694/1 find 回包。
// 遵循回包解析铁律：先校验首字节 result_code，非 SUCCESS 时不强读后续字段。
func ParsePhantomGemLotteryFindResult(payload []byte) (*PhantomGemLotteryFindResult, error) {
	r := NewReader(payload)
	code, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: phantom_gem_lottery find result: %v", ErrInvalidPayload, err)
	}
	res := &PhantomGemLotteryFindResult{Result: code}
	if code != PhantomGemLotteryResultSuccess {
		return res, nil
	}

	awardCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: phantom_gem_lottery find award count: %v", ErrInvalidPayload, err)
	}
	res.Awards = make([]PhantomGemLotteryAward, 0, awardCount)
	for i := 0; i < int(awardCount); i++ {
		itemID, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: phantom_gem_lottery find award[%d].item_id: %v", ErrInvalidPayload, i, err)
		}
		itemNum, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: phantom_gem_lottery find award[%d].item_num: %v", ErrInvalidPayload, i, err)
		}
		isHigh, err := r.ReadInt8()
		if err != nil {
			return nil, fmt.Errorf("%w: phantom_gem_lottery find award[%d].is_high: %v", ErrInvalidPayload, i, err)
		}
		res.Awards = append(res.Awards, PhantomGemLotteryAward{
			ItemID:  itemID,
			ItemNum: itemNum,
			IsHigh:  isHigh,
		})
	}

	luckyCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: phantom_gem_lottery find lucky count: %v", ErrInvalidPayload, err)
	}
	res.LuckyItems = make([]PhantomGemLotteryLuckyItem, 0, luckyCount)
	for i := 0; i < int(luckyCount); i++ {
		itemID, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: phantom_gem_lottery find lucky[%d].item_id: %v", ErrInvalidPayload, i, err)
		}
		itemNum, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: phantom_gem_lottery find lucky[%d].item_num: %v", ErrInvalidPayload, i, err)
		}
		res.LuckyItems = append(res.LuckyItems, PhantomGemLotteryLuckyItem{
			ItemID:  itemID,
			ItemNum: itemNum,
		})
	}

	res.PlayerRestTime, err = r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: phantom_gem_lottery find player_rest_time: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// BuildPhantomGemLotteryPanelResultPacket 构造 694/0 面板回包，供离线 mock 与单元测试使用。
func BuildPhantomGemLotteryPanelResultPacket(p PhantomGemLotteryPanel) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(p.Mountains)))
	for _, m := range p.Mountains {
		w.WriteInt8(m.MountainID)
		w.WriteUint16(uint16(len(m.NormalItems)))
		for _, it := range m.NormalItems {
			w.WriteInt32(it.ItemID)
			w.WriteInt32(it.ItemNum)
			w.WriteInt32(it.Prob)
			w.WriteInt8(it.IsHigh)
		}
		w.WriteInt32(m.RemainNormalTimes)
		w.WriteUint16(uint16(len(m.HighItems)))
		for _, it := range m.HighItems {
			w.WriteInt32(it.ItemID)
			w.WriteInt32(it.ItemNum)
			w.WriteInt32(it.Prob)
			w.WriteInt8(it.IsHigh)
		}
		w.WriteInt32(m.RemainHighTimes)
		w.WriteInt32(m.RemainExtraNormalTimes)
	}
	w.WriteUint16(0) // requite_list empty for synthetic mock
	w.WriteInt32(p.RequiteValue)
	w.WriteInt32(p.TicketNum)
	w.WriteInt32(p.IsLuckyDay)
	w.WriteInt32(p.PlayerRestTime)
	w.WriteInt32(p.DiscountNum)
	w.WriteInt32(p.SectBuffExpireTime)
	w.WriteInt32(p.IsCloudTreasuryPrivilegeFlag)
	return NewPacket(ActionIDPhantomGemLotteryPanel, w.Bytes())
}

// BuildPhantomGemLotteryFindResultPacket 构造 694/1 寻宝回包，供离线 mock 与单元测试使用。
func BuildPhantomGemLotteryFindResultPacket(res PhantomGemLotteryFindResult) *Packet {
	w := NewWriter()
	w.WriteUint8(res.Result)
	if res.Result == PhantomGemLotteryResultSuccess {
		w.WriteUint16(uint16(len(res.Awards)))
		for _, a := range res.Awards {
			w.WriteInt32(a.ItemID)
			w.WriteInt32(a.ItemNum)
			w.WriteInt8(a.IsHigh)
		}
		w.WriteUint16(uint16(len(res.LuckyItems)))
		for _, l := range res.LuckyItems {
			w.WriteInt32(l.ItemID)
			w.WriteInt32(l.ItemNum)
		}
		w.WriteInt32(res.PlayerRestTime)
	}
	return NewPacket(ActionIDPhantomGemLotteryFind, w.Bytes())
}
