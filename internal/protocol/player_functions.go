package protocol

import (
	"fmt"
)

// 已开放功能列表 (Mod_Player_Base.get_player_function, Module 0, Action 6)。
//
// 服务端只下发当前角色**已开放**的功能条目；客户端 FunctionType.updateOpenedList
// 据此维护“已开放/已体验”集合。相比逐个发送业务请求等待超时，本接口一次即可
// 判定任意功能是否开放，是自动化做功能门槛前置检查的官方通道。
const (
	// ActionIDPlayerGetFunctions 查询当前角色已开放功能列表 (Module 0, Action 6 -> 0x00000006)
	ActionIDPlayerGetFunctions uint32 = 0x00000006
)

// PlayerFunction 已开放功能条目。
type PlayerFunction struct {
	// ID 功能编号 (对应 FunctionTypeData.List 的键)
	ID int32
	// Played 是否已体验过该功能 (FunctionType.isPlayed)
	Played bool
}

// BuildGetPlayerFunctionsPacket 构造查询已开放功能列表封包。
//
// 官方描述符 request 为空，客户端 netCall 会在末尾追加 4 字节前序动作号。
func BuildGetPlayerFunctionsPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = ActionIDPlayerGetInfo
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDPlayerGetFunctions, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// BuildPlayerFunctionsResultPacket 构造已开放功能列表回包，供离线 Mock 与单测使用。
func BuildPlayerFunctionsResultPacket(ids []int32) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(ids)))
	for _, id := range ids {
		w.WriteInt32(id)
		w.WriteUint8(1)
	}
	return NewPacket(ActionIDPlayerGetFunctions, w.Bytes())
}

// ParsePlayerFunctionsResponse 解析已开放功能列表回包:
// Uint16 count, count × (Int32 functionID, Byte isPlayed)。
func ParsePlayerFunctionsResponse(payload []byte) ([]PlayerFunction, error) {
	decomp := DecompressPayloadIfNeeded(payload)
	r := NewReader(decomp)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("read player function count: %w", err)
	}
	functions := make([]PlayerFunction, 0, count)
	for i := 0; i < int(count); i++ {
		id, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("read player function #%d id: %w", i, err)
		}
		played, err := r.ReadUint8()
		if err != nil {
			return nil, fmt.Errorf("read player function #%d played flag: %w", i, err)
		}
		functions = append(functions, PlayerFunction{ID: id, Played: played == 1})
	}
	return functions, nil
}
