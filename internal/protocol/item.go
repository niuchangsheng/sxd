// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// @author Ateng
// @since 2026-10-22
package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

var (
	// ErrItemPayloadShort 表示物品回包二进制长度不足。
	ErrItemPayloadShort = errors.New("protocol/item: payload length is too short")
)

// ItemRecord 代表背包或仓库中的单件物品详细数据记录 (75 字节)。
type ItemRecord struct {
	PlayerItemID int32 // 玩家物品唯一实例数字标识
	ItemID       int32 // 对应数据字典 item 表的 id
	GridID       int32 // 背包或仓库格位编号 (从 1 起始)
	Field3       int32
	Field4       int32
	Field5       int32
	Field6       int32
	Field7       int32
	Field8       int32
	Field9       int8
	Field10      int32
	Field11      int32
	Field12      int8
	Field13      int16
	Field14      int16
	Field15      int8
	Field16      int32
	Field17      int32
	Field18      int32
	Field19      int64
	ItemType     int32 // 物品类型 (Field20)
}

// PackItemListResponse 对应 Mod 2, Act 41 (get_player_pack_item_list) 的回包结构。
type PackItemListResponse struct {
	PackCapacity      int16        // 主背包总格位容量
	PackEmptyGrids    int32        // 主背包剩余空格数量
	PackItems         []ItemRecord // 主背包已用物品列表
	WarehouseCapacity int16        // 随身仓库总格位容量
	WarehouseItems    []ItemRecord // 随身仓库物品列表
}

// BuildGetPackItemListPacket 构造查询主背包与仓库物品列表的协议封包 (Mod 2, Act 41)。
func BuildGetPackItemListPacket() *Packet {
	return NewPacket(ActionIDItemGetPackList, []byte{})
}

// ReadItemRecord 从流中精确解码 75 字节的物品单项记录。
func ReadItemRecord(r *bytes.Reader) (ItemRecord, error) {
	if r.Len() < 75 {
		return ItemRecord{}, ErrItemPayloadShort
	}

	var rec ItemRecord
	if err := binary.Read(r, binary.BigEndian, &rec.PlayerItemID); err != nil {
		return rec, err
	}
	if err := binary.Read(r, binary.BigEndian, &rec.ItemID); err != nil {
		return rec, err
	}
	if err := binary.Read(r, binary.BigEndian, &rec.GridID); err != nil {
		return rec, err
	}
	if err := binary.Read(r, binary.BigEndian, &rec.Field3); err != nil {
		return rec, err
	}
	if err := binary.Read(r, binary.BigEndian, &rec.Field4); err != nil {
		return rec, err
	}
	if err := binary.Read(r, binary.BigEndian, &rec.Field5); err != nil {
		return rec, err
	}
	if err := binary.Read(r, binary.BigEndian, &rec.Field6); err != nil {
		return rec, err
	}
	if err := binary.Read(r, binary.BigEndian, &rec.Field7); err != nil {
		return rec, err
	}
	if err := binary.Read(r, binary.BigEndian, &rec.Field8); err != nil {
		return rec, err
	}
	if err := binary.Read(r, binary.BigEndian, &rec.Field9); err != nil {
		return rec, err
	}
	if err := binary.Read(r, binary.BigEndian, &rec.Field10); err != nil {
		return rec, err
	}
	if err := binary.Read(r, binary.BigEndian, &rec.Field11); err != nil {
		return rec, err
	}
	if err := binary.Read(r, binary.BigEndian, &rec.Field12); err != nil {
		return rec, err
	}
	if err := binary.Read(r, binary.BigEndian, &rec.Field13); err != nil {
		return rec, err
	}
	if err := binary.Read(r, binary.BigEndian, &rec.Field14); err != nil {
		return rec, err
	}
	if err := binary.Read(r, binary.BigEndian, &rec.Field15); err != nil {
		return rec, err
	}
	if err := binary.Read(r, binary.BigEndian, &rec.Field16); err != nil {
		return rec, err
	}
	if err := binary.Read(r, binary.BigEndian, &rec.Field17); err != nil {
		return rec, err
	}
	if err := binary.Read(r, binary.BigEndian, &rec.Field18); err != nil {
		return rec, err
	}
	if err := binary.Read(r, binary.BigEndian, &rec.Field19); err != nil {
		return rec, err
	}
	if err := binary.Read(r, binary.BigEndian, &rec.ItemType); err != nil {
		return rec, err
	}

	return rec, nil
}

// WriteItemRecord 将 75 字节物品记录编码写入 Buffer。
func WriteItemRecord(buf *bytes.Buffer, rec ItemRecord) error {
	fields := []any{
		rec.PlayerItemID, rec.ItemID, rec.GridID,
		rec.Field3, rec.Field4, rec.Field5, rec.Field6, rec.Field7, rec.Field8,
		rec.Field9,
		rec.Field10, rec.Field11,
		rec.Field12,
		rec.Field13, rec.Field14,
		rec.Field15,
		rec.Field16, rec.Field17, rec.Field18,
		rec.Field19,
		rec.ItemType,
	}
	for _, f := range fields {
		if err := binary.Write(buf, binary.BigEndian, f); err != nil {
			return err
		}
	}
	return nil
}

// ParsePackItemListResponse 解析获取背包物品列表的响应 Payload。
func ParsePackItemListResponse(payload []byte) (*PackItemListResponse, error) {
	if len(payload) < 8 {
		return nil, ErrItemPayloadShort
	}

	r := bytes.NewReader(payload)
	var resp PackItemListResponse

	if err := binary.Read(r, binary.BigEndian, &resp.PackCapacity); err != nil {
		return nil, fmt.Errorf("protocol/item: read pack capacity failed: %w", err)
	}
	if err := binary.Read(r, binary.BigEndian, &resp.PackEmptyGrids); err != nil {
		return nil, fmt.Errorf("protocol/item: read pack empty grids failed: %w", err)
	}

	var packCount uint16
	if err := binary.Read(r, binary.BigEndian, &packCount); err != nil {
		return nil, fmt.Errorf("protocol/item: read pack items count failed: %w", err)
	}

	resp.PackItems = make([]ItemRecord, 0, packCount)
	for i := 0; i < int(packCount); i++ {
		rec, err := ReadItemRecord(r)
		if err != nil {
			return nil, fmt.Errorf("protocol/item: read pack item #%d failed: %w", i, err)
		}
		resp.PackItems = append(resp.PackItems, rec)
	}

	if r.Len() >= 2 {
		_ = binary.Read(r, binary.BigEndian, &resp.WarehouseCapacity)
	}
	if r.Len() >= 2 {
		var whCount uint16
		_ = binary.Read(r, binary.BigEndian, &whCount)
		resp.WarehouseItems = make([]ItemRecord, 0, whCount)
		for i := 0; i < int(whCount); i++ {
			rec, err := ReadItemRecord(r)
			if err != nil {
				break
			}
			resp.WarehouseItems = append(resp.WarehouseItems, rec)
		}
	}

	return &resp, nil
}

// BuildUseGridItemPacket 构造使用背包指定格位物品封包 (Mod 2, Act 53)。
func BuildUseGridItemPacket(gridID int16) (*Packet, error) {
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.BigEndian, gridID); err != nil {
		return nil, fmt.Errorf("protocol/item: encode grid_id failed: %w", err)
	}
	return NewPacket(ActionIDItemUseGrid, buf.Bytes()), nil
}

// ParseUseGridItemResponse 解析使用物品应答 (Mod 2, Act 53)。
func ParseUseGridItemResponse(payload []byte) (bool, uint8, error) {
	if len(payload) < 1 {
		return false, 0, ErrItemPayloadShort
	}
	resultCode := payload[0]
	// 神仙道业务中 20 为 ACTION_SUCCESS，0 为常规 SUCCESS
	isSuccess := resultCode == ItemActionSuccess || resultCode == ItemSuccess
	return isSuccess, resultCode, nil
}
