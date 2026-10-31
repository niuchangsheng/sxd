// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// @author Ateng
// @since 2026-10-24
package protocol

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
)

const (
	// ModuleQuest 任务模块编号 (Mod_Quest_Base)
	ModuleQuest uint16 = 3

	// ActionIDQuestGetNPCQuests 获取指定 NPC 的可接/已接任务列表 (Module 3, Action 1 -> 0x00030001)
	ActionIDQuestGetNPCQuests uint32 = 0x00030001

	// ActionIDQuestComplete 按任务ID打开NPC任务交互信息 (Module 3, Action 2 -> 0x00030002)
	ActionIDQuestComplete uint32 = 0x00030002

	// ActionIDQuestAccept 接受任务 (Module 3, Action 3 -> 0x00030003)
	ActionIDQuestAccept uint32 = 0x00030003

	// ActionIDQuestFinish 完成纯对话类任务目标 (Module 3, Action 4 -> 0x00030004)
	ActionIDQuestFinish uint32 = 0x00030004

	// ActionIDQuestSubmitComplete 交付已完成任务并领取奖励 (Module 3, Action 5 -> 0x00030005)
	ActionIDQuestSubmitComplete uint32 = 0x00030005

	// ActionIDQuestListPlayer 拉取玩家已接任务列表简表 (Module 3, Action 6 -> 0x00030006)
	ActionIDQuestListPlayer uint32 = 0x00030006

	// ActionIDQuestCanReceive 查询城镇可接任务列表 (Module 3, Action 8 -> 0x00030008)
	ActionIDQuestCanReceive uint32 = 0x00030008

	// ActionIDQuestList 玩家当前任务列表与状态更新 (Module 3, Action 9 -> 0x00030009)
	ActionIDQuestList uint32 = 0x00030009
)

// QuestState 任务状态枚举
const (
	QuestStateUnaccepted   uint8 = 1 // 可接取
	QuestStateIncompleted  uint8 = 0 // 进行中 (未完成目标)
	QuestStateCompletable  uint8 = 1 // 已达成目标 (可交付NPC)
	QuestStateFinished     uint8 = 2 // 已交付/已完成
)

// GetNPCQuestsRequest 查询指定 NPC 任务列表请求。
type GetNPCQuestsRequest struct {
	NPCID   int32
	PrevAct uint32
}

// NPCQuestItem NPC 处的任务项条目。
type NPCQuestItem struct {
	QuestID int32
	State   uint8 // 1=可接取, 0=不可接, 2=可交付
}

// BuildGetNPCQuestsPacket 构造查询 NPC 任务列表请求封包 (Module 3, Action 1)。
func BuildGetNPCQuestsPacket(req GetNPCQuestsRequest) (*Packet, error) {
	w := NewWriter()
	w.WriteInt32(req.NPCID)
	prevAct := req.PrevAct
	if prevAct == 0 {
		prevAct = 0x0000004F
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDQuestGetNPCQuests, w.Bytes()), nil
}

// ParseNPCQuestsResponse 解析 NPC 任务列表回包。
func ParseNPCQuestsResponse(payload []byte) ([]NPCQuestItem, error) {
	if len(payload) < 2 {
		return nil, fmt.Errorf("%w: payload too short for npc quest list", ErrInvalidPayload)
	}
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}
	items := make([]NPCQuestItem, 0, count)
	for i := uint16(0); i < count; i++ {
		if r.Remaining() < 5 {
			break
		}
		qid, _ := r.ReadInt32()
		st, _ := r.ReadUint8()
		items = append(items, NPCQuestItem{
			QuestID: qid,
			State:   st,
		})
	}
	return items, nil
}

// AcceptQuestRequest 接受任务请求。
type AcceptQuestRequest struct {
	QuestID int32
	PrevAct uint32
}

// BuildAcceptQuestPacket 构造接受任务请求封包 (Module 3, Action 3)。
func BuildAcceptQuestPacket(req AcceptQuestRequest) (*Packet, error) {
	w := NewWriter()
	w.WriteInt32(req.QuestID)
	prevAct := req.PrevAct
	if prevAct == 0 {
		prevAct = ActionIDQuestList
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDQuestAccept, w.Bytes()), nil
}

// CompleteQuestRequest 完成/交付任务请求。
type CompleteQuestRequest struct {
	QuestID int32
	PrevAct uint32
}

// BuildCompleteQuestPacket 构造打开任务交付对话请求封包 (Module 3, Action 2)。
func BuildCompleteQuestPacket(req CompleteQuestRequest) (*Packet, error) {
	w := NewWriter()
	w.WriteInt32(req.QuestID)
	prevAct := req.PrevAct
	if prevAct == 0 {
		prevAct = ActionIDQuestList
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDQuestComplete, w.Bytes())
	pkt.HasPrevAct = true
	return pkt, nil
}

// BuildFinishQuestPacket 构造完成对话类任务请求封包 (Module 3, Action 4)。
func BuildFinishQuestPacket(questID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(questID)
	if prevAct == 0 {
		prevAct = ActionIDQuestList
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDQuestFinish, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// BuildSubmitCompleteQuestPacket 构造交付已完成任务领取奖励请求封包 (Module 3, Action 5)。
func BuildSubmitCompleteQuestPacket(questID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(questID)
	if prevAct == 0 {
		prevAct = ActionIDQuestList
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDQuestSubmitComplete, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// BuildCanReceiveQuestPacket 构造查询指定城镇可接任务列表请求封包 (Module 3, Action 8)。
func BuildCanReceiveQuestPacket(townID int32, prevAct uint32) *Packet {
	w := NewWriter()
	w.WriteInt32(townID)
	if prevAct == 0 {
		prevAct = ActionIDQuestList
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDQuestCanReceive, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// BuildGetPlayerQuestListPacket 构造拉取玩家当前已接任务列表请求封包 (Module 3, Action 9)。
func BuildGetPlayerQuestListPacket(prevAct uint32) *Packet {
	w := NewWriter()
	if prevAct == 0 {
		prevAct = 0x00000002 // ActionIDPlayerGetInfo
	}
	w.WriteUint32(prevAct)
	pkt := NewPacket(ActionIDQuestList, w.Bytes())
	pkt.HasPrevAct = true
	return pkt
}

// PlayerQuestItem 玩家当前已接任务条目。
type PlayerQuestItem struct {
	QuestID      int32
	State        int32 // -1=等级不足, 0=可接取, 1=已接进行中, 2=已完成可交付
	NeedType     uint8 // 0=MONSTER, 1=GETITEM, 2=GETQUESTITEM, 3=SCENEITEM, 4=NOONE
	TargetID     int32 // 首个目标怪物或道具 ID
	CurrentCount int32 // 当前已完成进度
	MaxCount     int32 // 目标需求总数量
}

// ParsePlayerQuestListResponse 解析玩家当前任务列表。支持透明 zlib 解压。
func ParsePlayerQuestListResponse(payload []byte) ([]PlayerQuestItem, error) {
	data := payload
	// 检查是否为 zlib 压缩流
	if len(data) >= 2 && data[0] == 0x78 && (data[1] == 0x9c || data[1] == 0x01 || data[1] == 0xda) {
		zr, err := zlib.NewReader(bytes.NewReader(data))
		if err == nil {
			defer zr.Close()
			decomp, dErr := io.ReadAll(zr)
			if dErr == nil {
				data = decomp
			}
		}
	}

	// 如果解压后的数据带有 Module(2B)+Action(2B) 头，跳过前 4 字节
	if len(data) >= 4 && data[0] == 0x00 && data[1] == 0x03 && data[2] == 0x00 && data[3] == 0x09 {
		data = data[4:]
	}

	if len(data) < 2 {
		return nil, fmt.Errorf("%w: payload too short for player quest list", ErrInvalidPayload)
	}

	r := NewReader(data)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, err
	}

	items := make([]PlayerQuestItem, 0, count)
	for i := uint16(0); i < count; i++ {
		if r.Remaining() < 8 {
			break
		}
		qid, _ := r.ReadInt32()
		state, _ := r.ReadInt32()
		item := PlayerQuestItem{
			QuestID: qid,
			State:   state,
		}
		// 真实网关条目扩展字段: 1B need_type + 2B need_count + need_count * 12B (3 * Int32)
		if r.Remaining() >= 3 {
			needType, _ := r.ReadUint8()
			needCount, _ := r.ReadUint16()
			item.NeedType = needType
			for j := uint16(0); j < needCount; j++ {
				if r.Remaining() < 12 {
					break
				}
				tid, _ := r.ReadInt32()
				cur, _ := r.ReadInt32()
				max, _ := r.ReadInt32()
				if j == 0 {
					item.TargetID = tid
					item.CurrentCount = cur
					item.MaxCount = max
				}
			}
		}
		items = append(items, item)
	}

	return items, nil
}
