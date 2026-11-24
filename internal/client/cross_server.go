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
	"time"

	"sxd-pie-ng/internal/protocol"
)

// crossServerMaxReconnectAttempts 仙界跨服会话的最大建连尝试次数。
//
// 跨服节点偶发 DNS/网络抖动时不应直接放弃整轮玩法；凭据在登录成功前未消耗，
// 允许一次短间隔重拨即可覆盖绝大多数瞬时失败。认证被拒等业务失败仍会快速终止。
const crossServerMaxReconnectAttempts = 2

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

// EnsureCrossSessionAlive 确保当前主服会话常驻一条仙界跨服辅助连接。
//
// 2026-11-25 抓包与探针实测结论：
//   - 服务端每 5 分钟按墙钟扫描一次（本网关在整点后约 23~29 秒），回收
//     「最近约 120 秒没有真实业务活动」的主服会话；
//   - 官方客户端空闲 10 分钟不在主服上行任何业务包，仅靠常驻的 8219 等
//     辅助连接即可跨过多个扫描点；
//   - 探针 cross 模式（主服零上行 + 常驻一条跨服连接）同样存活 10 分钟，
//     而纯空闲模式在同一扫描点被回收。
//
// 该方法只应由调度器单协程调用（cross_keepalive 例程），避免在主服业务
// 序列中间插入跨服凭据请求导致 PrevAct 尾链错位。
func (s *RoleSession) EnsureCrossSessionAlive(ctx context.Context) error {
	s.crossMu.Lock()
	defer s.crossMu.Unlock()

	if s.crossSession != nil {
		if s.crossSession.State() != StateClosed {
			return nil
		}
		s.crossSession.Close()
		s.crossSession = nil
	}
	if s.State() != StateActive {
		return ErrNotConnected
	}
	// 用主会话生命周期作为父 context：跨服连接必须活过本轮调度执行。
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
	slog.Info("跨服辅助会话已常驻",
		"role_id", s.cfg.RoleID, "cross_role_id", cross.RoleID())
	return nil
}

// KeepaliveBusinessRead 发送一次无害的业务读 (0/6 功能列表) 以刷新主服活动。
//
// 2026-11-25 探针实测：不建立跨服连接，仅每 90 秒发一次 0/6，主服会话
// 可跨过 5 分钟回收扫描点并保持 6.5 分钟在线；而常驻跨服连接若进入仙界
// 城镇，会让主服返回城镇/写请求被静默丢弃。因此会话保活改用主服业务读。
func (s *RoleSession) KeepaliveBusinessRead(ctx context.Context) error {
	if s == nil || s.State() != StateActive {
		return ErrNotConnected
	}
	c, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	resp, err := s.Call(c, protocol.BuildGetPlayerFunctionsPacket(0), protocol.ActionIDPlayerGetFunctions)
	if err != nil {
		return err
	}
	if resp == nil {
		return fmt.Errorf("client: 保活读无回包")
	}
	return nil
}

// HasCrossSession 返回当前是否存在未关闭的常驻跨服会话（诊断用）。
func (s *RoleSession) HasCrossSession() bool {
	s.crossMu.Lock()
	defer s.crossMu.Unlock()
	return s.crossSession != nil && s.crossSession.State() != StateClosed
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
	return s.openCrossServerSession(ctx, cred, true)
}

// openCrossServerKeepaliveSession 建立仅完成登录、不进入仙界城镇的跨服连接。
//
// 常驻辅助连接若进入仙界城镇，会让主服认为角色停留在跨服地图，
// 导致主服 enter_town / 返回城镇被静默丢弃（2026-11-25 实机）。
// 只登录不进城即可维持账号级辅助连接，同时不影响主服上下文。
func (s *RoleSession) openCrossServerKeepaliveSession(ctx context.Context, cred *protocol.StCrossServerInfo) (*RoleSession, error) {
	return s.openCrossServerSession(ctx, cred, false)
}

func (s *RoleSession) openCrossServerSession(ctx context.Context, cred *protocol.StCrossServerInfo, enterTown bool) (*RoleSession, error) {
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
	cfg.MaxReconnectAttempts = crossServerMaxReconnectAttempts
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

		if !enterTown {
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
		"server_id", cred.ServerName,
	)

	cross := NewRoleSession(cfg)
	if err := cross.Start(ctx); err != nil {
		return nil, fmt.Errorf("client: 启动仙界跨服会话失败: %w", err)
	}

	// 等待激活必须有上限，但 Start 使用调用方 ctx（常驻场景下是主会话生命周期），
	// 不能在这里给 ctx 加 deadline，否则跨服连接会在等待结束后被一并取消。
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
