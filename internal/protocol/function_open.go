package protocol

import "fmt"

// 功能开放状态查询 (Mod_Player_Base.check_function_open, Module 0, Action 69)。
//
// 服务端对未解锁模块通常静默丢弃业务请求; 该接口用于在发起业务前探测功能是否开放,
// 避免每个调度周期都等待业务请求超时。
const (
	// ActionIDPlayerCheckFunctionOpen 查询指定功能是否开放 (Module 0, Action 69 -> 0x00000045)
	ActionIDPlayerCheckFunctionOpen uint32 = 0x00000045

	// PlayerFunctionOpenTrue 功能已开放 (Mod_Player_Base.TRUE)
	PlayerFunctionOpenTrue uint8 = 119
	// PlayerFunctionOpenFalse 功能未开放 (Mod_Player_Base.FALSE)
	PlayerFunctionOpenFalse uint8 = 120

	// FunctionIDRefinePool 粗铁淬炼池功能编号 (FunctionTypeData: RefinePool)
	FunctionIDRefinePool int32 = 163
	// FunctionIDWorshipMars 尊像膜拜功能编号 (FunctionTypeData: WorshipMars)
	FunctionIDWorshipMars int32 = 55
	// FunctionIDWuFaField 悟法领域功能编号 (FunctionTypeData: WuFaField)
	FunctionIDWuFaField int32 = 245
)

// BuildCheckFunctionOpenPacket 构造功能开放状态查询封包。
//
// 线格式: Int32 function_id, Uint32 prev_act。
func BuildCheckFunctionOpenPacket(functionID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(functionID)
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDPlayerCheckFunctionOpen, w.Bytes())
	// 心跳会推进服务端 PrevAct 链, 发包时由会话层用真实最近动作号覆盖尾部。
	pkt.HasPrevAct = true
	return pkt
}

// ParseCheckFunctionOpenResponse 解析功能开放状态查询回包。
func ParseCheckFunctionOpenResponse(payload []byte) (uint8, error) {
	r := NewReader(payload)
	flag, err := r.ReadUint8()
	if err != nil {
		return 0, fmt.Errorf("check function open: read flag: %w", err)
	}
	return flag, nil
}
