// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现城镇场景切换相关的最小协议编解码 (Mod_Town_Base, Module 1)。
package protocol

// BuildEnterTownPacket 构造进入/切换城镇封包 (Module 1, Action 0)。
//
// 官方描述符 request 为 [Int townID]，载荷仅 4 字节。2026-11-15 线上探针
// (test/probes/probe_sections_after_quest_sync_live.go) 实测：
//   - 裸 4 字节 [Int townID] 稳定有回包；
//   - 裸载荷会把会话切出任务上下文，紧随其后的 4/0 get_sections 被丢弃；
//   - 追加 4 字节 PrevAct (且锚点为 Mod 3/4 动作) 的 8 字节载荷既能复位城镇，
//     又能保持任务上下文，随后的 4/0 可正常回包。
//
// townID 对应玩家信息中的 townMapId（Mod_Player_Base.get_player_info 第 10 个字段），
// 服务端可能写入副本地图编号（如 21），裸载荷下同样可正常复位场景。
func BuildEnterTownPacket(townID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(townID)
	return NewPacket(ActionIDTownEnter, w.Bytes())
}

// BuildEnterTownWithPrevActPacket 构造带 PrevAct 链的 enter_town 请求。
//
// 官方客户端发出的 enter_town request 只有 [Int townID]，但 DataBase.netCall
// 会在尾部追加当前 PrevAct（最后收到的包）。2026-11-15 抓包确认 PrevAct 链
// 以「最后收到的包」为锚点；这里写入占位值并置 HasPrevAct=true，由会话层
// 在发包时用真实 lastRxActionID 覆盖。
//
// 裸 4 字节虽然能回包，但会把会话切出任务上下文，导致随后的 4/0 被丢弃，
// 因此不再提供裸载荷路径。
func BuildEnterTownWithPrevActPacket(townID int32, anchor uint32) *Packet {
	if anchor == 0 {
		anchor = ActionIDPlayerGetInfo
	}
	w := NewWriter()
	w.WriteInt32(townID)
	w.WriteUint32(anchor)
	pkt := NewPacket(ActionIDTownEnter, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}
