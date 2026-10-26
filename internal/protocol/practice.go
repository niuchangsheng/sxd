// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// @author Ateng
// @since 2026-10-08
package protocol

import (
	"fmt"
)

// StartPracticeRequest 代表开始普通关卡挂机扫荡请求 (Mod_MissionPractice_Base.start_practice, ActionID=0x00190001)。
type StartPracticeRequest struct {
	// MissionID 关卡唯一数字标识
	MissionID int32
	// Count 扫荡轮次/次数
	Count int16
	// AutoSale 自动卖出白绿蓝装配置 (0: 否, 1: 是)
	AutoSale int8
	// PrevAct 前序指令协议号 (默认 0x00190007)
	PrevAct uint32
}

// StartPracticeResult 代表开始扫荡服务器响应结果。
type StartPracticeResult struct {
	// Result 状态码 (0=SUCCESS, 1=NOT_ENOUGH_POWER, 2=BAG_FULL, 3=IN_PRACTICE)
	Result uint8
	// MissionID 当前开始扫荡的关卡 ID
	MissionID int32
	// Count 实际开始扫荡的总次数
	Count int16
}

// QuicklyRequest 代表加速秒完成扫荡请求 (Mod_MissionPractice_Base.quickly, ActionID=0x00190003)。
type QuicklyRequest struct {
	// Count 加速完成的次数
	Count int16
	// PrevAct 前序指令协议号 (默认 0x00190001)
	PrevAct uint32
}

// QuicklyResult 代表加速秒完成服务器响应结果。
type QuicklyResult struct {
	// Result 状态码 (0=SUCCESS)
	Result uint8
	// Count 成功加速完成的次数
	Count int16
}

// HeroPracticeRequest 代表章节英雄副本一键扫荡请求 (Mod_HeroMission_Base.start_practice, ActionID=0x00230002)。
type HeroPracticeRequest struct {
	// MapID 地图/章节唯一标识 (如 43, 54, 55)
	MapID uint16
	// Count 扫荡轮次 (通常为 1)
	Count uint8
	// AutoSale 自动出售配置 (0: 否, 1: 是)
	AutoSale uint8
	// PrevAct 前序指令号 (如 ActionIDHeroGetList 0x00230000)
	PrevAct uint32
}

// HeroPracticeResult 代表章节英雄副本扫荡启动回包。
type HeroPracticeResult struct {
	// Result 状态码 (3=成功, 8=今日次数已达上限)
	Result uint8
	// Battles 计划排队的战斗场次数
	Battles uint32
}

// HeroQuicklyRequest 代表极速秒完成英雄副本扫荡请求 (ActionID=0x00230007)。
type HeroQuicklyRequest struct {
	// PrevAct 前序指令号 (ActionIDHeroPracticeStart 0x00230002)
	PrevAct uint32
}

// BuildStartPracticePacket 构造开始扫荡二进制封包。
func BuildStartPracticePacket(req StartPracticeRequest) (*Packet, error) {
	w := NewWriter()
	// 1. 写入 MissionID (4 字节大端序有符号整型)
	w.WriteInt32(req.MissionID)
	// 2. 写入 Count (2 字节大端序有符号整型)
	w.WriteInt16(req.Count)
	// 3. 写入 AutoSale (1 字节有符号整型)
	w.WriteInt8(req.AutoSale)
	// 4. 追加 4 字节前序动作号 (若未指定则默认前序为 ActionIDPracticeGetStatus 0x00190000，对齐 c2s_m25_a1.bin)
	prevAct := req.PrevAct
	if prevAct == 0 {
		prevAct = ActionIDPracticeGetStatus
	}
	w.WriteUint32(prevAct)

	return NewPacket(ActionIDPracticeStart, w.Bytes()), nil
}

// ParseStartPracticeRequest 从封包载荷中反序列化开始扫荡请求参数。
func ParseStartPracticeRequest(payload []byte) (*StartPracticeRequest, error) {
	r := NewReader(payload)

	missionID, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read mission_id: %v", ErrInvalidPayload, err)
	}

	count, err := r.ReadInt16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read count: %v", ErrInvalidPayload, err)
	}

	autoSale, err := r.ReadInt8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read auto_sale: %v", ErrInvalidPayload, err)
	}

	var prevAct uint32
	if r.Remaining() >= 4 {
		prevAct, _ = r.ReadUint32()
	}

	return &StartPracticeRequest{
		MissionID: missionID,
		Count:     count,
		AutoSale:  autoSale,
		PrevAct:   prevAct,
	}, nil
}

// BuildStartPracticeResultPacket 构造开始扫荡响应二进制封包。
func BuildStartPracticeResultPacket(res StartPracticeResult) (*Packet, error) {
	w := NewWriter()
	w.WriteUint8(res.Result)
	w.WriteInt32(res.MissionID)
	w.WriteInt16(res.Count)
	return NewPacket(ActionIDPracticeStart, w.Bytes()), nil
}

// ParseStartPracticeResult 从响应载荷中反序列化开始扫荡结果。
func ParseStartPracticeResult(payload []byte) (*StartPracticeResult, error) {
	r := NewReader(payload)

	resCode, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read result: %v", ErrInvalidPayload, err)
	}

	missionID, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read mission_id: %v", ErrInvalidPayload, err)
	}

	var count int16
	if r.Remaining() >= 4 {
		c32, _ := r.ReadInt32()
		count = int16(c32)
	} else if r.Remaining() >= 2 {
		count, _ = r.ReadInt16()
	}

	return &StartPracticeResult{
		Result:    resCode,
		MissionID: missionID,
		Count:     count,
	}, nil
}

// BuildQuicklyPacket 构造扫荡加速完成二进制封包。
func BuildQuicklyPacket(req QuicklyRequest) (*Packet, error) {
	w := NewWriter()
	w.WriteInt16(req.Count)
	prevAct := req.PrevAct
	if prevAct == 0 {
		prevAct = ActionIDPracticeStart
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDPracticeQuickly, w.Bytes()), nil
}

// ParseQuicklyRequest 从封包载荷中反序列化加速完成请求参数。
func ParseQuicklyRequest(payload []byte) (*QuicklyRequest, error) {
	r := NewReader(payload)
	count, err := r.ReadInt16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read count: %v", ErrInvalidPayload, err)
	}
	var prevAct uint32
	if r.Remaining() >= 4 {
		prevAct, _ = r.ReadUint32()
	}
	return &QuicklyRequest{Count: count, PrevAct: prevAct}, nil
}

// BuildQuicklyResultPacket 构造扫荡加速完成响应二进制封包。
func BuildQuicklyResultPacket(res QuicklyResult) (*Packet, error) {
	w := NewWriter()
	w.WriteUint8(res.Result)
	w.WriteInt16(res.Count)
	return NewPacket(ActionIDPracticeQuickly, w.Bytes()), nil
}

// ParseQuicklyResult 从响应载荷中反序列化加速完成结果。
func ParseQuicklyResult(payload []byte) (*QuicklyResult, error) {
	r := NewReader(payload)
	resCode, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read result: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadInt16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read count: %v", ErrInvalidPayload, err)
	}
	return &QuicklyResult{
		Result: resCode,
		Count:  count,
	}, nil
}

// BuildHeroPracticePacket 构造章节英雄副本扫荡请求封包。
func BuildHeroPracticePacket(req HeroPracticeRequest) (*Packet, error) {
	w := NewWriter()
	w.WriteUint16(req.MapID)
	count := req.Count
	if count == 0 {
		count = 1
	}
	w.WriteUint8(count)
	w.WriteUint8(req.AutoSale)
	prevAct := req.PrevAct
	if prevAct == 0 {
		prevAct = ActionIDHeroGetList
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDHeroPracticeStart, w.Bytes()), nil
}

// ParseHeroPracticeResult 解析章节英雄副本扫荡响应回包。
func ParseHeroPracticeResult(payload []byte) (*HeroPracticeResult, error) {
	if len(payload) == 0 {
		return nil, ErrInvalidPayload
	}
	r := NewReader(payload)
	code, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}
	var battles uint32
	if r.Remaining() >= 4 {
		battles, _ = r.ReadUint32()
	}
	return &HeroPracticeResult{
		Result:  code,
		Battles: battles,
	}, nil
}

// BuildHeroQuicklyPacket 构造英雄副本极速秒完成封包。
func BuildHeroQuicklyPacket(req HeroQuicklyRequest) (*Packet, error) {
	w := NewWriter()
	prevAct := req.PrevAct
	if prevAct == 0 {
		prevAct = ActionIDHeroPracticeStart
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDHeroPracticeQuickly, w.Bytes()), nil
}

// PracticeStatusMissionItem 代表扫荡状态中的单个关卡配置项。
type PracticeStatusMissionItem struct {
	MissionID int32
	Level     int16
	Type      int8
}

// PracticeStatusResult 代表查询副本扫荡状态响应结果 (ActionID=0x00190000)。
type PracticeStatusResult struct {
	HeaderTag    string
	Missions     []PracticeStatusMissionItem
	IsPracticing uint8
	Remaining    uint32
	TotalTime    uint32
}

// BuildPracticeGetStatusPacket 构造查询副本扫荡状态请求封包 (Mod_MissionPractice_Base.get_practice_info, ActionID=0x00190000)。
func BuildPracticeGetStatusPacket(missionID int32, prevAct uint32) (*Packet, error) {
	w := NewWriter()
	w.WriteInt32(missionID)
	if prevAct == 0 {
		prevAct = ActionIDPlayerGetInfo
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDPracticeGetStatus, w.Bytes()), nil
}

// ParsePracticeStatusResult 解析副本扫荡状态响应载荷。
func ParsePracticeStatusResult(payload []byte) (*PracticeStatusResult, error) {
	if len(payload) < 4 {
		return nil, fmt.Errorf("%w: payload too short for practice status", ErrInvalidPayload)
	}
	r := NewReader(payload)
	tagBytes, err := r.ReadBytes(2)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read tag: %v", ErrInvalidPayload, err)
	}
	tag := string(tagBytes)

	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read missions count: %v", ErrInvalidPayload, err)
	}

	missions := make([]PracticeStatusMissionItem, 0, count)
	for i := uint16(0); i < count; i++ {
		mid, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read mission_id at %d: %v", ErrInvalidPayload, i, err)
		}
		lvl, err := r.ReadInt16()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read level at %d: %v", ErrInvalidPayload, i, err)
		}
		typ, err := r.ReadInt8()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read type at %d: %v", ErrInvalidPayload, i, err)
		}
		missions = append(missions, PracticeStatusMissionItem{
			MissionID: mid,
			Level:     lvl,
			Type:      typ,
		})
	}

	var isPracticing uint8
	var remaining uint32
	var totalTime uint32

	if r.Remaining() >= 1 {
		isPracticing, _ = r.ReadUint8()
	}
	if r.Remaining() >= 4 {
		remaining, _ = r.ReadUint32()
	}
	if r.Remaining() >= 4 {
		totalTime, _ = r.ReadUint32()
	}

	return &PracticeStatusResult{
		HeaderTag:    tag,
		Missions:     missions,
		IsPracticing: isPracticing,
		Remaining:    remaining,
		TotalTime:    totalTime,
	}, nil
}

// PracticeRoleExpItem 代表扫荡结算中单个参战角色的经验结算明细。
type PracticeRoleExpItem struct {
	RoleID  uint32
	ExpGain uint32
}

// PracticeNotifyResult 代表扫荡战斗轮次结算服务端主动通知结果 (ActionID=0x00190004)。
type PracticeNotifyResult struct {
	SubType       uint8
	RoundIndex    uint16
	RoundTotal    uint16
	RemainingSec  uint16
	CoinsGain     uint32
	ItemsCount    uint16
	RoleExpItems  []PracticeRoleExpItem
	RawTailBuffer []byte
}

// ParsePracticeNotifyResult 从解压缩后的载荷中解析扫荡轮次结算通知。
func ParsePracticeNotifyResult(payload []byte) (*PracticeNotifyResult, error) {
	if len(payload) < 1 {
		return nil, fmt.Errorf("%w: empty practice notify payload", ErrInvalidPayload)
	}
	r := NewReader(payload)
	subType, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}

	res := &PracticeNotifyResult{
		SubType: subType,
	}

	// 针对微端典型 SubType (0x05, 0x06, 0x07) 进行结构化解析
	if r.Remaining() >= 8 {
		switch subType {
		case 0x05:
			// 0x05: 轮次信息与经验列表
			res.RoundIndex, _ = r.ReadUint16()
			res.RoundTotal, _ = r.ReadUint16()
			_ , _ = r.ReadUint16() // 预留/状态
			res.RemainingSec, _ = r.ReadUint16()
			if r.Remaining() >= 2 {
				roleCount, _ := r.ReadUint16()
				res.RoleExpItems = make([]PracticeRoleExpItem, 0, roleCount)
				for i := uint16(0); i < roleCount && r.Remaining() >= 8; i++ {
					rid, _ := r.ReadUint32()
					exp, _ := r.ReadUint32()
					res.RoleExpItems = append(res.RoleExpItems, PracticeRoleExpItem{
						RoleID:  rid,
						ExpGain: exp,
					})
				}
			}
		case 0x06:
			// 0x06: 完整掉落与伙伴结算快照
			res.RoundIndex = 1
			_ , _ = r.ReadUint32() // 预留/标志
			res.CoinsGain, _ = r.ReadUint32()
			_ , _ = r.ReadUint32() // 预留
			_ , _ = r.ReadUint16() // 预留
			if r.Remaining() >= 2 {
				roleCount, _ := r.ReadUint16()
				res.RoleExpItems = make([]PracticeRoleExpItem, 0, roleCount)
				for i := uint16(0); i < roleCount && r.Remaining() >= 8; i++ {
					rid, _ := r.ReadUint32()
					exp, _ := r.ReadUint32()
					res.RoleExpItems = append(res.RoleExpItems, PracticeRoleExpItem{
						RoleID:  rid,
						ExpGain: exp,
					})
				}
			}
		}
	}

	if r.Remaining() > 0 {
		res.RawTailBuffer, _ = r.ReadBytes(r.Remaining())
	}

	return res, nil
}

