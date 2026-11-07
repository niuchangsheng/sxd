// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现战斗冷却查询 (Mod_Player_Base.get_player_war_cd_time, Module 0 / Action 41)：
//
//	request  = [Int cd_type]
//	response = [Int remaining_seconds]
//
// 官方客户端在发起摘仙桃/get_peach 前会先查询 WarCdTimeType.Peach (17)，
// 剩余秒数 > 0 时等待冷却结束再发起战斗。
package protocol

import "fmt"

// ActionIDPlayerGetWarCdTime 查询指定玩法类型的战斗冷却 (Module 0, Action 41 -> 0x00000029)。
const ActionIDPlayerGetWarCdTime uint32 = 0x00000029

// 战斗冷却类型 (WarCdTimeType)。
const (
	// WarCdTypeMission 任务战斗
	WarCdTypeMission int32 = 13
	// WarCdTypePeach 摘仙桃战斗
	WarCdTypePeach int32 = 17
	// WarCdTypeTower 爬塔战斗
	WarCdTypeTower int32 = 19
)

// BuildPlayerGetWarCdTimePacket 构造战斗冷却查询封包。
//
// 线格式: Int32 cd_type, Uint32 prev_act。
func BuildPlayerGetWarCdTimePacket(cdType int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(cdType)
	if prevAct == 0 {
		prevAct = ActionIDTownEnter
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDPlayerGetWarCdTime, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// ParsePlayerGetWarCdTimeResult 解析战斗冷却查询回包。
func ParsePlayerGetWarCdTimeResult(payload []byte) (int32, error) {
	r := NewReader(payload)
	cd, err := r.ReadInt32()
	if err != nil {
		return 0, fmt.Errorf("%w: 战斗冷却剩余秒数读取失败: %v", ErrInvalidPayload, err)
	}
	return cd, nil
}

// BuildPlayerGetWarCdTimeResultPacket 构造战斗冷却查询回包，供离线 Mock 与单测使用。
func BuildPlayerGetWarCdTimeResultPacket(cd int32) *Packet {
	w := NewWriter()
	w.WriteInt32(cd)
	return NewPacket(ActionIDPlayerGetWarCdTime, w.Bytes())
}
