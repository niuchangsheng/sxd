// Package client 提供特定区服角色的独立 TCP Socket 长连接会话、登录态维护与心跳保活状态机。
//
// @author Ateng
// @since 2026-10-08
package client

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
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
	// ErrAuthRejected 服务端明确拒绝登录凭据（resultCode 非成功值）。
	// 与网络闪断不同，该错误通常意味着票据失效或被顶号，需要重新换票。
	ErrAuthRejected = errors.New("client: authentication rejected by server")
	// ErrRoleSelectionRequired 服务端返回 FIRST_TIME：要求客户端选择/创建角色。
	// 自动化严禁替用户创建角色，只能等待用户在官方客户端选定角色后再登录。
	ErrRoleSelectionRequired = errors.New("client: role selection required (FIRST_TIME)")
)

const (
	// minCredentialRefreshInterval 两次 Web 换票之间的最小间隔。
	// 同一平台账号下的多个角色共用一次 Web 登录态，过于频繁换票会把
	// 另一角色的会话顶掉，形成「互相换票 → 互相踢下线」的活锁。
	minCredentialRefreshInterval = 2 * time.Minute
	// credentialRefreshFailureThreshold 普通断线连续失败多少次后才换票。
	// 正常 EOF 优先复用现有票据重连；只有连续失败才说明票据可能已失效。
	credentialRefreshFailureThreshold = 3
	// roleSelectionRetryInterval 服务端要求选角时的低频复查间隔。
	roleSelectionRetryInterval = 10 * time.Minute
	// sessionReleaseWait 登录握手超时后的最小等待，给网关释放旧会话留出时间。
	sessionReleaseWait = 20 * time.Second
)

const (
	// reconnectLongBackoffAfter 连续重连失败达到该次数后进入长时间退避。
	// 服务端持续拒绝握手（风控限流、区服维护、重复登录互踢）时，
	// 每 30 秒重试只会让风控更严；达到阈值后改为每 5 分钟探测一次。
	reconnectLongBackoffAfter = 10
	// reconnectLongBackoff 长时间退避的重试间隔上限。
	reconnectLongBackoff = 5 * time.Minute
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
	// Time1 跨服登录时间戳 (Mod_StLogin_Base 0x005E 第 4 字段)
	Time1 int32
	// Hash1 跨服验签串 passCode (Mod_StLogin_Base 0x005E 第 5 字段)
	Hash1 string
	// PlayerID 主服角色数字标识 (Mod_StLogin_Base 0x005E 第 2 字段)，跨服会话需显式由主服会话继承
	PlayerID int32
	// OriginNickName 带区服后缀的官方完整角色名 (Mod_StLogin_Base 0x005E 第 3 字段，如 "曹植始.s1")
	OriginNickName string

	// Extra 平台特定的扩展入场凭据 (例如官方 10 字段所需: regdate, id_card, open_time, is_newst 等)
	Extra map[string]string

	// HeartbeatInterval 心跳定时保活间隔 (默认 15s；2026-11-22 实机探针确认 0/75 为服务端静默吸收且空闲 12s+ 不断线)
	HeartbeatInterval time.Duration
	// HeartbeatTimeout 心跳超时时间 (默认 5s)
	HeartbeatTimeout time.Duration
	// HeartbeatActionID 心跳消息号 (默认 0x0000004B, Mod_Player_Base.heart)
	HeartbeatActionID uint32

	// ReconnectInterval 重连回退重试时间间隔 (默认 2s)
	ReconnectInterval time.Duration
	// MaxReconnectAttempts 最大重连尝试次数 (0 为无限重连)
	MaxReconnectAttempts int
	// SectQueryTimeout 主服仙盟只读查询超时 (默认 5s)。
	// 角色未加入仙盟时服务端对仙盟面板查询静默无回包，必须用短超时避免占用调用方的城镇会话预算。
	SectQueryTimeout time.Duration

	// Dialer 自定义网络拨号器接口（用于单元测试 net.Pipe 注入）
	Dialer func(ctx context.Context, network, addr string) (net.Conn, error)
	// Authenticator 自定义登录握手逻辑
	Authenticator AuthenticatorFunc
	// RefreshCredentials 凭据自愈刷新回调：在网络闪断或会话重连前自动换取最新一次性 Hash 凭据
	RefreshCredentials func(ctx context.Context, cfg *SessionConfig) error
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

	handlers       map[uint32][]func(*protocol.Packet)
	pendingCalls   map[uint32][]chan *protocol.Packet
	packetObserver func(*protocol.Packet)
	handlerMu      sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	reconnectAttempts int
	isCustomDialer    bool

	// 凭据自愈节流：记录最近一次 Web 换票时间、换票后连续失败次数与最近一次
	// 连接失败原因，避免普通 EOF 也触发 Web 登录（多角色同账号会互相顶号）。
	credRefreshMu        sync.Mutex
	lastCredRefresh      time.Time
	failuresSinceRefresh int
	lastConnectErr       error

	// prevAct 集中管理当前连接的业务动作尾链 (PrevAct) 与最近收包诊断缓冲。
	prevAct      *PrevActChain
	lastSendTime atomic.Int64
	// connGeneration 每次成功建立新连接后递增，供「主动重建会话」等待新连接使用。
	connGeneration atomic.Uint64
	// connSince 最近一次建立连接的时间戳 (UnixNano)，用于判断会话已存活多久。
	connSince atomic.Int64

	// factionUnavailable 标记本次会话内已确认帮派模块对当前角色不可用。
	// 服务端对未加入帮派角色的帮派类请求会静默丢弃（无任何回包），
	// 由首个超时的帮派任务置位，供其余帮派玩法直接跳过，避免重复空等。
	factionUnavailable atomic.Bool
	// factionAvailable 标记本次会话内已确认帮派上下文正常（如白檀香上香
	// 返回 SUCCESS/COUNT_USED 或帮派面板正常回包），用于避免重复探测。
	factionAvailable atomic.Bool

	// functionMu 保护已开放功能列表缓存 (Mod_Player_Base.get_player_function)。
	functionMu   sync.RWMutex
	functionOpen map[int32]bool
	functionAt   time.Time
}

// NewRoleSession 构造一个新的角色会话实例。
func NewRoleSession(cfg SessionConfig) *RoleSession {
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 15 * time.Second
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
	if cfg.SectQueryTimeout <= 0 {
		cfg.SectQueryTimeout = 5 * time.Second
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
		prevAct:        NewPrevActChain(cfg.HeartbeatActionID),
		// 启动时由 main 统一换取过票据，此处给出最小换票间隔，
		// 避免首个连接抖动就立刻再登录一次 Web。
		lastCredRefresh: time.Now(),
		handlers:        make(map[uint32][]func(*protocol.Packet)),
		pendingCalls:    make(map[uint32][]chan *protocol.Packet),
		playerState: PlayerState{
			Level:           300,
			Stamina:         201,
			MaxStamina:      300,
			Coins:           36226117234,
			Ingots:          39409,
			BagCapacity:     20,
			Quests:          make(map[uint32]uint32),
			IsGetOnlineGift: -2, // 与客户端 PlayerData 初值一致: 尚未收到 update_player_data
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

	// 注册角色属性与体力更新动态通知监听器 (Module 0, Action 3)
	s.RegisterHandler(protocol.ActionIDPlayerUpdateData, func(p *protocol.Packet) {
		if p == nil {
			return
		}
		s.applyPlayerUpdateData(p.Payload)
	})

	return s
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

// SetPacketObserver 注册全量入站报文观察器，用于探针与故障诊断时记录服务端所有下发动作。
// 观察器在报文分发给业务挂起调用与注册处理器之前同步执行，回调内请勿阻塞。
func (s *RoleSession) SetPacketObserver(fn func(*protocol.Packet)) {
	s.handlerMu.Lock()
	defer s.handlerMu.Unlock()
	s.packetObserver = fn
}

// defaultCallTimeout 调用方未携带 deadline 时的兜底等待上限。
//
// 服务端对不满足条件/场景不符的请求会静默丢弃。若某个协议封装直接透传 Routine
// 的调度上下文 (无 deadline)，Call 将永久阻塞，冻结该角色的整个调度协程
// (2026-11-10 叶公好龙卡死事故)。业务侧仍应显式设置更短的超时，这里仅作
// isMockAddr 判断当前会话是否运行在离线 mock/沙箱模式。
func (s *RoleSession) isMockAddr() bool {
	return s.cfg.ServerAddr == "" || s.cfg.ServerAddr == "mock" || s.cfg.ServerAddr == "dry-run" || s.cfg.ServerAddr == "sandbox"
}

// callTimeout 根据当前会话模式返回业务超时（离线 mock/沙箱使用更短超时）。
func (s *RoleSession) callTimeout(prod, mock time.Duration) time.Duration {
	if s.isMockAddr() {
		return mock
	}
	return prod
}

// 最后一道防线，避免一次漏设超时拖垮全部玩法。
var defaultCallTimeout = 30 * time.Second

// CallWithPrevActResync 发送需要 PrevAct 链的请求；若被服务端静默丢弃
// （表现为超时），用「重试时刻最新的一条锚点」以 PrevActExplicit 显式重试一次，
// 避免会话层再次覆盖。两次尝试各自使用独立的 timeout 上下文。
func (s *RoleSession) CallWithPrevActResync(parent context.Context, timeout time.Duration, req *protocol.Packet, respActionID uint32) (*protocol.Packet, error) {
	// 首次发送前快照锚点：重连后 lastRx 为 0 时沿用同一连接内的前序发送动作，
	// 绝不退回旧连接残留锚点。
	fallbackAnchor := s.prevAct.SnapshotResyncFallback()

	c1, cancel1 := context.WithTimeout(parent, timeout)
	resp, err := s.Call(c1, req, respActionID)
	cancel1()
	if err == nil {
		return resp, nil
	}
	// 仅在单次请求超时（疑似服务端静默丢弃）且上层上下文未取消时重同步重试；
	// 断线、会话关闭或父上下文取消直接返回，绝不无意义重试。
	if !errors.Is(err, context.DeadlineExceeded) || parent.Err() != nil {
		return nil, err
	}
	if req == nil || req.EffectivePrevActMode() != protocol.PrevActAutoBusiness || len(req.Payload) < 4 {
		return nil, err
	}
	anchor := s.prevAct.ResyncAnchor(fallbackAnchor)
	if anchor == 0 {
		return nil, err
	}
	lastSent := s.LastActionID()
	retry := protocol.NewPacketWithExplicitPrevAct(req.ActionID, req.Payload[:len(req.Payload)-4], anchor)
	c2, cancel2 := context.WithTimeout(parent, timeout)
	defer cancel2()
	resp, retryErr := s.Call(c2, retry, respActionID)
	slog.Info("PrevAct 重同步重试", "role_id", s.RoleID(),
		"action", fmt.Sprintf("0x%08X", req.ActionID),
		"last_sent", fmt.Sprintf("0x%08X", lastSent),
		"anchor", fmt.Sprintf("0x%08X", anchor),
		"recent_rx", fmt.Sprintf("%X", s.RecentRxActions()),
		"ok", retryErr == nil, "err", retryErr)
	if retryErr != nil {
		return nil, retryErr
	}
	return resp, nil
}

// RecentRxActions 返回最近收到的包 ActionID（按时间先后，最新的在最后）。
func (s *RoleSession) RecentRxActions() []uint32 {
	return s.prevAct.RecentRx()
}

// Call 发送指定请求封包并同步等待特定 ActionID 的对端回包。
func (s *RoleSession) Call(ctx context.Context, req *protocol.Packet, respActionID uint32) (*protocol.Packet, error) {
	// 兜底超时: 调用方漏设 deadline 时也要能返回，绝不无限等待。
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		callCtx, cancel := context.WithTimeout(ctx, defaultCallTimeout)
		defer cancel()
		ctx = callCtx
	}

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
		// 带参请求超时通常意味着服务端静默丢弃：客户端锚点已前进而服务端链
		// 未前进。标记失步，下一次带参请求回退到 lastAckActionID，避免连环丢包。
		if s.prevAct.UsesBusinessChain(req) {
			s.prevAct.MarkDirty()
		}
		return nil, ctx.Err()
	case <-s.ctx.Done():
		return nil, ErrSessionClosed
	case p := <-ch:
		return p, nil
	}
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

// MarkFactionFeatureUnavailable 标记本次会话内帮派模块对当前角色不可用。
// 触发条件为帮派类只读请求静默无回包（典型原因：角色未加入帮派）。
func (s *RoleSession) MarkFactionFeatureUnavailable() {
	s.factionUnavailable.Store(true)
}

// IsFactionFeatureUnavailable 返回本次会话内是否已确认帮派模块不可用。
func (s *RoleSession) IsFactionFeatureUnavailable() bool {
	return s.factionUnavailable.Load()
}

// MarkFactionFeatureAvailable 标记本次会话内已确认帮派上下文正常。
func (s *RoleSession) MarkFactionFeatureAvailable() {
	s.factionAvailable.Store(true)
}

// IsFactionFeatureAvailable 返回本次会话内是否已确认帮派上下文正常。
func (s *RoleSession) IsFactionFeatureAvailable() bool {
	return s.factionAvailable.Load()
}

// LastActionID 获取最后一次向对端发送的协议 ActionID（含心跳）。
func (s *RoleSession) LastActionID() uint32 {
	return s.prevAct.LastSent()
}

// LastBusinessActionID 获取最后一次发送的业务动作 ActionID（不含心跳 0/75）。
// 业务请求的 PrevAct 尾链应使用该值：心跳不参与服务端的业务尾链校验。
func (s *RoleSession) LastBusinessActionID() uint32 {
	return s.prevAct.LastBusiness()
}

// ConnectionGeneration 返回当前连接代数；每建立一条新连接递增。
// 主动 ReconnectNow 后可等待该值增长以确认新会话已建立。
func (s *RoleSession) ConnectionGeneration() uint64 {
	return s.connGeneration.Load()
}

// ConnectionSince 返回当前连接建立的时间；尚未连接时返回零值。
func (s *RoleSession) ConnectionSince() time.Time {
	ns := s.connSince.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

// HealthySessionAge 是重型玩法建议使用的会话健康窗口。
//
// 2026-11-24 对 11 小时日志做的相关性分析：同一批无量劫海请求在会话年龄
// 中位数 6 秒时全部成功，而在会话年龄中位数 250 秒（>4 分钟）时会被服务端
// 静默丢弃或直接断开连接。取 3 分钟留出安全余量。
const HealthySessionAge = 3 * time.Minute

// EnsureFreshConnection 确保当前会话年龄不超过 maxAge。
//
// 若已超过健康窗口，主动重建连接并等待新一代连接进入 Active：重型玩法
// （无量劫海/飞升灌注/魔界远征等）在旧会话上会被服务端丢弃甚至断链，
// 在登录后的早期窗口内才能稳定完成。等待超时返回 ErrNotConnected，
// 由调度器按 5 分钟短重试。
func (s *RoleSession) EnsureFreshConnection(ctx context.Context, maxAge time.Duration) error {
	if maxAge <= 0 {
		maxAge = HealthySessionAge
	}
	if s.State() != StateActive {
		return ErrNotConnected
	}
	since := s.ConnectionSince()
	age := time.Duration(0)
	if !since.IsZero() {
		age = time.Since(since)
	}
	if age <= maxAge {
		return nil
	}

	slog.Info("会话已超过健康窗口，主动重建后再执行重型玩法",
		"role_id", s.cfg.RoleID,
		"age", age.Round(time.Second),
		"max_age", maxAge,
	)
	gen := s.ConnectionGeneration()
	s.ReconnectNow()

	deadline := time.Now().Add(60 * time.Second)
	for {
		if s.ConnectionGeneration() > gen && s.State() == StateActive {
			return nil
		}
		if s.State() == StateClosed {
			return ErrNotConnected
		}
		if time.Now().After(deadline) {
			return ErrNotConnected
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// SetLastActionID 手动设置最后一次向对端发送的协议 ActionID。
func (s *RoleSession) SetLastActionID(actionID uint32) {
	s.prevAct.SetManualLastSent(actionID)
}

// LastRxActionID 获取最后一次收到的网关包 ActionID（含推送与心跳回包）。
// 仅用于诊断；业务 PrevAct 链应使用 LastBusinessActionID。
func (s *RoleSession) LastRxActionID() uint32 {
	return s.prevAct.LastRx()
}

// LastAckActionID 获取最后一次收到服务端回包的业务 ActionID。
// 用于需要与服务端 PrevAct 链严格对齐的协议锚点。
func (s *RoleSession) LastAckActionID() uint32 {
	return s.prevAct.LastAck()
}

// Send 向对端发送一个未压缩协议封包。

// setWriteDeadline 为当前连接设置写超时。
//
// 服务端限流/停止读取时 TCP 发送缓冲区会被填满，conn.Write 会永久阻塞并
// 持有 writeMu，导致整个会话（含心跳与调度）冻结。写超时可让 Send 返回，
// 由上层重连逻辑恢复。
func (s *RoleSession) setWriteDeadline() {
	if s.conn != nil {
		_ = s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	}
}

func (s *RoleSession) Send(p *protocol.Packet) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if s.State() == StateClosed {
		return ErrSessionClosed
	}
	if s.conn == nil {
		return ErrNotConnected
	}
	s.setWriteDeadline()

	p = s.prevAct.StampPayload(p)

	err := protocol.WritePacket(s.conn, p)
	if err == nil && p != nil {
		s.lastSendTime.Store(time.Now().UnixNano())
		s.prevAct.RecordSent(p.ActionID)
		slog.Debug("发送网关请求", "role_id", s.cfg.RoleID, "action_id", fmt.Sprintf("0x%08X (Mod %d, Act %d)", p.ActionID, p.ActionID>>16, p.ActionID&0xFFFF), "payload_hex", hex.EncodeToString(p.Payload))
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
	s.setWriteDeadline()

	err := protocol.WriteCompressedPacket(s.conn, p)
	if err == nil && p != nil {
		s.lastSendTime.Store(time.Now().UnixNano())
		s.prevAct.RecordSent(p.ActionID)
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
	s.setWriteDeadline()

	_, err := s.conn.Write(data)
	if err == nil {
		s.lastSendTime.Store(time.Now().UnixNano())
		if len(data) >= 8 {
			actID := binary.BigEndian.Uint32(data[4:8])
			s.prevAct.RecordSent(actID)
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

// ReconnectNow 主动断开当前连接，触发状态机按原配置重新登录。
//
// 用途：服务端会话上下文被污染后（例如 852/16 被丢弃），后续 Mod 4 请求会
// 持续被静默丢弃，客户端无法在同一连接内恢复；重连可获取全新的会话上下文。
// 重连由 runLoop 自行完成（含凭据自愈），调用方只需等待会话重新激活。
func (s *RoleSession) ReconnectNow() {
	if s.State() == StateClosed {
		return
	}
	slog.Info("主动断开连接以重建服务端会话上下文", "role_id", s.cfg.RoleID)
	s.closeCurrentConn()
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

		// 1. 若处于重连状态且配置了自愈回调，按需换取最新平台动态凭据。
		// 注意：普通 EOF 不应每次重连都重新 Web 登录——同一平台账号下多个角色
		// 共用一次登录态，频繁换票会把另一角色顶下线（2026-11-21 s0/s1 实测：
		// 一个角色的换票循环把另一个角色的握手持续顶掉，resultCode=2）。
		if s.State() == StateReconnecting && s.cfg.RefreshCredentials != nil && s.shouldRefreshCredentials() {
			slog.Info("正在通过自愈管道重新换取最新平台动态凭据...", "role_id", s.cfg.RoleID)
			if rErr := s.cfg.RefreshCredentials(s.ctx, &s.cfg); rErr != nil {
				slog.Warn("凭据自愈换票未成功", "role_id", s.cfg.RoleID, "error", rErr)
			} else {
				s.markCredentialRefreshed()
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
		s.connGeneration.Add(1)
		s.connSince.Store(time.Now().UnixNano())
		// 新连接的 PrevAct 链在服务端从零开始，必须清掉上一条连接的全部锚点状态。
		s.prevAct.Reset()
		s.writeMu.Unlock()

		// 2. 启动读循环通道
		readErrCh := make(chan error, 1)
		go s.readLoop(conn, readErrCh)

		// 3. 执行认证握手
		s.setState(StateAuthenticating)
		if s.cfg.Authenticator != nil {
			if err := s.cfg.Authenticator(s); err != nil {
				slog.Info("角色身份认证失败", "role_id", s.cfg.RoleID, "error", err)
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

// reconnectWait 在指数退避基础上，针对「登录握手超时」给出更长的等待。
//
// 网关在旧会话未释放时仍会接受 TCP 连接，但对 login 请求 15 秒内不回包；
// 2026-11-22 实测断线后立即重连会连续触发 3~4 次握手超时（每次 15 秒），
// 既拖慢恢复又增加登录频次。这里对握手超时至少等待 sessionReleaseWait，
// 让服务端先释放旧会话，再发起下一次登录。
func reconnectWait(err error, attempt int, base time.Duration) time.Duration {
	wait := nextReconnectDelay(attempt, base)
	if errors.Is(err, context.DeadlineExceeded) && wait < sessionReleaseWait {
		wait = sessionReleaseWait
	}
	return wait
}

// nextReconnectDelay 计算第 attempt 次连续重连失败后的等待时长。
//
// base 为配置的 ReconnectInterval；前置指数退避（×1/2/4/8/16），
// 常规上限 30 秒；连续失败达到 reconnectLongBackoffAfter 次后上限放宽到 5 分钟。
func nextReconnectDelay(attempt int, base time.Duration) time.Duration {
	if base <= 0 {
		base = 2 * time.Second
	}
	if attempt < 1 {
		attempt = 1
	}
	if attempt >= reconnectLongBackoffAfter {
		return reconnectLongBackoff
	}
	multiplier := 1
	if attempt > 1 {
		multiplier = 1 << min(attempt-1, 4)
	}
	wait := base * time.Duration(multiplier)
	if wait > 30*time.Second {
		wait = 30 * time.Second
	}
	return wait
}

// shouldRefreshCredentials 判断本次重连前是否需要重新向平台换票。
//
//   - 距上次换票不足 minCredentialRefreshInterval：不换，避免多角色同账号互相顶号；
//   - 上一次失败是服务端明确拒绝认证：尽快换票（票据多半已失效）；
//   - 普通断线（EOF/超时）：优先复用现有票据，连续失败达到阈值后才换票。
func (s *RoleSession) shouldRefreshCredentials() bool {
	s.credRefreshMu.Lock()
	since := time.Since(s.lastCredRefresh)
	failures := s.failuresSinceRefresh
	lastErr := s.lastConnectErr
	s.credRefreshMu.Unlock()

	if since < minCredentialRefreshInterval {
		return false
	}
	// 服务端要求选角：换票无济于事，只会制造登录抖动。
	if errors.Is(lastErr, ErrRoleSelectionRequired) {
		return false
	}
	if errors.Is(lastErr, ErrAuthRejected) {
		return true
	}
	return failures >= credentialRefreshFailureThreshold
}

// markCredentialRefreshed 记录一次成功的 Web 换票。
func (s *RoleSession) markCredentialRefreshed() {
	s.credRefreshMu.Lock()
	s.lastCredRefresh = time.Now()
	s.failuresSinceRefresh = 0
	s.lastConnectErr = nil
	s.credRefreshMu.Unlock()
}

// noteConnectFailure 记录连接失败原因，供换票节流策略判断。
func (s *RoleSession) noteConnectFailure(err error) {
	s.credRefreshMu.Lock()
	s.failuresSinceRefresh++
	s.lastConnectErr = err
	s.credRefreshMu.Unlock()
}

func (s *RoleSession) handleConnectFailure(err error) bool {
	select {
	case <-s.ctx.Done():
		s.setState(StateClosed)
		return false
	default:
	}

	s.reconnectAttempts++
	s.noteConnectFailure(err)
	if s.cfg.MaxReconnectAttempts > 0 && s.reconnectAttempts >= s.cfg.MaxReconnectAttempts {
		slog.Error("超过最大重连尝试次数，会话终止",
			"role_id", s.cfg.RoleID,
			"server_addr", s.cfg.ServerAddr,
			"attempts", s.reconnectAttempts,
			"error", err,
		)
		s.setState(StateClosed)
		return false
	}

	s.setState(StateReconnecting)

	// 计算指数退避等待时间：从 ReconnectInterval 开始递增。
	// 前 10 次重试上限 30 秒（网关短暂抖动/跨服切换常见）；
	// 连续 10 次以上仍失败时说明不是瞬时故障（例如服务端持续拒绝握手、
	// 风控限流），把上限拉到 5 分钟，避免每 30 秒反复登录触发更强风控。
	waitDuration := reconnectWait(err, s.reconnectAttempts, s.cfg.ReconnectInterval)
	// 服务端要求选择/创建角色：这是需要用户人工处理的状态，30 秒重试毫无意义，
	// 固定按 10 分钟低频复查（用户可能在官方客户端选好角色后自动恢复）。
	if errors.Is(err, ErrRoleSelectionRequired) {
		waitDuration = roleSelectionRetryInterval
		slog.Warn("服务端要求选择/创建角色，自动化不会创建角色；请在官方客户端选定角色后等待自动重连",
			"role_id", s.cfg.RoleID,
			"server_addr", s.cfg.ServerAddr,
			"retry_in", waitDuration,
		)
	}

	slog.Info("网络连接中断，等待重连",
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
			s.credRefreshMu.Lock()
			s.failuresSinceRefresh = 0
			s.lastConnectErr = nil
			s.credRefreshMu.Unlock()

		case err := <-readErrCh:
			slog.Info("网络读取循环退出", "role_id", s.cfg.RoleID, "error", err)
			s.closeCurrentConn()
			return s.handleConnectFailure(err)

		case <-ticker.C:
			// 仅在真实空闲期（距上次发包超过心跳间隔）才发送保活心跳包：
			//   - 常规业务请求 (< HeartbeatInterval) 期间不会插入心跳；
			//   - 若遇到长超时静默丢包 (> HeartbeatInterval)，仍允许发送保活包，避免连接被中间件/网关回收。
			lastSend := time.Unix(0, s.lastSendTime.Load())
			if time.Since(lastSend) < s.cfg.HeartbeatInterval {
				continue
			}

			// 发送心跳包：官方 Mod_Player_Base.heart 请求为空载荷 (request=[]),
			// 不能覆盖 lastActionID 的末 4 字节，否则会被服务端当作非法参数。
			heartbeat := protocol.NewPacket(s.cfg.HeartbeatActionID, nil)
			if err := s.Send(heartbeat); err != nil {
				slog.Warn("发送心跳失败", "role_id", s.cfg.RoleID, "error", err)
				s.closeCurrentConn()
				return s.handleConnectFailure(err)
			}
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

		s.prevAct.RecordRx(pkt.ActionID)
		hexDump := hex.EncodeToString(pkt.Payload)
		if len(hexDump) > 64 {
			hexDump = hexDump[:64] + "..."
		}
		slog.Debug("收到网关回包", "role_id", s.cfg.RoleID, "action_id", fmt.Sprintf("0x%08X (Mod %d, Act %d)", pkt.ActionID, pkt.ActionID>>16, pkt.ActionID&0xFFFF), "payload_len", len(pkt.Payload), "hex", hexDump)
		s.dispatchPacket(pkt)
	}
}

func (s *RoleSession) dispatchPacket(pkt *protocol.Packet) {
	s.handlerMu.Lock()
	observer := s.packetObserver
	if calls, ok := s.pendingCalls[pkt.ActionID]; ok && len(calls) > 0 {
		ch := calls[0]
		s.pendingCalls[pkt.ActionID] = calls[1:]
		// 只有真正被服务端处理并回包的非心跳业务动作才推进确认锚点并解除失步标记。
		s.prevAct.RecordAck(pkt.ActionID)
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

	if observer != nil {
		observer(pkt)
	}
	for _, h := range handlers {
		h(pkt)
	}
}
