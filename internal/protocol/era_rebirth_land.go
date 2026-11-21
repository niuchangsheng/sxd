// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现轮回净地挂机奖励 (Mod_EraRebirthLandAuto_Base, Module 852 / 0x0354)，
// 对齐官方每日活跃度任务 70「每日轮回净地领取挂机奖励」。
//
//	get_panel_info            (Act 1, 0x03540001) request=[] response=[Int,[Int,Int],Int x16]
//	get_auto_profit           (Act 9, 0x03540009) request=[] response=[UByte,Int,[Int,Long]]
//	one_click_quick_free_back (Act 6, 0x03540006) request=[] response=[UByte,[Int,Long]]
//	one_click_quick_ingot_back(Act 7) 元宝档，自动化永不调用
package protocol

import "fmt"

const (
	// ActionIDEraRebirthLandPanel 轮回净地面板 (Module 852, Action 1 -> 0x03540001)
	ActionIDEraRebirthLandPanel uint32 = 0x03540001
	// ActionIDEraRebirthLandFreeBack 免费一键领取挂机奖励 (Module 852, Action 6 -> 0x03540006)
	ActionIDEraRebirthLandFreeBack uint32 = 0x03540006
	// ActionIDEraRebirthLandAutoProfit 查询/领取挂机收益 (Module 852, Action 9 -> 0x03540009)
	ActionIDEraRebirthLandAutoProfit uint32 = 0x03540009
	// ActionIDEraRebirthLandQuickAuto 免费快速挂机 (Module 852, Action 8 -> 0x03540008)
	//
	// 抓包 (2026-11-21): 请求 [Uint32 PrevAct]（req 为空），回包与 852/6 相同，
	// 为 [UByte result, UInt16 count, [Int item_id, Long num] * count]。
	// 官方客户端每日可免费触发两次，每次直接产出挂机奖励；付费档是 852/5，自动化永不调用。
	ActionIDEraRebirthLandQuickAuto uint32 = 0x03540008
	// ActionIDEraRebirthLandPoolPanel 渡业台/渡业仙池面板 (Module 852, Action 12 -> 0x0354000C)
	ActionIDEraRebirthLandPoolPanel uint32 = 0x0354000C
	// ActionIDEraRebirthLandFreeAward 领取渡业仙池免费奖励 (Module 852, Action 13 -> 0x0354000D)
	ActionIDEraRebirthLandFreeAward uint32 = 0x0354000D
	// ActionIDEraRebirthLandTodayAward 领取渡业仙池今日奖励 (Module 852, Action 14 -> 0x0354000E)
	ActionIDEraRebirthLandTodayAward uint32 = 0x0354000E
	// ActionIDEraRebirthLandDraw 渡业台抽取 (Module 852, Action 16 -> 0x03540010)
	//
	// 抓包 (2026-11-10): 请求 [Int type][Uint32 PrevAct]，type 实测为 0；
	// 回包 [UByte result, Int, Int]，成功时 result=0。消耗渡业符/活动道具，不消耗元宝。
	ActionIDEraRebirthLandDraw uint32 = 0x03540010
)

// 轮回净地结果码 (Mod_EraRebirthLandAuto_Base)。
const (
	// EraRebirthResultSuccess 成功
	EraRebirthResultSuccess uint8 = 0
	// EraRebirthResultFuncNotOpen 功能未开放
	EraRebirthResultFuncNotOpen uint8 = 1
	// EraRebirthResultAlreadyStart 已在挂机
	EraRebirthResultAlreadyStart uint8 = 2
	// EraRebirthResultFailed 失败
	EraRebirthResultFailed uint8 = 3
	// EraRebirthResultNoCanGetGift 暂无可领取奖励
	EraRebirthResultNoCanGetGift uint8 = 4
	// EraRebirthResultPackageFull 背包已满
	EraRebirthResultPackageFull uint8 = 5
	// EraRebirthResultNoStorage 无储物
	EraRebirthResultNoStorage uint8 = 6
	// EraRebirthResultNotEnoughIngot 元宝不足
	EraRebirthResultNotEnoughIngot uint8 = 7
	// EraRebirthResultNoFreeStorage 无免费储物
	EraRebirthResultNoFreeStorage uint8 = 8
	// EraRebirthResultNoTimes 次数不足
	EraRebirthResultNoTimes uint8 = 9
	// EraRebirthResultNoProfit 暂无收益
	EraRebirthResultNoProfit uint8 = 10
	// EraRebirthResultNotEnoughItem 道具不足
	EraRebirthResultNotEnoughItem uint8 = 11
	// EraRebirthResultNotStart 未开始挂机
	EraRebirthResultNotStart uint8 = 12
	// EraRebirthResultNotMatchCon 条件不匹配
	EraRebirthResultNotMatchCon uint8 = 13
	// EraRebirthResultAlreadyGet 已领取
	EraRebirthResultAlreadyGet uint8 = 14
	// EraRebirthResultAlreadyBuy 已购买
	EraRebirthResultAlreadyBuy uint8 = 15
	// EraRebirthResultNotEnoughTodayIngot 今日元宝不足
	EraRebirthResultNotEnoughTodayIngot uint8 = 16
	// EraRebirthResultNotEnoughFu 渡业符不足
	EraRebirthResultNotEnoughFu uint8 = 17
)

// EraRebirthQuickFreeDailyLimit 每日免费快速挂机次数上限。
//
// 2026-11-21 线上实测: 免费次数用尽后 852/8 仍返回 result=0，但会扣除 10 元宝；
// 因此自动化必须先读取 852/1 面板的 QuickFreeUsed，达到上限后绝不再调用。
const EraRebirthQuickFreeDailyLimit int32 = 2

// EraRebirthLandPanel 表示轮回净地面板 (852/1) 的关键字段。
//
// 线格式 (2026-11-21 两个账号实测一致):
//
//	[Int level][UInt16 entry_count][entry_count × (Int id, Int value)][Int × 12][...]
//
// 尾部 12 个 Int 中:
//
//	[4] 当前挂机累计秒数
//	[5] 挂机收益上限秒数 (172800 = 48 小时)
//	[10] 当日已使用的免费快速挂机次数 (上限 2)
//	[11] 当日已使用的付费快速挂机次数 (每次 10 元宝)
type EraRebirthLandPanel struct {
	// Level 面板首字段（实测为 4）
	Level int32
	// Entries 键值对条目数
	Entries int
	// IdleSeconds 当前挂机累计秒数
	IdleSeconds int32
	// IdleCapSeconds 挂机收益上限秒数
	IdleCapSeconds int32
	// QuickFreeUsed 当日已使用的免费快速挂机次数
	QuickFreeUsed int32
	// QuickPaidUsed 当日已使用的付费快速挂机次数
	QuickPaidUsed int32
	// Tail 面板尾部 12 个 Int 原始值，便于后续字段重新定位
	Tail []int32
}

// eraRebirthPanelTailInts 面板键值对之后的固定 Int 数量。
const eraRebirthPanelTailInts = 12

// ParseEraRebirthLandPanel 解析轮回净地面板 (852/1) 回包。
func ParseEraRebirthLandPanel(payload []byte) (*EraRebirthLandPanel, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	p := &EraRebirthLandPanel{}
	var err error
	if p.Level, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("轮回净地面板 level 读取失败: %w", err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("轮回净地面板条目数读取失败: %w", err)
	}
	// 防御异常回包：条目数超过合理上限说明结构漂移，直接拒绝以免误读免费次数。
	if count > 512 {
		return nil, fmt.Errorf("轮回净地面板条目数异常: %d", count)
	}
	p.Entries = int(count)
	for i := 0; i < int(count); i++ {
		if _, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("轮回净地面板条目[%d] id 读取失败: %w", i, err)
		}
		if _, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("轮回净地面板条目[%d] value 读取失败: %w", i, err)
		}
	}
	p.Tail = make([]int32, 0, eraRebirthPanelTailInts)
	for i := 0; i < eraRebirthPanelTailInts; i++ {
		v, rErr := r.ReadInt32()
		if rErr != nil {
			return nil, fmt.Errorf("轮回净地面板尾部字段[%d]读取失败: %w", i, rErr)
		}
		p.Tail = append(p.Tail, v)
	}
	p.IdleSeconds = p.Tail[4]
	p.IdleCapSeconds = p.Tail[5]
	p.QuickFreeUsed = p.Tail[10]
	p.QuickPaidUsed = p.Tail[11]
	// 字段合理性校验：结构漂移时宁可解析失败让例程跳过快速挂机，也不能误判为
	// 「免费次数未用」而调用 852/8 导致扣元宝。
	if p.QuickFreeUsed < 0 || p.QuickFreeUsed > EraRebirthQuickFreeDailyLimit {
		return nil, fmt.Errorf("轮回净地面板免费快速挂机次数异常: %d", p.QuickFreeUsed)
	}
	if p.QuickPaidUsed < 0 {
		return nil, fmt.Errorf("轮回净地面板付费快速挂机次数异常: %d", p.QuickPaidUsed)
	}
	if p.IdleCapSeconds <= 0 || p.IdleCapSeconds > 7*24*3600 {
		return nil, fmt.Errorf("轮回净地面板挂机收益上限异常: %d", p.IdleCapSeconds)
	}
	if p.IdleSeconds < 0 {
		return nil, fmt.Errorf("轮回净地面板挂机累计秒数异常: %d", p.IdleSeconds)
	}
	return p, nil
}

// QuickFreeRemaining 返回当日剩余免费快速挂机次数。
func (p *EraRebirthLandPanel) QuickFreeRemaining() int32 {
	remaining := EraRebirthQuickFreeDailyLimit - p.QuickFreeUsed
	if remaining < 0 {
		return 0
	}
	return remaining
}

// EraRebirthAutoProfit 表示 852/9 查询到的挂机收益。
type EraRebirthAutoProfit struct {
	// Result 结果码
	Result uint8
	// Profit 服务端返回的收益值（抓包实测为 87105，语义疑似挂机时长/收益系数）
	Profit int32
	// Rewards 可领取奖励列表
	Rewards []EraRebirthReward
}

// ParseEraRebirthAutoProfit 解析挂机收益回包 [UByte, Int, [Int, Long]]。
//
// 抓包 (2026-11-21) 样本 43 字节:
// 00 | 00 01 54 41 | 00 03 | (Int 10993, Long 154009) (Int 10992, Long 6802260) ...
func ParseEraRebirthAutoProfit(payload []byte) (*EraRebirthAutoProfit, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	res := &EraRebirthAutoProfit{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("轮回净地挂机收益结果码读取失败: %w", err)
	}
	if res.Profit, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("轮回净地挂机收益值读取失败: %w", err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("轮回净地挂机收益奖励数量读取失败: %w", err)
	}
	res.Rewards = make([]EraRebirthReward, 0, count)
	for i := 0; i < int(count); i++ {
		var reward EraRebirthReward
		if reward.ItemID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("轮回净地挂机收益奖励[%d]道具读取失败: %w", i, err)
		}
		if reward.Num, err = r.ReadInt64(); err != nil {
			return nil, fmt.Errorf("轮回净地挂机收益奖励[%d]数量读取失败: %w", i, err)
		}
		res.Rewards = append(res.Rewards, reward)
	}
	return res, nil
}

// EraRebirthReward 表示轮回净地领取的一笔奖励。
type EraRebirthReward struct {
	// ItemID 道具编号
	ItemID int32
	// Num 数量
	Num int64
}

// EraRebirthFreeBackResult 表示免费一键领取结果。
type EraRebirthFreeBackResult struct {
	// Result 结果码
	Result uint8
	// Rewards 奖励列表
	Rewards []EraRebirthReward
}

// BuildEraRebirthLandPanelPacket 构造轮回净地面板查询封包。
func BuildEraRebirthLandPanelPacket() *Packet {
	return NewPacket(ActionIDEraRebirthLandPanel, nil)
}

// BuildEraRebirthLandAutoProfitPacket 构造挂机收益查询封包。
func BuildEraRebirthLandAutoProfitPacket() *Packet {
	return NewPacket(ActionIDEraRebirthLandAutoProfit, nil)
}

// BuildEraRebirthLandPanelPrevActPacket 构造带回包锚点的轮回净地面板封包 (852/1)。
//
// 官方客户端所有 852 请求尾部都带 4 字节 PrevAct（空 req 时为唯一载荷）。
// HasPrevAct 置位后由会话在发送瞬间用最近收到的包覆盖，避免本地链漂移。
func BuildEraRebirthLandPanelPrevActPacket(prevAct uint32) *Packet {
	return buildEraRebirthLandPrevActPacket(ActionIDEraRebirthLandPanel, prevAct)
}

// BuildEraRebirthLandAutoProfitPrevActPacket 构造带回包锚点的挂机收益封包 (852/9)。
func BuildEraRebirthLandAutoProfitPrevActPacket(prevAct uint32) *Packet {
	return buildEraRebirthLandPrevActPacket(ActionIDEraRebirthLandAutoProfit, prevAct)
}

// BuildEraRebirthLandQuickAutoPacket 构造免费快速挂机封包 (852/8)。
//
// 抓包线格式: 仅 4 字节 PrevAct。HasPrevAct 置位，由会话用最近收到的包覆盖。
func BuildEraRebirthLandQuickAutoPacket(prevAct uint32) *Packet {
	return buildEraRebirthLandPrevActPacket(ActionIDEraRebirthLandQuickAuto, prevAct)
}

// buildEraRebirthLandPrevActPacket 构造「空 req + PrevAct」的 852 请求。
func buildEraRebirthLandPrevActPacket(actionID, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteUint32(prevAct)
	return &Packet{ActionID: actionID, Payload: w.Bytes(), HasPrevAct: true}
}

// BuildStateRecordGetStatePacket 构造状态记录查询封包 (701/0)。
//
// 抓包 (2026-11-21): 渡业台每次快速挂机前，官方客户端先以 key=8 查询状态记录，
// 服务端回包 [Int key, Int value]（无结果码）。该请求同时把 PrevAct 链锚点推进到
// 701/0，紧随其后的 852/8 才能被受理。
func BuildStateRecordGetStatePacket(key int32) *Packet {
	w := NewWriter()
	w.WriteInt32(key)
	return &Packet{ActionID: ActionIDStateRecordGetState, Payload: w.Bytes(), HasPrevAct: true}
}

// ParseStateRecordGetStateResult 解析 701/0 回包 [Int key, Int value]。
func ParseStateRecordGetStateResult(payload []byte) (key, value int32, err error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	if key, err = r.ReadInt32(); err != nil {
		return 0, 0, fmt.Errorf("状态记录 key 读取失败: %w", err)
	}
	if value, err = r.ReadInt32(); err != nil {
		return 0, 0, fmt.Errorf("状态记录 value 读取失败: %w", err)
	}
	return key, value, nil
}

// BuildEraRebirthLandFreeBackPacket 构造免费一键领取挂机奖励封包。
func BuildEraRebirthLandFreeBackPacket() *Packet {
	return NewPacket(ActionIDEraRebirthLandFreeBack, nil)
}

// BuildEraRebirthLandPoolPanelPacket 构造渡业台/渡业仙池面板查询封包 (852/12)。
//
// 注意: 官方客户端请求带 4 字节 PrevAct（12 字节包）。空载荷版本服务端虽仍会回包，
// 但不会登记 PrevAct 链，随后 852/16 抽取会以错误 PrevAct 被静默丢弃。
// 自动化链路请使用 BuildEraRebirthLandPoolPanelPrevActPacket。
func BuildEraRebirthLandPoolPanelPacket() *Packet {
	return NewPacket(ActionIDEraRebirthLandPoolPanel, nil)
}

// BuildEraRebirthLandPoolPanelPrevActPacket 构造带 PrevAct 的渡业仙池面板封包 (852/12)。
//
// 这里传入的必须是「服务端最后处理并回包的动作」(RoleSession.LastAckActionID)，
// 而不是本地最后发送的动作：被静默丢弃的请求会污染本地 lastActionID。
// 因此不置 HasPrevAct，避免会话用本地动作号覆盖该锚点。
func BuildEraRebirthLandPoolPanelPrevActPacket(prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDEraRebirthLandPoolPanel, w.Bytes())
}

// BuildEraRebirthLandFreeAwardPacket 构造领取渡业仙池免费奖励封包 (852/13)。
func BuildEraRebirthLandFreeAwardPacket() *Packet {
	return NewPacket(ActionIDEraRebirthLandFreeAward, nil)
}

// BuildEraRebirthLandTodayAwardPacket 构造领取渡业仙池今日奖励封包 (852/14)。
func BuildEraRebirthLandTodayAwardPacket() *Packet {
	return NewPacket(ActionIDEraRebirthLandTodayAward, nil)
}

// BuildEraRebirthLandTodayAwardPrevActPacket 构造带显式 PrevAct 的今日奖励封包 (852/14)。
//
// 线上实测裸载荷 852/14 被网关静默丢弃，而同链路的 852/13 正常回包；
// 与 852/12 一致，部分请求需要携带服务端最后受理的动作号。这里写入显式锚点
// 且不置 HasPrevAct，避免会话用被丢弃的本地动作号覆盖。
func BuildEraRebirthLandTodayAwardPrevActPacket(prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDEraRebirthLandTodayAward, w.Bytes())
}

// BuildEraRebirthLandDrawPacket 构造渡业台抽取封包 (852/16)。
//
// 线格式: Int type, Uint32 PrevAct；type 实测为 0（普通抽取）。
// HasPrevAct 置位后由会话在发送时填充最新动作号。
func BuildEraRebirthLandDrawPacket(drawType int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(drawType)
	w.WriteUint32(prevAct)
	// 由会话层在发包瞬间用真实最近动作号覆盖尾部：登录阶段多个钩子并发发包，
	// 只有“最后实际发送的动作”才能与服务端动作链匹配。
	return &Packet{ActionID: ActionIDEraRebirthLandDraw, Payload: w.Bytes(), HasPrevAct: true}
}

// EraRebirthDrawResult 表示渡业台抽取回包 [UByte result, Int, Int]。
type EraRebirthDrawResult struct {
	// Result 结果码，0 = SUCCESS
	Result uint8
	// First 第一个返回值（抓包成功时为 1）
	First int32
	// Second 第二个返回值（抓包成功时为 4）
	Second int32
}

// ParseEraRebirthDrawResult 解析渡业台抽取回包。
func ParseEraRebirthDrawResult(payload []byte) (*EraRebirthDrawResult, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	res := &EraRebirthDrawResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("渡业台结果码读取失败: %w", err)
	}
	if res.First, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("渡业台返回值读取失败: %w", err)
	}
	if res.Second, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("渡业台第二返回值读取失败: %w", err)
	}
	return res, nil
}

// ParseEraRebirthSimpleResult 解析仅含单字节结果码的 852 回包。
func ParseEraRebirthSimpleResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("轮回净地结果码为空")
	}
	return payload[0], nil
}

// ParseEraRebirthFreeBackResult 解析免费一键领取回包 [UByte result, [Int, Long]]。
func ParseEraRebirthFreeBackResult(payload []byte) (*EraRebirthFreeBackResult, error) {
	r := NewReader(payload)
	res := &EraRebirthFreeBackResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("轮回净地结果码读取失败: %w", err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("轮回净地奖励数量读取失败: %w", err)
	}
	res.Rewards = make([]EraRebirthReward, 0, count)
	for i := 0; i < int(count); i++ {
		var reward EraRebirthReward
		if reward.ItemID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("轮回净地奖励[%d]道具读取失败: %w", i, err)
		}
		if reward.Num, err = r.ReadInt64(); err != nil {
			return nil, fmt.Errorf("轮回净地奖励[%d]数量读取失败: %w", i, err)
		}
		res.Rewards = append(res.Rewards, reward)
	}
	return res, nil
}

// EraRebirthResultText 返回轮回净地结果码的可读文本。
func EraRebirthResultText(code uint8) string {
	switch code {
	case EraRebirthResultSuccess:
		return "成功"
	case EraRebirthResultFuncNotOpen:
		return "功能未开放"
	case EraRebirthResultAlreadyStart:
		return "已在挂机"
	case EraRebirthResultFailed:
		return "失败"
	case EraRebirthResultNoCanGetGift:
		return "暂无可领取奖励"
	case EraRebirthResultPackageFull:
		return "背包已满"
	case EraRebirthResultNoStorage:
		return "无储物"
	case EraRebirthResultNotEnoughIngot:
		return "元宝不足"
	case EraRebirthResultNoFreeStorage:
		return "无免费储物"
	case EraRebirthResultNoTimes:
		return "次数不足"
	case EraRebirthResultNoProfit:
		return "暂无收益"
	case EraRebirthResultNotEnoughItem:
		return "道具不足"
	case EraRebirthResultNotStart:
		return "未开始挂机"
	case EraRebirthResultNotMatchCon:
		return "条件不匹配"
	case EraRebirthResultAlreadyGet:
		return "已领取"
	case EraRebirthResultAlreadyBuy:
		return "已购买"
	case EraRebirthResultNotEnoughTodayIngot:
		return "今日元宝不足"
	case EraRebirthResultNotEnoughFu:
		return "渡业符不足"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// BuildEraRebirthFreeBackResultPacket 构造免费领取回包，供离线 mock 与单测使用。
func BuildEraRebirthFreeBackResultPacket(result uint8, rewards []EraRebirthReward) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	w.WriteUint16(uint16(len(rewards)))
	for _, reward := range rewards {
		w.WriteInt32(reward.ItemID)
		w.WriteInt64(reward.Num)
	}
	return NewPacket(ActionIDEraRebirthLandFreeBack, w.Bytes())
}
