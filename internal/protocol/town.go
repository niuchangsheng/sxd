// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现城镇场景切换相关的最小协议编解码 (Mod_Town_Base, Module 1)。
package protocol

// BuildEnterTownPacket 构造进入/切换城镇封包 (Module 1, Action 0)。
//
// 官方描述符 request 为 [Int townID]，客户端的 DataBase.netCall 会在末尾自动追加
// 4 字节前序动作号；这里显式写入并由会话层在真正发包时用最近动作号覆盖。
// townID 对应玩家信息中的 townMapId（Mod_Player_Base.get_player_info 第 10 个字段）。
func BuildEnterTownPacket(townID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(townID)
	if prevAct == 0 {
		prevAct = ActionIDPlayerInitStep3
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDTownEnter, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}
