// Package client 提供神仙道 2026 网关协议客户端、会话状态机与自动化辅助能力。
//
// 本文件实现全网聊天室独立会话建立 (Mod_ServerChatRoom, Module 336)。
package client

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"sxd-pie-ng/internal/protocol"
)

// OpenServerChatRoomSession 使用 336/11 换取的登录信息建立独立聊天服会话。
//
// 聊天服是独立 TCP 节点：先 connect，再以 336/12 登录，成功后即可发 336/20 发言。
// 会话按短连接使用，调用方发言完毕后应立即 Close。
func (s *RoleSession) OpenServerChatRoomSession(
	ctx context.Context,
	info *protocol.ChatRoomLoginInfo,
) (*RoleSession, error) {
	if info == nil {
		return nil, fmt.Errorf("client: 聊天服登录信息为空")
	}
	if info.Host == "" || info.Port <= 0 {
		return nil, fmt.Errorf("client: 聊天服地址无效: %s:%d", info.Host, info.Port)
	}
	if info.LoginCode == "" {
		return nil, fmt.Errorf("client: 聊天服登录码为空")
	}

	cfg := s.snapshotConfig()
	cfg.RoleID = cfg.RoleID + "-chat"
	cfg.ServerAddr = net.JoinHostPort(info.Host, strconv.Itoa(int(info.Port)))
	cfg.ServerID = info.ServerName
	cfg.MaxReconnectAttempts = 1
	cfg.RefreshCredentials = nil
	// 聊天室会话短连接，不需要主服心跳；拉长间隔避免向聊天服发送未知动作。
	cfg.HeartbeatInterval = time.Hour

	playerID := s.PlayerID()
	loginInfo := *info
	cfg.Authenticator = func(cs *RoleSession) error {
		aCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		loginPkt, pErr := protocol.BuildServerChatRoomLoginPacket(&loginInfo, playerID)
		if pErr != nil {
			return pErr
		}
		resp, cErr := cs.Call(aCtx, loginPkt, protocol.ActionIDServerChatRoomLogin)
		if cErr != nil {
			return fmt.Errorf("client: 聊天服登录失败: %w", cErr)
		}
		result, pid, rErr := protocol.ParseServerChatRoomLoginResult(resp.Payload)
		if rErr != nil {
			return fmt.Errorf("client: 解析聊天服登录回包失败: %w", rErr)
		}
		if result != protocol.ServerChatRoomResultSuccess {
			return fmt.Errorf("client: 聊天服登录被拒绝: result=%d", result)
		}
		if pid > 0 {
			cs.UpdatePlayerState(func(ps *PlayerState) { ps.PlayerID = pid })
		}
		return nil
	}

	return s.openChildSession(ctx, ctx, "聊天服", cfg, 0)
}
