// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现全网聊天室 (Mod_ServerChatRoom_Base, Module 336) 的协议编解码：
//   - 336/0  get_recent_room_list      最近聊天室房间列表
//   - 336/1  get_chat_room_status      房间人数与状态
//   - 336/11 get_chat_room_logincode   换取聊天服地址与登录码
//   - 336/12 login_chat_room           登录独立聊天服
//   - 336/20 chat_with_players         在聊天室发言/发表情
//
// 字段序于 2026-11-15 按线上回包实测校正，探针见
// test/probes/probe_server_chat_room_live.go。
package protocol

import "fmt"

const (
	// ActionIDServerChatRoomRecentRooms 查询最近聊天室列表 (Module 336, Action 0 -> 0x01500000)
	ActionIDServerChatRoomRecentRooms uint32 = 0x01500000
	// ActionIDServerChatRoomStatus 查询聊天室人数与开放状态 (Module 336, Action 1 -> 0x01500001)
	ActionIDServerChatRoomStatus uint32 = 0x01500001
	// ActionIDServerChatRoomLoginCode 换取聊天服地址与登录码 (Module 336, Action 11 -> 0x0150000B)
	ActionIDServerChatRoomLoginCode uint32 = 0x0150000B
	// ActionIDServerChatRoomLogin 登录独立聊天服 (Module 336, Action 12 -> 0x0150000C)
	ActionIDServerChatRoomLogin uint32 = 0x0150000C
	// ActionIDServerChatRoomSpeak 聊天室发言/发表情 (Module 336, Action 20 -> 0x01500014)
	ActionIDServerChatRoomSpeak uint32 = 0x01500014

	// ServerChatRoomResultSuccess 聊天室操作成功 (SUCCESS=9)。
	ServerChatRoomResultSuccess uint8 = 9
	// ServerChatRoomResultFailed 聊天室操作失败 (FAILED=15)。
	ServerChatRoomResultFailed uint8 = 15
	// ServerChatRoomResultTimeDiff 发言间隔未到 (TIME_DIFF=25)。
	ServerChatRoomResultTimeDiff uint8 = 25
	// ServerChatRoomResultDisable 聊天室被禁言 (DISABLE=27)。
	ServerChatRoomResultDisable uint8 = 27
	// ServerChatRoomResultForbid 发言被禁止 (FORBID=28)。
	ServerChatRoomResultForbid uint8 = 28
)

// ChatRoomInfo 表示最近聊天室房间摘要。
type ChatRoomInfo struct {
	// RoomID 房间编号 (336/11 请求参数)。
	RoomID int8
	// PlayerCount 房间人数 (336/1)。
	PlayerCount int32
	// Status 房间状态 (11=FREE 可进入)。
	Status uint8
}

// ChatRoomLoginInfo 表示换取到的聊天服连接信息。
//
// 实测字段: [UByte flag, String node, String servername, String stagename,
// Int timestamp, String login_code, String host, Int port]。
type ChatRoomLoginInfo struct {
	// Flag 服务端返回的首字节标志 (实测值为 1，客户端不据此判定成功)。
	Flag uint8
	// Node 聊天服节点标识，形如 "s0@9x050.sxdweb.xd.com"。
	Node string
	// ServerName 区服名，如 "s0"。
	ServerName string
	// StageName 渠道/阶段名，如 "verycd"。
	StageName string
	// Timestamp 登录时间戳。
	Timestamp int32
	// LoginCode 一次性登录码。
	LoginCode string
	// Host 聊天服主机。
	Host string
	// Port 聊天服端口。
	Port int32
}

// BuildServerChatRoomRecentRoomsPacket 构造最近聊天室列表请求 (空载荷)。
func BuildServerChatRoomRecentRoomsPacket() *Packet {
	return NewPacket(ActionIDServerChatRoomRecentRooms, nil)
}

// ParseServerChatRoomRecentRoomsResult 解析 336/0 回包:
// [UByte result, [Byte room_id]]，数组长度前缀为 Uint16。
func ParseServerChatRoomRecentRoomsResult(payload []byte) (uint8, []int8, error) {
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return 0, nil, fmt.Errorf("%w: failed to read chat room result: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return 0, nil, fmt.Errorf("%w: failed to read chat room count: %v", ErrInvalidPayload, err)
	}
	rooms := make([]int8, 0, count)
	for i := 0; i < int(count); i++ {
		id, rErr := r.ReadInt8()
		if rErr != nil {
			return 0, nil, fmt.Errorf("%w: failed to read chat room id: %v", ErrInvalidPayload, rErr)
		}
		rooms = append(rooms, id)
	}
	return result, rooms, nil
}

// BuildServerChatRoomStatusPacket 构造聊天室状态请求 (空载荷)。
func BuildServerChatRoomStatusPacket() *Packet {
	return NewPacket(ActionIDServerChatRoomStatus, nil)
}

// ParseServerChatRoomStatusResult 解析 336/1 回包:
// [UByte result, [Byte room_id, Int player_count, UByte status]]。
func ParseServerChatRoomStatusResult(payload []byte) ([]ChatRoomInfo, error) {
	r := NewReader(payload)
	if _, err := r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read chat room status result: %v", ErrInvalidPayload, err)
	}
	count, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read chat room status count: %v", ErrInvalidPayload, err)
	}
	rooms := make([]ChatRoomInfo, 0, count)
	for i := 0; i < int(count); i++ {
		id, rErr := r.ReadInt8()
		if rErr != nil {
			return nil, fmt.Errorf("%w: failed to read chat room id: %v", ErrInvalidPayload, rErr)
		}
		count, cErr := r.ReadInt32()
		if cErr != nil {
			return nil, fmt.Errorf("%w: failed to read chat room players: %v", ErrInvalidPayload, cErr)
		}
		status, sErr := r.ReadUint8()
		if sErr != nil {
			return nil, fmt.Errorf("%w: failed to read chat room state: %v", ErrInvalidPayload, sErr)
		}
		rooms = append(rooms, ChatRoomInfo{RoomID: id, PlayerCount: count, Status: status})
	}
	return rooms, nil
}

// BuildServerChatRoomLoginCodePacket 构造换取聊天服登录信息请求 (336/11)。
func BuildServerChatRoomLoginCodePacket(roomID int8) *Packet {
	w := NewWriter()
	w.WriteInt8(roomID)
	return NewPacket(ActionIDServerChatRoomLoginCode, w.Bytes())
}

// ParseServerChatRoomLoginCodeResult 解析 336/11 回包 (字段序见 ChatRoomLoginInfo)。
func ParseServerChatRoomLoginCodeResult(payload []byte) (*ChatRoomLoginInfo, error) {
	r := NewReader(payload)
	var info ChatRoomLoginInfo
	var err error
	if info.Flag, err = r.ReadUint8(); err != nil {
		return nil, fmt.Errorf("%w: failed to read chat login flag: %v", ErrInvalidPayload, err)
	}
	if info.Node, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: failed to read chat login node: %v", ErrInvalidPayload, err)
	}
	if info.ServerName, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: failed to read chat login server: %v", ErrInvalidPayload, err)
	}
	if info.StageName, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: failed to read chat login stage: %v", ErrInvalidPayload, err)
	}
	if info.Timestamp, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read chat login timestamp: %v", ErrInvalidPayload, err)
	}
	if info.LoginCode, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: failed to read chat login code: %v", ErrInvalidPayload, err)
	}
	if info.Host, err = r.ReadString(); err != nil {
		return nil, fmt.Errorf("%w: failed to read chat login host: %v", ErrInvalidPayload, err)
	}
	if info.Port, err = r.ReadInt32(); err != nil {
		return nil, fmt.Errorf("%w: failed to read chat login port: %v", ErrInvalidPayload, err)
	}
	return &info, nil
}

// BuildServerChatRoomLoginPacket 构造聊天服登录请求 (336/12)。
//
// 官方请求: [String node, Int player_id, String server_name, String stage_name,
// Int timestamp, String login_code]。
func BuildServerChatRoomLoginPacket(info *ChatRoomLoginInfo, playerID int32) (*Packet, error) {
	if info == nil {
		return nil, fmt.Errorf("protocol: chat room login info is nil")
	}
	if info.Node == "" || info.LoginCode == "" {
		return nil, fmt.Errorf("protocol: incomplete chat room login info")
	}
	w := NewWriter()
	w.WriteString(info.Node)
	w.WriteInt32(playerID)
	w.WriteString(info.ServerName)
	w.WriteString(info.StageName)
	w.WriteInt32(info.Timestamp)
	w.WriteString(info.LoginCode)
	return NewPacket(ActionIDServerChatRoomLogin, w.Bytes()), nil
}

// ParseServerChatRoomLoginResult 解析 336/12 回包: [UByte result, Int player_id]。
func ParseServerChatRoomLoginResult(payload []byte) (uint8, int32, error) {
	r := NewReader(payload)
	result, err := r.ReadUint8()
	if err != nil {
		return 0, 0, fmt.Errorf("%w: failed to read chat login result: %v", ErrInvalidPayload, err)
	}
	playerID, _ := r.ReadInt32()
	return result, playerID, nil
}

// BuildServerChatRoomSpeakPacket 构造聊天室发言请求 (336/20)。
//
// 发表情时 message 可为空，eip_num/eip_index 指定表情资源编号
// (客户端以 eip_index 渲染 eip<index> 图标)。
func BuildServerChatRoomSpeakPacket(message, eipNum, eipIndex string) *Packet {
	w := NewWriter()
	w.WriteString(message)
	w.WriteString(eipNum)
	w.WriteString(eipIndex)
	return NewPacket(ActionIDServerChatRoomSpeak, w.Bytes())
}

// ParseServerChatRoomSpeakResult 解析 336/20 回包: [Int time_diff, UByte result]。
func ParseServerChatRoomSpeakResult(payload []byte) (uint8, int32, error) {
	r := NewReader(payload)
	timeDiff, err := r.ReadInt32()
	if err != nil {
		return 0, 0, fmt.Errorf("%w: failed to read chat speak time diff: %v", ErrInvalidPayload, err)
	}
	result, err := r.ReadUint8()
	if err != nil {
		return 0, 0, fmt.Errorf("%w: failed to read chat speak result: %v", ErrInvalidPayload, err)
	}
	return result, timeDiff, nil
}

// ServerChatRoomResultText 返回聊天室结果码中文说明。
func ServerChatRoomResultText(result uint8) string {
	switch result {
	case ServerChatRoomResultSuccess:
		return "成功"
	case ServerChatRoomResultFailed:
		return "失败"
	case ServerChatRoomResultTimeDiff:
		return "发言间隔未到"
	case ServerChatRoomResultDisable:
		return "已被禁言"
	case ServerChatRoomResultForbid:
		return "禁止发言"
	default:
		return fmt.Sprintf("未知结果码 %d", result)
	}
}
