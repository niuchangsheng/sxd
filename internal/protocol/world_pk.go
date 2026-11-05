// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方跨服 PK 排行协议 (Mod_WorldPkRanking_Base, Module 349 / 0x15D)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_WorldPkRanking_Base：
// 先查询活动状态，仅在 ON 时加入 PK；OFF/超级城镇关闭时跳过。
package protocol

import "fmt"

const (
	// ActionIDWorldPkStatus 查询跨服 PK 状态 (Module 349, Action 0 -> 0x015D0000)
	ActionIDWorldPkStatus uint32 = 0x015D0000
	// ActionIDWorldPkJoin 加入跨服 PK (Module 349, Action 3 -> 0x015D0003)
	ActionIDWorldPkJoin uint32 = 0x015D0003
)

// Mod_WorldPkRanking_Base 状态/结果码。
const (
	// WorldPkStatusOn 活动开启
	WorldPkStatusOn uint8 = 0
	// WorldPkStatusOff 活动关闭
	WorldPkStatusOff uint8 = 1
	// WorldPkStatusSuperTownClose 超级城镇关闭
	WorldPkStatusSuperTownClose uint8 = 2
	// WorldPkResultSuccess 加入成功
	WorldPkResultSuccess uint8 = 3
	// WorldPkResultActivityNotOpen 活动未开放
	WorldPkResultActivityNotOpen uint8 = 4
	// WorldPkResultFailed 失败
	WorldPkResultFailed uint8 = 5
	// WorldPkResultTimeCD 冷却中
	WorldPkResultTimeCD uint8 = 6
	// WorldPkResultTimesLimit 次数上限
	WorldPkResultTimesLimit uint8 = 12
	// WorldPkResultLowLevel 等级不足
	WorldPkResultLowLevel uint8 = 23
)

// WorldPkStatus 表示跨服 PK 状态。
//
// 线格式: UByte status, Short, Int, Int, Int (后四项内容不消费)。
type WorldPkStatus struct {
	// Status 状态码
	Status uint8
}

// WorldPkJoinResult 表示加入 PK 的结果。
type WorldPkJoinResult struct {
	// Result 结果码
	Result uint8
}

// BuildWorldPkStatusPacket 构造查询跨服 PK 状态封包 (0x015D0000)。
func BuildWorldPkStatusPacket() *Packet {
	return NewPacket(ActionIDWorldPkStatus, nil)
}

// BuildWorldPkJoinPacket 构造加入跨服 PK 封包 (0x015D0003)。
func BuildWorldPkJoinPacket() *Packet {
	return NewPacket(ActionIDWorldPkJoin, nil)
}

// ParseWorldPkStatusResult 解析状态回包 (只消费首字节)。
func ParseWorldPkStatusResult(payload []byte) (*WorldPkStatus, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("%w: empty world pk status", ErrInvalidPayload)
	}
	return &WorldPkStatus{Status: payload[0]}, nil
}

// ParseWorldPkJoinResult 解析加入结果 (只消费首字节)。
func ParseWorldPkJoinResult(payload []byte) (*WorldPkJoinResult, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("%w: empty world pk join result", ErrInvalidPayload)
	}
	return &WorldPkJoinResult{Result: payload[0]}, nil
}

// BuildWorldPkStatusResultPacket 构造状态回包，供离线 mock 与单测使用。
func BuildWorldPkStatusResultPacket(status uint8) *Packet {
	w := NewWriter()
	w.WriteUint8(status)
	w.WriteInt16(0)
	w.WriteInt32(0)
	w.WriteInt32(0)
	w.WriteInt32(0)
	return NewPacket(ActionIDWorldPkStatus, w.Bytes())
}

// BuildWorldPkJoinResultPacket 构造加入回包，供离线 mock 与单测使用。
func BuildWorldPkJoinResultPacket(result uint8) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	w.WriteInt32(0)
	w.WriteInt32(0)
	return NewPacket(ActionIDWorldPkJoin, w.Bytes())
}
