// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// 本文件实现仙界跨服三段式登录链路中的凭据下发与城镇进入协议
// (Mod_StcLogin_Base / Mod_StTown_Base)，字段布局对齐官方 AS3 客户端。
//
// @author Ateng
// @since 2026-11-02
package protocol

import (
	"fmt"
)

const (
	// ActionIDStcLoginGetInfo 申请仙界跨服节点登录凭据 (Mod_StcLogin_Base.get_login_info, Module 96 Action 0 -> 0x00600000)
	ActionIDStcLoginGetInfo uint32 = 0x00600000
	// ActionIDStTownEnter 进入仙界跨服城镇 (Mod_StTown_Base.enter_town, Module 95 Action 0 -> 0x005F0000)
	ActionIDStTownEnter uint32 = 0x005F0000
	// ActionIDStTownLeave 离开仙界跨服城镇 (Mod_StTown_Base.leave_town, Module 95 Action 2 -> 0x005F0002)
	ActionIDStTownLeave uint32 = 0x005F0002
	// ActionIDChatEnterStChannel 进入仙界跨服聊天频道 (Mod_Chat_Base.enter_st_channel, Module 6 Action 12 -> 0x0006000C)
	ActionIDChatEnterStChannel uint32 = 0x0006000C
	// TownIDXianJie 仙界城镇编号 (官方 TownType.getId(TownType.XianJie))
	TownIDXianJie int32 = 41

	// StTownResultSuccess 进入跨服城镇成功状态码 (Mod_StTown_Base.SUCCESS = 2)。
	//
	// 注意该模块常量并非从 0 开始：TRUE=0、FALSE=1、SUCCESS=2、FAILED=3、FULL=4、
	// NOT_ENOUGH_INGOT=5。实机回包首字节为 0x02 即成功。
	StTownResultSuccess uint8 = 2
	// StTownResultTrue 跨服城镇布尔成功值 (Mod_StTown_Base.TRUE = 0，leave_town 等使用)。
	StTownResultTrue uint8 = 0
	// StTownResultFailed 跨服城镇失败状态码 (Mod_StTown_Base.FAILED = 3)。
	StTownResultFailed uint8 = 3
	// ChatChannelResultSuccess 进入跨服聊天频道成功值 (Mod_Chat_Base.SUCCESS = 0)。
	ChatChannelResultSuccess uint8 = 0
)

// StCrossServerInfo 代表主服下发的仙界跨服节点登录凭据。
//
// 线格式 (Mod_StcLogin_Base.get_login_info response):
//
//	String serverHost, Short port, String serverName, Int time, String passCode, String serverTownName
type StCrossServerInfo struct {
	// Host 跨服节点 TCP 地址
	Host string
	// Port 跨服节点 TCP 端口
	Port uint16
	// ServerName 跨服目标区服标识符 (StLogin 第 1 字段)
	ServerName string
	// Time 登录时间戳 (StLogin 第 4 字段)
	Time int32
	// PassCode 登录验签串 (StLogin 第 5 字段)
	PassCode string
	// TownSign 跨服城镇场景标识 (如 "XianJie")
	TownSign string
}

// ParseStCrossServerInfo 从 get_login_info 回包载荷中解析仙界跨服节点凭据。
func ParseStCrossServerInfo(payload []byte) (*StCrossServerInfo, error) {
	r := NewReader(payload)

	host, err := r.ReadString()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read cross host: %v", ErrInvalidPayload, err)
	}
	port, err := r.ReadUint16()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read cross port: %v", ErrInvalidPayload, err)
	}
	serverName, err := r.ReadString()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read cross server_name: %v", ErrInvalidPayload, err)
	}
	time1, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read cross time: %v", ErrInvalidPayload, err)
	}
	passCode, err := r.ReadString()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read cross pass_code: %v", ErrInvalidPayload, err)
	}
	townSign, err := r.ReadString()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read cross town_sign: %v", ErrInvalidPayload, err)
	}

	return &StCrossServerInfo{
		Host:       host,
		Port:       port,
		ServerName: serverName,
		Time:       time1,
		PassCode:   passCode,
		TownSign:   townSign,
	}, nil
}

// BuildStTownEnterPacket 构造进入仙界跨服城镇请求封包 (0x005F0000)。
func BuildStTownEnterPacket(townID int32) *Packet {
	w := NewWriter()
	w.WriteInt32(townID)
	return NewPacket(ActionIDStTownEnter, w.Bytes())
}

// BuildStTownLeavePacket 构造离开仙界跨服城镇请求封包 (0x005F0002)。
// 对齐官方 ServerTownMapView.as:614，请求为空参数并由跨服 Socket 追加前序动作号。
func BuildStTownLeavePacket() *Packet {
	return NewPacketWithAutoPrevAct(ActionIDStTownLeave, nil)
}

// ParseStTownLeaveResult 解析离开仙界跨服城镇响应首字节结果码。
func ParseStTownLeaveResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty st town leave response", ErrInvalidPayload)
	}
	return payload[0], nil
}

// BuildChatEnterStChannelPacket 构造进入仙界聊天频道请求封包 (0x0006000C)。
// 对齐官方 View.as:16669 chatConnectLoginBack，请求为空参数并携带前序动作号。
func BuildChatEnterStChannelPacket() *Packet {
	return NewPacketWithAutoPrevAct(ActionIDChatEnterStChannel, nil)
}

// ParseChatEnterStChannelResult 解析进入仙界聊天频道回包结果码。
func ParseChatEnterStChannelResult(payload []byte) (uint8, error) {
	if len(payload) == 0 {
		return 0, fmt.Errorf("%w: empty enter_st_channel response", ErrInvalidPayload)
	}
	return payload[0], nil
}
