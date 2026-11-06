// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现猎命 (Mod_Fate_Base, Module 22 / 0x0016) 与命宫碎片兑换协议。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_Fate_Base：
//
//	get_fate_npc (Act 0, 0x00160000) request=[] response=[Byte,[Byte,Byte,Byte,Byte,Short],Int]
//	appoint_fate_npc (Act 9, 0x00160009) request=[Byte] response=[UByte,Short,Byte,Long]
//	get_ming_gong_info (Act 31, 0x0016001F) request=[] response=[Int,Int,Int]
//	exchange_ming_gong_scrap (Act 34, 0x00160022) request=[Int] response=[UByte,Int]
//
// 线上实测：
//   - get_fate_npc / get_ming_gong_info 必须在载荷尾部携带 PrevAct；
//   - appoint_fate_npc 仅接受 1 字节 npc_id，追加 PrevAct 会被静默丢弃；
//   - 猎命师列表中只有当前已解锁的猎命师可指定，其余 id 返回 INVALID_FATE_NPC_ID(12)。
package protocol

import "fmt"

const (
	// ActionIDFateGetNPC 查询猎命师列表 (Module 22, Action 0 -> 0x00160000)
	ActionIDFateGetNPC uint32 = 0x00160000
	// ActionIDFateAppointNPC 指定猎命师 (Module 22, Action 9 -> 0x00160009)
	ActionIDFateAppointNPC uint32 = 0x00160009
	// ActionIDFateMingGongInfo 查询命宫信息 (Module 22, Action 31 -> 0x0016001F)
	ActionIDFateMingGongInfo uint32 = 0x0016001F
	// ActionIDFateExchangeMingGongScrap 购买命宫碎片 (Module 22, Action 34 -> 0x00160022)
	ActionIDFateExchangeMingGongScrap uint32 = 0x00160022
)

// 猎命业务结果码 (Mod_Fate_Base)。
const (
	// FateHuntResultSuccess 成功
	FateHuntResultSuccess uint8 = 0
	// FateHuntResultNotEnoughCoin 铜钱不足
	FateHuntResultNotEnoughCoin uint8 = 14
	// FateHuntResultInvalidFateNPCID 猎命师编号无效/未解锁
	FateHuntResultInvalidFateNPCID uint8 = 12
	// FateHuntResultLimitNum 兑换次数已达上限
	FateHuntResultLimitNum uint8 = 42
)

// FateHunter 表示猎命师列表中的一位猎命师。
type FateHunter struct {
	// NpcID 猎命师编号
	NpcID uint8
	// Status 状态 (1 表示当前已激活)
	Status uint8
	// CanBuyTimer 可购买次数计数
	CanBuyTimer uint8
	// MaxBuyTimes 最大购买次数
	MaxBuyTimes uint8
	// BuyPrice 购买价格
	BuyPrice int16
}

// FateHunterPanel 表示猎命师面板。
type FateHunterPanel struct {
	// FreeAppointTimes 剩余免费指定次数
	FreeAppointTimes uint8
	// Hunters 猎命师列表
	Hunters []FateHunter
	// Tail 面板尾字段 (线上固定为 100，含义未明)
	Tail int32
}

// AppointFateResult 表示指定猎命师的结算结果。
type AppointFateResult struct {
	// Result 结果码
	Result uint8
	// FateID 获得的命格编号
	FateID int16
	// NpcID 服务端确认的猎命师编号
	NpcID uint8
	// TempFateID 临时命格唯一编号
	TempFateID int64
}

// MingGongInfo 表示命宫信息。
type MingGongInfo struct {
	// MingGong 当前命宫等级
	MingGong int32
	// Scrap 命宫碎片持有数量
	Scrap int32
	// ExchangeTimes 今日剩余兑换次数
	ExchangeTimes int32
}

// MingGongExchangeResult 表示命宫碎片购买结果。
type MingGongExchangeResult struct {
	// Result 结果码
	Result uint8
	// Scrap 交易后的命宫碎片数量
	Scrap int32
}

// BuildFateGetNPCPacket 构造猎命师列表查询封包。
//
// 官方 request 为空，但线上服务端要求尾部携带 PrevAct，因此载荷固定为 4 字节，
// 由会话层用最近一次动作号覆盖。
func BuildFateGetNPCPacket() *Packet {
	pkt := NewPacket(ActionIDFateGetNPC, []byte{0, 0, 0, 0})
	pkt.HasPrevAct = true
	return pkt
}

// BuildFateAppointNPCPacket 构造指定猎命师封包 (仅 1 字节 npc_id)。
func BuildFateAppointNPCPacket(npcID uint8) *Packet {
	return NewPacket(ActionIDFateAppointNPC, []byte{npcID})
}

// BuildFateMingGongInfoPacket 构造命宫信息查询封包 (需携带 PrevAct)。
func BuildFateMingGongInfoPacket() *Packet {
	pkt := NewPacket(ActionIDFateMingGongInfo, []byte{0, 0, 0, 0})
	pkt.HasPrevAct = true
	return pkt
}

// BuildFateExchangeMingGongScrapPacket 构造购买命宫碎片封包。
func BuildFateExchangeMingGongScrapPacket(num int32) *Packet {
	w := NewWriter()
	w.WriteInt32(num)
	return NewPacket(ActionIDFateExchangeMingGongScrap, w.Bytes())
}

// ParseFateHunterPanel 解析猎命师列表回包。
func ParseFateHunterPanel(payload []byte) (*FateHunterPanel, error) {
	r := NewReader(payload)
	free, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("猎命师免费次数读取失败: %w", err)
	}
	n, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("猎命师数量读取失败: %w", err)
	}
	panel := &FateHunterPanel{
		FreeAppointTimes: free,
		Hunters:          make([]FateHunter, 0, n),
	}
	for i := 0; i < int(n); i++ {
		var hunter FateHunter
		if hunter.NpcID, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("猎命师[%d]编号读取失败: %w", i, err)
		}
		if hunter.Status, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("猎命师[%d]状态读取失败: %w", i, err)
		}
		if hunter.CanBuyTimer, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("猎命师[%d]可购买计数读取失败: %w", i, err)
		}
		if hunter.MaxBuyTimes, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("猎命师[%d]最大购买次数读取失败: %w", i, err)
		}
		if hunter.BuyPrice, err = r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("猎命师[%d]价格读取失败: %w", i, err)
		}
		panel.Hunters = append(panel.Hunters, hunter)
	}
	if panel.Tail, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("猎命师面板尾字段读取失败: %w", err)
	}
	return panel, nil
}

// ParseFateAppointResult 解析指定猎命师回包。
func ParseFateAppointResult(payload []byte) (*AppointFateResult, error) {
	r := NewReader(payload)
	var res AppointFateResult
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("猎命结果码读取失败: %w", err)
	}
	if res.FateID, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("猎命命格编号读取失败: %w", err)
	}
	if res.NpcID, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("猎命师编号读取失败: %w", err)
	}
	if res.TempFateID, err = r.ReadInt64(); err != nil {
		return nil, fmt.Errorf("临时命格编号读取失败: %w", err)
	}
	return &res, nil
}

// ParseFateMingGongInfo 解析命宫信息回包。
func ParseFateMingGongInfo(payload []byte) (*MingGongInfo, error) {
	r := NewReader(payload)
	var info MingGongInfo
	var err error
	if info.MingGong, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("命宫等级读取失败: %w", err)
	}
	if info.Scrap, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("命宫碎片数量读取失败: %w", err)
	}
	if info.ExchangeTimes, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("命宫碎片兑换次数读取失败: %w", err)
	}
	return &info, nil
}

// ParseFateMingGongExchangeResult 解析命宫碎片购买回包。
func ParseFateMingGongExchangeResult(payload []byte) (*MingGongExchangeResult, error) {
	r := NewReader(payload)
	var res MingGongExchangeResult
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("命宫碎片兑换结果码读取失败: %w", err)
	}
	if res.Scrap, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("命宫碎片余额读取失败: %w", err)
	}
	return &res, nil
}

// BuildFateHunterPanelResultPacket 构造猎命师面板回包，供离线 mock 与单测使用。
func BuildFateHunterPanelResultPacket(panel FateHunterPanel) *Packet {
	w := NewWriter()
	w.WriteUint8(panel.FreeAppointTimes)
	w.WriteUint16(uint16(len(panel.Hunters)))
	for _, hunter := range panel.Hunters {
		w.WriteUint8(hunter.NpcID)
		w.WriteUint8(hunter.Status)
		w.WriteUint8(hunter.CanBuyTimer)
		w.WriteUint8(hunter.MaxBuyTimes)
		w.WriteInt16(hunter.BuyPrice)
	}
	w.WriteInt32(panel.Tail)
	return NewPacket(ActionIDFateGetNPC, w.Bytes())
}

// BuildFateAppointResultPacket 构造指定猎命师回包，供离线 mock 与单测使用。
func BuildFateAppointResultPacket(res AppointFateResult) *Packet {
	w := NewWriter()
	w.WriteUint8(res.Result)
	w.WriteInt16(res.FateID)
	w.WriteUint8(res.NpcID)
	w.WriteInt64(res.TempFateID)
	return NewPacket(ActionIDFateAppointNPC, w.Bytes())
}

// BuildFateMingGongInfoResultPacket 构造命宫信息回包，供离线 mock 与单测使用。
func BuildFateMingGongInfoResultPacket(info MingGongInfo) *Packet {
	w := NewWriter()
	w.WriteInt32(info.MingGong)
	w.WriteInt32(info.Scrap)
	w.WriteInt32(info.ExchangeTimes)
	return NewPacket(ActionIDFateMingGongInfo, w.Bytes())
}

// BuildFateMingGongExchangeResultPacket 构造命宫碎片购买回包，供离线 mock 与单测使用。
func BuildFateMingGongExchangeResultPacket(res MingGongExchangeResult) *Packet {
	w := NewWriter()
	w.WriteUint8(res.Result)
	w.WriteInt32(res.Scrap)
	return NewPacket(ActionIDFateExchangeMingGongScrap, w.Bytes())
}
