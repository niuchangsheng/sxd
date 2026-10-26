// Package client 提供特定区服角色的独立 TCP Socket 长连接会话、登录态维护与心跳保活状态机。
//
// @author Ateng
// @since 2026-10-08
package client

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"sxd-pie-ng/internal/protocol"
)

var (
	// ErrSessionClosed 会话已主动关闭
	ErrSessionClosed = errors.New("client: session is closed")
	// ErrNotConnected 当前未处于已连接且激活状态
	ErrNotConnected = errors.New("client: session is not active")
)

// AuthenticatorFunc 角色登录认证握手函数
type AuthenticatorFunc func(session *RoleSession) error

// SessionConfig 角色会话配置项
type SessionConfig struct {
	// Username 平台登录账号
	Username string
	// RoleID 角色唯一标识
	RoleID string
	// RoleName 角色显示名称
	RoleName string
	// ServerAddr 游戏区服网关 TCP 地址 (例如: 127.0.0.1:843)
	ServerAddr string
	// Token 平台认证授权票据
	Token string

	// ServerID 游戏区服标识符 (如 "fengwanyx_s813")
	ServerID string
	// Platform 所属运营平台标识 (如 "fengwan")
	Platform string
	// Code 游戏授权码
	Code string
	// Time 本服登录时间戳 (Mod_Player_Base 0x0000)
	Time int32
	// Hash 本服验签散列 (Mod_Player_Base 0x0000)
	Hash string
	// Time1 跨服登录时间戳 (Mod_StLogin_Base 0x005E)
	Time1 int32
	// Hash1 跨服验签散列 (Mod_StLogin_Base 0x005E)
	Hash1 string

	// Extra 平台特定的扩展入场凭据 (例如官方 10 字段所需: regdate, id_card, open_time, is_newst 等)
	Extra map[string]string

	// HeartbeatInterval 心跳定时保活间隔 (默认 1.5s，抓包实测网关超时为 5s)
	HeartbeatInterval time.Duration
	// HeartbeatTimeout 心跳超时时间 (默认 5s)
	HeartbeatTimeout time.Duration
	// HeartbeatActionID 心跳消息号 (默认 0x00000017)
	HeartbeatActionID uint32

	// ReconnectInterval 重连回退重试时间间隔 (默认 2s)
	ReconnectInterval time.Duration
	// MaxReconnectAttempts 最大重连尝试次数 (0 为无限重连)
	MaxReconnectAttempts int

	// Dialer 自定义网络拨号器接口（用于单元测试 net.Pipe 注入）
	Dialer func(ctx context.Context, network, addr string) (net.Conn, error)
	// Authenticator 自定义登录握手逻辑
	Authenticator AuthenticatorFunc
	// RefreshCredentials 凭据自愈刷新回调：在网络闪断或会话重连前自动换取最新一次性 Hash 凭据
	RefreshCredentials func(ctx context.Context, cfg *SessionConfig) error
}

// PlayerState 纳管角色在游戏世界中的动态属性与资源状态。
type PlayerState struct {
	Level       int   `json:"level"`
	VIP         int   `json:"vip"`
	Stamina     int   `json:"stamina"`
	MaxStamina  int   `json:"max_stamina"`
	ExtraStamina int  `json:"extra_stamina"`
	Coins       int64 `json:"coins"`
	Ingots      int64 `json:"ingots"`
	StateSource int64 `json:"state_source"`
	BagCapacity int   `json:"bag_capacity"`
	TownID      int   `json:"town_id"`
	Quests      map[uint32]uint32 `json:"quests"`
}

// RoleSession 代表与游戏服务器建立的长连接角色会话。
type RoleSession struct {
	cfg SessionConfig

	state   SessionState
	stateMu sync.RWMutex

	playerState PlayerState
	playerMu    sync.RWMutex

	conn    net.Conn
	writeMu sync.Mutex

	handlers     map[uint32][]func(*protocol.Packet)
	pendingCalls map[uint32][]chan *protocol.Packet
	handlerMu    sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	reconnectAttempts int
	isCustomDialer    bool

	lastActionID atomic.Uint32
}

// NewRoleSession 构造一个新的角色会话实例。
func NewRoleSession(cfg SessionConfig) *RoleSession {
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 1500 * time.Millisecond
	}
	if cfg.HeartbeatTimeout <= 0 {
		cfg.HeartbeatTimeout = 5 * time.Second
	}
	if cfg.HeartbeatActionID == 0 {
		cfg.HeartbeatActionID = protocol.ActionHeartbeat
	}
	if cfg.ReconnectInterval <= 0 {
		cfg.ReconnectInterval = 2 * time.Second
	}
	isCustom := cfg.Dialer != nil
	if cfg.Authenticator == nil {
		if !isCustom || (cfg.ServerAddr != "mock" && cfg.ServerAddr != "mock-pipe") {
			cfg.Authenticator = DefaultGatewayAuthenticator
		}
	}
	if cfg.Dialer == nil {
		if cfg.ServerAddr == "" || cfg.ServerAddr == "mock" || cfg.ServerAddr == "dry-run" || cfg.ServerAddr == "sandbox" {
			cfg.Dialer = func(ctx context.Context, network, addr string) (net.Conn, error) {
				c1, c2 := net.Pipe()
				go runMockGameServer(c2)
				return c1, nil
			}
		} else {
			cfg.Dialer = func(ctx context.Context, network, addr string) (net.Conn, error) {
				d := net.Dialer{Timeout: 5 * time.Second}
				return d.DialContext(ctx, network, addr)
			}
		}
	}

	s := &RoleSession{
		cfg:            cfg,
		isCustomDialer: isCustom,
		state:          StateDisconnected,
		handlers:       make(map[uint32][]func(*protocol.Packet)),
		pendingCalls:   make(map[uint32][]chan *protocol.Packet),
		playerState: PlayerState{
			Level:       300,
			Stamina:     201,
			MaxStamina:  300,
			Coins:       36226117234,
			Ingots:      39409,
			BagCapacity: 20,
			Quests:      make(map[uint32]uint32),
		},
	}

	// 注册任务列表下发协议监听器 (Module 3, Action 9)
	s.RegisterHandler(protocol.ActionIDQuestList, func(pkt *protocol.Packet) {
		if pkt == nil {
			return
		}
		items, err := protocol.ParsePlayerQuestListResponse(pkt.Payload)
		if err == nil && len(items) > 0 {
			s.playerMu.Lock()
			if s.playerState.Quests == nil {
				s.playerState.Quests = make(map[uint32]uint32)
			}
			for _, it := range items {
				s.playerState.Quests[uint32(it.QuestID)] = uint32(it.State)
			}
			s.playerMu.Unlock()
			slog.Info("收到玩家全量任务列表同步", "role_id", s.cfg.RoleID, "total_quests", len(items))
		}
	})

	return s
}

// RoleID 获取会话绑定的角色唯一标识符。
func (s *RoleSession) RoleID() string {
	return s.cfg.RoleID
}

// RoleName 获取会话绑定的角色显示名称。
func (s *RoleSession) RoleName() string {
	return s.cfg.RoleName
}

// GetPlayerState 获取角色当前快照状态副本。
func (s *RoleSession) GetPlayerState() PlayerState {
	s.playerMu.RLock()
	defer s.playerMu.RUnlock()
	state := s.playerState
	if state.Quests != nil {
		qCopy := make(map[uint32]uint32, len(state.Quests))
		for k, v := range state.Quests {
			qCopy[k] = v
		}
		state.Quests = qCopy
	}
	return state
}

// GetQuests 获取当前角色身上已接任务的快照 (QuestID -> State: 0-进行中, 1-可交)。
func (s *RoleSession) GetQuests() map[uint32]uint32 {
	s.playerMu.RLock()
	defer s.playerMu.RUnlock()
	res := make(map[uint32]uint32, len(s.playerState.Quests))
	for k, v := range s.playerState.Quests {
		res[k] = v
	}
	return res
}

// SetQuestState 更新指定任务的状态。
func (s *RoleSession) SetQuestState(questID uint32, state uint32) {
	s.playerMu.Lock()
	defer s.playerMu.Unlock()
	if s.playerState.Quests == nil {
		s.playerState.Quests = make(map[uint32]uint32)
	}
	s.playerState.Quests[questID] = state
}

// RemoveQuest 从身上移除已完成或放弃的任务。
func (s *RoleSession) RemoveQuest(questID uint32) {
	s.playerMu.Lock()
	defer s.playerMu.Unlock()
	delete(s.playerState.Quests, questID)
}

// GetStamina 获取当前总剩余体力 (基础体力 + 存储/额外体力)。
func (s *RoleSession) GetStamina() int {
	s.playerMu.RLock()
	defer s.playerMu.RUnlock()
	return s.playerState.Stamina + s.playerState.ExtraStamina
}

// GetBaseStamina 获取基础体力池数值 (基准上限 300)。
func (s *RoleSession) GetBaseStamina() int {
	s.playerMu.RLock()
	defer s.playerMu.RUnlock()
	return s.playerState.Stamina
}

// GetExtraStamina 获取存储/额外体力池数值。
func (s *RoleSession) GetExtraStamina() int {
	s.playerMu.RLock()
	defer s.playerMu.RUnlock()
	return s.playerState.ExtraStamina
}

// SetStamina 设置角色当前基础体力。
func (s *RoleSession) SetStamina(val int) {
	s.playerMu.Lock()
	defer s.playerMu.Unlock()
	s.playerState.Stamina = val
}

// SetExtraStamina 设置角色当前存储/额外体力。
func (s *RoleSession) SetExtraStamina(val int) {
	s.playerMu.Lock()
	defer s.playerMu.Unlock()
	s.playerState.ExtraStamina = val
}

// ConsumeStamina 扣除角色体力并返回扣除后的总剩余体力。
// 服务端扫荡遵循优先扣除存储/额外体力池，耗尽后再扣除基础体力池的业务机制。
func (s *RoleSession) ConsumeStamina(amount int) (int, error) {
	s.playerMu.Lock()
	defer s.playerMu.Unlock()
	total := s.playerState.Stamina + s.playerState.ExtraStamina
	if total < amount {
		return total, errors.New("client: stamina not enough")
	}
	if s.playerState.ExtraStamina >= amount {
		s.playerState.ExtraStamina -= amount
	} else {
		rem := amount - s.playerState.ExtraStamina
		s.playerState.ExtraStamina = 0
		s.playerState.Stamina -= rem
	}
	return s.playerState.Stamina + s.playerState.ExtraStamina, nil
}

// AddStamina 增加角色基础体力并返回增加后的总剩余体力。
func (s *RoleSession) AddStamina(amount int) int {
	s.playerMu.Lock()
	defer s.playerMu.Unlock()
	s.playerState.Stamina += amount
	return s.playerState.Stamina + s.playerState.ExtraStamina
}

// AddRewards 增加角色经验与铜钱收益。
func (s *RoleSession) AddRewards(exp, coins int64) {
	s.playerMu.Lock()
	defer s.playerMu.Unlock()
	s.playerState.Coins += coins
}

// UpdatePlayerState 线程安全地原子更新角色状态。
func (s *RoleSession) UpdatePlayerState(fn func(*PlayerState)) {
	s.playerMu.Lock()
	defer s.playerMu.Unlock()
	if fn != nil {
		fn(&s.playerState)
	}
}

// State 获取会话当前所处状态。
func (s *RoleSession) State() SessionState {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.state
}

func (s *RoleSession) setState(newState SessionState) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.state != newState {
		s.state = newState
		slog.Debug("角色会话状态变更",
			"role_id", s.cfg.RoleID,
			"role_name", s.cfg.RoleName,
			"state", newState.String(),
		)
	}
}

// RegisterHandler 注册特定 Action ID 的消息处理器回调。
func (s *RoleSession) RegisterHandler(actionID uint32, handler func(*protocol.Packet)) {
	s.handlerMu.Lock()
	defer s.handlerMu.Unlock()
	if s.handlers == nil {
		s.handlers = make(map[uint32][]func(*protocol.Packet))
	}
	s.handlers[actionID] = append(s.handlers[actionID], handler)
}

// Call 发送指定请求封包并同步等待特定 ActionID 的对端回包。
func (s *RoleSession) Call(ctx context.Context, req *protocol.Packet, respActionID uint32) (*protocol.Packet, error) {
	ch := make(chan *protocol.Packet, 1)

	s.handlerMu.Lock()
	if s.pendingCalls == nil {
		s.pendingCalls = make(map[uint32][]chan *protocol.Packet)
	}
	s.pendingCalls[respActionID] = append(s.pendingCalls[respActionID], ch)
	s.handlerMu.Unlock()

	defer func() {
		s.handlerMu.Lock()
		calls := s.pendingCalls[respActionID]
		for i, c := range calls {
			if c == ch {
				s.pendingCalls[respActionID] = append(calls[:i], calls[i+1:]...)
				break
			}
		}
		s.handlerMu.Unlock()
	}()

	if err := s.Send(req); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.ctx.Done():
		return nil, ErrSessionClosed
	case p := <-ch:
		return p, nil
	}
}

// DefaultGatewayAuthenticator 角色默认主服网关认证握手、城镇场景与全量资产四步初始化实现。
// 严格遵循真实二进制交互规范：
// 0. Module 0, Action 0 (0x00000000) 登录握手并校验 ResultCode=4
// 1. Module 0, Action 72 (0x00000048) 城镇场景初始化同步 TownID
// 2. Module 0, Action 99 (0x00000063) 场景进阶状态同步 (携带前序 0x48)
// 3. Module 165, Action 0 (0x00A50000) 扩展业务模块初始化 (携带前序 0x63)
// 4. Module 0, Action 2 (0x00000002) 全量角色资产拉取 (携带前序 0x00A50000) 并更新真实资产快照
func DefaultGatewayAuthenticator(s *RoleSession) error {
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()

	// 0. 构造并发送登录握手请求包 (优先主服 0x00000000，或跨服 0x005E0000)
	var loginPkt *protocol.Packet
	var loginRespActionID uint32 = protocol.ActionIDPlayerLogin

	if s.cfg.Time1 != 0 && s.cfg.Hash1 != "" && s.cfg.Hash == "" {
		// 跨服网关凭据
		loginRespActionID = protocol.ActionIDStLogin
		var err error
		loginPkt, err = protocol.BuildStLoginPacket(protocol.StLoginRequest{
			ServerID:   s.cfg.ServerID,
			ClientType: 4,
			RoleName:   s.RoleName(),
			Time1:      s.cfg.Time1,
			Hash1:      s.cfg.Hash1,
		})
		if err != nil {
			return fmt.Errorf("client: failed to build st login packet: %w", err)
		}
	} else {
		// 主服原生网关凭据
		timeStr := fmt.Sprintf("%d", s.cfg.Time)
		if s.cfg.Time == 0 {
			timeStr = fmt.Sprintf("%d", time.Now().Unix())
		}
		platform := s.cfg.Platform
		if platform == "" || platform == "fengwan" {
			platform = "疯玩"
		}
		hash := s.cfg.Hash
		if hash == "" {
			hash = s.cfg.Token
		}
		username := s.cfg.Username
		if username == "" {
			username = s.cfg.RoleID
		}

		loginReq := protocol.PlayerLoginRequest{
			Username:   username,
			Hash:       hash,
			Time:       timeStr,
			Source:     "",
			Platform:   platform,
			ClientType: "web",
		}

		if s.cfg.Extra != nil {
			if rd, ok := s.cfg.Extra["regdate"]; ok {
				if v, err := strconv.ParseInt(rd, 10, 32); err == nil {
					loginReq.RegDate = int32(v)
				}
			}
			if ot, ok := s.cfg.Extra["open_time"]; ok {
				if v, err := strconv.ParseInt(ot, 10, 32); err == nil {
					loginReq.OpenTime = int32(v)
				}
			}
			if in, ok := s.cfg.Extra["is_newst"]; ok {
				if v, err := strconv.ParseInt(in, 10, 8); err == nil {
					loginReq.IsNewst = int8(v)
				}
			}
			if stage, ok := s.cfg.Extra["stage"]; ok && stage != "" {
				loginReq.Stage = stage
			}
			if client, ok := s.cfg.Extra["client"]; ok && client != "" {
				loginReq.Client = client
			}
			if src, ok := s.cfg.Extra["source"]; ok {
				loginReq.Source = src
			}
			if idCard, ok := s.cfg.Extra["id_card"]; ok && idCard != "" {
				loginReq.IDCard = idCard
			}
			if s.cfg.Extra["non_kid"] == "1" {
				loginReq.IDCard = ""
			}
			if s.cfg.Extra["hash_code"] != "" {
				loginReq.IsOfficial = true
			}
		}

		if loginReq.Stage == "" && (s.cfg.Platform == "xindong" || s.cfg.Platform == "xd") {
			loginReq.Stage = "心动"
		}
		if loginReq.Client == "" {
			loginReq.Client = "web"
		}
		if loginReq.Client == "pc_wd" {
			loginReq.Flag = 1
			if loginReq.TailUsername == "" {
				loginReq.TailUsername = strings.Split(username, ".")[0]
			}
		}

		var err error
		loginPkt, err = protocol.BuildPlayerLoginPacket(loginReq)
		if err != nil {
			return fmt.Errorf("client: failed to build login packet: %w", err)
		}
	}

	// 注册握手回包通道并流水线发送首部封包
	respCh := make(chan *protocol.Packet, 1)
	s.handlerMu.Lock()
	if s.pendingCalls == nil {
		s.pendingCalls = make(map[uint32][]chan *protocol.Packet)
	}
	s.pendingCalls[loginRespActionID] = append(s.pendingCalls[loginRespActionID], respCh)
	s.handlerMu.Unlock()

	defer func() {
		s.handlerMu.Lock()
		calls := s.pendingCalls[loginRespActionID]
		for i, c := range calls {
			if c == respCh {
				s.pendingCalls[loginRespActionID] = append(calls[:i], calls[i+1:]...)
				break
			}
		}
		s.handlerMu.Unlock()
	}()

	if err := s.Send(loginPkt); err != nil {
		return fmt.Errorf("client: failed to send login packet: %w", err)
	}

	// 官方流水线对齐：官方微端在发送 M0:A0 登录请求后，立刻连发 Action 72 触发场景与分线初始化
	if loginRespActionID == protocol.ActionIDPlayerLogin {
		step1Pkt := protocol.BuildPlayerInitStep1Packet(0x00000000)
		_ = s.Send(step1Pkt)
	}

	var respPkt *protocol.Packet
	select {
	case <-ctx.Done():
		return fmt.Errorf("client: login handshake timeout or failed: %w", ctx.Err())
	case respPkt = <-respCh:
	}

	// 校验登录回包状态码
	if loginRespActionID == protocol.ActionIDPlayerLogin {
		authResp, err := protocol.ParsePlayerLoginAuthResponse(respPkt.Payload)
		if err == nil {
			if authResp.ResultCode != 4 && authResp.ResultCode != 0 {
				return fmt.Errorf("client: authentication rejected by server: resultCode=%d", authResp.ResultCode)
			}
		} else {
			res, pErr := protocol.ParsePlayerLoginResult(respPkt.Payload)
			if pErr != nil {
				return fmt.Errorf("client: invalid login response payload: %w", err)
			}
			if res.Result != 0 {
				return fmt.Errorf("client: login failed with status code %d", res.Result)
			}
			s.UpdatePlayerState(func(ps *PlayerState) {
				if res.Level > 0 {
					ps.Level = int(res.Level)
				}
				if res.Stamina > 0 {
					ps.Stamina = int(res.Stamina)
				}
				if res.Coins > 0 {
					ps.Coins = res.Coins
				}
				if res.Ingots > 0 {
					ps.Ingots = int64(res.Ingots)
				}
			})
		}
	}

	// 1. Step 1: 自动执行 Action 72 城镇场景初始化同步
	step1Pkt := protocol.BuildPlayerInitStep1Packet(0x0048)
	step1Resp, err := s.Call(ctx, step1Pkt, protocol.ActionIDPlayerInitStep1)
	if err == nil {
		if initRes, pErr := protocol.ParsePlayerInitStep1Result(step1Resp.Payload); pErr == nil {
			s.UpdatePlayerState(func(ps *PlayerState) {
				ps.TownID = int(initRes.TownID)
			})
		}
	}

	// 2. Step 2: 场景进阶同步 (ActionID 0x00000063，携带前序动作 0x0048，单向通知包)
	step2Pkt := protocol.BuildPlayerInitStep2Packet(0x0048)
	_ = s.Send(step2Pkt)

	// 3. Step 3: 扩展模块初始化 (ActionID 0x00A50000，携带前序动作 0x0063，单向通知包)
	step3Pkt := protocol.BuildPlayerInitStep3Packet(0x0063)
	_ = s.Send(step3Pkt)

	// 4. Step 4: 全量角色资产快照获取 (ActionID 0x00000002，携带前序动作 0x00A50000)
	step4Pkt := protocol.BuildPlayerGetInfoPacket(0x00A50000)
	step4Resp, err := s.Call(ctx, step4Pkt, protocol.ActionIDPlayerGetInfo)
	if err == nil {
		infoRes, pErr := protocol.ParsePlayerLoginResult(step4Resp.Payload)
		if pErr == nil && infoRes != nil {
			s.UpdatePlayerState(func(ps *PlayerState) {
				if infoRes.Level > 0 {
					ps.Level = int(infoRes.Level)
				}
				if infoRes.Coins > 0 {
					ps.Coins = infoRes.Coins
				}
				if infoRes.Ingots > 0 {
					ps.Ingots = int64(infoRes.Ingots)
				}
				if infoRes.Stamina > 0 {
					ps.Stamina = int(infoRes.Stamina)
				}
				if infoRes.ExtraStamina > 0 {
					ps.ExtraStamina = int(infoRes.ExtraStamina)
				}
				if infoRes.MaxStamina > 0 {
					ps.MaxStamina = int(infoRes.MaxStamina)
				}
				if infoRes.VIP > 0 {
					ps.VIP = int(infoRes.VIP)
				}
			})
			slog.Info("网关握手与全量角色资产同步成功",
				"role_id", s.RoleID(),
				"role_name", infoRes.RoleName,
				"town_id", s.GetPlayerState().TownID,
				"level", infoRes.Level,
				"coins", infoRes.Coins,
				"ingots", infoRes.Ingots,
				"stamina", infoRes.Stamina,
			)
		}
	}

	return nil
}


// Start 启动独立 Goroutine 驱动的网络状态机会话。
func (s *RoleSession) Start(parentCtx context.Context) error {
	s.stateMu.Lock()
	if s.state != StateDisconnected {
		s.stateMu.Unlock()
		return fmt.Errorf("client: cannot start session in state %s", s.state)
	}
	s.ctx, s.cancel = context.WithCancel(parentCtx)
	s.stateMu.Unlock()

	s.wg.Add(1)
	go s.runLoop()

	return nil
}

// Close 主动关闭角色会话并释放全部资源。
func (s *RoleSession) Close() {
	s.stateMu.Lock()
	if s.state == StateClosed {
		s.stateMu.Unlock()
		return
	}
	s.state = StateClosed
	s.stateMu.Unlock()

	if s.cancel != nil {
		s.cancel()
	}

	s.closeCurrentConn()
	s.wg.Wait()

	slog.Info("角色会话已安全关闭", "role_id", s.cfg.RoleID, "role_name", s.cfg.RoleName)
}

// LastActionID 获取最后一次向对端发送的协议 ActionID。
func (s *RoleSession) LastActionID() uint32 {
	return s.lastActionID.Load()
}

// SetLastActionID 手动设置最后一次向对端发送的协议 ActionID。
func (s *RoleSession) SetLastActionID(actionID uint32) {
	s.lastActionID.Store(actionID)
}

// Send 向对端发送一个未压缩协议封包。
func (s *RoleSession) Send(p *protocol.Packet) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if s.State() == StateClosed {
		return ErrSessionClosed
	}
	if s.conn == nil {
		return ErrNotConnected
	}

	err := protocol.WritePacket(s.conn, p)
	if err == nil && p != nil {
		if p.ActionID != s.cfg.HeartbeatActionID {
			s.lastActionID.Store(p.ActionID)
		}
		slog.Debug("发送网关请求", "action_id", fmt.Sprintf("0x%08X (Mod %d, Act %d)", p.ActionID, p.ActionID>>16, p.ActionID&0xFFFF), "payload_hex", hex.EncodeToString(p.Payload))
	}
	return err
}

// SendCompressed 向对端发送经 zlib 压缩的协议封包。
func (s *RoleSession) SendCompressed(p *protocol.Packet) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if s.State() == StateClosed {
		return ErrSessionClosed
	}
	if s.conn == nil {
		return ErrNotConnected
	}

	err := protocol.WriteCompressedPacket(s.conn, p)
	if err == nil && p != nil && p.ActionID != s.cfg.HeartbeatActionID {
		s.lastActionID.Store(p.ActionID)
	}
	return err
}

// SendRaw 向对端直接写入原始字节切片 (并发安全加锁)。
func (s *RoleSession) SendRaw(data []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if s.State() == StateClosed {
		return ErrSessionClosed
	}
	if s.conn == nil {
		return ErrNotConnected
	}

	_, err := s.conn.Write(data)
	if err == nil && len(data) >= 8 {
		actID := binary.BigEndian.Uint32(data[4:8])
		if actID != s.cfg.HeartbeatActionID {
			s.lastActionID.Store(actID)
		}
	}
	return err
}

func (s *RoleSession) closeCurrentConn() {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
}

func (s *RoleSession) runLoop() {
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			s.setState(StateClosed)
			return
		default:
		}

		// 1. 若处于重连状态且配置了自愈回调，重新换取最新平台动态凭据
		if s.State() == StateReconnecting && s.cfg.RefreshCredentials != nil {
			slog.Info("正在通过自愈管道重新换取最新平台动态凭据...", "role_id", s.cfg.RoleID)
			if rErr := s.cfg.RefreshCredentials(s.ctx, &s.cfg); rErr != nil {
				slog.Warn("凭据自愈换票未成功", "role_id", s.cfg.RoleID, "error", rErr)
			} else {
				slog.Info("凭据自愈换票成功，已装载新动态凭据", "role_id", s.cfg.RoleID, "server_addr", s.cfg.ServerAddr)
			}
		}

		if s.State() != StateReconnecting {
			s.setState(StateConnecting)
		}
		conn, err := s.cfg.Dialer(s.ctx, "tcp", s.cfg.ServerAddr)
		if err != nil {
			if !s.handleConnectFailure(err) {
				return
			}
			continue
		}

		s.writeMu.Lock()
		s.conn = conn
		s.writeMu.Unlock()

		// 2. 启动读循环通道
		readErrCh := make(chan error, 1)
		go s.readLoop(conn, readErrCh)

		// 3. 执行认证握手
		s.setState(StateAuthenticating)
		if s.cfg.Authenticator != nil {
			if err := s.cfg.Authenticator(s); err != nil {
				slog.Warn("角色身份认证失败", "role_id", s.cfg.RoleID, "error", err)
				s.closeCurrentConn()
				if !s.handleConnectFailure(err) {
					return
				}
				continue
			}
		}

		// 4. 激活会话状态
		s.setState(StateActive)

		// 5. 运行激活态心跳保活与异常等待
		if !s.runActiveSession(conn, readErrCh) {
			return
		}
	}
}

func (s *RoleSession) handleConnectFailure(err error) bool {
	select {
	case <-s.ctx.Done():
		s.setState(StateClosed)
		return false
	default:
	}

	s.reconnectAttempts++
	if s.cfg.MaxReconnectAttempts > 0 && s.reconnectAttempts >= s.cfg.MaxReconnectAttempts {
		slog.Error("超过最大重连尝试次数，会话终止",
			"role_id", s.cfg.RoleID,
			"attempts", s.reconnectAttempts,
		)
		s.setState(StateClosed)
		return false
	}


	s.setState(StateReconnecting)

	// 计算指数退避等待时间：从 ReconnectInterval 开始递增，上限 30 秒
	backoffMultiplier := 1
	if s.reconnectAttempts > 1 {
		shift := min(s.reconnectAttempts-1, 4)
		backoffMultiplier = 1 << shift
	}
	waitDuration := s.cfg.ReconnectInterval * time.Duration(backoffMultiplier)
	if waitDuration > 30*time.Second {
		waitDuration = 30 * time.Second
	}

	slog.Warn("网络连接中断，等待重连",
		"role_id", s.cfg.RoleID,
		"server_addr", s.cfg.ServerAddr,
		"attempts", s.reconnectAttempts,
		"retry_in", waitDuration,
		"error", err,
	)

	select {
	case <-s.ctx.Done():
		s.setState(StateClosed)
		return false
	case <-time.After(waitDuration):
		return true
	}
}

func (s *RoleSession) runActiveSession(conn net.Conn, readErrCh <-chan error) bool {
	ticker := time.NewTicker(s.cfg.HeartbeatInterval)
	defer ticker.Stop()

	// 连接若能持续稳定超过 10 秒（避开网关 5s 未鉴权断链熔断期），重置重连计数
	stableTimer := time.NewTimer(10 * time.Second)
	defer stableTimer.Stop()

	for {
		select {
		case <-s.ctx.Done():
			s.closeCurrentConn()
			s.setState(StateClosed)
			return false

		case <-stableTimer.C:
			// 连接已稳定存活，重置异常重试计数
			s.reconnectAttempts = 0

		case err := <-readErrCh:
			slog.Warn("网络读取循环退出", "role_id", s.cfg.RoleID, "error", err)
			s.closeCurrentConn()
			return s.handleConnectFailure(err)

		case <-ticker.C:
			// 发送心跳包：携带 4 字节前序 ActionID（神仙道网关防假死与保活校验）
			prevAct := s.lastActionID.Load()
			if prevAct == 0 {
				prevAct = s.cfg.HeartbeatActionID
			}
			hbPayload := make([]byte, 4)
			binary.BigEndian.PutUint32(hbPayload, prevAct)
			heartbeat := protocol.NewPacket(s.cfg.HeartbeatActionID, hbPayload)
			if err := s.Send(heartbeat); err != nil {
				slog.Warn("发送心跳失败", "role_id", s.cfg.RoleID, "error", err)
				s.closeCurrentConn()
				return s.handleConnectFailure(err)
			}
			s.lastActionID.Store(s.cfg.HeartbeatActionID)
		}
	}
}

func (s *RoleSession) readLoop(conn net.Conn, errCh chan<- error) {
	for {
		pkt, err := protocol.ReadPacket(conn)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				errCh <- io.EOF
			} else {
				errCh <- err
			}
			return
		}

		hexDump := hex.EncodeToString(pkt.Payload)
		if len(hexDump) > 64 {
			hexDump = hexDump[:64] + "..."
		}
		slog.Debug("收到网关回包", "action_id", fmt.Sprintf("0x%08X (Mod %d, Act %d)", pkt.ActionID, pkt.ActionID>>16, pkt.ActionID&0xFFFF), "payload_len", len(pkt.Payload), "hex", hexDump)
		s.dispatchPacket(pkt)
	}
}

func (s *RoleSession) dispatchPacket(pkt *protocol.Packet) {
	s.handlerMu.Lock()
	if calls, ok := s.pendingCalls[pkt.ActionID]; ok && len(calls) > 0 {
		ch := calls[0]
		s.pendingCalls[pkt.ActionID] = calls[1:]
		select {
		case ch <- pkt:
		default:
		}
	}
	var handlers []func(*protocol.Packet)
	if list := s.handlers[pkt.ActionID]; len(list) > 0 {
		handlers = make([]func(*protocol.Packet), len(list))
		copy(handlers, list)
	}
	s.handlerMu.Unlock()

	for _, h := range handlers {
		h(pkt)
	}
}

// runMockGameServer 为离线调试、沙箱环境与单元测试提供轻量级虚拟游戏协议服务端。
func runMockGameServer(conn net.Conn) {
	defer conn.Close()
	for {
		pkt, err := protocol.ReadPacket(conn)
		if err != nil {
			return
		}

		switch pkt.ActionID {
		case protocol.ActionIDPlayerLogin:
			// 响应主服登录成功 Golden Case 2 (ResultCode=4)
			respPayload := []byte{
				0x00, 0x00, 0x00, 0x00, 0x04, 0x0a, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x01, 0x00, 0x0b, 0x00, 0x00, 0x10, 0x85, 0x01,
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDPlayerLogin, respPayload))

		case protocol.ActionIDPlayerInitStep1:
			// 响应 Golden Case 3 (TownID=4879, TownLine=2, SceneID=4879, TargetID=4882)
			initResp := protocol.NewPacket(protocol.ActionIDPlayerInitStep1, []byte{
				0x00, 0x00, 0x13, 0x0f, 0x00, 0x02, 0x00, 0x00, 0x13, 0x0f, 0x00, 0x00, 0x13, 0x12,
			})
			_ = protocol.WritePacket(conn, initResp)

		case protocol.ActionIDPlayerInitStep2:
			// 响应场景初始化步 2 确认
			initResp2 := protocol.NewPacket(protocol.ActionIDPlayerInitStep2, []byte{0x00})
			_ = protocol.WritePacket(conn, initResp2)

		case protocol.ActionIDPlayerInitStep3:
			// 响应扩展模块初始化步 3 确认
			initResp3 := protocol.NewPacket(protocol.ActionIDPlayerInitStep3, make([]byte, 8))
			_ = protocol.WritePacket(conn, initResp3)

		case protocol.ActionIDPlayerGetInfo:
			// 响应全量角色资产快照 (201 体力、39409 元宝、362 亿铜钱)
			var rawBuf bytes.Buffer
			binary.Write(&rawBuf, binary.BigEndian, int16(0)) // Result
			binary.Write(&rawBuf, binary.BigEndian, int16(2)) // RoleID
			name := "梦一场"
			binary.Write(&rawBuf, binary.BigEndian, uint16(len(name)))
			rawBuf.WriteString(name)
			binary.Write(&rawBuf, binary.BigEndian, int32(300))         // Level
			binary.Write(&rawBuf, binary.BigEndian, int32(39409))       // Ingots
			binary.Write(&rawBuf, binary.BigEndian, int64(36200000000)) // Coins
			rawBuf.Write(make([]byte, 16))                              // 16B 占位
			binary.Write(&rawBuf, binary.BigEndian, int32(201))         // Stamina 201 点
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDPlayerGetInfo, rawBuf.Bytes()))


		case protocol.ActionIDStLogin:
			// 响应跨服登录成功，并推送 0x0300 初始体力
			res := protocol.StLoginResult{
				Result:     0,
				PlayerID:   65536,
				ServerTime: 1791469358,
			}
			resPkt, _ := protocol.BuildStLoginResultPacket(res)
			_ = protocol.WritePacket(conn, resPkt)

			// 推送体力 201 点 (Mod_Player_Base 0x0300)
			upw := protocol.NewWriter()
			upw.WriteUint8(protocol.PlayerPropPower)
			upw.WriteInt32(201)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDPlayerUpdateData, upw.Bytes()))

		case protocol.ActionHeartbeat:
			// 响应心跳包
			hbPkt := protocol.NewPacket(protocol.ActionHeartbeat, []byte{0x06, 0x00, 0x0a, 0x28})
			_ = protocol.WritePacket(conn, hbPkt)

		case protocol.ActionIDPracticeGetStatus:
			// 响应扫荡状态查询 (HeaderTag: "FP", 0 missions, isPracticing: 0)
			w := protocol.NewWriter()
			w.WriteBytes([]byte{'F', 'P'})
			w.WriteUint16(0)
			w.WriteUint8(0)
			w.WriteUint32(0)
			w.WriteUint32(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDPracticeGetStatus, w.Bytes()))

		case protocol.ActionIDPracticeStart:
			// 响应关卡扫荡请求 (兼容 StartPracticeRequest 与 SweepRequest)
			if req, err := protocol.ParseStartPracticeRequest(pkt.Payload); err == nil {
				res := protocol.StartPracticeResult{
					Result:    protocol.PracticeResultSuccess,
					MissionID: req.MissionID,
					Count:     req.Count,
				}
				resPkt, _ := protocol.BuildStartPracticeResultPacket(res)
				_ = protocol.WritePacket(conn, resPkt)
			} else if sReq, err := protocol.ParseSweepRequest(pkt.Payload); err == nil {
				res := protocol.SweepResult{
					Success:   true,
					MissionID: sReq.MissionID,
					Times:     sReq.Times,
					CostPower: int(sReq.Times) * 5,
					GainExp:   int64(sReq.Times) * 2500,
					GainCoins: int64(sReq.Times) * 12000,
					Message:   "扫荡完成",
				}
				resPkt, _ := protocol.BuildSweepResultPacket(res)
				_ = protocol.WritePacket(conn, resPkt)
			}

		case protocol.ActionIDPracticeQuickly:
			// 响应 Mod_MissionPractice_Base 加速完成包
			req, err := protocol.ParseQuicklyRequest(pkt.Payload)
			if err == nil {
				res := protocol.QuicklyResult{
					Result: protocol.PracticeResultSuccess,
					Count:  req.Count,
				}
				resPkt, _ := protocol.BuildQuicklyResultPacket(res)
				_ = protocol.WritePacket(conn, resPkt)
			}

		case protocol.ActionIDDailySignIn:
			// 响应每日签到请求
			resPkt := protocol.BuildDailySignInResultPacket(protocol.DailySignInResult{
				Success:       true,
				AlreadySigned: false,
				SignedDays:    1,
				RewardCoins:   50000,
				RewardIngots:  20,
				Message:       "签到成功，获得5万铜钱与20元宝！",
			})
			_ = protocol.WritePacket(conn, resPkt)

		case protocol.ActionIDMailCollect:
			// 响应一键收取邮件附件
			resPkt := protocol.BuildMailCollectResultPacket(protocol.MailCollectResult{
				Success:        true,
				CollectedCount: 2,
				RewardCoins:    20000,
				Message:        "成功提取 2 封邮件附件！",
			})
			_ = protocol.WritePacket(conn, resPkt)

		case protocol.ActionIDFateGetInfo:
			// 响应仙履奇缘状态查询 (剩余 1 次)
			resPkt := protocol.BuildFateGetInfoResultPacket(protocol.FateGetInfoResult{
				RemainingTimes: 1,
				MaxTimes:       3,
			})
			_ = protocol.WritePacket(conn, resPkt)

		case protocol.ActionIDFateGetQuestion:
			// 响应仙履奇缘故事题目 (对齐 answers.txt 真实故事)
			resPkt := protocol.BuildFateQuestionResultPacket(protocol.FateQuestionResult{
				QuestionID: 46,
				Story:      "灵木玉神树，千年产一次凝神汁...",
				Options: []protocol.FateOption{
					{ChoiceID: 1, Key: "A", Text: "除掉守护灵兽，夺取凝神汁。"},
					{ChoiceID: 2, Key: "B", Text: "灵兽过于强大，遗憾的放弃。"},
				},
			})
			_ = protocol.WritePacket(conn, resPkt)

		case protocol.ActionIDFateAnswer:
			// 响应仙履奇缘答题结算
			w := protocol.NewWriter()
			w.WriteString("获得5体力,50声望！")
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDFateAnswer, w.Bytes()))

		case protocol.ActionIDArenaGetTimes:
			// 响应竞技场剩余挑战次数 (10 次)
			resPkt, _ := protocol.BuildArenaGetTimesResultPacket(10)
			_ = protocol.WritePacket(conn, resPkt)

		case protocol.ActionIDArenaGetOpponents:
			// 响应竞技场可挑战对手列表
			opponents := []protocol.ArenaOpponent{
				{Rank: 5, RoleID: 1001, Name: "初级剑仙", FightValue: 35000},
				{Rank: 4, RoleID: 1002, Name: "天罗神将", FightValue: 68000},
				{Rank: 3, RoleID: 1003, Name: "万妖统帅", FightValue: 92000},
			}
			resPkt, _ := protocol.BuildArenaGetOpponentsResultPacket(opponents)
			_ = protocol.WritePacket(conn, resPkt)

		case protocol.ActionIDArenaChallenge:
			// 响应竞技场挑战胜利 (提升至第 5 名)
			resPkt, _ := protocol.BuildArenaChallengeResultPacket(protocol.ArenaChallengeResult{
				Success: true,
				NewRank: 5,
			})
			_ = protocol.WritePacket(conn, resPkt)
		}
	}
}
