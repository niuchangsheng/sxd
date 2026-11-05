// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方符文/玛瑙批量炼化查询协议 (Mod_Rune_Base, Module 16 / 0x10)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_Rune_Base：
// batch_rune_list 返回 4 个整型字段，batch_rune_use 执行批量炼化。
// 批量炼化的字段语义在客户端资源 SWF 中，自动化在语义未对齐前只查询不执行。
package protocol

import "fmt"

const (
	// ActionIDRuneBatchList 查询符文批量炼化信息 (Module 16, Action 3 -> 0x00100003)
	ActionIDRuneBatchList uint32 = 0x00100003
	// ActionIDRuneBatchUse 执行符文批量炼化 (Module 16, Action 4 -> 0x00100004)
	ActionIDRuneBatchUse uint32 = 0x00100004
)

// RuneBatchInfo 表示符文批量炼化信息 (4 个整型字段)。
type RuneBatchInfo struct {
	// Field1 字段 1 (实机示例: 600)
	Field1 int32
	// Field2 字段 2 (实机示例: 10)
	Field2 int32
	// Field3 字段 3 (实机示例: 98)
	Field3 int32
	// Field4 字段 4 (实机示例: 24050000)
	Field4 int32
}

// BuildRuneBatchListPacket 构造查询符文批量炼化封包 (0x00100003)。
func BuildRuneBatchListPacket() *Packet {
	return NewPacket(ActionIDRuneBatchList, nil)
}

// ParseRuneBatchListResult 解析符文批量炼化信息 (4 × Int32)。
func ParseRuneBatchListResult(payload []byte) (*RuneBatchInfo, error) {
	r := NewReader(payload)
	res := &RuneBatchInfo{}
	var err error
	if res.Field1, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read rune field1: %v", ErrInvalidPayload, err)
	}
	if res.Field2, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read rune field2: %v", ErrInvalidPayload, err)
	}
	if res.Field3, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read rune field3: %v", ErrInvalidPayload, err)
	}
	if res.Field4, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read rune field4: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// BuildRuneBatchListResultPacket 构造符文批量信息回包，供离线 mock 与单测使用。
func BuildRuneBatchListResultPacket(info RuneBatchInfo) *Packet {
	w := NewWriter()
	w.WriteInt32(info.Field1)
	w.WriteInt32(info.Field2)
	w.WriteInt32(info.Field3)
	w.WriteInt32(info.Field4)
	return NewPacket(ActionIDRuneBatchList, w.Bytes())
}
