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
	TownID       int               `json:"town_id"`
	Quests       map[uint32]uint32 `json:"quests"`
	IsPracticing bool              `json:"is_practicing"`
	PracticeExp  int64             `json:"practice_exp"`
	Experience   int64             `json:"experience"`
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
		if err == nil {
			s.playerMu.Lock()
			s.playerState.Quests = make(map[uint32]uint32, len(items))
			for _, it := range items {
				s.playerState.Quests[uint32(it.QuestID)] = uint32(it.State)
			}
			s.playerMu.Unlock()
			slog.Info("收到玩家全量任务列表同步", "role_id", s.cfg.RoleID, "total_quests", len(items))
		}
	})

	// 注册城镇打坐练功通知监听器 (Module 0, Action 40)
	s.RegisterHandler(protocol.ActionIDPlayerPracticeNotify, func(pkt *protocol.Packet) {
		if pkt == nil {
			return
		}
		notify, err := protocol.ParseTownPracticeNotify(pkt.Payload)
		if err != nil {
			slog.Debug("解析城镇打坐练功通知失败", "err", err)
			return
		}
		s.playerMu.Lock()
		defer s.playerMu.Unlock()
		switch notify.NotifyType {
		case protocol.PlayerPracticeNotifyStart:
			s.playerState.IsPracticing = true
			slog.Info("收到通知: 角色已进入城镇打坐练功状态", "role_id", s.cfg.RoleID)
		case protocol.PlayerPracticeNotifyUpdate:
			s.playerState.IsPracticing = true
			s.playerState.PracticeExp = int64(notify.Exp)
			slog.Debug("收到通知: 城镇打坐练功经验更新", "role_id", s.cfg.RoleID, "exp", notify.Exp, "super_exp", notify.SuperExp)
		case protocol.PlayerPracticeNotifyFinished:
			s.playerState.IsPracticing = false
			s.playerState.PracticeExp = 0
			slog.Info("收到通知: 城镇打坐练功已正常结束结算", "role_id", s.cfg.RoleID, "gain_exp", notify.Exp)
		case protocol.PlayerPracticeNotifyCancel:
			s.playerState.IsPracticing = false
			slog.Info("收到通知: 城镇打坐练功已取消/中断", "role_id", s.cfg.RoleID)
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

			// 5. 现代版神仙道机制：离线自动打坐，上线后直接结算并领取累积练功经验
			go func() {
				select {
				case <-s.ctx.Done():
					return
				case <-time.After(500 * time.Millisecond):
					if err := s.ClaimTownPracticeExp(s.ctx); err != nil {
						slog.Debug("上线直接领取打坐经验跳过或无需领取", "role_id", s.RoleID(), "err", err)
					}
				}
			}()
		}
	}

	return nil
}

// SyncQuests 主动向服务端发送 Mod 3, Act 9 协议全量拉取当前已接任务列表并同步至内存状态快照。
func (s *RoleSession) SyncQuests(ctx context.Context) ([]protocol.PlayerQuestItem, error) {
	questListPkt := protocol.BuildGetPlayerQuestListPacket(s.LastActionID())
	timeout := 3 * time.Second
	if s.cfg.ServerAddr == "" || s.cfg.ServerAddr == "mock" || s.cfg.ServerAddr == "dry-run" || s.cfg.ServerAddr == "sandbox" {
		timeout = 300 * time.Millisecond
	}
	qCtx, qCancel := context.WithTimeout(ctx, timeout)
	defer qCancel()

	qResp, qErr := s.Call(qCtx, questListPkt, protocol.ActionIDQuestList)
	if qErr != nil {
		return nil, qErr
	}
	if qResp == nil {
		return nil, errors.New("empty quest list response")
	}

	items, pErr := protocol.ParsePlayerQuestListResponse(qResp.Payload)
	if pErr != nil {
		return nil, pErr
	}

	s.playerMu.Lock()
	s.playerState.Quests = make(map[uint32]uint32, len(items))
	for _, it := range items {
		s.playerState.Quests[uint32(it.QuestID)] = uint32(it.State)
	}
	s.playerMu.Unlock()

	slog.Info("成功同步服务端全量任务列表", "role_id", s.RoleID(), "total_quests", len(items))
	return items, nil
}

// StartTownPractice 发起城镇打坐练功请求 (Mod_Player_Base.start_practice, ActionID 0x00000024)。
func (s *RoleSession) StartTownPractice(ctx context.Context) error {
	pkt := protocol.BuildPlayerStartPracticePacket(s.LastActionID())
	timeout := 3 * time.Second
	if s.cfg.ServerAddr == "" || s.cfg.ServerAddr == "mock" || s.cfg.ServerAddr == "dry-run" || s.cfg.ServerAddr == "sandbox" {
		timeout = 300 * time.Millisecond
	}
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	_, err := s.Call(cCtx, pkt, protocol.ActionIDPlayerStartPractice)
	if err != nil {
		return fmt.Errorf("start town practice: %w", err)
	}

	s.playerMu.Lock()
	s.playerState.IsPracticing = true
	s.playerMu.Unlock()
	slog.Info("角色已成功进入普通城镇打坐练功", "role_id", s.RoleID())
	return nil
}

// GetTownPracticeData 获取城镇打坐练功实时数据 (Mod_Player_Base.get_practice_data, ActionID 0x00000025)。
func (s *RoleSession) GetTownPracticeData(ctx context.Context) (*protocol.TownPracticeData, error) {
	pkt := protocol.BuildPlayerGetPracticeDataPacket(s.LastActionID())
	timeout := 3 * time.Second
	if s.cfg.ServerAddr == "" || s.cfg.ServerAddr == "mock" || s.cfg.ServerAddr == "dry-run" || s.cfg.ServerAddr == "sandbox" {
		timeout = 300 * time.Millisecond
	}
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDPlayerGetPracticeData)
	if err != nil {
		return nil, fmt.Errorf("get town practice data: %w", err)
	}
	if resp == nil {
		return nil, errors.New("empty town practice data response")
	}

	data, err := protocol.ParseTownPracticeDataResponse(resp.Payload)
	if err != nil {
		return nil, err
	}

	s.playerMu.Lock()
	s.playerState.PracticeExp = data.PracticeExp
	if data.PracticeTime > 0 || data.PracticeExp > 0 {
		s.playerState.IsPracticing = true
	}
	s.playerMu.Unlock()

	return data, nil
}

// CancelTownPractice 取消/中断当前城镇打坐练功 (Mod_Player_Base.cancel_practice, ActionID 0x00000027)。
func (s *RoleSession) CancelTownPractice(ctx context.Context) error {
	pkt := protocol.BuildPlayerCancelPracticePacket(s.LastActionID())
	if err := s.Send(pkt); err != nil {
		return fmt.Errorf("cancel town practice: %w", err)
	}

	s.playerMu.Lock()
	s.playerState.IsPracticing = false
	s.playerMu.Unlock()
	return nil
}

// SignFinishedTownPractice 结算并领取城镇打坐练功累积经验 (Mod_Player_Base.sign_finished_practice, ActionID 0x00000026)。
func (s *RoleSession) SignFinishedTownPractice(ctx context.Context) error {
	pkt := protocol.BuildPlayerSignFinishedPracticePacket(s.LastActionID())
	if err := s.Send(pkt); err != nil {
		return fmt.Errorf("sign finished town practice: %w", err)
	}

	s.playerMu.Lock()
	s.playerState.IsPracticing = false
	s.playerState.PracticeExp = 0
	s.playerMu.Unlock()
	slog.Info("已触发领取城镇打坐练功累积经验", "role_id", s.RoleID())
	return nil
}

// ClaimTownPracticeExp 上线直接结算并领取离线练功经验 (对齐官方客户端 MapViewBase.onCancelPraceice: 取消练功 + 确认结算)。
func (s *RoleSession) ClaimTownPracticeExp(ctx context.Context) error {
	// 1. 官方流水线：先发送 cancel_practice (Mod 0, Act 39)
	_ = s.CancelTownPractice(ctx)
	// 2. 官方流水线：再发送 sign_finished_practice (Mod 0, Act 38) 领取累积经验
	return s.SignFinishedTownPractice(ctx)
}

// BeelzebubTrialsCheckCanChallenge 检查今日是否可挑战魔王试炼 (Module 57, Action 20)。
func (s *RoleSession) BeelzebubTrialsCheckCanChallenge(ctx context.Context) (bool, error) {
	pkt := protocol.BuildBeelzebubTrialsCheckCanChallengePacket(s.LastActionID())
	timeout := 3 * time.Second
	if s.cfg.ServerAddr == "" || s.cfg.ServerAddr == "mock" || s.cfg.ServerAddr == "dry-run" || s.cfg.ServerAddr == "sandbox" {
		timeout = 300 * time.Millisecond
	}
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDBeelzebubTrialsCheckCanChallenge)
	if err != nil {
		return false, fmt.Errorf("check beelzebub trials can challenge: %w", err)
	}
	if resp == nil {
		return false, errors.New("empty beelzebub check response")
	}
	return protocol.ParseBeelzebubTrialsCheckCanChallengeResponse(resp.Payload)
}

// BeelzebubTrialsGetCanChallengeList 获取可挑战魔王编号列表 (Module 57, Action 22)。
func (s *RoleSession) BeelzebubTrialsGetCanChallengeList(ctx context.Context) ([]int32, error) {
	pkt := protocol.BuildBeelzebubTrialsGetCanChallengeListPacket(s.LastActionID())
	timeout := 3 * time.Second
	if s.cfg.ServerAddr == "" || s.cfg.ServerAddr == "mock" || s.cfg.ServerAddr == "dry-run" || s.cfg.ServerAddr == "sandbox" {
		timeout = 300 * time.Millisecond
	}
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDBeelzebubTrialsGetCanChallengeList)
	if err != nil {
		return nil, fmt.Errorf("get beelzebub trials challenge list: %w", err)
	}
	if resp == nil {
		return nil, errors.New("empty beelzebub challenge list response")
	}
	return protocol.ParseBeelzebubTrialsGetCanChallengeListResponse(resp.Payload)
}

// BeelzebubTrialsOpen 打开魔王试炼面板并获取基础道行与活动信息 (Module 57, Action 1)。
func (s *RoleSession) BeelzebubTrialsOpen(ctx context.Context) (*protocol.BeelzebubTrialsOpenData, error) {
	pkt := protocol.BuildBeelzebubTrialsOpenPacket(s.LastActionID())
	timeout := 3 * time.Second
	if s.cfg.ServerAddr == "" || s.cfg.ServerAddr == "mock" || s.cfg.ServerAddr == "dry-run" || s.cfg.ServerAddr == "sandbox" {
		timeout = 300 * time.Millisecond
	}
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDBeelzebubTrialsOpen)
	if err != nil {
		return nil, fmt.Errorf("open beelzebub trials: %w", err)
	}
	if resp == nil {
		return nil, errors.New("empty beelzebub trials open response")
	}
	return protocol.ParseBeelzebubTrialsOpenResponse(resp.Payload)
}

// BeelzebubTrialsCreateTeam 创建指定魔王挑战队伍 (Module 57, Action 4)。
func (s *RoleSession) BeelzebubTrialsCreateTeam(ctx context.Context, bossID int32, teamType uint8) (uint8, error) {
	pkt := protocol.BuildBeelzebubTrialsCreateTeamPacket(bossID, teamType, s.LastActionID())
	timeout := 3 * time.Second
	if s.cfg.ServerAddr == "" || s.cfg.ServerAddr == "mock" || s.cfg.ServerAddr == "dry-run" || s.cfg.ServerAddr == "sandbox" {
		timeout = 300 * time.Millisecond
	}
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDBeelzebubTrialsCreateTeam)
	if err != nil {
		return 0, fmt.Errorf("create beelzebub trials team: %w", err)
	}
	if resp == nil {
		return 0, errors.New("empty create team response")
	}
	return protocol.ParseBeelzebubTrialsCreateTeamResponse(resp.Payload)
}

// BeelzebubTrialsJoinTeam 加入已有魔王试炼队伍 (Module 57, Action 6)。
func (s *RoleSession) BeelzebubTrialsJoinTeam(ctx context.Context, teamID int32) (uint8, error) {
	pkt := protocol.BuildBeelzebubTrialsJoinTeamPacket(teamID, s.LastActionID())
	timeout := 3 * time.Second
	if s.cfg.ServerAddr == "" || s.cfg.ServerAddr == "mock" || s.cfg.ServerAddr == "dry-run" || s.cfg.ServerAddr == "sandbox" {
		timeout = 300 * time.Millisecond
	}
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDBeelzebubTrialsJoinTeam)
	if err != nil {
		return 0, fmt.Errorf("join beelzebub trials team: %w", err)
	}
	if resp == nil {
		return 0, errors.New("empty join team response")
	}
	return protocol.ParseBeelzebubTrialsJoinTeamResponse(resp.Payload)
}

// BeelzebubTrialsGetPlayerTeamInfo 获取当前魔王试炼队伍成员状态 (Module 57, Action 3)。
func (s *RoleSession) BeelzebubTrialsGetPlayerTeamInfo(ctx context.Context) (int32, []protocol.BeelzebubTeamMember, error) {
	pkt := protocol.BuildBeelzebubTrialsGetPlayerTeamInfoPacket(s.LastActionID())
	timeout := 3 * time.Second
	if s.cfg.ServerAddr == "" || s.cfg.ServerAddr == "mock" || s.cfg.ServerAddr == "dry-run" || s.cfg.ServerAddr == "sandbox" {
		timeout = 300 * time.Millisecond
	}
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDBeelzebubTrialsGetPlayerTeamInfo)
	if err != nil {
		return 0, nil, fmt.Errorf("get beelzebub trials player team info: %w", err)
	}
	if resp == nil {
		return 0, nil, errors.New("empty player team info response")
	}
	return protocol.ParseBeelzebubTrialsPlayerTeamInfoResponse(resp.Payload)
}

// BeelzebubTrialsGetHelperRoles 获取可调遣助战伙伴/镜像列表 (Module 57, Action 8)。
func (s *RoleSession) BeelzebubTrialsGetHelperRoles(ctx context.Context) ([]protocol.BeelzebubHelperRole, error) {
	pkt := protocol.BuildBeelzebubTrialsGetHelperRoleListPacket(s.LastActionID())
	timeout := 3 * time.Second
	if s.cfg.ServerAddr == "" || s.cfg.ServerAddr == "mock" || s.cfg.ServerAddr == "dry-run" || s.cfg.ServerAddr == "sandbox" {
		timeout = 300 * time.Millisecond
	}
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDBeelzebubTrialsGetHelperRoleList)
	if err != nil {
		return nil, fmt.Errorf("get beelzebub helper roles: %w", err)
	}
	if resp == nil {
		return nil, errors.New("empty helper roles response")
	}
	return protocol.ParseBeelzebubTrialsHelperRoleListResponse(resp.Payload)
}

// BeelzebubTrialsChooseHelper 选择助战伙伴填充队伍二号或三号空位 (Module 57, Action 9)。
func (s *RoleSession) BeelzebubTrialsChooseHelper(ctx context.Context, roleID int32, slot uint8) (uint8, error) {
	pkt := protocol.BuildBeelzebubTrialsChooseHelperRolePacket(roleID, slot, s.LastActionID())
	timeout := 3 * time.Second
	if s.cfg.ServerAddr == "" || s.cfg.ServerAddr == "mock" || s.cfg.ServerAddr == "dry-run" || s.cfg.ServerAddr == "sandbox" {
		timeout = 300 * time.Millisecond
	}
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDBeelzebubTrialsChooseHelperRole)
	if err != nil {
		return 0, fmt.Errorf("choose beelzebub helper role: %w", err)
	}
	if resp == nil {
		return 0, errors.New("empty choose helper response")
	}
	return protocol.ParseBeelzebubTrialsChooseHelperRoleResponse(resp.Payload)
}

// BeelzebubTrialsFight 队长向服务器发起战斗指令 (Module 57, Action 13)。
// 官方 AS3 Mod_BeelzebubTrials_Base 规范：Action 13 属于单向无回包发包指令，触发后由 Action 14 拉取完整战报。
func (s *RoleSession) BeelzebubTrialsFight(ctx context.Context) (uint8, error) {
	pkt := protocol.BuildBeelzebubTrialsFightPacket(s.LastActionID())
	if err := s.Send(pkt); err != nil {
		return 0, fmt.Errorf("fight beelzebub trials: %w", err)
	}
	return protocol.BeelzebubTrialsSuccess, nil
}

// BeelzebubTrialsGetWarResult 拉取魔王试炼战报结果与奖励结算 (Module 57, Action 14)。
func (s *RoleSession) BeelzebubTrialsGetWarResult(ctx context.Context) (*protocol.BeelzebubTrialsWarResult, error) {
	pkt := protocol.BuildBeelzebubTrialsGetWarResultPacket(s.LastActionID())
	timeout := 3 * time.Second
	if s.cfg.ServerAddr == "" || s.cfg.ServerAddr == "mock" || s.cfg.ServerAddr == "dry-run" || s.cfg.ServerAddr == "sandbox" {
		timeout = 300 * time.Millisecond
	}
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDBeelzebubTrialsGetWarResult)
	if err != nil {
		return nil, fmt.Errorf("get beelzebub war result: %w", err)
	}
	if resp == nil {
		return nil, errors.New("empty war result response")
	}
	return protocol.ParseBeelzebubTrialsWarResultResponse(resp.Payload)
}

// BeelzebubTrialsLeaveWar 退出魔王试炼战斗结算场景 (Module 57, Action 16)。
func (s *RoleSession) BeelzebubTrialsLeaveWar(ctx context.Context, warID int32) error {
	pkt := protocol.BuildBeelzebubTrialsLeaveWarPacket(warID, s.LastActionID())
	timeout := 3 * time.Second
	if s.cfg.ServerAddr == "" || s.cfg.ServerAddr == "mock" || s.cfg.ServerAddr == "dry-run" || s.cfg.ServerAddr == "sandbox" {
		timeout = 300 * time.Millisecond
	}
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	_, _ = s.Call(cCtx, pkt, protocol.ActionIDBeelzebubTrialsLeaveWar)
	return nil
}

// BeelzebubTrialsExitTeam 退出或解散当前队伍 (Module 57, Action 7)。
func (s *RoleSession) BeelzebubTrialsExitTeam(ctx context.Context) error {
	pkt := protocol.BuildBeelzebubTrialsExitTeamPacket(s.LastActionID())
	timeout := 3 * time.Second
	if s.cfg.ServerAddr == "" || s.cfg.ServerAddr == "mock" || s.cfg.ServerAddr == "dry-run" || s.cfg.ServerAddr == "sandbox" {
		timeout = 300 * time.Millisecond
	}
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	_, _ = s.Call(cCtx, pkt, protocol.ActionIDBeelzebubTrialsExitTeam)
	return nil
}

// BeelzebubTrialsOpenMoral 查询当前道行属性与等级奖励信息 (Module 57, Action 23)。
func (s *RoleSession) BeelzebubTrialsOpenMoral(ctx context.Context) (*protocol.BeelzebubMoralInfo, error) {
	pkt := protocol.BuildBeelzebubTrialsOpenMoralPacket(s.LastActionID())
	timeout := 3 * time.Second
	if s.cfg.ServerAddr == "" || s.cfg.ServerAddr == "mock" || s.cfg.ServerAddr == "dry-run" || s.cfg.ServerAddr == "sandbox" {
		timeout = 300 * time.Millisecond
	}
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDBeelzebubTrialsOpenMoral)
	if err != nil {
		return nil, fmt.Errorf("open beelzebub moral: %w", err)
	}
	if resp == nil {
		return nil, errors.New("empty open moral response")
	}
	return protocol.ParseBeelzebubTrialsOpenMoralResponse(resp.Payload)
}

// BeelzebubTrialsGetMoralAward 领取指定道行等级奖励 (Module 57, Action 24)。
func (s *RoleSession) BeelzebubTrialsGetMoralAward(ctx context.Context, level int32) (uint8, error) {
	pkt := protocol.BuildBeelzebubTrialsGetMoralAwardPacket(level, s.LastActionID())
	timeout := 3 * time.Second
	if s.cfg.ServerAddr == "" || s.cfg.ServerAddr == "mock" || s.cfg.ServerAddr == "dry-run" || s.cfg.ServerAddr == "sandbox" {
		timeout = 300 * time.Millisecond
	}
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDBeelzebubTrialsGetMoralAward)
	if err != nil {
		return 0, fmt.Errorf("get beelzebub moral award: %w", err)
	}
	if resp == nil {
		return 0, errors.New("empty moral award response")
	}
	return protocol.ParseBeelzebubTrialsGetMoralAwardResponse(resp.Payload)
}

// BeelzebubTrialsAutoChallenge 全自动执行魔王试炼单挑通关闭环：
// 1. 检查今日试炼资格
// 2. 拉取可挑战魔王列表并选定最高难度 (或优先选定 preferBossID)
// 3. 自建队伍
// 4. 招募助战镜像补齐队伍
// 5. 发起战斗并拉取收益
// 6. 清理离开战场与队伍
// 7. 领取累积道行等级奖励
func (s *RoleSession) BeelzebubTrialsAutoChallenge(ctx context.Context, preferBossID int32) (*protocol.BeelzebubTrialsWarResult, error) {
	// 1. 检查今日挑战资格
	canChallenge, err := s.BeelzebubTrialsCheckCanChallenge(ctx)
	if err != nil {
		return nil, fmt.Errorf("检查魔王试炼挑战状态失败: %w", err)
	}
	if !canChallenge {
		slog.Info("今日魔王试炼挑战次数已用尽，无需重复挑战", "role_id", s.RoleID())
		return nil, nil
	}

	// 2. 拉取可挑战魔王列表
	canList, err := s.BeelzebubTrialsGetCanChallengeList(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取可挑战魔王列表失败: %w", err)
	}
	if len(canList) == 0 {
		slog.Warn("未检测到可挑战的魔王列表，退出魔王试炼", "role_id", s.RoleID())
		return nil, nil
	}

	// 选定挑战目标
	targetBossID := canList[len(canList)-1] // 默认选最高难度
	if preferBossID > 0 {
		for _, bid := range canList {
			if bid == preferBossID {
				targetBossID = preferBossID
				break
			}
		}
	}
	bossName := protocol.BeelzebubTrialsBossName(targetBossID)
	slog.Info("魔王试炼选定挑战目标", "role_id", s.RoleID(), "boss_id", targetBossID, "boss_name", bossName)

	// 3. 自建队伍
	cRes, err := s.BeelzebubTrialsCreateTeam(ctx, targetBossID, protocol.BeelzebubTrialsNotLimit)
	if err != nil {
		return nil, fmt.Errorf("创建试炼队伍失败: %w", err)
	}
	if cRes != protocol.BeelzebubTrialsSuccess {
		return nil, fmt.Errorf("创建试炼队伍服务端返回失败代码: %d", cRes)
	}

	// 4. 招募助战镜像补齐队伍
	helpers, err := s.BeelzebubTrialsGetHelperRoles(ctx)
	if err != nil {
		slog.Warn("获取助战伙伴列表异常", "err", err)
	} else if len(helpers) > 0 {
		slot := uint8(1)
		for _, h := range helpers {
			if slot > 2 {
				break
			}
			if !h.InBody {
				if _, err := s.BeelzebubTrialsChooseHelper(ctx, h.RoleID, slot); err == nil {
					slog.Info("已成功招募助战镜像加入队伍", "role_id", s.RoleID(), "slot", slot, "helper_name", h.Name)
					slot++
				}
			}
		}
	}

	// 5. 发起战斗 (单向通知指令)
	fRes, err := s.BeelzebubTrialsFight(ctx)
	if err != nil {
		_ = s.BeelzebubTrialsExitTeam(ctx)
		return nil, fmt.Errorf("发起魔王试炼战斗指令失败: %w", err)
	}
	if fRes != protocol.BeelzebubTrialsSuccess {
		_ = s.BeelzebubTrialsExitTeam(ctx)
		return nil, fmt.Errorf("发起战斗服务端返回错误代码: %d", fRes)
	}

	// 等待服务端生成战报
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(500 * time.Millisecond):
	}

	// 6. 获取战斗结果与收益结算
	warRes, err := s.BeelzebubTrialsGetWarResult(ctx)
	if err != nil {
		slog.Warn("拉取战斗结算数据异常", "err", err)
	} else {
		slog.Info("魔王试炼战斗结算完成",
			"role_id", s.RoleID(),
			"boss_name", bossName,
			"result", warRes.Result,
			"award_fame", warRes.AwardFame,
			"award_coin", warRes.AwardCoin,
			"award_moral", warRes.AwardMoral,
		)
	}

	// 7. 清理离场
	_ = s.BeelzebubTrialsLeaveWar(ctx, 0)
	_ = s.BeelzebubTrialsExitTeam(ctx)

	// 8. 检查并领取道行升级奖励
	if moral, err := s.BeelzebubTrialsOpenMoral(ctx); err == nil && moral != nil {
		if moral.CanClaim && moral.Level > 0 {
			if aRes, err := s.BeelzebubTrialsGetMoralAward(ctx, moral.Level); err == nil && aRes == protocol.BeelzebubTrialsSuccess {
				slog.Info("已成功领取道行等级宝箱奖励", "role_id", s.RoleID(), "moral_lv", moral.Level)
			}
		}
	}

	return warRes, nil
}

// GetTowerInfo 查询六道轮回塔面板与当前挑战层进度 (Mod_Tower_Base.get_tower_info, ActionID 0x00200000)。
func (s *RoleSession) GetTowerInfo(ctx context.Context) (*protocol.TowerInfo, error) {
	pkt := protocol.BuildTowerGetInfoPacket(s.LastActionID())
	timeout := 3 * time.Second
	if s.cfg.ServerAddr == "" || s.cfg.ServerAddr == "mock" || s.cfg.ServerAddr == "dry-run" || s.cfg.ServerAddr == "sandbox" {
		timeout = 300 * time.Millisecond
	}
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDTowerGetInfo)
	if err != nil {
		return nil, fmt.Errorf("get tower info: %w", err)
	}
	if resp == nil {
		return nil, errors.New("empty tower info response")
	}

	return protocol.ParseTowerInfoResponse(resp.Payload)
}

// SellItem 出售背包指定格位物品 (Mod_Item_Base.player_sell_item, ActionID 0x00020049)。
func (s *RoleSession) SellItem(ctx context.Context, gridID int16) (bool, error) {
	pkt, err := protocol.BuildSellItemPacket(gridID)
	if err != nil {
		return false, fmt.Errorf("build sell item packet: %w", err)
	}
	timeout := 3 * time.Second
	if s.cfg.ServerAddr == "" || s.cfg.ServerAddr == "mock" || s.cfg.ServerAddr == "dry-run" || s.cfg.ServerAddr == "sandbox" {
		timeout = 300 * time.Millisecond
	}
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDItemSell)
	if err != nil {
		return false, fmt.Errorf("sell item grid %d: %w", gridID, err)
	}
	if resp == nil {
		return false, errors.New("empty sell item response")
	}

	success, _, pErr := protocol.ParseSellItemResponse(resp.Payload)
	return success, pErr
}

// ChallengeTower 发起六道轮回塔单层战斗挑战 (Mod_Tower_Base.start_challenge, ActionID 0x00200002)。
func (s *RoleSession) ChallengeTower(ctx context.Context, warIdx uint8, autoSellL, autoSellZ bool) (*protocol.TowerChallengeResult, error) {
	pkt := protocol.BuildTowerChallengePacket(warIdx, autoSellL, autoSellZ, s.LastActionID())
	timeout := 5 * time.Second
	if s.cfg.ServerAddr == "" || s.cfg.ServerAddr == "mock" || s.cfg.ServerAddr == "dry-run" || s.cfg.ServerAddr == "sandbox" {
		timeout = 300 * time.Millisecond
	}
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDTowerStartChallenge)
	if err != nil {
		return nil, fmt.Errorf("challenge tower: %w", err)
	}
	if resp == nil {
		return nil, errors.New("empty tower challenge response")
	}

	return protocol.ParseTowerChallengeResponse(resp.Payload)
}

// ResetTowerLayer 重置六道轮回塔层数 (Mod_Tower_Base.reset_layer, ActionID 0x00200003)。
func (s *RoleSession) ResetTowerLayer(ctx context.Context) (*protocol.TowerResetResult, error) {
	pkt := protocol.BuildTowerResetPacket(s.LastActionID())
	timeout := 3 * time.Second
	if s.cfg.ServerAddr == "" || s.cfg.ServerAddr == "mock" || s.cfg.ServerAddr == "dry-run" || s.cfg.ServerAddr == "sandbox" {
		timeout = 300 * time.Millisecond
	}
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDTowerResetLayer)
	if err != nil {
		return nil, fmt.Errorf("reset tower layer: %w", err)
	}
	if resp == nil {
		return nil, errors.New("empty tower reset response")
	}

	return protocol.ParseTowerResetResponse(resp.Payload)
}

// StartTowerAutoChallenge 发起六道轮回自动挑战/爬塔扫荡 (Mod_Tower_Base.start_auto_challenge, ActionID 0x00200004)。
func (s *RoleSession) StartTowerAutoChallenge(ctx context.Context, autoSellL, autoSellZ bool) error {
	pkt := protocol.BuildTowerStartAutoChallengePacket(autoSellL, autoSellZ, s.LastActionID())
	timeout := 3 * time.Second
	if s.cfg.ServerAddr == "" || s.cfg.ServerAddr == "mock" || s.cfg.ServerAddr == "dry-run" || s.cfg.ServerAddr == "sandbox" {
		timeout = 300 * time.Millisecond
	}
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDTowerStartAutoChallenge)
	if err != nil {
		return fmt.Errorf("start tower auto challenge: %w", err)
	}
	if resp != nil && len(resp.Payload) > 0 {
		code, _ := protocol.ParseTowerAutoChallengeResponse(resp.Payload)
		if code != protocol.TowerResultSuccess {
			return fmt.Errorf("start tower auto challenge failed with code %d", code)
		}
	}
	return nil
}

// QuicklyTowerAutoChallenge 立即加速秒完成六道轮回自动爬塔 (Mod_Tower_Base.quickly_auto_challenge, ActionID 0x00200007)。
func (s *RoleSession) QuicklyTowerAutoChallenge(ctx context.Context, autoSellL, autoSellZ bool) error {
	pkt := protocol.BuildTowerQuicklyAutoChallengePacket(autoSellL, autoSellZ, s.LastActionID())
	timeout := 3 * time.Second
	if s.cfg.ServerAddr == "" || s.cfg.ServerAddr == "mock" || s.cfg.ServerAddr == "dry-run" || s.cfg.ServerAddr == "sandbox" {
		timeout = 300 * time.Millisecond
	}
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := s.Call(cCtx, pkt, protocol.ActionIDTowerQuicklyAutoChallenge)
	if err != nil {
		return fmt.Errorf("quickly tower auto challenge: %w", err)
	}
	if resp != nil && len(resp.Payload) > 0 {
		code, _ := protocol.ParseTowerAutoChallengeResponse(resp.Payload)
		if code != protocol.TowerResultSuccess {
			return fmt.Errorf("quickly tower auto challenge failed with code %d", code)
		}
	}
	return nil
}

// StopTowerAutoChallenge 停止六道轮回自动爬塔 (Mod_Tower_Base.stop_auto_challenge, ActionID 0x00200005)。
func (s *RoleSession) StopTowerAutoChallenge(ctx context.Context) error {
	pkt := protocol.BuildTowerStopAutoChallengePacket(s.LastActionID())
	timeout := 3 * time.Second
	if s.cfg.ServerAddr == "" || s.cfg.ServerAddr == "mock" || s.cfg.ServerAddr == "dry-run" || s.cfg.ServerAddr == "sandbox" {
		timeout = 300 * time.Millisecond
	}
	cCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	_, err := s.Call(cCtx, pkt, protocol.ActionIDTowerStopAutoChallenge)
	return err
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

	if p != nil {
		mod := p.ActionID >> 16
		if prev := s.lastActionID.Load(); prev != 0 {
			if (p.ActionID == protocol.ActionIDItemGetPackList && len(p.Payload) == 0) ||
				(p.ActionID == protocol.ActionIDItemUseGrid && len(p.Payload) == 2) ||
				(p.ActionID == protocol.ActionIDItemSell && len(p.Payload) == 2) {
				cloned := make([]byte, len(p.Payload)+4)
				copy(cloned, p.Payload)
				binary.BigEndian.PutUint32(cloned[len(p.Payload):], prev)
				p = &protocol.Packet{
					ActionID:   p.ActionID,
					Payload:    cloned,
					HasPrevAct: true,
				}
			} else if (p.HasPrevAct || mod == 3 || mod == 4 || mod == 25 || p.ActionID == s.cfg.HeartbeatActionID) && len(p.Payload) >= 4 {
				cloned := make([]byte, len(p.Payload))
				copy(cloned, p.Payload)
				binary.BigEndian.PutUint32(cloned[len(cloned)-4:], prev)
				p = &protocol.Packet{
					ActionID:   p.ActionID,
					Payload:    cloned,
					HasPrevAct: p.HasPrevAct,
				}
			}
		}
	}

	err := protocol.WritePacket(s.conn, p)
	if err == nil && p != nil {
		s.lastActionID.Store(p.ActionID)
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
		s.lastActionID.Store(0)
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

		case protocol.ActionIDPlayerStartPractice:
			// 响应城镇打坐开始
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDPlayerStartPractice, []byte{}))

		case protocol.ActionIDPlayerGetPracticeData:
			// 响应城镇打坐练功数据查询
			data := protocol.TownPracticeData{
				PracticeTime:         1800,
				PracticeExp:          250000,
				PracticeSpeedNum:     24,
				PracticeExpOne:       10000,
				SuperTownPracticeExp: 2000,
			}
			resPkt, _ := protocol.BuildTownPracticeDataResponsePacket(data)
			_ = protocol.WritePacket(conn, resPkt)

		case protocol.ActionIDPlayerCancelPractice:
			// 响应城镇打坐取消
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDPlayerCancelPractice, []byte{}))

		case protocol.ActionIDPlayerSignFinishedPractice:
			// 响应城镇打坐结束并结算经验
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDPlayerSignFinishedPractice, []byte{}))

		case protocol.ActionIDTowerGetInfo:
			// 响应六道轮回塔面板查询 (SequenceMax=25, Layer=1, SequenceWar=1, BagNum=2, ResetNum=1, ResetIngot=0, HideID=0, HighStone=50)
			tw := protocol.NewWriter()
			tw.WriteUint8(25) // SequenceMax
			tw.WriteUint8(1)  // Layer
			tw.WriteUint8(1)  // SequenceWar
			tw.WriteUint8(2)  // BagNum
			tw.WriteUint8(1)  // ResetNum
			tw.WriteUint16(0) // ResetIngot
			tw.WriteUint8(0)  // HideID
			tw.WriteInt32(50) // HighStone
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDTowerGetInfo, tw.Bytes()))

		case protocol.ActionIDTowerStartChallenge:
			// 响应六道轮回塔单关挑战成功
			cw := protocol.NewWriter()
			cw.WriteUint8(protocol.TowerResultSuccess) // Result: SUCCESS
			cw.WriteInt32(1001)                       // WarSoulID
			cw.WriteInt32(300000)                     // WarExp
			cw.WriteInt32(300)                        // WarFame
			cw.WriteInt32(50000)                      // WarCoin
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDTowerStartChallenge, cw.Bytes()))

		case protocol.ActionIDTowerResetLayer:
			// 响应六道轮回重置
			rw := protocol.NewWriter()
			rw.WriteUint8(protocol.TowerResultSuccess) // Result
			rw.WriteUint8(1)                          // Layer
			rw.WriteUint16(50)                         // ResetIngot
			rw.WriteUint8(0)                          // ResetNum
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDTowerResetLayer, rw.Bytes()))

		case protocol.ActionIDTowerStartAutoChallenge:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDTowerStartAutoChallenge, []byte{protocol.TowerResultSuccess}))

		case protocol.ActionIDTowerQuicklyAutoChallenge:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDTowerQuicklyAutoChallenge, []byte{protocol.TowerResultSuccess}))

		case protocol.ActionIDTowerStopAutoChallenge:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDTowerStopAutoChallenge, []byte{}))

		case protocol.ActionIDBeelzebubTrialsCheckCanChallenge:
			// 响应魔王试炼可挑战检查
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDBeelzebubTrialsCheckCanChallenge, []byte{protocol.BeelzebubTrialsSuccess}))

		case protocol.ActionIDBeelzebubTrialsGetCanChallengeList:
			// 响应可挑战魔王列表: 3个魔王 (1, 71, 72: 魔掌门神识)
			bw := protocol.NewWriter()
			bw.WriteUint16(3)
			bw.WriteInt32(1)
			bw.WriteInt32(71)
			bw.WriteInt32(72)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDBeelzebubTrialsGetCanChallengeList, bw.Bytes()))

		case protocol.ActionIDBeelzebubTrialsOpen:
			// 响应魔王试炼主面板
			ow := protocol.NewWriter()
			ow.WriteUint16(0) // 0 teams
			ow.WriteInt32(0)  // int32
			ow.WriteUint16(0) // 0 bosses in list
			ow.WriteInt32(5000) // moralExp
			ow.WriteInt32(3)    // moralLv
			ow.WriteInt32(1)    // bGet (canClaim)
			ow.WriteUint8(protocol.BeelzebubTrialsOnActivity)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDBeelzebubTrialsOpen, ow.Bytes()))

		case protocol.ActionIDBeelzebubTrialsCreateTeam:
			// 响应创建队伍成功
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDBeelzebubTrialsCreateTeam, []byte{protocol.BeelzebubTrialsSuccess}))

		case protocol.ActionIDBeelzebubTrialsJoinTeam:
			// 响应加入队伍成功
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDBeelzebubTrialsJoinTeam, []byte{protocol.BeelzebubTrialsSuccess}))

		case protocol.ActionIDBeelzebubTrialsGetPlayerTeamInfo:
			// 响应队伍成员信息 (队长1人)
			tiw := protocol.NewWriter()
			tiw.WriteInt32(1001) // leader ID
			tiw.WriteUint16(1)   // 1 member
			tiw.WriteInt32(1001) // playerID
			tiw.WriteString("大侠")
			tiw.WriteString("主角")
			tiw.WriteInt16(120)
			tiw.WriteInt32(0)
			tiw.WriteInt32(0)
			tiw.WriteInt32(0)
			tiw.WriteString("")
			tiw.WriteInt16(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDBeelzebubTrialsGetPlayerTeamInfo, tiw.Bytes()))

		case protocol.ActionIDBeelzebubTrialsGetHelperRoleList:
			// 响应助战伙伴列表 (2个助战镜像)
			hw := protocol.NewWriter()
			hw.WriteUint16(2)
			hw.WriteInt32(501)
			hw.WriteString("巨灵神镜像")
			hw.WriteInt32(110)
			hw.WriteInt32(1)
			hw.WriteUint8(protocol.BeelzebubTrialsNoDeploy)
			hw.WriteInt16(0)

			hw.WriteInt32(502)
			hw.WriteString("杨戬镜像")
			hw.WriteInt32(115)
			hw.WriteInt32(2)
			hw.WriteUint8(protocol.BeelzebubTrialsNoDeploy)
			hw.WriteInt16(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDBeelzebubTrialsGetHelperRoleList, hw.Bytes()))

		case protocol.ActionIDBeelzebubTrialsChooseHelperRole:
			// 响应选择助战成功
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDBeelzebubTrialsChooseHelperRole, []byte{protocol.BeelzebubTrialsSuccess}))

		case protocol.ActionIDBeelzebubTrialsFight:
			// 响应发起战斗成功
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDBeelzebubTrialsFight, []byte{protocol.BeelzebubTrialsSuccess}))

		case protocol.ActionIDBeelzebubTrialsGetWarResult:
			// 响应战斗结算 (击败魔王，奖励声望2000，铜钱100万，道行500，经验20万)
			ww := protocol.NewWriter()
			ww.WriteUint8(protocol.BeelzebubTrialsSuccess) // warResult
			ww.WriteInt32(0)                              // placeholder
			ww.WriteInt32(0)                              // placeholder
			ww.WriteUint16(0)                             // 0 war frames
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDBeelzebubTrialsGetWarResult, ww.Bytes()))

		case protocol.ActionIDBeelzebubTrialsLeaveWar:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDBeelzebubTrialsLeaveWar, []byte{}))

		case protocol.ActionIDBeelzebubTrialsExitTeam:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDBeelzebubTrialsExitTeam, []byte{protocol.BeelzebubTrialsSuccess}))

		case protocol.ActionIDBeelzebubTrialsOpenMoral:
			mw := protocol.NewWriter()
			mw.WriteInt32(6000) // exp
			mw.WriteInt32(2)    // lv
			mw.WriteInt32(1)    // bGet
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDBeelzebubTrialsOpenMoral, mw.Bytes()))

		case protocol.ActionIDBeelzebubTrialsGetMoralAward:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDBeelzebubTrialsGetMoralAward, []byte{protocol.BeelzebubTrialsSuccess}))
		}
	}
}
