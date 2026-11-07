// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现「摘仙桃」(Mod_GetPeach_Base, Module 40 / 0x0028) 协议编解码，
// 对齐官方客户端 com.controllers.GetPeachController 与 com.datas.GetPeachData：
//
//	peach_info     (Act 3, 0x00280003) request=[] response=[Byte peachLv, Byte peachNum, Byte is_can_batch, Byte is_can_call_peach]
//	player_info    (Act 4, 0x00280004) request=[] response=[Byte playerCount, Byte haveMonkeyNum, Byte canBuyCount, Byte call_coin_times, Byte call_ingot_times, Int call_need_coin, Int call_need_ingot]
//	get_peach      (Act 1, 0x00280001) request=[] response=[UByte result, Long war_exp, war_data, Int war_aura]
//	call_peach     (Act 6, 0x00280006) request=[] response=[UByte result]
//
// 自动化只会在 call_coin_times > 0 时调用 call_peach；call_coin_times == 0 时
// 只能消耗元宝，任何路径都不允许调用。
package protocol

import "fmt"

// 摘仙桃动作编号 (Mod_GetPeach_Base)。
const (
	// ActionIDGetPeachHarvest 摘取一棵已召唤的仙桃 (Action 1 -> 0x00280001)
	ActionIDGetPeachHarvest uint32 = 0x00280001
	// ActionIDGetPeachInfo 查询仙桃面板 (Action 3 -> 0x00280003)
	ActionIDGetPeachInfo uint32 = 0x00280003
	// ActionIDGetPeachPlayerInfo 查询玩家仙桃信息 (Action 4 -> 0x00280004)
	ActionIDGetPeachPlayerInfo uint32 = 0x00280004
	// ActionIDGetPeachCall 召唤仙桃 (Action 6 -> 0x00280006)
	ActionIDGetPeachCall uint32 = 0x00280006
)

// 摘仙桃结果码 (Mod_GetPeach_Base)。
const (
	// GetPeachResultSuccess 成功
	GetPeachResultSuccess uint8 = 0
	// GetPeachResultNotEnoughPeachCount 没有足够的桃子
	GetPeachResultNotEnoughPeachCount uint8 = 1
	// GetPeachResultWarCD 战斗冷却中
	GetPeachResultWarCD uint8 = 2
	// GetPeachResultFailure 失败 (已达召唤上限等)
	GetPeachResultFailure uint8 = 3
	// GetPeachResultNoEnoughCoin 铜钱不足
	GetPeachResultNoEnoughCoin uint8 = 4
	// GetPeachResultNoEnoughIngot 元宝不足
	GetPeachResultNoEnoughIngot uint8 = 5
)

// GetPeachPanel 表示摘仙桃面板。
//
// 线格式 (peach_info): Byte peachLv, Byte peachNum, Byte is_can_batch, Byte is_can_call_peach。
type GetPeachPanel struct {
	// PeachLv 仙桃等级
	PeachLv uint8
	// PeachNum 当前已召唤、尚未摘取的仙桃数量
	PeachNum uint8
	// CanBatch 是否可一键摘取
	CanBatch bool
	// CanCall 是否可继续召唤 (服务端当前是否允许召唤)
	CanCall bool
}

// GetPeachPlayerInfo 表示玩家摘仙桃相关信息。
//
// 线格式 (player_info):
// Byte playerCount, Byte haveMonkeyNum, Byte canBuyCount,
// Byte call_coin_times, Byte call_ingot_times,
// Int call_need_coin, Int call_need_ingot。
type GetPeachPlayerInfo struct {
	// PlayerCount 玩家累计摘桃数
	PlayerCount uint8
	// HaveMonkeyNum 已拥有猴子数量
	HaveMonkeyNum uint8
	// CanBuyCount 可购买次数
	CanBuyCount uint8
	// CallCoinTimes 剩余铜钱/免费召唤次数；为 0 时召唤将消耗元宝
	CallCoinTimes uint8
	// CallIngotTimes 剩余元宝召唤次数 (自动化永不使用)
	CallIngotTimes uint8
	// CallNeedCoin 单次铜钱召唤所需铜钱
	CallNeedCoin int32
	// CallNeedIngot 单次元宝召唤所需元宝 (自动化永不使用)
	CallNeedIngot int32
}

// GetPeachCallResult 表示召唤仙桃结果。
type GetPeachCallResult struct {
	// Result 结果码 (GetPeachResultSuccess 等)
	Result uint8
}

// GetPeachHarvestResult 表示摘取仙桃结果。
//
// 官方回包在 result 之后紧跟 Long war_exp 与完整战报结构；自动化只消费前两个字段。
type GetPeachHarvestResult struct {
	// Result 结果码 (GetPeachResultSuccess 等)
	Result uint8
	// WarExp 本次摘取获得的经验
	WarExp int64
}

// BuildGetPeachInfoPacket 构造仙桃面板查询封包 (0x00280003)。
//
// 官方 request 为空，客户端网络层会在末尾追加 4 字节前序动作号。
func BuildGetPeachInfoPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDGetPeachInfo, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// BuildGetPeachPlayerInfoPacket 构造玩家仙桃信息查询封包 (0x00280004)。
func BuildGetPeachPlayerInfoPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDGetPeachInfo
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDGetPeachPlayerInfo, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// BuildGetPeachHarvestPacket 构造摘取仙桃封包 (0x00280001)。
func BuildGetPeachHarvestPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDGetPeachPlayerInfo
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDGetPeachHarvest, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// BuildGetPeachCallPacket 构造召唤仙桃封包 (0x00280006)。
//
// 线上实测空载荷即可被服务端接受；是否真正扣费由服务端依据 call_coin_times 判定。
func BuildGetPeachCallPacket() *Packet {
	return NewPacket(ActionIDGetPeachCall, nil)
}

// ParseGetPeachPanelResult 解析仙桃面板回包。
func ParseGetPeachPanelResult(payload []byte) (*GetPeachPanel, error) {
	r := NewReader(payload)
	peachLv, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: 摘仙桃 peachLv 读取失败: %v", ErrInvalidPayload, err)
	}
	peachNum, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: 摘仙桃 peachNum 读取失败: %v", ErrInvalidPayload, err)
	}
	canBatch, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: 摘仙桃 is_can_batch 读取失败: %v", ErrInvalidPayload, err)
	}
	canCall, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: 摘仙桃 is_can_call_peach 读取失败: %v", ErrInvalidPayload, err)
	}
	return &GetPeachPanel{
		PeachLv:  peachLv,
		PeachNum: peachNum,
		CanBatch: canBatch == 1,
		CanCall:  canCall == 1,
	}, nil
}

// ParseGetPeachPlayerInfoResult 解析玩家仙桃信息回包。
func ParseGetPeachPlayerInfoResult(payload []byte) (*GetPeachPlayerInfo, error) {
	r := NewReader(payload)
	info := &GetPeachPlayerInfo{}
	bytes := []*uint8{
		&info.PlayerCount, &info.HaveMonkeyNum, &info.CanBuyCount,
		&info.CallCoinTimes, &info.CallIngotTimes,
	}
	for i, dst := range bytes {
		v, err := r.ReadUint8()
		if err != nil {
			return nil, fmt.Errorf("%w: 摘仙桃 player_info 字段[%d] 读取失败: %v", ErrInvalidPayload, i, err)
		}
		*dst = v
	}
	var err error
	if info.CallNeedCoin, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: 摘仙桃 call_need_coin 读取失败: %v", ErrInvalidPayload, err)
	}
	if info.CallNeedIngot, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: 摘仙桃 call_need_ingot 读取失败: %v", ErrInvalidPayload, err)
	}
	return info, nil
}

// ParseGetPeachCallResult 解析召唤仙桃回包。
func ParseGetPeachCallResult(payload []byte) (*GetPeachCallResult, error) {
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: 摘仙桃召唤结果码读取失败: %v", ErrInvalidPayload, err)
	}
	return &GetPeachCallResult{Result: result}, nil
}

// ParseGetPeachHarvestResult 解析摘取仙桃回包。
//
// 只读取 [UByte result, Long war_exp]，战报正文由客户端渲染，自动化无需消费。
func ParseGetPeachHarvestResult(payload []byte) (*GetPeachHarvestResult, error) {
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: 摘仙桃摘取结果码读取失败: %v", ErrInvalidPayload, err)
	}
	res := &GetPeachHarvestResult{Result: result}
	if result != GetPeachResultSuccess {
		return res, nil
	}
	if res.WarExp, err = r.ReadInt64(); err != nil {
		return nil, fmt.Errorf("%w: 摘仙桃 war_exp 读取失败: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// GetPeachResultText 返回摘仙桃结果码的可读文本。
func GetPeachResultText(code uint8) string {
	switch code {
	case GetPeachResultSuccess:
		return "成功"
	case GetPeachResultNotEnoughPeachCount:
		return "没有足够的桃子"
	case GetPeachResultWarCD:
		return "战斗冷却中"
	case GetPeachResultFailure:
		return "失败"
	case GetPeachResultNoEnoughCoin:
		return "铜钱不足"
	case GetPeachResultNoEnoughIngot:
		return "元宝不足"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}

// BuildGetPeachPanelResultPacket 构造仙桃面板回包，供离线 Mock 与单测使用。
func BuildGetPeachPanelResultPacket(panel GetPeachPanel) *Packet {
	w := NewWriter()
	w.WriteUint8(panel.PeachLv)
	w.WriteUint8(panel.PeachNum)
	w.WriteUint8(boolToByte(panel.CanBatch))
	w.WriteUint8(boolToByte(panel.CanCall))
	return NewPacket(ActionIDGetPeachInfo, w.Bytes())
}

// BuildGetPeachPlayerInfoResultPacket 构造玩家仙桃信息回包，供离线 Mock 与单测使用。
func BuildGetPeachPlayerInfoResultPacket(info GetPeachPlayerInfo) *Packet {
	w := NewWriter()
	w.WriteUint8(info.PlayerCount)
	w.WriteUint8(info.HaveMonkeyNum)
	w.WriteUint8(info.CanBuyCount)
	w.WriteUint8(info.CallCoinTimes)
	w.WriteUint8(info.CallIngotTimes)
	w.WriteInt32(info.CallNeedCoin)
	w.WriteInt32(info.CallNeedIngot)
	return NewPacket(ActionIDGetPeachPlayerInfo, w.Bytes())
}

// BuildGetPeachCallResultPacket 构造召唤仙桃回包，供离线 Mock 与单测使用。
func BuildGetPeachCallResultPacket(result uint8) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	return NewPacket(ActionIDGetPeachCall, w.Bytes())
}

// BuildGetPeachHarvestResultPacket 构造摘取仙桃回包，供离线 Mock 与单测使用。
func BuildGetPeachHarvestResultPacket(result uint8, warExp int64) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	if result == GetPeachResultSuccess {
		w.WriteInt64(warExp)
	}
	return NewPacket(ActionIDGetPeachHarvest, w.Bytes())
}

func boolToByte(v bool) uint8 {
	if v {
		return 1
	}
	return 0
}
