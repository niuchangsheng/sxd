// Package protocol 提供仙界虚空协议 (Mod_StVoid_Base, Module 652)。
//
//	open_panel 652/1 request=[]
//	like       652/2 request=[Int player_id] response=[UByte result, [Int,Int]]
//	world_pk_rank 652/4 request=[] response=[[Int player_id, Int, String, String, Int, Int, Int]]
package protocol

import "fmt"

const (
	// ActionIDStVoidOpen 仙界虚空面板 (652/1 -> 0x028C0001)
	ActionIDStVoidOpen uint32 = 0x028C0001
	// ActionIDStVoidLike 仙界虚空膜拜/点赞 (652/2 -> 0x028C0002)
	ActionIDStVoidLike uint32 = 0x028C0002
	// ActionIDStVoidWorldPkRank 世界 PK 榜 (652/4 -> 0x028C0004)
	ActionIDStVoidWorldPkRank uint32 = 0x028C0004
)

// 仙界虚空结果码。
const (
	StVoidResultSuccess  uint8 = 0
	StVoidResultFailed   uint8 = 1
	StVoidResultIsLiked  uint8 = 2
	StVoidResultPackFull uint8 = 3
)

// BuildStVoidWorldPkRankPacket 构造世界 PK 榜查询封包。
func BuildStVoidWorldPkRankPacket() *Packet {
	return NewPacket(ActionIDStVoidWorldPkRank, nil)
}

// BuildStVoidLikePacket 构造膜拜/点赞封包。
func BuildStVoidLikePacket(playerID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(playerID)
	w.WriteUint32(0)
	pkt := NewPacket(ActionIDStVoidLike, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// ParseStVoidRankPlayerIDs 解析排行榜中的玩家 ID 列表。
func ParseStVoidRankPlayerIDs(payload []byte) ([]int32, error) {
	r := NewReader(DecompressPayloadIfNeeded(payload))
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("仙界虚空排行榜数量读取失败: %w", err)
	}
	ids := make([]int32, 0, count)
	for i := 0; i < int(count); i++ {
		id, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("仙界虚空排行榜玩家[%d]读取失败: %w", i, err)
		}
		if _, err = r.ReadInt32(); err != nil {
			return nil, err
		}
		if _, err = r.ReadString(); err != nil {
			return nil, err
		}
		if _, err = r.ReadString(); err != nil {
			return nil, err
		}
		for j := 0; j < 3; j++ {
			if _, err = r.ReadInt32(); err != nil {
				return nil, err
			}
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// ParseStVoidResult 解析单字节结果码。
func ParseStVoidResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("仙界虚空结果码为空")
	}
	return payload[0], nil
}
