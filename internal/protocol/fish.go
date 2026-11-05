// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方自动钓鱼协议 (Mod_Fish_Base, Module 217 / 0xD9)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_Fish_Base 与
// com.datas.FishData：查询鱼钩数量后逐个下钩，产出命格/道具。
// 自动化只使用免费鱼钩，不消耗元宝。
package protocol

import "fmt"

const (
	// ActionIDFishHooks 查询玩家鱼钩数量 (Module 217, Action 0 -> 0x00D90000)
	ActionIDFishHooks uint32 = 0x00D90000
	// ActionIDFishDo 执行一次钓鱼 (Module 217, Action 1 -> 0x00D90001)
	ActionIDFishDo uint32 = 0x00D90001
)

// Mod_Fish_Base 结果码。
const (
	// FishResultSuccess 钓鱼成功
	FishResultSuccess uint8 = 0
	// FishResultNotEnoughFishhook 鱼钩不足
	FishResultNotEnoughFishhook uint8 = 1
	// FishResultNotEnoughFateGrid 命格背包不足
	FishResultNotEnoughFateGrid uint8 = 2
	// FishResultItemPackFull 物品背包已满
	FishResultItemPackFull uint8 = 3
	// FishResultFiveElementsPackFull 五行之灵背包已满
	FishResultFiveElementsPackFull uint8 = 4
	// FishResultDragonballNoFreeGrid 龙珠无空位
	FishResultDragonballNoFreeGrid uint8 = 5
	// FishResultCanJuanPackFull 残卷背包已满
	FishResultCanJuanPackFull uint8 = 6
)

// FishDoResult 表示一次钓鱼结果。
type FishDoResult struct {
	// Result 结果码
	Result uint8
	// ItemID 产出道具编号
	ItemID int32
	// Number 产出数量
	Number int32
}

// BuildFishHooksPacket 构造查询鱼钩封包 (0x00D90000)。
func BuildFishHooksPacket() *Packet {
	return NewPacket(ActionIDFishHooks, nil)
}

// BuildFishDoPacket 构造执行钓鱼封包 (0x00D90001)。
func BuildFishDoPacket() *Packet {
	return NewPacket(ActionIDFishDo, nil)
}

// ParseFishHooksResult 解析鱼钩数量回包 (Int fishhook_number)。
func ParseFishHooksResult(payload []byte) (int32, error) {
	r := NewReader(payload)
	n, err := r.ReadInt32()
	if err != nil {
		return 0, fmt.Errorf("%w: failed to read fishhook_number: %v", ErrInvalidPayload, err)
	}
	return n, nil
}

// ParseFishDoResult 解析钓鱼回包: UByte result, Int item_id, Int number。
func ParseFishDoResult(payload []byte) (*FishDoResult, error) {
	r := NewReader(payload)
	res := &FishDoResult{}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read fish result: %v", ErrInvalidPayload, err)
	}
	if res.ItemID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read fish item_id: %v", ErrInvalidPayload, err)
	}
	if res.Number, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read fish number: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// BuildFishHooksResultPacket 构造鱼钩数量回包，供离线 mock 与单测使用。
func BuildFishHooksResultPacket(n int32) *Packet {
	w := NewWriter()
	w.WriteInt32(n)
	return NewPacket(ActionIDFishHooks, w.Bytes())
}

// BuildFishDoResultPacket 构造钓鱼回包，供离线 mock 与单测使用。
func BuildFishDoResultPacket(result uint8, itemID, number int32) *Packet {
	w := NewWriter()
	w.WriteUint8(result)
	w.WriteInt32(itemID)
	w.WriteInt32(number)
	return NewPacket(ActionIDFishDo, w.Bytes())
}
