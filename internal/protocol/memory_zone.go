// Package protocol 提供神仙道二进制协议封包编解码与响应解析能力。
//
// 本文件实现「回忆之境」(Mod_MemoryZone_Base, Module 526 / 0x020E) 的
// 请求封包构建、结果码语义映射与响应解码。
//
// 协议基准来源：
//   - 官方 Main.swf (com.protocols.Mod_MemoryZone_Base / com.datas.MemoryZoneData / com.assist.server.source.MemoryZoneTypeData)
//   - 真实抓包 huiyizhijing.pcapng (2026-11-28, 主服 TCP 8200)：
//     * 526/1 main_panel    request=[PrevAct]                -> UByte result, [[Int type, Int have_num, Int max_num, Int get_award]], Int award_daoyuan
//     * 526/2 back_panel    request=[Int type, PrevAct]      -> UByte result, [[Int role_id, Int have_scrap, Int need_scrap]], Int today_back_times, Int daily_free_times, Int daily_ingot_times, Int cost_ingot
//     * 526/3 get_bind_award request=[Int type, PrevAct]     -> UByte result, [[Int item_id, Int item_num]]
//     * 526/4 backtrack     request=[Int role_id, PrevAct]   -> UByte result, [[Int item_id, Int item_num]], [[Int role_id, Int have_scrap, Int need_scrap]], Int today_back_times, Int cost_ingot
package protocol

import "fmt"

// ModuleMemoryZone 回忆之境模块号 (Mod_MemoryZone_Base, 0x020E)。
const ModuleMemoryZone uint32 = 526

// 回忆之境协议动作号。
const (
	// ActionIDMemoryZoneMainPanel 查询回忆之境主面板与各分区收集进度 (526/1, 0x020E0001)
	ActionIDMemoryZoneMainPanel uint32 = (ModuleMemoryZone << 16) | 1
	// ActionIDMemoryZoneBackPanel 查询指定分区伙伴碎片进度与今日回溯次数 (526/2, 0x020E0002)
	ActionIDMemoryZoneBackPanel uint32 = (ModuleMemoryZone << 16) | 2
	// ActionIDMemoryZoneGetBindAward 领取指定分区集齐羁绊奖励 (526/3, 0x020E0003)
	ActionIDMemoryZoneGetBindAward uint32 = (ModuleMemoryZone << 16) | 3
	// ActionIDMemoryZoneBacktrack 对指定伙伴执行一次回忆回溯 (526/4, 0x020E0004)
	ActionIDMemoryZoneBacktrack uint32 = (ModuleMemoryZone << 16) | 4
)

// Mod_MemoryZone_Base 结果码常量。
const (
	MemoryZoneResultSuccess  uint8 = 0 // SUCCESS
	MemoryZoneResultFailed   uint8 = 1 // FAILED
	MemoryZoneResultMaxTimes uint8 = 2 // MAX_TIMES (回溯次数满)
	MemoryZoneResultNoIngot  uint8 = 3 // NO_INGOT (元宝不足)
)

// MemoryZoneResultText 返回回忆之境结果码的可读说明。
func MemoryZoneResultText(code uint8) string {
	switch code {
	case MemoryZoneResultSuccess:
		return "成功"
	case MemoryZoneResultFailed:
		return "失败"
	case MemoryZoneResultMaxTimes:
		return "回溯次数已满"
	case MemoryZoneResultNoIngot:
		return "元宝不足"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// MemoryZoneCategory 表示 526/1 main_panel 中的单个回忆分区进度条目。
type MemoryZoneCategory struct {
	Type     int32 // 分区类型 (例如 1/2/3，其中 3 为三个魔神专区)
	HaveNum  int32 // 已拥有伙伴数量
	MaxNum   int32 // 分区伙伴总数
	GetAward int32 // 羁绊奖励领取标记
}

// MemoryZoneMainPanel 表示 526/1 main_panel 的响应快照。
type MemoryZoneMainPanel struct {
	Result       uint8
	Categories   []MemoryZoneCategory
	AwardDaoYuan int32
}

// MemoryZonePartner 表示 526/2 back_panel 与 526/4 backtrack 中的伙伴碎片条目。
type MemoryZonePartner struct {
	RoleID    int32 // 伙伴 role_id (如魔神 486/487/488)
	HaveScrap int32 // 当前已有碎片数
	NeedScrap int32 // 招募/集齐所需碎片数 (如 500)
}

// Owned 返回该伙伴是否已拥有（已拥有时不可回忆，对齐 MemoryZoneInfo.isCurRoleOwn）。
func (p MemoryZonePartner) Owned() bool {
	return p.NeedScrap > 0 && p.HaveScrap >= p.NeedScrap
}

// MemoryZoneBackPanel 表示 526/2 back_panel 的响应快照。
type MemoryZoneBackPanel struct {
	Result          uint8
	Partners        []MemoryZonePartner
	TodayBackTimes  int32 // 今日已回溯次数
	DailyFreeTimes  int32 // 每日免费回溯次数
	DailyIngotTimes int32 // 每日可元宝回溯次数上限
	CostIngot       int32 // 下次回溯所需元宝 (0 表示免费)
}

// HasFreeBacktrack 返回当前是否还有免费回溯次数（严格 0 元宝约束）。
func (p *MemoryZoneBackPanel) HasFreeBacktrack() bool {
	if p == nil {
		return false
	}
	return p.TodayBackTimes < p.DailyFreeTimes && p.CostIngot == 0
}

// MemoryZoneAward 表示回溯或羁绊领奖获得的道具条目。
type MemoryZoneAward struct {
	ItemID  int32
	ItemNum int32
}

// MemoryZoneBacktrackResult 表示 526/4 backtrack 的响应结果。
type MemoryZoneBacktrackResult struct {
	Result         uint8
	Awards         []MemoryZoneAward
	Partners       []MemoryZonePartner
	TodayBackTimes int32
	CostIngot      int32
}

// BuildMemoryZoneMainPanelPacket 构造 526/1 main_panel 请求包 (空业务参数 + 自动 PrevAct 尾链)。
func BuildMemoryZoneMainPanelPacket() *Packet {
	return NewPacketWithAutoPrevAct(ActionIDMemoryZoneMainPanel, nil)
}

// BuildMemoryZoneBackPanelPacket 构造 526/2 back_panel 请求包 (Int32 zoneType + 自动 PrevAct 尾链)。
func BuildMemoryZoneBackPanelPacket(zoneType int32) *Packet {
	w := NewWriter()
	w.WriteInt32(zoneType)
	return NewPacketWithAutoPrevAct(ActionIDMemoryZoneBackPanel, w.Bytes())
}

// BuildMemoryZoneGetBindAwardPacket 构造 526/3 get_bind_award 请求包 (Int32 zoneType + 自动 PrevAct 尾链)。
func BuildMemoryZoneGetBindAwardPacket(zoneType int32) *Packet {
	w := NewWriter()
	w.WriteInt32(zoneType)
	return NewPacketWithAutoPrevAct(ActionIDMemoryZoneGetBindAward, w.Bytes())
}

// BuildMemoryZoneBacktrackPacket 构造 526/4 backtrack 请求包 (Int32 roleID + 自动 PrevAct 尾链)。
func BuildMemoryZoneBacktrackPacket(roleID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(roleID)
	return NewPacketWithAutoPrevAct(ActionIDMemoryZoneBacktrack, w.Bytes())
}

// ParseMemoryZoneMainPanel 解析 526/1 main_panel 响应。
// 先校验结果码，非 SUCCESS 时不读取后续业务字段。
func ParseMemoryZoneMainPanel(payload []byte) (*MemoryZoneMainPanel, error) {
	if len(payload) == 0 {
		return nil, ErrPayloadTruncated
	}
	r := NewReader(payload)
	res, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("protocol: parse memory_zone main_panel result: %w", err)
	}
	out := &MemoryZoneMainPanel{Result: res}
	if res != MemoryZoneResultSuccess {
		return out, nil
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("protocol: parse memory_zone main_panel count: %w", err)
	}
	out.Categories = make([]MemoryZoneCategory, 0, count)
	for i := 0; i < int(count); i++ {
		var c MemoryZoneCategory
		if c.Type, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("protocol: parse memory_zone category[%d].type: %w", i, err)
		}
		if c.HaveNum, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("protocol: parse memory_zone category[%d].have_num: %w", i, err)
		}
		if c.MaxNum, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("protocol: parse memory_zone category[%d].max_num: %w", i, err)
		}
		if c.GetAward, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("protocol: parse memory_zone category[%d].get_award: %w", i, err)
		}
		out.Categories = append(out.Categories, c)
	}
	if out.AwardDaoYuan, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("protocol: parse memory_zone main_panel award_daoyuan: %w", err)
	}
	return out, nil
}

// ParseMemoryZoneBackPanel 解析 526/2 back_panel 响应。
// 先校验结果码，非 SUCCESS 时不读取后续业务字段。
func ParseMemoryZoneBackPanel(payload []byte) (*MemoryZoneBackPanel, error) {
	if len(payload) == 0 {
		return nil, ErrPayloadTruncated
	}
	r := NewReader(payload)
	res, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("protocol: parse memory_zone back_panel result: %w", err)
	}
	out := &MemoryZoneBackPanel{Result: res}
	if res != MemoryZoneResultSuccess {
		return out, nil
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("protocol: parse memory_zone back_panel count: %w", err)
	}
	out.Partners = make([]MemoryZonePartner, 0, count)
	for i := 0; i < int(count); i++ {
		var p MemoryZonePartner
		if p.RoleID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("protocol: parse memory_zone partner[%d].role_id: %w", i, err)
		}
		if p.HaveScrap, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("protocol: parse memory_zone partner[%d].have_scrap: %w", i, err)
		}
		if p.NeedScrap, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("protocol: parse memory_zone partner[%d].need_scrap: %w", i, err)
		}
		out.Partners = append(out.Partners, p)
	}
	if out.TodayBackTimes, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("protocol: parse memory_zone back_panel today_back_times: %w", err)
	}
	if out.DailyFreeTimes, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("protocol: parse memory_zone back_panel daily_free_times: %w", err)
	}
	if out.DailyIngotTimes, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("protocol: parse memory_zone back_panel daily_ingot_times: %w", err)
	}
	if out.CostIngot, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("protocol: parse memory_zone back_panel cost_ingot: %w", err)
	}
	return out, nil
}

// ParseMemoryZoneBacktrack 解析 526/4 backtrack 响应。
// 先校验结果码，非 SUCCESS 时不读取后续业务字段。
func ParseMemoryZoneBacktrack(payload []byte) (*MemoryZoneBacktrackResult, error) {
	if len(payload) == 0 {
		return nil, ErrPayloadTruncated
	}
	r := NewReader(payload)
	res, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("protocol: parse memory_zone backtrack result: %w", err)
	}
	out := &MemoryZoneBacktrackResult{Result: res}
	if res != MemoryZoneResultSuccess {
		return out, nil
	}
	awardCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("protocol: parse memory_zone backtrack award_count: %w", err)
	}
	out.Awards = make([]MemoryZoneAward, 0, awardCount)
	for i := 0; i < int(awardCount); i++ {
		var a MemoryZoneAward
		if a.ItemID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("protocol: parse memory_zone backtrack award[%d].item_id: %w", i, err)
		}
		if a.ItemNum, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("protocol: parse memory_zone backtrack award[%d].item_num: %w", i, err)
		}
		out.Awards = append(out.Awards, a)
	}
	partnerCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("protocol: parse memory_zone backtrack partner_count: %w", err)
	}
	out.Partners = make([]MemoryZonePartner, 0, partnerCount)
	for i := 0; i < int(partnerCount); i++ {
		var p MemoryZonePartner
		if p.RoleID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("protocol: parse memory_zone backtrack partner[%d].role_id: %w", i, err)
		}
		if p.HaveScrap, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("protocol: parse memory_zone backtrack partner[%d].have_scrap: %w", i, err)
		}
		if p.NeedScrap, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("protocol: parse memory_zone backtrack partner[%d].need_scrap: %w", i, err)
		}
		out.Partners = append(out.Partners, p)
	}
	if out.TodayBackTimes, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("protocol: parse memory_zone backtrack today_back_times: %w", err)
	}
	if out.CostIngot, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("protocol: parse memory_zone backtrack cost_ingot: %w", err)
	}
	return out, nil
}
