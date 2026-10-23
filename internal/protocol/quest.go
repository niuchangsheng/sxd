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

	// ActionIDQuestComplete 交付/完成任务领取奖励 (Module 3, Action 2 -> 0x00030002)
	ActionIDQuestComplete uint32 = 0x00030002

	// ActionIDQuestAccept 接受任务 (Module 3, Action 3 -> 0x00030003)
	ActionIDQuestAccept uint32 = 0x00030003

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

// BuildCompleteQuestPacket 构造交付任务请求封包 (Module 3, Action 2)。
func BuildCompleteQuestPacket(req CompleteQuestRequest) (*Packet, error) {
	w := NewWriter()
	w.WriteInt32(req.QuestID)
	prevAct := req.PrevAct
	if prevAct == 0 {
		prevAct = ActionIDQuestList
	}
	w.WriteUint32(prevAct)
	return NewPacket(ActionIDQuestComplete, w.Bytes()), nil
}

// PlayerQuestItem 玩家当前已接任务条目。
type PlayerQuestItem struct {
	QuestID int32
	State   int32 // 0=进行中 (未完成目标), 1=已完成可交付
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
		items = append(items, PlayerQuestItem{
			QuestID: qid,
			State:   state,
		})
		// 真实网关每个条目包含 23 字节 (4B QuestID + 4B State + 15B 扩展元数据)，跳过剩余 15 字节
		if r.Remaining() >= 15 {
			_, _ = r.ReadBytes(15)
		}
	}

	return items, nil
}
