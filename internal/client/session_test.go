// Package client_test 验证角色会话长连接客户端生命周期、握手与状态机。
//
// @author Ateng
// @since 2026-10-08
package client_test

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
)

func TestSessionState_String(t *testing.T) {
	tests := []struct {
		state    client.SessionState
		expected string
	}{
		{client.StateDisconnected, "Disconnected"},
		{client.StateConnecting, "Connecting"},
		{client.StateAuthenticating, "Authenticating"},
		{client.StateActive, "Active"},
		{client.StateReconnecting, "Reconnecting"},
		{client.StateClosed, "Closed"},
	}

	for _, tt := range tests {
		if tt.state.String() != tt.expected {
			t.Errorf("expected state %s, got %s", tt.expected, tt.state.String())
		}
	}
}

func TestRoleSession_ConnectAndAuthenticate(t *testing.T) {
	clientConn, serverConn := net.Pipe()

	const actionLogin uint32 = 100
	const actionChat uint32 = 200

	var serverWg sync.WaitGroup
	serverWg.Add(1)

	// 模拟游戏服务端 Goroutine
	go func() {
		defer serverWg.Done()
		defer serverConn.Close()

		// 1. 读取客户端登录握手包
		loginPkt, err := protocol.ReadPacket(serverConn)
		if err != nil {
			t.Errorf("server read login packet failed: %v", err)
			return
		}
		if loginPkt.ActionID != actionLogin {
			t.Errorf("expected login action %d, got %d", actionLogin, loginPkt.ActionID)
			return
		}

		// 2. 回复登录成功响应 (Payload: status=0)
		resp := protocol.NewPacket(actionLogin, []byte{0x00})
		if err := protocol.WritePacket(serverConn, resp); err != nil {
			t.Errorf("server send login response failed: %v", err)
			return
		}

		// 3. 读取后续业务聊天包
		chatPkt, err := protocol.ReadPacket(serverConn)
		if err != nil {
			t.Errorf("server read chat packet failed: %v", err)
			return
		}
		if chatPkt.ActionID != actionChat {
			t.Errorf("expected chat action %d, got %d", actionChat, chatPkt.ActionID)
			return
		}

		// 4. 回复聊天确认包
		chatResp := protocol.NewPacket(actionChat, []byte("ok"))
		_ = protocol.WritePacket(serverConn, chatResp)
	}()

	receivedChatResp := make(chan *protocol.Packet, 1)

	cfg := client.SessionConfig{
		RoleID:            "role-1001",
		RoleName:          "剑灵测试角色",
		ServerAddr:        "mock-pipe",
		Token:             "mock-token-xyz",
		HeartbeatInterval: 1 * time.Second,
		HeartbeatTimeout:  500 * time.Millisecond,
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(s *client.RoleSession) error {
			// 发送登录包并等待回包
			loginPkt := protocol.NewPacket(actionLogin, []byte("mock-token-xyz"))
			return s.Send(loginPkt)
		},
	}

	session := client.NewRoleSession(cfg)
	session.RegisterHandler(actionChat, func(p *protocol.Packet) {
		receivedChatResp <- p
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := session.Start(ctx); err != nil {
		t.Fatalf("session.Start failed: %v", err)
	}

	// 等待会话进入 Active 状态 (或超时)
	timeout := time.After(2 * time.Second)
	for session.State() != client.StateActive {
		select {
		case <-timeout:
			t.Fatalf("timed out waiting for session to become Active, current: %s", session.State())
		case <-time.After(10 * time.Millisecond):
		}
	}

	// 发送业务聊天包
	chatPkt := protocol.NewPacket(actionChat, []byte("hello-server"))
	if err := session.Send(chatPkt); err != nil {
		t.Fatalf("session.Send failed: %v", err)
	}

	select {
	case p := <-receivedChatResp:
		if string(p.Payload) != "ok" {
			t.Errorf("expected chat response 'ok', got '%s'", string(p.Payload))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for chat response")
	}

	// 关闭会话
	session.Close()
	serverWg.Wait()

	if session.State() != client.StateClosed {
		t.Errorf("expected state Closed, got %s", session.State())
	}
}

func TestRoleSession_Heartbeat(t *testing.T) {
	clientConn, serverConn := net.Pipe()

	const actionHeartbeat uint32 = 0x00000017
	heartbeatCount := 0
	var countMu sync.Mutex

	done := make(chan struct{})

	go func() {
		defer serverConn.Close()
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			if pkt.ActionID == actionHeartbeat {
				if len(pkt.Payload) != 4 {
					t.Errorf("expected 4-byte heartbeat payload, got %d", len(pkt.Payload))
				}
				countMu.Lock()
				heartbeatCount++
				if heartbeatCount >= 3 {
					select {
					case <-done:
					default:
						close(done)
					}
				}
				countMu.Unlock()
			}
		}
	}()

	cfg := client.SessionConfig{
		RoleID:            "role-heartbeat",
		RoleName:          "心跳测试角色",
		ServerAddr:        "mock-pipe",
		HeartbeatInterval: 40 * time.Millisecond,
		HeartbeatTimeout:  200 * time.Millisecond,
		HeartbeatActionID: actionHeartbeat,
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(s *client.RoleSession) error { return nil },
	}

	session := client.NewRoleSession(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := session.Start(ctx); err != nil {
		t.Fatalf("session.Start failed: %v", err)
	}

	select {
	case <-done:
		// 成功收到至少 3 次心跳
	case <-time.After(2 * time.Second):
		countMu.Lock()
		c := heartbeatCount
		countMu.Unlock()
		t.Fatalf("timed out waiting for 3 heartbeats, received: %d", c)
	}

	session.Close()
}

func TestRoleSession_AutomaticReconnect(t *testing.T) {
	pipe1Client, pipe1Server := net.Pipe()
	pipe2Client, pipe2Server := net.Pipe()

	const actionLogin uint32 = 100
	dialCount := 0
	var dialMu sync.Mutex

	dialer := func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialMu.Lock()
		defer dialMu.Unlock()
		dialCount++
		if dialCount == 1 {
			return pipe1Client, nil
		}
		return pipe2Client, nil
	}

	// Server 1 处理首次连接，然后主动断开
	go func() {
		defer pipe1Server.Close()
		// 读取登录包
		_, _ = protocol.ReadPacket(pipe1Server)
		// 回复登录响应
		_ = protocol.WritePacket(pipe1Server, protocol.NewPacket(actionLogin, []byte{0x00}))
		// 稍微等待一下后主动掐断连接
		time.Sleep(30 * time.Millisecond)
	}()

	reconnected := make(chan struct{})

	// Server 2 处理重连后的第二次握手
	go func() {
		defer pipe2Server.Close()
		pkt, err := protocol.ReadPacket(pipe2Server)
		if err == nil && pkt.ActionID == actionLogin {
			_ = protocol.WritePacket(pipe2Server, protocol.NewPacket(actionLogin, []byte{0x00}))
			close(reconnected)
		}
	}()

	cfg := client.SessionConfig{
		RoleID:            "role-reconnect",
		RoleName:          "重连测试角色",
		ServerAddr:        "mock-pipe",
		HeartbeatInterval: 1 * time.Second,
		ReconnectInterval: 20 * time.Millisecond,
		Dialer:            dialer,
		Authenticator: func(s *client.RoleSession) error {
			return s.Send(protocol.NewPacket(actionLogin, []byte{0x01}))
		},
	}

	session := client.NewRoleSession(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := session.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	select {
	case <-reconnected:
		// 重连成功！
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for automatic reconnection")
	}

	session.Close()
}

func TestRoleSession_MaxReconnectLimit(t *testing.T) {
	cfg := client.SessionConfig{
		RoleID:               "role-max-retry",
		RoleName:             "重试超限测试",
		ServerAddr:           "mock-pipe",
		ReconnectInterval:    10 * time.Millisecond,
		MaxReconnectAttempts: 2,
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return nil, errors.New("connection refused")
		},
	}

	session := client.NewRoleSession(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := session.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	timeout := time.After(2 * time.Second)
	for session.State() != client.StateClosed {
		select {
		case <-timeout:
			t.Fatalf("expected session to reach StateClosed after max retries, got %s", session.State())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestRoleSession_SendErrors(t *testing.T) {
	cfg := client.SessionConfig{
		RoleID:     "role-err",
		RoleName:   "发送错误测试",
		ServerAddr: "mock-pipe",
	}

	session := client.NewRoleSession(cfg)
	pkt := protocol.NewPacket(1, nil)

	// 1. 未连接时发送
	err := session.Send(pkt)
	if !errors.Is(err, client.ErrNotConnected) {
		t.Errorf("expected ErrNotConnected, got %v", err)
	}
	err = session.SendCompressed(pkt)
	if !errors.Is(err, client.ErrNotConnected) {
		t.Errorf("expected ErrNotConnected for SendCompressed, got %v", err)
	}

	// 2. 关闭后发送
	session.Close()
	err = session.Send(pkt)
	if !errors.Is(err, client.ErrSessionClosed) {
		t.Errorf("expected ErrSessionClosed, got %v", err)
	}
	err = session.SendCompressed(pkt)
	if !errors.Is(err, client.ErrSessionClosed) {
		t.Errorf("expected ErrSessionClosed for SendCompressed, got %v", err)
	}
}

func TestRoleSession_SendCompressed_Active(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	const actionCompressed uint32 = 888

	received := make(chan *protocol.Packet, 1)
	go func() {
		defer serverConn.Close()
		pkt, err := protocol.ReadPacket(serverConn)
		if err == nil {
			received <- pkt
		}
	}()

	cfg := client.SessionConfig{
		RoleID:     "role-comp",
		RoleName:   "压缩测试角色",
		ServerAddr: "mock-pipe",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(s *client.RoleSession) error { return nil },
	}

	session := client.NewRoleSession(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := session.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	for session.State() != client.StateActive {
		time.Sleep(10 * time.Millisecond)
	}

	rawText := "compressed-payload-content"
	if err := session.SendCompressed(protocol.NewPacket(actionCompressed, []byte(rawText))); err != nil {
		t.Fatalf("SendCompressed failed: %v", err)
	}

	select {
	case pkt := <-received:
		if pkt.ActionID != actionCompressed || string(pkt.Payload) != rawText {
			t.Errorf("received packet mismatch: %+v", pkt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for compressed packet")
	}

	session.Close()
}

func TestRoleSession_StLoginWithCredentials(t *testing.T) {
	cfg := client.SessionConfig{
		RoleID:        "role-stlogin",
		RoleName:      "梦一场",
		ServerAddr:    "mock",
		ServerID:      "fengwanyx_s813",
		Time1:         1791469000,
		Hash1:         "5f4dcc3b5aa765d61d8327deb882cf99",
		Authenticator: client.DefaultAuthenticator,
	}

	session := client.NewRoleSession(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := session.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for session.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if session.State() != client.StateActive {
		t.Fatalf("expected StateActive, got %s", session.State())
	}

	// 验证体力同步到了 200 或 201 点 (主服真实全量资产快照)
	stamina := session.GetStamina()
	if stamina != 200 && stamina != 201 {
		t.Errorf("expected stamina 200 or 201, got %d", stamina)
	}

	session.Close()
}

func TestRoleSession_SyncQuests(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	go func() {
		for {
			pkt, err := protocol.ReadPacket(c2)
			if err != nil {
				return
			}
			if pkt.ActionID == protocol.ActionIDQuestList {
				// 模拟下发 1 个已接任务 (Quest 157, State 0)
				w := protocol.NewWriter()
				w.WriteUint16(1)
				w.WriteInt32(157)
				w.WriteInt32(0)
				w.WriteBytes(make([]byte, 15)) // 15B 扩展元数据
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDQuestList, w.Bytes()))
			}
		}
	}()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role-sync-quests",
		RoleName:   "任务同步角色",
		ServerAddr: "mock",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return c1, nil
		},
		Authenticator: func(s *client.RoleSession) error {
			return nil
		},
	})
	_ = session.Start(context.Background())
	defer session.Close()

	deadline := time.Now().Add(2 * time.Second)
	for session.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	items, err := session.SyncQuests(context.Background())
	if err != nil {
		t.Fatalf("SyncQuests 失败: %v", err)
	}
	if len(items) != 1 || items[0].QuestID != 157 {
		t.Fatalf("SyncQuests 任务数据不匹配: %+v", items)
	}

	quests := session.GetQuests()
	if st, ok := quests[157]; !ok || st != 0 {
		t.Fatalf("会话快照中任务状态未正确更新: %+v", quests)
	}
}
