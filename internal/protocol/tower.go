// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现六道轮回塔 (Mod_Tower_Base, Module 32) 与封灵灵件 (Mod_SealSoul_Base, Module 34) 核心协议报文编解码。
package protocol

import (
	"fmt"
)

// 六道轮回塔道层级定义 (Mod_Tower_Base / TowerType)
const (
	TowerLayerHell        uint8 = 1  // 地狱道 (需求等级: 45)
	TowerLayerHungryGhost uint8 = 2  // 饿鬼道 (需求等级: 56)
	TowerLayerBeast       uint8 = 3  // 畜生道 (需求等级: 76)
	TowerLayerAsura       uint8 = 4  // 修罗道 (需求等级: 96)
	TowerLayerHuman       uint8 = 5  // 人间道 (需求等级: 106)
	TowerLayerHeaven      uint8 = 6  // 天道 (需求等级: 116)
	TowerLayerXianHell    uint8 = 7  // 仙·地狱道 (需求等级: 136)
	TowerLayerXianGhost   uint8 = 8  // 仙·饿鬼道 (需求等级: 146)
	TowerLayerXianBeast   uint8 = 9  // 仙·畜生道 (需求等级: 156)
	TowerLayerXianAsura   uint8 = 10 // 仙·修罗道 (需求等级: 166)
	TowerLayerXianHuman   uint8 = 11 // 仙·人间道 (需求等级: 176)
	TowerLayerXianHeaven  uint8 = 12 // 仙·天道 (需求等级: 186)
)

// TowerLayerName 返回道层级对应的中文名。
func TowerLayerName(layer uint8) string {
	switch layer {
	case TowerLayerHell:
		return "地狱道"
	case TowerLayerHungryGhost:
		return "饿鬼道"
	case TowerLayerBeast:
		return "畜生道"
	case TowerLayerAsura:
		return "修罗道"
	case TowerLayerHuman:
		return "人间道"
	case TowerLayerHeaven:
		return "天道"
	case TowerLayerXianHell:
		return "仙·地狱道"
	case TowerLayerXianGhost:
		return "仙·饿鬼道"
	case TowerLayerXianBeast:
		return "仙·畜生道"
	case TowerLayerXianAsura:
		return "仙·修罗道"
	case TowerLayerXianHuman:
		return "仙·人间道"
	case TowerLayerXianHeaven:
		return "仙·天道"
	default:
		return fmt.Sprintf("未知道(%d)", layer)
	}
}

// TowerLayerLevelLimit 返回挑战指定道所需的玩家角色最低等级门槛。
func TowerLayerLevelLimit(layer uint8) int {
	switch layer {
	case 1:
		return 45
	case 2:
		return 56
	case 3:
		return 76
	case 4:
		return 96
	case 5:
		return 106
	case 6:
		return 116
	case 7:
		return 136
	case 8:
		return 146
	case 9:
		return 156
	case 10:
		return 166
	case 11:
		return 176
	case 12:
		return 186
	default:
		return 45
	}
}

// TowerInfo 表示六道轮回塔面板快照与挑战进度数据。
type TowerInfo struct {
	SequenceMax uint8  // 本道历史最高通关层数 (0~25)
	Layer       uint8  // 当前所在道 (1: 地狱道 .. 6: 天道 .. 12)
	SequenceWar uint8  // 当前待挑战层数 (1~25，若 > 25 表示当前道今日已全部通关)
	BagNum      uint8  // 灵件背包数量 / 占用格数
	ResetNum    uint8  // 今日剩余免费重置次数
	ResetIngot  uint16 // 元宝重置费用
	HideID      uint8  // 隐藏关卡标记 ID
	HighStone   int32  // 玩家持有的高级洗炼石数量
}

// TowerChallengeResult 表示六道轮回单关战斗挑战结果。
type TowerChallengeResult struct {
	Result    uint8 // 结果状态码 (0: SUCCESS, 1: INVALID_LAYER, 2: NO_LV, 3: WAR_CD, 5: FULL_TIMES, 6: FULL_BAG)
	WarSoulID int32 // 战斗掉落/结算获得的灵件 ID (0 表示未获得)
	WarExp    int32 // 获得的经验
	WarFame   int32 // 获得的声望
	WarCoin   int32 // 获得的铜钱
	IsPass    bool  // 是否挑战通关
	WarAura   int32 // 灵气值
	WarHstone int32 // 获得的高级洗炼石
}

// TowerResetResult 表示六道轮回重置回包。
type TowerResetResult struct {
	Result     uint8  // 结果状态码 (0: SUCCESS, etc.)
	Layer      uint8  // 重置后的道
	ResetIngot uint16 // 下次重置所需元宝
	ResetNum   uint8  // 剩余重置次数
}

// TowerAutoReport 表示自动爬塔/扫荡服务端战报推送。
type TowerAutoReport struct {
	Result uint8 // 状态码 (0: SUCCESS)
	Layer  uint8 // 当前道
	Exp    int32 // 累计获得经验
	Fame   int32 // 累计获得声望
	Coin   uint8 // 铜钱标志或数量
	Hstone int32 // 洗炼石
}

// --------------------------------------------------------------------------------
// 六道轮回塔封包构造器 (Client -> Server)
// --------------------------------------------------------------------------------

// BuildTowerGetInfoPacket 构造查询六道轮回塔信息封包 (Module 32, Action 0)。
func BuildTowerGetInfoPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct != 0 {
		w.WriteUint32(prevAct)
	}
	return NewPacket(ActionIDTowerGetInfo, w.Bytes())
}

// BuildTowerChallengePacket 构造挑战六道轮回塔指定层封包 (Module 32, Action 2)。
// warIdx: 挑战关卡序号 (1~25)。
// autoSellL: 是否自动卖出蓝色/绿色低级灵件。
// autoSellZ: 是否自动卖出紫色灵件。
func BuildTowerChallengePacket(warIdx uint8, autoSellL, autoSellZ bool, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteUint8(warIdx)
	if autoSellL {
		w.WriteUint8(1)
	} else {
		w.WriteUint8(0)
	}
	if autoSellZ {
		w.WriteUint8(1)
	} else {
		w.WriteUint8(0)
	}
	if prevAct != 0 {
		w.WriteUint32(prevAct)
	}
	return NewPacket(ActionIDTowerStartChallenge, w.Bytes())
}

// BuildTowerResetPacket 构造重置六道轮回塔层数封包 (Module 32, Action 3)。
func BuildTowerResetPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct != 0 {
		w.WriteUint32(prevAct)
	}
	return NewPacket(ActionIDTowerResetLayer, w.Bytes())
}

// BuildTowerStartAutoChallengePacket 构造开始六道轮回自动爬塔/扫荡封包 (Module 32, Action 4)。
func BuildTowerStartAutoChallengePacket(autoSellL, autoSellZ bool, prevAct uint32) *Packet {
	w := NewWriter()
	if autoSellL {
		w.WriteUint8(1)
	} else {
		w.WriteUint8(0)
	}
	if autoSellZ {
		w.WriteUint8(1)
	} else {
		w.WriteUint8(0)
	}
	if prevAct != 0 {
		w.WriteUint32(prevAct)
	}
	return NewPacket(ActionIDTowerStartAutoChallenge, w.Bytes())
}

// BuildTowerStopAutoChallengePacket 构造停止六道轮回自动挑战封包 (Module 32, Action 5)。
func BuildTowerStopAutoChallengePacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct != 0 {
		w.WriteUint32(prevAct)
	}
	return NewPacket(ActionIDTowerStopAutoChallenge, w.Bytes())
}

// BuildTowerQuicklyAutoChallengePacket 构造秒完成/加速自动挑战封包 (Module 32, Action 7)。
func BuildTowerQuicklyAutoChallengePacket(autoSellL, autoSellZ bool, prevAct uint32) *Packet {
	w := NewWriter()
	if autoSellL {
		w.WriteUint8(1)
	} else {
		w.WriteUint8(0)
	}
	if autoSellZ {
		w.WriteUint8(1)
	} else {
		w.WriteUint8(0)
	}
	if prevAct != 0 {
		w.WriteUint32(prevAct)
	}
	return NewPacket(ActionIDTowerQuicklyAutoChallenge, w.Bytes())
}

// BuildSealSoulGetTowerKeyPacket 构造查询爬塔钥匙封包 (Module 34, Action 7)。
func BuildSealSoulGetTowerKeyPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct != 0 {
		w.WriteUint32(prevAct)
	}
	return NewPacket(ActionIDSealSoulGetTowerKey, w.Bytes())
}

// BuildSealSoulSellPacket 构造出售/分解灵件封包 (Module 34, Action 8)。
func BuildSealSoulSellPacket(soulID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(soulID)
	if prevAct != 0 {
		w.WriteUint32(prevAct)
	}
	return NewPacket(ActionIDSealSoulSell, w.Bytes())
}

// --------------------------------------------------------------------------------
// 六道轮回塔回包解析器 (Server -> Client)
// --------------------------------------------------------------------------------

// ParseTowerInfoResponse 解析六道轮回塔面板查询回包 (Module 32, Action 0)。
// 结构契约: ByteUtil(sequenceMax), ByteUtil(layer), ByteUtil(sequenceWar), ByteUtil(bagNum), ByteUtil(resetNum), ShortUtil(resetIngot), ByteUtil(hideId), IntUtil(highStone)
func ParseTowerInfoResponse(payload []byte) (*TowerInfo, error) {
	if len(payload) < 11 {
		return nil, fmt.Errorf("%w: payload too short for tower info (%d < 11)", ErrInvalidPayload, len(payload))
	}
	r := NewReader(payload)
	seqMax, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}
	layer, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}
	seqWar, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}
	bagNum, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}
	resetNum, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}
	resetIngot, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	hideID, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}
	highStone, err := r.ReadInt32()
	if err != nil {
		return nil, err
	}

	return &TowerInfo{
		SequenceMax: seqMax,
		Layer:       layer,
		SequenceWar: seqWar,
		BagNum:      bagNum,
		ResetNum:    resetNum,
		ResetIngot:  resetIngot,
		HideID:      hideID,
		HighStone:   highStone,
	}, nil
}

// ParseTowerChallengeResponse 解析单关挑战结算回包 (Module 32, Action 2)。
func ParseTowerChallengeResponse(payload []byte) (*TowerChallengeResult, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("%w: empty challenge response", ErrInvalidPayload)
	}
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}

	res := &TowerChallengeResult{
		Result: result,
	}

	// 挑战失败或非成功状态直接返回状态码
	if result != TowerResultSuccess {
		return res, nil
	}

	// 成功状态下，后续可能包含战斗数据体或奖励字段
	// 按照 AS3 TowerData.start_challenge 解析奖励
	if r.Remaining() >= 16 {
		// 尝试跳过变长战报主体或读取紧随战报的数据
		// 容错读取奖励
		if soulID, err := r.ReadInt32(); err == nil {
			res.WarSoulID = soulID
		}
		if exp, err := r.ReadInt32(); err == nil {
			res.WarExp = exp
		}
		if fame, err := r.ReadInt32(); err == nil {
			res.WarFame = fame
		}
		if coin, err := r.ReadInt32(); err == nil {
			res.WarCoin = coin
		}
		res.IsPass = true
	} else {
		res.IsPass = true
	}

	return res, nil
}

// ParseTowerResetResponse 解析重置六道轮回塔回包 (Module 32, Action 3)。
// 结构契约: UByteUtil(result), ByteUtil(layer), ShortUtil(resetIngot), ByteUtil(resetNum)
func ParseTowerResetResponse(payload []byte) (*TowerResetResult, error) {
	if len(payload) < 5 {
		return nil, fmt.Errorf("%w: payload too short for tower reset (%d < 5)", ErrInvalidPayload, len(payload))
	}
	r := NewReader(payload)
	resCode, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}
	layer, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}
	resetIngot, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	resetNum, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}

	return &TowerResetResult{
		Result:     resCode,
		Layer:      layer,
		ResetIngot: resetIngot,
		ResetNum:   resetNum,
	}, nil
}

// ParseTowerAutoChallengeResponse 解析开始或加速自动扫荡回包 (Module 32, Action 4 / 7)。
func ParseTowerAutoChallengeResponse(payload []byte) (uint8, error) {
	if len(payload) < 1 {
		return 0, fmt.Errorf("%w: empty auto challenge response", ErrInvalidPayload)
	}
	return payload[0], nil
}

// ParseTowerAutoReport 解析自动爬塔战报推送 (Module 32, Action 6)。
func ParseTowerAutoReport(payload []byte) (*TowerAutoReport, error) {
	if len(payload) < 15 {
		return nil, fmt.Errorf("%w: payload too short for auto report (%d < 15)", ErrInvalidPayload, len(payload))
	}
	r := NewReader(payload)
	resCode, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}
	layer, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}
	exp, err := r.ReadInt32()
	if err != nil {
		return nil, err
	}
	fame, err := r.ReadInt32()
	if err != nil {
		return nil, err
	}
	coin, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}
	hstone, err := r.ReadInt32()
	if err != nil {
		return nil, err
	}

	return &TowerAutoReport{
		Result: resCode,
		Layer:  layer,
		Exp:    exp,
		Fame:   fame,
		Coin:   coin,
		Hstone: hstone,
	}, nil
}

// ParseSealSoulGetTowerKeyResponse 解析爬塔钥匙回包 (Module 34, Action 7)。
func ParseSealSoulGetTowerKeyResponse(payload []byte) (int32, uint8, error) {
	if len(payload) < 5 {
		return 0, 0, fmt.Errorf("%w: payload too short for seal soul key (%d < 5)", ErrInvalidPayload, len(payload))
	}
	r := NewReader(payload)
	key, err := r.ReadInt32()
	if err != nil {
		return 0, 0, err
	}
	status, err := r.ReadUint8()
	if err != nil {
		return 0, 0, err
	}
	return key, status, nil
}

// ParseSealSoulSellResponse 解析灵件出售/分解回包 (Module 34, Action 8)。
func ParseSealSoulSellResponse(payload []byte) (uint8, error) {
	if len(payload) < 1 {
		return 0, fmt.Errorf("%w: empty seal soul sell response", ErrInvalidPayload)
	}
	return payload[0], nil
}
