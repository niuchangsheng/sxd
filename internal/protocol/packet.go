// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// @author Ateng
// @since 2026-10-08
package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// HeaderSize 固定包头长度: [4B Length] + [4B ActionID]
	HeaderSize = 8

	// ActionIDSize 动作编号字段占用字节数 (4B: 高 16 位 Module, 低 16 位 Action)
	ActionIDSize = 4

	// MaxPayloadSize 默认单包载荷最大长度 (16MB)，防御恶意大包导致 OOM
	MaxPayloadSize = 16 * 1024 * 1024
)

var (
	// ErrPacketTooShort 数据长度不足最小包头大小
	ErrPacketTooShort = errors.New("protocol: packet data is shorter than header size")
	// ErrPacketTooLarge 数据长度超出单包允许上限
	ErrPacketTooLarge = errors.New("protocol: packet payload exceeds maximum allowed size")
	// ErrPayloadTruncated 数据长度小于包头声明的载荷长度
	ErrPayloadTruncated = errors.New("protocol: payload data is truncated")
)

// MakeActionID 计算由 Module 与 Action 组合的大端序 32 位 ActionID: (module << 16) | action。
func MakeActionID(module uint16, action uint16) uint32 {
	return (uint32(module) << 16) | uint32(action)
}

// SplitActionID 拆解 32 位 ActionID 为底层的 Module 模块号与 Action 动作号。
func SplitActionID(actionID uint32) (module uint16, action uint16) {
	return uint16(actionID >> 16), uint16(actionID & 0xFFFF)
}

// PrevActMode 声明封包载荷末尾 4 字节前序动作号 (PrevAct) 的填充策略。
// 用于统一取代早期分散的 HasPrevAct / PrevActFromLastSent 双布尔标记。
type PrevActMode uint8

const (
	// PrevActNone 不启用会话层自动尾链管理（零值默认）。
	PrevActNone PrevActMode = iota
	// PrevActAutoBusiness 发送时由会话层以当前业务尾链锚点
	// (lastBusinessActionID，链失步时回退到 lastAckActionID) 填充载荷末尾 4 字节；
	// 心跳 (0/75) 不参与业务尾链。
	PrevActAutoBusiness
	// PrevActExplicit 载荷末尾 4 字节已由调用方显式指定，会话层严禁覆盖。
	PrevActExplicit
)

// Packet 代表神仙道私有二进制协议数据封包。
type Packet struct {
	// ActionID 业务动作指令编号 (大端序 uint32: [Module uint16][Action uint16])
	ActionID uint32
	// Payload 封包承载的应用层数据载荷 (已透明解压)
	Payload []byte
	// PrevActMode 声明载荷末尾 4 字节 PrevAct 的会话层处理策略。
	// 非零时优先于旧布尔标记 HasPrevAct / PrevActFromLastSent。
	PrevActMode PrevActMode
	// HasPrevAct 兼容旧标记：等价于 PrevActAutoBusiness。新代码请优先使用 PrevActMode。
	HasPrevAct bool
	// PrevActFromLastSent 兼容旧标记：等价于 PrevActAutoBusiness。新代码请优先使用 PrevActMode。
	PrevActFromLastSent bool
}

// EffectivePrevActMode 返回封包实际生效的 PrevAct 策略。
func (p *Packet) EffectivePrevActMode() PrevActMode {
	if p == nil {
		return PrevActNone
	}
	if p.PrevActMode != PrevActNone {
		return p.PrevActMode
	}
	if p.PrevActFromLastSent || p.HasPrevAct {
		return PrevActAutoBusiness
	}
	return PrevActNone
}

// NewPacket 创建一个初始化好的 Packet 实例。
func NewPacket(actionID uint32, payload []byte) *Packet {
	if payload == nil {
		payload = []byte{}
	}
	return &Packet{
		ActionID: actionID,
		Payload:  payload,
	}
}

// NewPacketWithAutoPrevAct 构造由会话层在发送瞬间自动填充业务 PrevAct 尾链的封包。
// 载荷由 args 与 4 字节占位尾链组成。
func NewPacketWithAutoPrevAct(actionID uint32, args []byte) *Packet {
	buf := make([]byte, len(args)+4)
	copy(buf, args)
	return &Packet{
		ActionID:    actionID,
		Payload:     buf,
		PrevActMode: PrevActAutoBusiness,
	}
}

// NewPacketWithExplicitPrevAct 构造携带显式 PrevAct 尾链的封包；会话层不会覆盖末尾 4 字节。
func NewPacketWithExplicitPrevAct(actionID uint32, args []byte, prevAct uint32) *Packet {
	buf := make([]byte, len(args)+4)
	copy(buf, args)
	binary.BigEndian.PutUint32(buf[len(args):], prevAct)
	return &Packet{
		ActionID:    actionID,
		Payload:     buf,
		PrevActMode: PrevActExplicit,
	}
}

// Marshal 将封包按原生二进制线格式序列化为字节切片。
// 线格式首部 4 字节大端序长度包含 ActionID 的 4 字节与载荷长度。
func (p *Packet) Marshal() ([]byte, error) {
	if len(p.Payload) > MaxPayloadSize {
		return nil, fmt.Errorf("%w: length %d > %d", ErrPacketTooLarge, len(p.Payload), MaxPayloadSize)
	}

	bodyLen := uint32(ActionIDSize + len(p.Payload))
	buf := make([]byte, 4+bodyLen)

	binary.BigEndian.PutUint32(buf[0:4], bodyLen)
	binary.BigEndian.PutUint32(buf[4:8], p.ActionID)
	copy(buf[HeaderSize:], p.Payload)

	return buf, nil
}

// MarshalCompressed 将封包载荷执行 zlib 压缩后序列化为字节切片。
func (p *Packet) MarshalCompressed() ([]byte, error) {
	compressed, err := Compress(p.Payload)
	if err != nil {
		return nil, fmt.Errorf("protocol: failed to compress payload: %w", err)
	}

	if len(compressed) > MaxPayloadSize {
		return nil, fmt.Errorf("%w: compressed length %d > %d", ErrPacketTooLarge, len(compressed), MaxPayloadSize)
	}

	bodyLen := uint32(ActionIDSize + len(compressed))
	buf := make([]byte, 4+bodyLen)

	binary.BigEndian.PutUint32(buf[0:4], bodyLen)
	binary.BigEndian.PutUint32(buf[4:8], p.ActionID)
	copy(buf[HeaderSize:], compressed)

	return buf, nil
}

// Unmarshal 解析原生线格式数据为 Packet 结构，自动完成基础校验与透明解压缩。
func Unmarshal(data []byte) (*Packet, error) {
	if len(data) < HeaderSize {
		return nil, ErrPacketTooShort
	}

	bodyLen := binary.BigEndian.Uint32(data[0:4])
	if bodyLen < ActionIDSize {
		return nil, ErrPacketTooShort
	}
	if bodyLen > MaxPayloadSize+ActionIDSize {
		return nil, fmt.Errorf("%w: length %d > %d", ErrPacketTooLarge, bodyLen, MaxPayloadSize+ActionIDSize)
	}

	payloadLen := bodyLen - ActionIDSize
	if uint32(len(data)-HeaderSize) < payloadLen {
		return nil, ErrPayloadTruncated
	}

	fullBody := data[4 : 4+bodyLen]
	// 1. 尝试整包压缩自愈嗅探 (神仙道整包压缩格式: [4B len] + [zlib 流包含 4B ActionID 与载荷])
	if decompressed, ok := DecompressWholePacketIfNeeded(fullBody); ok {
		if len(decompressed) < ActionIDSize {
			return nil, ErrPacketTooShort
		}
		actionID := binary.BigEndian.Uint32(decompressed[0:4])
		payload := decompressed[4:]
		return &Packet{
			ActionID: actionID,
			Payload:  payload,
		}, nil
	}

	// 2. 标准格式与载荷级压缩
	actionID := binary.BigEndian.Uint32(data[4:8])
	rawPayload := data[HeaderSize : HeaderSize+payloadLen]

	// 透明处理 zlib 压缩嗅探与自愈
	payload, err := DecompressIfNeeded(rawPayload)
	if err != nil {
		return nil, err
	}

	return &Packet{
		ActionID: actionID,
		Payload:  payload,
	}, nil
}
