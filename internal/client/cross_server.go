// Package client 提供神仙道客户端网络会话、跨服登录与业务协议调用能力。
//
// @author Ateng
// @since 2026-11-02
package client

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	"sxd-pie-ng/internal/protocol"
)

// crossServerMaxReconnectAttempts 临时仙界跨服城镇会话的最大建连尝试次数。
const crossServerMaxReconnectAttempts = 2

// FetchCrossServerCredentials 通过主服会话申请仙界跨服节点登录凭据 (Mod 96 Act 0)。
//
// 官方客户端在连接仙界节点前会先调用 Mod_StcLogin_Base.get_login_info 获取
// 跨服地址、一次性时间戳与验签串，随后在独立连接上执行 Mod_StLogin_Base.login。
func (s *RoleSession) FetchCrossServerCredentials(ctx context.Context) (*protocol.StCrossServerInfo, error) {
	cCtx, cancel := context.WithTimeout(ctx, s.callTimeout(8*time.Second, 300*time.Millisecond))
	defer cancel()
	pkt := protocol.NewPacket(protocol.ActionIDStcLoginGetInfo, nil)
	resp, err := s.Call(cCtx, pkt, protocol.ActionIDStcLoginGetInfo)
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

// dropStaleCrossSession 在主服会话重新建立新一代连接后清理旧代数的跨服辅助连接。
func (s *RoleSession) dropStaleCrossSession() {
	s.crossMu.Lock()
	var stale *RoleSession
	if s.crossSession != nil && s.crossParentGen != s.ConnectionGeneration() {
		stale = s.crossSession
		s.crossSession = nil
		s.crossParentGen = 0
	}
	s.crossMu.Unlock()
	if stale != nil {
		stale.Close()
	}
}

// suspendCrossSession 在临时进入仙界城镇前挂起常驻辅助连接，避免同一角色双 8219 连接互踢。
func (s *RoleSession) suspendCrossSession() {
	s.crossMu.Lock()
	cs := s.crossSession
	s.crossSession = nil
	s.crossParentGen = 0
	s.crossMu.Unlock()
	if cs != nil {
		cs.Close()
	}
}

// EnsureCrossSessionAlive 确保当前主服会话常驻一条仙界跨服辅助连接 (login-only + enter_st_channel)。
//
// 2026-11-25 抓包 (/tmp/daily.pcapng) 与 A/B 对照探针实测结论：
//   - 服务端每 5 分钟按墙钟扫描回收无跨服辅助连接的主服会话（纯主服 0/6、0/22、0/75
//     均在扫描点收到 EOF）；
//   - 对齐原版 Main.swf (View.as:16639 chatConnect)：登录 8219/8480 并加入仙界频道 (6/12)，
//     不调用 95/0 enter_town，既让主服持续免于 5 分钟扫描回收，又不改变主服城镇场景上下文；
//   - 当临时跨服城镇玩法正在执行 (crossTownActive) 时跳过，玩法退出时自动恢复。
func (s *RoleSession) EnsureCrossSessionAlive(ctx context.Context) error {
	if s == nil || s.State() != StateActive {
		return ErrNotConnected
	}
	if s.crossTownActive.Load() {
		return nil
	}

	s.crossMu.Lock()
	defer s.crossMu.Unlock()

	if s.crossTownActive.Load() {
		return nil
	}
	curGen := s.ConnectionGeneration()
	if s.crossSession != nil {
		if s.crossSession.State() == StateActive && s.crossParentGen == curGen {
			return nil
		}
		old := s.crossSession
		s.crossSession = nil
		s.crossParentGen = 0
		old.Close()
	}
	if s.State() != StateActive {
		return ErrNotConnected
	}

	life := s.ctx
	if life == nil {
		life = ctx
	}
	cred, err := s.FetchCrossServerCredentials(ctx)
	if err != nil {
		return fmt.Errorf("client: 申请常驻跨服凭据失败: %w", err)
	}
	cross, err := s.openCrossServerKeepaliveSession(life, cred)
	if err != nil {
		return fmt.Errorf("client: 建立常驻跨服会话失败: %w", err)
	}
	s.crossSession = cross
	s.crossParentGen = curGen
	slog.Info("跨服辅助会话已常驻",
		"role_id", s.cfg.RoleID, "cross_role_id", cross.RoleID())
	return nil
}

// KeepaliveBusinessRead 发送一次无害的业务读 (0/6 功能列表)。
func (s *RoleSession) KeepaliveBusinessRead(ctx context.Context) error {
	if s == nil || s.State() != StateActive {
		return ErrNotConnected
	}
	c, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	resp, err := s.Call(c, protocol.BuildGetPlayerFunctionsPacket(s.LastBusinessActionID()), protocol.ActionIDPlayerGetFunctions)
	if err != nil {
		return err
	}
	if resp == nil {
		return fmt.Errorf("client: 保活读无回包")
	}
	return nil
}

// HasCrossSession 返回当前是否存在激活中的常驻跨服辅助会话（诊断与单测用）。
func (s *RoleSession) HasCrossSession() bool {
	s.crossMu.Lock()
	defer s.crossMu.Unlock()
	return s.crossSession != nil && s.crossSession.State() != StateClosed
}

// OpenCrossServerSession 拉取仙界跨服凭据并建立已进入仙界城镇的独立会话。
//
// 返回的会话仅在调用方使用期间有效，调用方负责在结束后调用 Close；
// Close 时会自动发送 95/2 leave_town 清除跨服城镇状态，并在后台恢复常驻辅助连接。
func (s *RoleSession) OpenCrossServerSession(ctx context.Context) (*RoleSession, error) {
	cred, err := s.FetchCrossServerCredentials(ctx)
	if err != nil {
		return nil, err
	}
	return s.OpenCrossServerSessionWithInfo(ctx, cred)
}

// OpenCrossServerSessionWithInfo 使用已获取的仙界跨服凭据建立已进城的独立会话。
func (s *RoleSession) OpenCrossServerSessionWithInfo(ctx context.Context, cred *protocol.StCrossServerInfo) (*RoleSession, error) {
	s.crossTownActive.Store(true)
	s.suspendCrossSession()

	cross, err := s.openCrossServerSession(ctx, cred, true)
	if err != nil {
		s.crossTownActive.Store(false)
		s.restoreCrossKeepaliveAsync()
		return nil, err
	}
	cross.onBeforeClose = func() {
		leaveTimeout := 2 * time.Second
		if s.isCustomDialer {
			leaveTimeout = 50 * time.Millisecond
		}
		lCtx, cancel := context.WithTimeout(context.Background(), leaveTimeout)
		defer cancel()
		resp, lErr := cross.Call(lCtx, protocol.BuildStTownLeavePacket(), protocol.ActionIDStTownLeave)
		if lErr == nil && resp != nil {
			if code, pErr := protocol.ParseStTownLeaveResult(resp.Payload); pErr == nil && code != protocol.StTownResultTrue && code != protocol.StTownResultSuccess {
				slog.Debug("离开仙界城镇返回非成功码", "role_id", cross.RoleID(), "code", code)
			}
		}
	}
	cross.onAfterClose = func() {
		s.crossTownActive.Store(false)
		s.restoreCrossKeepaliveAsync()
	}
	return cross, nil
}

func (s *RoleSession) restoreCrossKeepaliveAsync() {
	if s == nil || s.isCustomDialer || s.State() != StateActive || s.ctx == nil {
		return
	}
	parentCtx := s.ctx
	go func() {
		rCtx, cancel := context.WithTimeout(parentCtx, 15*time.Second)
		defer cancel()
		if err := s.EnsureCrossSessionAlive(rCtx); err != nil && parentCtx.Err() == nil {
			slog.Debug("跨服业务结束后恢复常驻辅助连接未成功，交由保活例程重试",
				"role_id", s.RoleID(), "err", err)
		}
	}()
}

// openCrossServerKeepaliveSession 建立仅完成登录与频道加入、不进入仙界城镇的常驻跨服连接。
func (s *RoleSession) openCrossServerKeepaliveSession(ctx context.Context, cred *protocol.StCrossServerInfo) (*RoleSession, error) {
	return s.openCrossServerSession(ctx, cred, false)
}

func (s *RoleSession) openCrossServerSession(ctx context.Context, cred *protocol.StCrossServerInfo, enterTown bool) (*RoleSession, error) {
	if cred == nil {
		return nil, fmt.Errorf("client: 仙界跨服凭据为空")
	}

	var credMu sync.Mutex
	curCred := *cred

	cfg := s.cfg
	cfg.RoleID = s.cfg.RoleID + "-st"
	cfg.ServerAddr = net.JoinHostPort(curCred.Host, strconv.Itoa(int(curCred.Port)))
	cfg.ServerID = curCred.ServerName
	cfg.Time1 = curCred.Time
	cfg.Hash1 = curCred.PassCode
	cfg.Hash = ""  // 强制走跨服分支
	cfg.Token = "" // 跨服登录不使用平台票据
	cfg.PlayerID = s.PlayerID()
	cfg.OriginNickName = s.OriginNickName()
	if enterTown {
		cfg.MaxReconnectAttempts = crossServerMaxReconnectAttempts
		cfg.RefreshCredentials = nil
	} else {
		// 常驻辅助连接随主会话无限自愈：8219 偶发 EOF 后自动向主服换取新一次性 PassCode。
		cfg.MaxReconnectAttempts = 0
		cfg.RefreshCredentials = func(c context.Context, sCfg *SessionConfig) error {
			if s.State() != StateActive || s.crossTownActive.Load() {
				return ErrNotConnected
			}
			fresh, fErr := s.FetchCrossServerCredentials(c)
			if fErr != nil {
				return fErr
			}
			credMu.Lock()
			curCred = *fresh
			credMu.Unlock()
			sCfg.ServerAddr = net.JoinHostPort(fresh.Host, strconv.Itoa(int(fresh.Port)))
			sCfg.ServerID = fresh.ServerName
			sCfg.Time1 = fresh.Time
			sCfg.Hash1 = fresh.PassCode
			return nil
		}
	}

	originNickName := cfg.OriginNickName
	if originNickName == "" {
		originNickName = s.RoleName()
	}
	cfg.Authenticator = func(cs *RoleSession) error {
		aCtx, cancel := context.WithTimeout(cs.ctx, 5*time.Second)
		defer cancel()

		credMu.Lock()
		activeCred := curCred
		credMu.Unlock()

		loginPkt, err := protocol.BuildStLoginPacket(protocol.StLoginRequest{
			ServerName:     activeCred.ServerName,
			PlayerID:       cfg.PlayerID,
			OriginNickName: originNickName,
			Time:           activeCred.Time,
			PassCode:       activeCred.PassCode,
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
			return fmt.Errorf("%w: 仙界跨服登录被拒绝: result=%d", ErrAuthRejected, loginRes.Result)
		}
		cs.markTicketConsumed()
		cs.UpdatePlayerState(func(ps *PlayerState) {
			if loginRes.PlayerID > 0 {
				ps.PlayerID = loginRes.PlayerID
			}
			ps.OriginNickName = originNickName
		})

		if !enterTown {
			// 对齐原版 View.as:16669 chatConnectLoginBack：加入仙界聊天频道使节点维持活跃态。
			if !s.isCustomDialer {
				chCtx, chCancel := context.WithTimeout(cs.ctx, 3*time.Second)
				chResp, chErr := cs.Call(chCtx, protocol.BuildChatEnterStChannelPacket(), protocol.ActionIDChatEnterStChannel)
				chCancel()
				if chErr == nil && chResp != nil {
					if chCode, pErr := protocol.ParseChatEnterStChannelResult(chResp.Payload); pErr == nil && chCode != protocol.ChatChannelResultSuccess {
						slog.Debug("加入仙界频道返回非零状态", "role_id", cs.RoleID(), "code", chCode)
					}
				}
			}
			return nil
		}
		townResp, err := cs.Call(aCtx, protocol.BuildStTownEnterPacket(protocol.TownIDXianJie), protocol.ActionIDStTownEnter)
		if err != nil {
			return fmt.Errorf("client: 进入仙界城镇失败: %w", err)
		}
		if len(townResp.Payload) > 0 && townResp.Payload[0] != protocol.StTownResultSuccess {
			return fmt.Errorf("client: 进入仙界城镇被拒绝: result=%d", townResp.Payload[0])
		}
		return nil
	}

	slog.Info("正在建立仙界跨服会话",
		"role_id", s.cfg.RoleID,
		"cross_role_id", cfg.RoleID,
		"addr", cfg.ServerAddr,
		"server_id", curCred.ServerName,
	)

	cross := NewRoleSession(cfg)
	if err := cross.Start(ctx); err != nil {
		return nil, fmt.Errorf("client: 启动仙界跨服会话失败: %w", err)
	}

	waitCtx, waitCancel := context.WithTimeout(ctx, 30*time.Second)
	defer waitCancel()

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
		case <-waitCtx.Done():
			cross.Close()
			return nil, fmt.Errorf("client: 等待仙界跨服会话激活超时: %w", waitCtx.Err())
		case <-ticker.C:
		}
	}
}
