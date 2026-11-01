// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// @author Ateng
// @since 2026-10-08
package protocol

import (
	"fmt"
)

// StLoginRequest 代表跨服登录请求参数 (Mod_StLogin_Base.login, ActionID=0x005E0000)。
//
// 字段顺序与编解码类型严格对齐官方客户端 `View.as` 的调用参数
// [serverName, playerId, originNickName, time, passCode]，对应官方
// `com.Protocol.writeArray` 的 [StringUtil, IntUtil, StringUtil, IntUtil, StringUtil]：
// StringUtil 走 `ByteArray.writeUTF`，即 **2 字节长度前缀** + UTF-8 字节。
type StLoginRequest struct {
	// ServerName 跨服目标区服标识符 (Mod_StcLogin_Base.get_login_info 下发的 serverName)，
	// 按 2 字节长度前缀存储 (官方 StringUtil/writeUTF)。
	ServerName string
	// PlayerID 主服角色数字标识 (playerInfo.id)，按 4 字节大端序整型存储。
	PlayerID int32
	// OriginNickName 带区服后缀的官方完整角色名 (如 "曹植始.s1")，按 2 字节长度前缀存储。
	OriginNickName string
	// Time 跨服登录时间戳 (get_login_info 下发的 time)，按 4 字节大端序整型存储。
	Time int32
	// PassCode 跨服登录验签串 (get_login_info 下发的 passCode)，按 2 字节长度前缀存储。
	PassCode string
}

// StLoginResult 代表跨服登录服务器回包结果 (response: [UByte, Int, Int])。
type StLoginResult struct {
	// Result 登录结果状态码 (0=SUCCESS, 1=FAILED)
	Result uint8
	// PlayerID 角色在跨服节点上的数字标识
	PlayerID int32
	// ServerTime 跨服节点当前 Unix 时间戳
	ServerTime int32
}

// BuildStLoginPacket 构造跨服登录请求二进制协议封包。
func BuildStLoginPacket(req StLoginRequest) (*Packet, error) {
	w := NewWriter()
	// 1. 写入 serverName (2 字节长度前缀，官方 StringUtil/writeUTF)
	w.WriteString(req.ServerName)
	// 2. 写入 playerId (4 字节大端序整型)
	w.WriteInt32(req.PlayerID)
	// 3. 写入 originNickName (2 字节长度前缀)
	w.WriteString(req.OriginNickName)
	// 4. 写入 time (4 字节大端序整型)
	w.WriteInt32(req.Time)
	// 5. 写入 passCode (2 字节长度前缀)
	w.WriteString(req.PassCode)

	return NewPacket(ActionIDStLogin, w.Bytes()), nil
}

// ParseStLoginRequest 从封包载荷中反序列化跨服登录请求参数。
func ParseStLoginRequest(payload []byte) (*StLoginRequest, error) {
	r := NewReader(payload)

	serverName, err := r.ReadString()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read server_name: %v", ErrInvalidPayload, err)
	}

	playerID, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read player_id: %v", ErrInvalidPayload, err)
	}

	originNickName, err := r.ReadString()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read origin_nick_name: %v", ErrInvalidPayload, err)
	}

	time1, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read time: %v", ErrInvalidPayload, err)
	}

	passCode, err := r.ReadString()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read pass_code: %v", ErrInvalidPayload, err)
	}

	return &StLoginRequest{
		ServerName:     serverName,
		PlayerID:       playerID,
		OriginNickName: originNickName,
		Time:           time1,
		PassCode:       passCode,
	}, nil
}

// BuildStLoginResultPacket 构造跨服登录响应二进制封包。
func BuildStLoginResultPacket(res StLoginResult) (*Packet, error) {
	w := NewWriter()
	w.WriteUint8(res.Result)
	w.WriteInt32(res.PlayerID)
	w.WriteInt32(res.ServerTime)
	return NewPacket(ActionIDStLogin, w.Bytes()), nil
}

// ParseStLoginResult 从响应载荷中解析登录结果。
func ParseStLoginResult(payload []byte) (*StLoginResult, error) {
	r := NewReader(payload)

	resCode, err := r.ReadUint8()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read result code: %v", ErrInvalidPayload, err)
	}

	playerID, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read player_id: %v", ErrInvalidPayload, err)
	}

	serverTime, err := r.ReadInt32()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read server_time: %v", ErrInvalidPayload, err)
	}

	return &StLoginResult{
		Result:     resCode,
		PlayerID:   playerID,
		ServerTime: serverTime,
	}, nil
}
