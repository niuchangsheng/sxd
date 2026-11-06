// Package client 提供神仙道客户端网络会话、跨服登录与业务协议调用能力。
//
// 本文件实现圣域跨服连接：主服下发临时凭据后建立独立 TCP 长连接并登录，
// 供圣域修炼房、圣域取经等圣域专属玩法使用。
package client

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"time"

	"sxd-pie-ng/internal/protocol"
)

// FetchSaintAreaCredentials 通过主服会话申请圣域连接凭据 (Mod 293 Act 1)。
func (s *RoleSession) FetchSaintAreaCredentials(ctx context.Context) (*protocol.SaintAreaCredentials, error) {
	pkt := protocol.BuildSaintAreaLoginInfoPacket()
	resp, err := s.Call(ctx, pkt, protocol.ActionIDSaintAreaLoginInfo)
	if err != nil {
		return nil, fmt.Errorf("client: 申请圣域连接凭据失败: %w", err)
	}
	if resp == nil || len(resp.Payload) == 0 {
		return nil, fmt.Errorf("client: 圣域连接凭据回包为空")
	}
	cred, err := protocol.ParseSaintAreaCredentials(resp.Payload)
	if err != nil {
		return nil, fmt.Errorf("client: 解析圣域连接凭据失败: %w", err)
	}
	if cred.Host == "" || cred.Port == 0 || cred.ServerName == "" || cred.PassCode == "" {
		return nil, fmt.Errorf("client: 圣域连接凭据不完整 (host=%q port=%d server=%q)",
			cred.Host, cred.Port, cred.ServerName)
	}
	return cred, nil
}

// OpenSaintAreaSession 拉取圣域凭据并建立已登录的独立会话。
//
// 返回的会话仅在调用方使用期间有效，调用方负责在结束后调用 Close。
func (s *RoleSession) OpenSaintAreaSession(ctx context.Context) (*RoleSession, error) {
	cred, err := s.FetchSaintAreaCredentials(ctx)
	if err != nil {
		return nil, err
	}
	return s.OpenSaintAreaSessionWithInfo(ctx, cred)
}

// OpenSaintAreaSessionWithInfo 使用已获取的圣域凭据建立独立会话。
func (s *RoleSession) OpenSaintAreaSessionWithInfo(ctx context.Context, cred *protocol.SaintAreaCredentials) (*RoleSession, error) {
	if cred == nil {
		return nil, fmt.Errorf("client: 圣域连接凭据为空")
	}

	cfg := s.cfg
	cfg.RoleID = s.cfg.RoleID + "-sa"
	cfg.ServerAddr = net.JoinHostPort(cred.Host, strconv.Itoa(int(cred.Port)))
	cfg.ServerID = cred.ServerName
	cfg.Time1 = cred.Time
	cfg.Hash1 = cred.PassCode
	cfg.Hash = ""
	cfg.Token = ""
	cfg.PlayerID = s.PlayerID()
	originNickName := s.OriginNickName()
	if originNickName == "" {
		originNickName = s.RoleName()
	}
	cfg.OriginNickName = originNickName
	cfg.MaxReconnectAttempts = 2
	cfg.RefreshCredentials = nil

	cfg.Authenticator = func(sa *RoleSession) error {
		aCtx, cancel := context.WithTimeout(sa.ctx, 8*time.Second)
		defer cancel()
		loginPkt := protocol.BuildSaintAreaLoginPacket(cred.ServerName, cfg.PlayerID, originNickName, cred.Time, cred.PassCode)
		loginResp, err := sa.Call(aCtx, loginPkt, protocol.ActionIDSaintAreaLogin)
		if err != nil {
			return fmt.Errorf("client: 圣域登录失败: %w", err)
		}
		result, playerID, _, err := protocol.ParseSaintAreaLoginResult(loginResp.Payload)
		if err != nil {
			return fmt.Errorf("client: 解析圣域登录回包失败: %w", err)
		}
		if result != protocol.SaintAreaResultSuccess {
			return fmt.Errorf("client: 圣域登录被拒绝: result=%d", result)
		}
		sa.UpdatePlayerState(func(ps *PlayerState) {
			if playerID > 0 {
				ps.PlayerID = playerID
			}
			ps.OriginNickName = originNickName
		})
		return nil
	}

	slog.Info("正在建立圣域会话",
		"role_id", s.cfg.RoleID,
		"saint_role_id", cfg.RoleID,
		"addr", cfg.ServerAddr,
		"server_id", cred.ServerName,
	)

	sa := NewRoleSession(cfg)
	if err := sa.Start(ctx); err != nil {
		return nil, fmt.Errorf("client: 启动圣域会话失败: %w", err)
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		switch sa.State() {
		case StateActive:
			return sa, nil
		case StateClosed:
			sa.Close()
			return nil, fmt.Errorf("client: 圣域会话在激活前关闭")
		}
		select {
		case <-ctx.Done():
			sa.Close()
			return nil, fmt.Errorf("client: 等待圣域会话激活超时: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
