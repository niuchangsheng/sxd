package client

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/protocol"
)

// TestClose_OnBeforeCloseSendsLeaveTown 验证跨服城镇会话 Close 时，onBeforeClose 能在
// 会话尚未进入 StateClosed、底层连接仍存活时成功发送 95/2 leave_town 并收到服务端确认。
func TestClose_OnBeforeCloseSendsLeaveTown(t *testing.T) {
	serverConn, dialConn := net.Pipe()
	defer serverConn.Close()

	leaveReceived := make(chan struct{}, 1)
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDStLogin:
				w := protocol.NewWriter()
				w.WriteUint8(0)
				w.WriteInt32(65536)
				w.WriteInt32(1793549704)
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDStLogin, w.Bytes()))
			case protocol.ActionIDStTownEnter:
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDStTownEnter, []byte{protocol.StTownResultSuccess}))
			case protocol.ActionIDStTownLeave:
				select {
				case leaveReceived <- struct{}{}:
				default:
				}
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDStTownLeave, []byte{protocol.StTownResultSuccess}))
			}
		}
	}()

	main := NewRoleSession(SessionConfig{
		RoleID:     "role-leave-town",
		RoleName:   "测试角色",
		ServerAddr: "main-gateway",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialConn, nil
		},
	})
	main.UpdatePlayerState(func(ps *PlayerState) {
		ps.PlayerID = 65536
		ps.OriginNickName = "测试角色.s1"
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cross, err := main.OpenCrossServerSessionWithInfo(ctx, &protocol.StCrossServerInfo{
		Host:       "9x703.sxdweb.xd.com",
		Port:       8480,
		ServerName: "s703",
		Time:       1793549704,
		PassCode:   "mock_pass_code",
	})
	if err != nil {
		t.Fatalf("OpenCrossServerSessionWithInfo failed: %v", err)
	}

	cross.Close()

	select {
	case <-leaveReceived:
	case <-time.After(time.Second):
		t.Fatal("跨服城镇会话 Close 时未向服务端发送 95/2 leave_town")
	}
	<-serverDone
}

// TestCall_WakesImmediatelyOnDisconnect 验证底层 TCP 断开时，挂起中的 Call 立刻返回
// ErrNotConnected，而不是死等到业务 ctx 超时后误报 context.DeadlineExceeded。
func TestCall_WakesImmediatelyOnDisconnect(t *testing.T) {
	c1, s1 := net.Pipe()
	c2, s2 := net.Pipe()
	defer c2.Close()
	defer s2.Close()

	var dialCount atomic.Int32
	sess := NewRoleSession(SessionConfig{
		RoleID:            "role-call-eof",
		ServerAddr:        "mock-pipe",
		HeartbeatInterval: time.Hour,
		ReconnectInterval: 20 * time.Millisecond,
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if dialCount.Add(1) == 1 {
				return c1, nil
			}
			return c2, nil
		},
		Authenticator: func(s *RoleSession) error { return nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sess.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sess.Close()
	if err := sess.AwaitActive(ctx); err != nil {
		t.Fatalf("await active: %v", err)
	}

	const testAction uint32 = 0x00120001
	go func() {
		// 读到请求后直接断开连接（EOF），不回包。
		_, _ = protocol.ReadPacket(s1)
		_ = s1.Close()
	}()

	callCtx, callCancel := context.WithTimeout(ctx, 10*time.Second)
	defer callCancel()
	start := time.Now()
	_, err := sess.Call(callCtx, protocol.NewPacket(testAction, nil), testAction)
	elapsed := time.Since(start)

	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("断线后 Call 应返回 ErrNotConnected, got %v", err)
	}
	if elapsed > time.Second {
		t.Fatalf("断线后 Call 应立刻返回而非死等超时, took %v", elapsed)
	}
}

// TestCall_NoCrossConnectionStaleWaiter 验证旧连接断开时挂起的 pendingCalls 会被排空，
// 不会滞留到新连接上抢占同 ActionID 的回包。
func TestCall_NoCrossConnectionStaleWaiter(t *testing.T) {
	c1, s1 := net.Pipe()
	c2, s2 := net.Pipe()
	defer s1.Close()
	defer s2.Close()

	var dialCount atomic.Int32
	sess := NewRoleSession(SessionConfig{
		RoleID:            "role-stale-waiter",
		ServerAddr:        "mock-pipe",
		HeartbeatInterval: time.Hour,
		ReconnectInterval: 20 * time.Millisecond,
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if dialCount.Add(1) == 1 {
				return c1, nil
			}
			return c2, nil
		},
		Authenticator: func(s *RoleSession) error { return nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sess.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sess.Close()
	if err := sess.AwaitActive(ctx); err != nil {
		t.Fatalf("await active: %v", err)
	}

	const testAction uint32 = 0x00150002
	// 连接 1：收到请求后不回包直接断开。
	go func() {
		_, _ = protocol.ReadPacket(s1)
		_ = s1.Close()
	}()
	// 连接 2：收到请求后正常回复 "gen2"。
	go func() {
		pkt, err := protocol.ReadPacket(s2)
		if err != nil {
			return
		}
		_ = protocol.WritePacket(s2, protocol.NewPacket(pkt.ActionID, []byte("gen2")))
	}()

	gen1 := sess.ConnectionGeneration()
	_, err1 := sess.Call(ctx, protocol.NewPacket(testAction, nil), testAction)
	if !errors.Is(err1, ErrNotConnected) {
		t.Fatalf("旧连接 Call 期望 ErrNotConnected, got %v", err1)
	}

	// 等待连接 2 激活。
	deadline := time.Now().Add(2 * time.Second)
	for (sess.ConnectionGeneration() <= gen1 || sess.State() != StateActive) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if sess.ConnectionGeneration() <= gen1 || sess.State() != StateActive {
		t.Fatalf("未恢复到新连接: gen=%d state=%s", sess.ConnectionGeneration(), sess.State())
	}

	resp, err2 := sess.Call(ctx, protocol.NewPacket(testAction, nil), testAction)
	if err2 != nil {
		t.Fatalf("新连接 Call 失败: %v", err2)
	}
	if string(resp.Payload) != "gen2" {
		t.Fatalf("新连接回包异常: got %q, want %q", string(resp.Payload), "gen2")
	}
}

// TestGatewayHandshake_FailsFastOnEOF 验证网关在登录包后立即断链（EOF）时，
// DefaultGatewayAuthenticator 立刻失败返回，不白等 15 秒握手超时。
func TestGatewayHandshake_FailsFastOnEOF(t *testing.T) {
	c1, s1 := net.Pipe()
	defer s1.Close()

	go func() {
		_, _ = protocol.ReadPacket(s1)
		_ = s1.Close()
	}()

	sess := NewRoleSession(SessionConfig{
		RoleID:               "role-handshake-eof",
		Username:             "tester",
		Token:                "tok",
		ServerAddr:           "gateway-eof-pipe",
		MaxReconnectAttempts: 1,
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return c1, nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	start := time.Now()
	if err := sess.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sess.Close()

	deadline := time.Now().Add(2 * time.Second)
	for sess.State() != StateClosed && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	elapsed := time.Since(start)
	if sess.State() != StateClosed {
		t.Fatalf("期望握手 EOF 后快速终止, current state=%s", sess.State())
	}
	if elapsed > 2*time.Second {
		t.Fatalf("握手 EOF 不应等待 15s 超时, took %v", elapsed)
	}
}

// TestReconnectNow_SkipsFailureBackoff 验证主动 ReconnectNow 不被当成连接失败，
// 不触发长退避休眠即可立刻重建。
func TestReconnectNow_SkipsFailureBackoff(t *testing.T) {
	c1, s1 := net.Pipe()
	c2, s2 := net.Pipe()
	defer s1.Close()
	defer s2.Close()

	var dialCount atomic.Int32
	sess := NewRoleSession(SessionConfig{
		RoleID:            "role-intentional-reconnect",
		ServerAddr:        "mock-pipe",
		HeartbeatInterval: time.Hour,
		ReconnectInterval: 5 * time.Second, // 若误走失败退避，将阻塞 5 秒
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if dialCount.Add(1) == 1 {
				return c1, nil
			}
			return c2, nil
		},
		Authenticator: func(s *RoleSession) error { return nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sess.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sess.Close()
	if err := sess.AwaitActive(ctx); err != nil {
		t.Fatalf("await active: %v", err)
	}

	gen := sess.ConnectionGeneration()
	start := time.Now()
	sess.ReconnectNow()

	deadline := time.Now().Add(1500 * time.Millisecond)
	for (sess.ConnectionGeneration() <= gen || sess.State() != StateActive) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if sess.ConnectionGeneration() <= gen || sess.State() != StateActive {
		t.Fatalf("主动重连不应等待 5s 失败退避, elapsed=%v gen=%d state=%s",
			time.Since(start), sess.ConnectionGeneration(), sess.State())
	}
}

// TestEnsureFreshConnection_SkipsTeardownIfAlreadyReconnectedDuringRefresh 验证在换票期间
// 底层已自然重连生成新连接时，EnsureFreshConnection 直接复用该新连接，不再误拆。
func TestEnsureFreshConnection_SkipsTeardownIfAlreadyReconnectedDuringRefresh(t *testing.T) {
	c1, s1 := net.Pipe()
	c2, s2 := net.Pipe()
	c3, s3 := net.Pipe()
	defer s1.Close()
	defer s2.Close()
	defer s3.Close()
	defer c3.Close()

	var dialCount atomic.Int32
	var sess *RoleSession
	sess = NewRoleSession(SessionConfig{
		RoleID:            "role-refresh-race",
		ServerAddr:        "mock-pipe",
		HeartbeatInterval: time.Hour,
		ReconnectInterval: 10 * time.Millisecond,
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			switch dialCount.Add(1) {
			case 1:
				return c1, nil
			case 2:
				return c2, nil
			default:
				return c3, nil
			}
		},
		Authenticator: func(s *RoleSession) error { return nil },
		RefreshCredentials: func(ctx context.Context, cfg *SessionConfig) error {
			// 模拟换票期间旧连接自然断开并由 runLoop 恢复到第 2 代连接。
			_ = s1.Close()
			deadline := time.Now().Add(2 * time.Second)
			for sess.ConnectionGeneration() < 2 && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			return nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sess.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sess.Close()
	if err := sess.AwaitActive(ctx); err != nil {
		t.Fatalf("await active: %v", err)
	}

	sess.connSince.Store(time.Now().Add(-10 * time.Minute).UnixNano())
	if err := sess.EnsureFreshConnection(ctx, HealthySessionAge); err != nil {
		t.Fatalf("EnsureFreshConnection failed: %v", err)
	}
	if got := dialCount.Load(); got != 2 {
		t.Fatalf("换票期间已自然恢复到第 2 代连接，不应再次拆建第 3 代连接, dialCount=%d", got)
	}
}

// TestConcurrentRefreshCredentialsAndSnapshotConfig_NoRace 验证并发换票写 s.cfg 与
// 子会话读取 snapshotConfig (含 Extra map) 在 -race 下无数据竞争。
func TestConcurrentRefreshCredentialsAndSnapshotConfig_NoRace(t *testing.T) {
	sess := NewRoleSession(SessionConfig{
		RoleID:     "role-cfg-race",
		ServerAddr: "127.0.0.1:8000",
		Extra:      map[string]string{"stage": "a"},
		RefreshCredentials: func(ctx context.Context, cfg *SessionConfig) error {
			cfg.ServerAddr = "127.0.0.1:8001"
			cfg.Hash = "new-hash"
			cfg.Extra = map[string]string{"stage": "b", "k": "v"}
			return nil
		},
	})

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = sess.runRefreshCredentials(context.Background())
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				snap := sess.snapshotConfig()
				_ = snap.ServerAddr
				_ = snap.Extra["stage"]
			}
		}()
	}
	wg.Wait()
}

// TestHalfOpenZombieProbe_TriggersReconnect 验证连接空闲无下行包达到 StaleProbeInterval 后，
// 若 0/6 活性探针在 HeartbeatTimeout 内无回包（半开僵尸连接），会话会主动断开并重连。
func TestHalfOpenZombieProbe_TriggersReconnect(t *testing.T) {
	c1, s1 := net.Pipe()
	c2, s2 := net.Pipe()
	defer s1.Close()
	defer s2.Close()

	// 连接 1 模拟半开黑洞：吃掉所有请求（含 0/6 探针）但不回任何包。
	go func() {
		for {
			if _, err := protocol.ReadPacket(s1); err != nil {
				return
			}
		}
	}()
	// 连接 2 正常响应 0/6 探针。
	go func() {
		for {
			pkt, err := protocol.ReadPacket(s2)
			if err != nil {
				return
			}
			if pkt.ActionID == protocol.ActionIDPlayerGetFunctions {
				_ = protocol.WritePacket(s2, protocol.NewPacket(protocol.ActionIDPlayerGetFunctions, []byte{0, 0}))
			}
		}
	}()

	var dialCount atomic.Int32
	sess := NewRoleSession(SessionConfig{
		RoleID:             "role-zombie-probe",
		ServerAddr:         "mock-pipe",
		HeartbeatInterval:  25 * time.Millisecond,
		HeartbeatTimeout:   60 * time.Millisecond,
		StaleProbeInterval: 40 * time.Millisecond,
		ReconnectInterval:  10 * time.Millisecond,
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if dialCount.Add(1) == 1 {
				return c1, nil
			}
			return c2, nil
		},
		Authenticator: func(s *RoleSession) error { return nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sess.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sess.Close()
	if err := sess.AwaitActive(ctx); err != nil {
		t.Fatalf("await active: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for (sess.ConnectionGeneration() < 2 || sess.State() != StateActive) && time.Now().Before(deadline) {
		time.Sleep(15 * time.Millisecond)
	}
	if sess.ConnectionGeneration() < 2 || sess.State() != StateActive {
		t.Fatalf("半开僵尸连接探针超时后应自动重连到第 2 代连接, gen=%d state=%s",
			sess.ConnectionGeneration(), sess.State())
	}
}

// TestHalfOpenZombieProbe_HealthyRepliesKeepConnAlive 验证健康连接在 0/6 活性探针正常回包时
// 保持原连接不触发误重连。
func TestHalfOpenZombieProbe_HealthyRepliesKeepConnAlive(t *testing.T) {
	c1, s1 := net.Pipe()
	defer s1.Close()

	var probeReplies atomic.Int32
	go func() {
		for {
			pkt, err := protocol.ReadPacket(s1)
			if err != nil {
				return
			}
			if pkt.ActionID == protocol.ActionIDPlayerGetFunctions {
				probeReplies.Add(1)
				_ = protocol.WritePacket(s1, protocol.NewPacket(protocol.ActionIDPlayerGetFunctions, []byte{0, 0}))
			}
		}
	}()

	var dialCount atomic.Int32
	sess := NewRoleSession(SessionConfig{
		RoleID:             "role-healthy-probe",
		ServerAddr:         "mock-pipe",
		HeartbeatInterval:  25 * time.Millisecond,
		HeartbeatTimeout:   100 * time.Millisecond,
		StaleProbeInterval: 40 * time.Millisecond,
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialCount.Add(1)
			return c1, nil
		},
		Authenticator: func(s *RoleSession) error { return nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sess.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sess.Close()
	if err := sess.AwaitActive(ctx); err != nil {
		t.Fatalf("await active: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for probeReplies.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(15 * time.Millisecond)
	}
	if probeReplies.Load() < 2 {
		t.Fatalf("期望至少完成 2 次活性探针, got %d", probeReplies.Load())
	}
	if dialCount.Load() != 1 || sess.ConnectionGeneration() != 1 {
		t.Fatalf("探针正常回包时不应触发重连, dialCount=%d gen=%d", dialCount.Load(), sess.ConnectionGeneration())
	}
}

// TestSessionMetrics_TracksLifecycleEvents 验证 SessionMetrics 准确统计连接代数、
// 主动重建、被动断线与握手失败次数及最近断线原因。
func TestSessionMetrics_TracksLifecycleEvents(t *testing.T) {
	pipes := make([][2]net.Conn, 4)
	for i := range pipes {
		pipes[i][0], pipes[i][1] = net.Pipe()
		defer pipes[i][0].Close()
		defer pipes[i][1].Close()
	}

	var dialIdx atomic.Int32
	var authCalls atomic.Int32
	sess := NewRoleSession(SessionConfig{
		RoleID:            "role-metrics",
		ServerAddr:        "mock-pipe",
		HeartbeatInterval: time.Hour,
		ReconnectInterval: 10 * time.Millisecond,
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			i := int(dialIdx.Add(1)) - 1
			if i >= len(pipes) {
				return nil, ErrNotConnected
			}
			return pipes[i][0], nil
		},
		Authenticator: func(s *RoleSession) error {
			// 第 2 次握手模拟失败一次，验证 HandshakeFailures 计数。
			if authCalls.Add(1) == 2 {
				return ErrAuthRejected
			}
			return nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sess.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sess.Close()
	if err := sess.AwaitActive(ctx); err != nil {
		t.Fatalf("await active: %v", err)
	}

	// 1. 主动重建 -> 触发第 2 次握手失败 -> 自动重连到第 3 代连接。
	sess.ReconnectNow()
	deadline := time.Now().Add(2 * time.Second)
	for (sess.ConnectionGeneration() < 3 || sess.State() != StateActive) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if sess.ConnectionGeneration() < 3 || sess.State() != StateActive {
		t.Fatalf("期望恢复到第 3 代连接, gen=%d state=%s", sess.ConnectionGeneration(), sess.State())
	}

	// 2. 被动断开第 3 代连接 -> 自动重连到第 4 代连接。
	_ = pipes[2][1].Close()
	deadline = time.Now().Add(2 * time.Second)
	for (sess.ConnectionGeneration() < 4 || sess.State() != StateActive) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if sess.ConnectionGeneration() < 4 || sess.State() != StateActive {
		t.Fatalf("期望恢复到第 4 代连接, gen=%d state=%s", sess.ConnectionGeneration(), sess.State())
	}

	m := sess.Metrics()
	if m.ConnGeneration != 4 {
		t.Errorf("ConnGeneration = %d, want 4", m.ConnGeneration)
	}
	if m.IntentionalReconnects != 1 {
		t.Errorf("IntentionalReconnects = %d, want 1", m.IntentionalReconnects)
	}
	if m.HandshakeFailures != 1 {
		t.Errorf("HandshakeFailures = %d, want 1", m.HandshakeFailures)
	}
	if m.PassiveDisconnects != 1 {
		t.Errorf("PassiveDisconnects = %d, want 1", m.PassiveDisconnects)
	}
	if m.LastDisconnectReason == "" {
		t.Errorf("期望记录 LastDisconnectReason")
	}
}

// TestOpenChildSession_RespectsWaitCtxTimeout 验证 openChildSession 区分 lifeCtx 与 waitCtx：
// 即使 lifeCtx 无超时且 activateTimeout=30s，只要调用方 waitCtx 到期就会立刻超时并清理子会话。
func TestOpenChildSession_RespectsWaitCtxTimeout(t *testing.T) {
	parent := NewRoleSession(SessionConfig{RoleID: "role-parent", ServerAddr: "mock-pipe"})

	blockDial := make(chan struct{})
	defer close(blockDial)

	cfg := SessionConfig{
		RoleID:     "role-child-timeout",
		ServerAddr: "mock-pipe",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-blockDial:
				return nil, ErrNotConnected
			}
		},
	}

	waitCtx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()

	start := time.Now()
	child, err := parent.openChildSession(context.Background(), waitCtx, "测试子会话", cfg, 30*time.Second)
	elapsed := time.Since(start)
	if err == nil || child != nil {
		if child != nil {
			child.Close()
		}
		t.Fatalf("期望 waitCtx 到期触发超时失败, got child=%v err=%v", child, err)
	}
	if elapsed > time.Second {
		t.Fatalf("openChildSession 应遵守 waitCtx (120ms) 而非等待 30s, took %v", elapsed)
	}
}
