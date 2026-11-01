// Package client 提供神仙道客户端网络会话、跨服登录与业务协议调用能力。
//
// @author Ateng
// @since 2026-11-02
package client

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"sxd-pie-ng/internal/protocol"
)

// FetchCrossServerCredentials 通过主服会话申请仙界跨服节点登录凭据 (Mod 96 Act 0)。
//
// 官方客户端在进入仙界城镇前会先调用 Mod_StcLogin_Base.get_login_info 获取
// 跨服地址、一次性时间戳与验签串，随后在独立连接上执行 Mod_StLogin_Base.login。
func (s *RoleSession) FetchCrossServerCredentials(ctx context.Context) (*protocol.StCrossServerInfo, error) {
	pkt := protocol.NewPacket(protocol.ActionIDStcLoginGetInfo, nil)
	resp, err := s.Call(ctx, pkt, protocol.ActionIDStcLoginGetInfo)
	if err != nil {
		return nil, fmt.Errorf("client: 申请仙界跨服凭据失败: %w", err)
	}
	if resp == nil || len(resp.Payload) == 0 {
		return nil, fmt.Errorf("client: 仙界跨服凭据回包为空")
	}
	info, err := protocol.ParseStCrossServerInfo(resp.Payload)
	if err != nil {
		return nil, fmt.Errorf("client: 解析仙界跨服凭据失败: %w", err)
	}
	if info.Host == "" || info.Port == 0 || info.ServerName == "" || info.PassCode == "" {
		return nil, fmt.Errorf("client: 仙界跨服凭据不完整 (host=%q port=%d server=%q)",
			info.Host, info.Port, info.ServerName)
	}
	return info, nil
}

// OpenCrossServerSession 拉取仙界跨服凭据并建立已进入仙界城镇的独立会话。
//
// 返回的会话仅在调用方使用期间有效，调用方负责在结束后调用 Close。
// 跨服连接不支持主服四步初始化，因此这里使用专用认证器：
// Mod 94 登录握手 → Mod 95 进入仙界城镇 (town_id=41)。
func (s *RoleSession) OpenCrossServerSession(ctx context.Context) (*RoleSession, error) {
	cred, err := s.FetchCrossServerCredentials(ctx)
	if err != nil {
		return nil, err
	}
	return s.OpenCrossServerSessionWithInfo(ctx, cred)
}

// OpenCrossServerSessionWithInfo 使用已获取的仙界跨服凭据建立独立会话，
// 供调用方在同一轮任务中复用凭据、避免重复向主服申请一次性登录串。
func (s *RoleSession) OpenCrossServerSessionWithInfo(ctx context.Context, cred *protocol.StCrossServerInfo) (*RoleSession, error) {
	if cred == nil {
		return nil, fmt.Errorf("client: 仙界跨服凭据为空")
	}

	cfg := s.cfg
	cfg.RoleID = s.cfg.RoleID + "-st"
	cfg.ServerAddr = net.JoinHostPort(cred.Host, strconv.Itoa(int(cred.Port)))
	cfg.ServerID = cred.ServerName
	cfg.Time1 = cred.Time
	cfg.Hash1 = cred.PassCode
	cfg.Hash = ""  // 强制走跨服分支
	cfg.Token = "" // 跨服登录不使用平台票据
	cfg.PlayerID = s.PlayerID()
	cfg.OriginNickName = s.OriginNickName()
	cfg.MaxReconnectAttempts = 1
	cfg.RefreshCredentials = nil

	originNickName := cfg.OriginNickName
	if originNickName == "" {
		originNickName = s.RoleName()
	}
	cfg.Authenticator = func(cs *RoleSession) error {
		aCtx, cancel := context.WithTimeout(cs.ctx, 5*time.Second)
		defer cancel()

		loginPkt, err := protocol.BuildStLoginPacket(protocol.StLoginRequest{
			ServerName:     cred.ServerName,
			PlayerID:       cfg.PlayerID,
			OriginNickName: originNickName,
			Time:           cred.Time,
			PassCode:       cred.PassCode,
		})
		if err != nil {
			return fmt.Errorf("client: 构造仙界跨服登录封包失败: %w", err)
		}
		loginResp, err := cs.Call(aCtx, loginPkt, protocol.ActionIDStLogin)
		if err != nil {
			return fmt.Errorf("client: 仙界跨服登录失败: %w", err)
		}
		loginRes, err := protocol.ParseStLoginResult(loginResp.Payload)
		if err != nil {
			return fmt.Errorf("client: 解析仙界跨服登录回包失败: %w", err)
		}
		if loginRes.Result != 0 {
			return fmt.Errorf("client: 仙界跨服登录被拒绝: result=%d", loginRes.Result)
		}
		cs.UpdatePlayerState(func(ps *PlayerState) {
			if loginRes.PlayerID > 0 {
				ps.PlayerID = loginRes.PlayerID
			}
			ps.OriginNickName = originNickName
		})

		townResp, err := cs.Call(aCtx, protocol.BuildStTownEnterPacket(protocol.TownIDXianJie), protocol.ActionIDStTownEnter)
		if err != nil {
			return fmt.Errorf("client: 进入仙界城镇失败: %w", err)
		}
		if len(townResp.Payload) > 0 && townResp.Payload[0] != protocol.StTownResultSuccess {
			return fmt.Errorf("client: 进入仙界城镇被拒绝: result=%d", townResp.Payload[0])
		}
		return nil
	}

	cross := NewRoleSession(cfg)
	if err := cross.Start(ctx); err != nil {
		return nil, fmt.Errorf("client: 启动仙界跨服会话失败: %w", err)
	}

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		switch cross.State() {
		case StateActive:
			return cross, nil
		case StateClosed:
			cross.Close()
			return nil, fmt.Errorf("client: 仙界跨服会话在激活前关闭")
		}
		select {
		case <-ctx.Done():
			cross.Close()
			return nil, fmt.Errorf("client: 等待仙界跨服会话激活超时: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
