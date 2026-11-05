// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现淬炼池 (粗铁提炼/炼器精华) 的协议常量、请求封包构造与回包解析。
// 协议基准: Main.swf 反编译 Mod_RefinePool_Base + haha.pcapng 实机抓包对齐。
package protocol

import (
	"fmt"
)

// --------------------------------------------------------------------------------
// 淬炼池协议常量
// --------------------------------------------------------------------------------

// 铁槽生产状态 (Mod_RefinePool_Base)
const (
	RefinePoolProduceUndo   uint8 = 0 // 空闲/未生产
	RefinePoolProduceDoing  uint8 = 1 // 生产进行中
	RefinePoolProduceDone   uint8 = 2 // 生产完成，可淬炼
	RefinePoolProduceLock   uint8 = 3 // 已锁定
	RefinePoolProduceUnlock uint8 = 4 // 已解锁
)

// 淬炼目标 (refine request 第 1 字节, 对应 Mod_RefinePool_Base.REFINE_1/REFINE_2)
const (
	RefinePoolTargetXuanTie uint8 = 5 // REFINE_1: 淬炼, 产出玄铁与炼器精华
	RefinePoolTargetLingCai uint8 = 6 // REFINE_2: 提炼, 产出灵材
)

// 淬炼消耗类型 (refine request 第 2 字节, Mod_RefinePool_Base.COINS/INGOT)
const (
	RefinePoolCostCoins uint8 = 7 // 铜钱淬炼
	RefinePoolCostIngot uint8 = 8 // 元宝淬炼
)

// 淬炼方式 (refine request 第 3 字节, Mod_RefinePool_Base.NORMAL/ONE_KEY)
const (
	RefinePoolTypeNormal uint8 = 9  // 单次淬炼
	RefinePoolTypeOneKey uint8 = 10 // 一键淬炼 (服务端对该角色全部已完成槽位生效)
)

// 淬炼结果 (refine response result)
const (
	RefinePoolResultSuccess     uint8 = 11 // 成功
	RefinePoolResultLackOfCoins uint8 = 12 // 铜钱不足
	RefinePoolResultLackOfIngot uint8 = 13 // 元宝不足
	RefinePoolResultFailed      uint8 = 14 // 失败
)

// 道具 ID (来源 Pieb.db)
const (
	// ItemRefineEssence 炼器精华, 用于圣器升级。
	ItemRefineEssence int32 = 4759
	// ItemXuanTie 玄铁, 用于圣器锻造与进阶。
	ItemXuanTie int32 = 4760
)

// RefinePoolIron 代表单个粗铁槽位的生产信息。
//
// 线格式 (大端序): Byte seq, UByte produce_status, Int produce_need_time,
// Int produce_remain_time, Byte quality
type RefinePoolIron struct {
	// Seq 槽位序号 (1~10)
	Seq int8
	// ProduceStatus 生产状态 (RefinePoolProduceUndo/Doing/Done/Lock/Unlock)
	ProduceStatus uint8
	// ProduceNeedTime 本轮生产所需秒数
	ProduceNeedTime int32
	// ProduceRemainTime 本轮生产剩余秒数
	ProduceRemainTime int32
	// Quality 粗铁品质 (1 粗铁 ~ 5 玄铁, 0 表示本轮尚未产出)
	Quality int8
}

// RefinePoolAwardItem 代表一份淬炼奖励 (道具 ID + 数量)。
type RefinePoolAwardItem struct {
	// ItemID 道具 ID
	ItemID int32
	// Count 数量
	Count int32
}

// RefinePoolAward 代表单个铁槽的淬炼产出。
//
// 线格式 (大端序): Byte seq, UByte result, Byte quality, Short exp,
// [Int,Int] award_info, [Int,Int] lingcai_info
//
// 注: haha.pcapng 实机回包在 award_info 之后还存在第二个奖励列表 (多数槽位为空,
// 暴击槽位携带额外奖励), 反编译的旧版协议仅声明一个列表, 此处以实机线格式为准。
// 客户端数据层将该列表命名为 lingcai_info, 其中的 Int 对为「灵材编号 + 数量」,
// 灵材编号对应 LingCaiType, 并非 Pieb.db item 表主键。
type RefinePoolAward struct {
	// Seq 槽位序号
	Seq int8
	// Result 该槽位的淬炼结果/奖励类型
	Result uint8
	// Quality 产出粗铁品质
	Quality int8
	// Exp 增加的淬炼池经验
	Exp int16
	// Items 主奖励列表 (award_info, 道具 ID + 数量)
	Items []RefinePoolAwardItem
	// LingCaiItems 灵材奖励列表 (lingcai_info, 灵材编号 + 数量)
	LingCaiItems []RefinePoolAwardItem
}

// RefinePoolPanel 代表 open_panel 回包中的淬炼池状态。
type RefinePoolPanel struct {
	// Irons 全部粗铁槽位
	Irons []RefinePoolIron
	// CurSeq 当前选中的槽位序号
	CurSeq int8
	// Level 淬炼池等级
	Level int8
	// Exp 当前经验
	Exp int32
	// OneKeyLockFlag 一键淬炼锁标记 (4 表示已解锁)
	OneKeyLockFlag uint8
	// CoinsNeed 单槽铜钱淬炼消耗
	CoinsNeed int32
	// IngotNeed 单槽元宝淬炼消耗
	IngotNeed int8
	// LianQiJingHua 持有的炼器精华数量
	LianQiJingHua int32
	// XuanTie 持有的玄铁数量
	XuanTie int16
}

// DoneCount 返回当前已完成生产、可参与一键淬炼的槽位数量。
func (p *RefinePoolPanel) DoneCount() int {
	if p == nil {
		return 0
	}
	n := 0
	for _, iron := range p.Irons {
		if iron.ProduceStatus == RefinePoolProduceDone {
			n++
		}
	}
	return n
}

// RefinePoolRefineResult 代表 refine 回包。
type RefinePoolRefineResult struct {
	// Result 淬炼结果 (RefinePoolResult*)
	Result uint8
	// Irons 淬炼后的槽位状态
	Irons []RefinePoolIron
	// CurSeq 当前选中的槽位序号
	CurSeq int8
	// Awards 各槽位的淬炼产出
	Awards []RefinePoolAward
	// Level 淬炼池等级
	Level int8
	// Exp 当前经验
	Exp int32
	// OneKeyLockFlag 一键淬炼锁标记
	OneKeyLockFlag uint8
	// LianQiJingHua 持有的炼器精华数量
	LianQiJingHua int32
	// XuanTie 持有的玄铁数量
	XuanTie int16
}

// --------------------------------------------------------------------------------
// 请求封包构造
// --------------------------------------------------------------------------------

// BuildRefinePoolOpenPacket 构造打开淬炼池面板封包 (Module 282, Action 0)。
func BuildRefinePoolOpenPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDRefinePoolOpen, w.Bytes())
}

// BuildRefinePoolPanelPacket 构造查询淬炼池资源面板封包 (Module 534, Action 5)。
func BuildRefinePoolPanelPacket(prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteUint32(4) // 默认面板类型
	if prevAct == 0 {
		prevAct = ActionIDRefinePoolOpen
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDRefinePoolPanel, w.Bytes())
}

// BuildRefineExecutePacket 构造淬炼执行封包 (Module 282, Action 1)。
//
// target 为 RefinePoolTargetXuanTie/RefinePoolTargetLingCai, cost 为
// RefinePoolCostCoins/RefinePoolCostIngot, mode 为 RefinePoolTypeNormal/RefinePoolTypeOneKey。
// 请求线格式: UByte target, UByte cost, UByte mode, prevAct。
// 请求本身不携带槽位数量; 服务端在 OneKey 模式下自动处理全部已完成槽位。
func BuildRefineExecutePacket(target, cost, mode uint8, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteUint8(target)
	w.WriteUint8(cost)
	w.WriteUint8(mode)
	if prevAct == 0 {
		prevAct = ActionIDRefinePoolPanel
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDRefineExecute, w.Bytes())
}

// --------------------------------------------------------------------------------
// 回包解析
// --------------------------------------------------------------------------------

func readRefinePoolIrons(r *Reader) ([]RefinePoolIron, error) {
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("refine pool: read iron count: %w", err)
	}
	irons := make([]RefinePoolIron, 0, count)
	for i := 0; i < int(count); i++ {
		seq, err := r.ReadInt8()
		if err != nil {
			return nil, fmt.Errorf("refine pool: read iron[%d] seq: %w", i, err)
		}
		status, err := r.ReadUint8()
		if err != nil {
			return nil, fmt.Errorf("refine pool: read iron[%d] status: %w", i, err)
		}
		need, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("refine pool: read iron[%d] need time: %w", i, err)
		}
		remain, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("refine pool: read iron[%d] remain time: %w", i, err)
		}
		quality, err := r.ReadInt8()
		if err != nil {
			return nil, fmt.Errorf("refine pool: read iron[%d] quality: %w", i, err)
		}
		irons = append(irons, RefinePoolIron{
			Seq:               seq,
			ProduceStatus:     status,
			ProduceNeedTime:   need,
			ProduceRemainTime: remain,
			Quality:           quality,
		})
	}
	return irons, nil
}

func readRefinePoolAwardItems(r *Reader) ([]RefinePoolAwardItem, error) {
	count, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	items := make([]RefinePoolAwardItem, 0, count)
	for i := 0; i < int(count); i++ {
		itemID, err := r.ReadInt32()
		if err != nil {
			return nil, err
		}
		n, err := r.ReadInt32()
		if err != nil {
			return nil, err
		}
		items = append(items, RefinePoolAwardItem{ItemID: itemID, Count: n})
	}
	return items, nil
}

// ParseRefinePoolOpenResponse 解析 open_panel 回包 (Module 282, Action 0)。
func ParseRefinePoolOpenResponse(payload []byte) (*RefinePoolPanel, error) {
	r := NewReader(payload)
	irons, err := readRefinePoolIrons(r)
	if err != nil {
		return nil, err
	}
	panel := &RefinePoolPanel{Irons: irons}
	if panel.CurSeq, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("refine pool: read cur_seq: %w", err)
	}
	if panel.Level, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("refine pool: read level: %w", err)
	}
	if panel.Exp, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("refine pool: read exp: %w", err)
	}
	if panel.OneKeyLockFlag, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("refine pool: read one_key_lock_flag: %w", err)
	}
	if panel.CoinsNeed, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("refine pool: read coins_need: %w", err)
	}
	if panel.IngotNeed, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("refine pool: read ingot_need: %w", err)
	}
	if panel.LianQiJingHua, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("refine pool: read lian_qi_jing_hua: %w", err)
	}
	if panel.XuanTie, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("refine pool: read xuan_tie: %w", err)
	}
	return panel, nil
}

// ParseRefineExecuteResponse 解析 refine 回包 (Module 282, Action 1)。
func ParseRefineExecuteResponse(payload []byte) (*RefinePoolRefineResult, error) {
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("refine pool: read result: %w", err)
	}
	irons, err := readRefinePoolIrons(r)
	if err != nil {
		return nil, err
	}
	res := &RefinePoolRefineResult{Result: result, Irons: irons}
	if res.CurSeq, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("refine pool: read cur_seq: %w", err)
	}
	awardCount, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("refine pool: read award count: %w", err)
	}
	res.Awards = make([]RefinePoolAward, 0, awardCount)
	for i := 0; i < int(awardCount); i++ {
		var award RefinePoolAward
		if award.Seq, err = r.ReadInt8(); err != nil {
			return nil, fmt.Errorf("refine pool: read award[%d] seq: %w", i, err)
		}
		if award.Result, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("refine pool: read award[%d] result: %w", i, err)
		}
		if award.Quality, err = r.ReadInt8(); err != nil {
			return nil, fmt.Errorf("refine pool: read award[%d] quality: %w", i, err)
		}
		if award.Exp, err = r.ReadInt16(); err != nil {
			return nil, fmt.Errorf("refine pool: read award[%d] exp: %w", i, err)
		}
		if award.Items, err = readRefinePoolAwardItems(r); err != nil {
			return nil, fmt.Errorf("refine pool: read award[%d] items: %w", i, err)
		}
		if award.LingCaiItems, err = readRefinePoolAwardItems(r); err != nil {
			return nil, fmt.Errorf("refine pool: read award[%d] lingcai items: %w", i, err)
		}
		res.Awards = append(res.Awards, award)
	}
	if res.Level, err = r.ReadInt8(); err != nil {
		return nil, fmt.Errorf("refine pool: read level: %w", err)
	}
	if res.Exp, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("refine pool: read exp: %w", err)
	}
	if res.OneKeyLockFlag, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("refine pool: read one_key_lock_flag: %w", err)
	}
	if res.LianQiJingHua, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("refine pool: read lian_qi_jing_hua: %w", err)
	}
	if res.XuanTie, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("refine pool: read xuan_tie: %w", err)
	}
	return res, nil
}
