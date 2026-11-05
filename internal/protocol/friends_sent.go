// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现官方好友来信协议 (Mod_FriendsSent_Base, Module 684 / 0x2AC)。
// 依据官方 Main.swf 反编译的 com.protocols.Mod_FriendsSent_Base 与
// com.datas.FriendSentData：好友来信按时间/任务解锁，阅读后领取附件奖励。
// 自动化只领取已解锁且未领取的来信奖励。
package protocol

import "fmt"

const (
	// ActionIDFriendsSentPanel 查询好友来信面板 (Module 684, Action 1 -> 0x02AC0001)
	ActionIDFriendsSentPanel uint32 = 0x02AC0001
	// ActionIDFriendsSentOpen 打开指定来信 (Module 684, Action 2 -> 0x02AC0002)
	ActionIDFriendsSentOpen uint32 = 0x02AC0002
	// ActionIDFriendsSentRead 标记来信已读 (Module 684, Action 3 -> 0x02AC0003)
	ActionIDFriendsSentRead uint32 = 0x02AC0003
	// ActionIDFriendsSentAward 领取来信奖励 (Module 684, Action 4 -> 0x02AC0004)
	ActionIDFriendsSentAward uint32 = 0x02AC0004
)

// Mod_FriendsSent_Base 结果码。
const (
	// FriendsSentResultSuccess 成功
	FriendsSentResultSuccess uint8 = 0
	// FriendsSentResultFailed 失败
	FriendsSentResultFailed uint8 = 1
	// FriendsSentResultItemPackFull 背包已满
	FriendsSentResultItemPackFull uint8 = 2
	// FriendsSentResultUnRead 未阅读
	FriendsSentResultUnRead uint8 = 3
)

// FriendsSentLetter 表示一封好友来信。
type FriendsSentLetter struct {
	// ID 来信编号
	ID uint8
	// UnlockTime 解锁时间
	UnlockTime int32
	// UnlockMissionID 解锁所需任务
	UnlockMissionID int32
	// IsTimeLock 时间未到
	IsTimeLock uint8
	// IsMissionLock 任务未完成
	IsMissionLock uint8
}

// FriendsSentOpenResult 表示打开来信后的内容。
type FriendsSentOpenResult struct {
	// IsRead 是否已读
	IsRead uint8
	// IsGetAward 是否已领取奖励
	IsGetAward uint8
	// ContentType 内容类型
	ContentType uint8
	// Awards 附件奖励 [item_id, item_num]
	Awards [][2]int32
	// ID 来信编号
	ID uint8
}

// BuildFriendsSentPanelPacket 构造查询好友来信面板封包 (0x02AC0001)。
func BuildFriendsSentPanelPacket() *Packet {
	return NewPacket(ActionIDFriendsSentPanel, nil)
}

// BuildFriendsSentOpenPacket 构造打开来信封包 (0x02AC0002)。
func BuildFriendsSentOpenPacket(letterID uint8) *Packet {
	return NewPacket(ActionIDFriendsSentOpen, []byte{letterID})
}

// BuildFriendsSentReadPacket 构造标记已读封包 (0x02AC0003)。
func BuildFriendsSentReadPacket(letterID uint8) *Packet {
	return NewPacket(ActionIDFriendsSentRead, []byte{letterID})
}

// BuildFriendsSentAwardPacket 构造领取来信奖励封包 (0x02AC0004)。
func BuildFriendsSentAwardPacket(letterID uint8) *Packet {
	return NewPacket(ActionIDFriendsSentAward, []byte{letterID})
}

// ParseFriendsSentPanelResult 解析好友来信列表。
func ParseFriendsSentPanelResult(payload []byte) ([]FriendsSentLetter, error) {
	r := NewReader(payload)
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read friends sent count: %v", ErrInvalidPayload, err)
	}
	out := make([]FriendsSentLetter, 0, count)
	for i := 0; i < int(count); i++ {
		var l FriendsSentLetter
		if l.ID, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read letter[%d].id: %v", ErrInvalidPayload, i, err)
		}
		if l.UnlockTime, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read letter[%d].unlock_time: %v", ErrInvalidPayload, i, err)
		}
		if l.UnlockMissionID, err = r.ReadInt32(); err != nil {
			return nil, fmt.Errorf("%w: failed to read letter[%d].unlock_mission_id: %v", ErrInvalidPayload, i, err)
		}
		if l.IsTimeLock, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read letter[%d].is_time_lock: %v", ErrInvalidPayload, i, err)
		}
		if l.IsMissionLock, err = r.ReadUint8(); err != nil {
			return nil, fmt.Errorf("%w: failed to read letter[%d].is_mission_lock: %v", ErrInvalidPayload, i, err)
		}
		out = append(out, l)
	}
	return out, nil
}

// ParseFriendsSentOpenResult 解析打开来信回包。
func ParseFriendsSentOpenResult(payload []byte) (*FriendsSentOpenResult, error) {
	r := NewReader(payload)
	res := &FriendsSentOpenResult{}
	var err error
	if res.IsRead, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read letter is_read: %v", ErrInvalidPayload, err)
	}
	if res.IsGetAward, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read letter is_get_award: %v", ErrInvalidPayload, err)
	}
	if res.ContentType, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read letter content_type: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read letter award count: %v", ErrInvalidPayload, err)
	}
	res.Awards = make([][2]int32, 0, count)
	for i := 0; i < int(count); i++ {
		id, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read letter award[%d].item_id: %v", ErrInvalidPayload, i, err)
		}
		num, err := r.ReadInt32()
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read letter award[%d].item_num: %v", ErrInvalidPayload, i, err)
		}
		res.Awards = append(res.Awards, [2]int32{id, num})
	}
	if res.ID, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read letter id: %v", ErrInvalidPayload, err)
	}
	return res, nil
}

// ParseFriendsSentActionResult 解析阅读/领奖结果 (单字节结果码)。
func ParseFriendsSentActionResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty friends sent result", ErrInvalidPayload)
	}
	return payload[0], nil
}

// BuildFriendsSentPanelResultPacket 构造好友来信列表回包，供离线 mock 与单测使用。
func BuildFriendsSentPanelResultPacket(letters []FriendsSentLetter) *Packet {
	w := NewWriter()
	w.WriteUint16(uint16(len(letters)))
	for _, l := range letters {
		w.WriteUint8(l.ID)
		w.WriteInt32(l.UnlockTime)
		w.WriteInt32(l.UnlockMissionID)
		w.WriteUint8(l.IsTimeLock)
		w.WriteUint8(l.IsMissionLock)
	}
	return NewPacket(ActionIDFriendsSentPanel, w.Bytes())
}

// BuildFriendsSentOpenResultPacket 构造打开来信回包，供离线 mock 与单测使用。
func BuildFriendsSentOpenResultPacket(res FriendsSentOpenResult) *Packet {
	w := NewWriter()
	w.WriteUint8(res.IsRead)
	w.WriteUint8(res.IsGetAward)
	w.WriteUint8(res.ContentType)
	w.WriteUint16(uint16(len(res.Awards)))
	for _, a := range res.Awards {
		w.WriteInt32(a[0])
		w.WriteInt32(a[1])
	}
	w.WriteUint8(res.ID)
	return NewPacket(ActionIDFriendsSentOpen, w.Bytes())
}

// BuildFriendsSentActionResultPacket 构造阅读/领奖回包，供离线 mock 与单测使用。
func BuildFriendsSentActionResultPacket(actionID uint32, result uint8) *Packet {
	return NewPacket(actionID, []byte{result})
}
