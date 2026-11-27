// Package protocol 提供神仙道客户端与服务器之间的二进制协议编解码。
//
// 本文件实现每日灵脉 (Mod_Nimbus_Base, Module 104 / 0x68)。
//
// 2026-11-27 官方抓包 (/tmp/lingmai.pcapng, s1) 实测：
//
//	104/0 nimbus_info   request=[Uint32 prevAct]
//	                    response=[Int32 nimbus][Int16 remain][Int16 gain][...tail]
//	104/1 gather_nimbus request=[Uint8 gourdType][Uint32 prevAct]  (木葫芦 type=0)
//	                    response=[Int32 result][8B nimbusState][Int16 remain][Int16 gain][4B tail]
//	                    30 次木葫芦：每次 result=0，remain 从 29 递减到 0。
//	52/0  assistant info 为活动助手刷新，服务端另有 52/2 complete_number 推送活跃度。
package protocol

import "fmt"

// 灵脉模块协议号 (Mod_Nimbus_Base, Module 104)。
const (
	// ActionIDNimbusInfo 查询灵脉面板 (Action 0 -> 0x00680000)
	ActionIDNimbusInfo uint32 = 0x00680000
	// ActionIDNimbusGather 采集灵脉 (Action 1 -> 0x00680001)
	ActionIDNimbusGather uint32 = 0x00680001
)

// 灵脉葫芦类型。官方客户端按类型 0 连续采集 30 次。
const (
	// NimbusGourdWood 木葫芦
	NimbusGourdWood uint8 = 0
)

// NimbusInfo 表示灵脉面板 (104/0 回包)。
type NimbusInfo struct {
	// Nimbus 当前灵脉值 (官方抓包首帧为 80)
	Nimbus int32
	// Remain 今日剩余采集次数 (官方抓包为 30)
	Remain int16
	// Gain 单次采集收益 (官方抓包为 5)
	Gain int16
	// Raw 原始载荷，便于字段漂移时诊断
	Raw []byte
}

// NimbusGatherResult 表示采集灵脉回包 (104/1)。
type NimbusGatherResult struct {
	// Result 结果码 (0=成功)
	Result int32
	// Remain 本次采集后的剩余次数
	Remain int16
	// Gain 本次收益
	Gain int16
	// Raw 原始载荷
	Raw []byte
}

// BuildNimbusInfoPacket 构造灵脉面板查询封包 (104/0)。
// 空参数，尾链由会话层自动填充 (官方抓包为最近收到的 0/22)。
func BuildNimbusInfoPacket() *Packet {
	return NewPacketWithAutoPrevAct(ActionIDNimbusInfo, nil)
}

// BuildNimbusGatherPacket 构造采集灵脉封包 (104/1)。
// 官方线格式: [Uint8 gourdType][Uint32 prevAct]。
func BuildNimbusGatherPacket(gourdType uint8) *Packet {
	return NewPacketWithAutoPrevAct(ActionIDNimbusGather, []byte{gourdType})
}

// ParseNimbusInfo 解析灵脉面板回包 (104/0)。
//
// 官方 14 字节: Int32 nimbus, Int16 remain, Int16 gain, 6 字节尾随状态。
func ParseNimbusInfo(payload []byte) (*NimbusInfo, error) {
	if len(payload) < 8 {
		return nil, fmt.Errorf("%w: nimbus info too short (%d)", ErrInvalidPayload, len(payload))
	}
	r := NewReader(payload)
	info := &NimbusInfo{Raw: append([]byte(nil), payload...)}
	var err error
	if info.Nimbus, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: nimbus info nimbus: %v", ErrInvalidPayload, err)
	}
	if info.Remain, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: nimbus info remain: %v", ErrInvalidPayload, err)
	}
	if info.Gain, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: nimbus info gain: %v", ErrInvalidPayload, err)
	}
	return info, nil
}

// ParseNimbusGatherResult 解析采集灵脉回包 (104/1)。
//
// 官方 20 字节布局: Int32 result, 8 字节 nimbus 状态, Int16 remain, Int16 gain, 4 字节尾随。
func ParseNimbusGatherResult(payload []byte) (*NimbusGatherResult, error) {
	if len(payload) < 16 {
		return nil, fmt.Errorf("%w: nimbus gather too short (%d)", ErrInvalidPayload, len(payload))
	}
	r := NewReader(payload)
	res := &NimbusGatherResult{Raw: append([]byte(nil), payload...)}
	var err error
	if res.Result, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: nimbus gather result: %v", ErrInvalidPayload, err)
	}
	if _, err = r.ReadInt32(); err != nil { // nimbus 状态高 4 字节
		return nil, fmt.Errorf("%w: nimbus gather state0: %v", ErrInvalidPayload, err)
	}
	if _, err = r.ReadInt32(); err != nil { // nimbus 状态低 4 字节
		return nil, fmt.Errorf("%w: nimbus gather state1: %v", ErrInvalidPayload, err)
	}
	if res.Remain, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: nimbus gather remain: %v", ErrInvalidPayload, err)
	}
	if res.Gain, err = r.ReadInt16(); err != nil {
		return nil, fmt.Errorf("%w: nimbus gather gain: %v", ErrInvalidPayload, err)
	}
	return res, nil
}