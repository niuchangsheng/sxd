// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化与大端序编解码。
//
// 本文件实现全服伏魔塔「跨服房间」协议 (Module 341 / 0x0155)。
// 协议基准: 2026-11-10 官方客户端抓包 (qq s0「同尘」)，类名尚未从 SWF 反编译确认。
//
// 实测链路 (每个请求载荷仅 4 字节 PrevAct，无业务参数):
//
//	341/1  (0x01550001)  入口/面板，抓包中作为 341/4 的 PrevAct 出现
//	341/4  (0x01550004)  创建房间 (回包返回房间 ID 与成员快照)
//	341/12 (0x0155000C)  解除封印 (回包仅 1 字节结果码)
//	341/13 (0x0155000D)  语义待确认 (回包仅 1 字节结果码)
//
// 回包首字节结果码: 4 = SUCCESS (与 Module 0 登录回包一致)。
package protocol

import "fmt"

// ModuleTowerRoom 全服伏魔塔跨服房间模块编号 (0x0155)。
const ModuleTowerRoom uint16 = 341

// 模块 341 动作编号。
const (
	// ActionIDTowerRoomOpen 入口/面板动作 (341/1)，抓包中以 PrevAct 形式出现。
	ActionIDTowerRoomOpen uint32 = 0x01550001
	// ActionIDTowerRoomCreate 创建房间 (341/4)，实测。
	ActionIDTowerRoomCreate uint32 = 0x01550004
	// ActionIDTowerRoomUnseal 解除封印 (341/12)，实测。
	ActionIDTowerRoomUnseal uint32 = 0x0155000C
	// ActionIDTowerRoomNext 解除封印后的下一步动作 (341/13)，语义待确认。
	ActionIDTowerRoomNext uint32 = 0x0155000D
)

// TowerRoomResultSuccess 房间流程成功结果码 (与登录回包 SUCCESS=4 一致)。
const TowerRoomResultSuccess uint8 = 4

// TowerRoomResultNotOpen 活动未开放/条件不满足。
// 2026-11-11 01:44 非活动时间实测：341/1、341/4、341/12、341/13 均返回 5。
const TowerRoomResultNotOpen uint8 = 5

// BuildTowerRoomOpenPacket 构造房间入口/面板封包 (341/1，抓包中以 PrevAct 形式出现)。
func BuildTowerRoomOpenPacket(prevAct uint32) *Packet {
	return buildTowerRoomPacket(ActionIDTowerRoomOpen, prevAct)
}

// BuildTowerRoomCreatePacket 构造创建房间封包 (341/4)。
//
// 请求载荷仅包含前序动作号；HasPrevAct 置位后由会话在发送时覆盖为
// 当前最后发送的 ActionID，避免心跳插入导致 PrevAct 链断裂。
func BuildTowerRoomCreatePacket(prevAct uint32) *Packet {
	return buildTowerRoomPacket(ActionIDTowerRoomCreate, prevAct)
}

// BuildTowerRoomUnsealPacket 构造解除封印封包 (341/12)。
func BuildTowerRoomUnsealPacket(prevAct uint32) *Packet {
	return buildTowerRoomPacket(ActionIDTowerRoomUnseal, prevAct)
}

// BuildTowerRoomNextPacket 构造解除封印后的下一步封包 (341/13，语义待确认)。
func BuildTowerRoomNextPacket(prevAct uint32) *Packet {
	return buildTowerRoomPacket(ActionIDTowerRoomNext, prevAct)
}

func buildTowerRoomPacket(actionID, prevAct uint32) *Packet {
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w := NewWriter()
	w.WriteUint32(prevAct)
	return &Packet{ActionID: actionID, Payload: w.Bytes(), HasPrevAct: true}
}

// TowerRoomCreateResult 表示创建房间回包中可确定的部分。
type TowerRoomCreateResult struct {
	// Result 结果码，4 = SUCCESS
	Result uint8
	// RoomID 房间编号 (回包第 2~5 字节)
	RoomID int32
	// Raw 原始载荷，成员快照布局待更多样本确认
	Raw []byte
}

// ParseTowerRoomCreateResult 解析创建房间回包。
func ParseTowerRoomCreateResult(payload []byte) (*TowerRoomCreateResult, error) {
	if len(payload) < 5 {
		return nil, fmt.Errorf("房间创建回包过短: %d", len(payload))
	}
	r := NewReader(payload)
	res := &TowerRoomCreateResult{Raw: payload}
	var err error
	if res.Result, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("房间创建结果码读取失败: %w", err)
	}
	if res.RoomID, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("房间编号读取失败: %w", err)
	}
	return res, nil
}

// TowerRoomResultText 返回房间流程结果码文本。
func TowerRoomResultText(code uint8) string {
	switch code {
	case TowerRoomResultSuccess:
		return "成功"
	case TowerRoomResultNotOpen:
		return "未开放或条件不满足"
	default:
		return fmt.Sprintf("未知结果码(%d)", code)
	}
}
