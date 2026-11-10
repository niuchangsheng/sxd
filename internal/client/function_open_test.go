package client

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"sxd-pie-ng/internal/protocol"
)

// newSilentPipeSession 构造一个“只收不回”的会话：服务端读走请求但永不回包，
// 用于验证客户端协议封装在服务端静默丢包时不会永久阻塞。
func newSilentPipeSession(t *testing.T) *RoleSession {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})
	go func() {
		for {
			if _, err := protocol.ReadPacket(serverConn); err != nil {
				return
			}
			// 静默丢包: 读取请求后不回任何响应。
		}
	}()

	s := NewRoleSession(SessionConfig{
		ServerAddr: "mock", // 沙箱短超时 (300ms)，避免单测等待 8s
		RoleID:     "1001",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(*RoleSession) error { return nil },
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	if err := s.Start(ctx); err != nil {
		t.Fatalf("session start failed: %v", err)
	}
	t.Cleanup(s.Close)

	deadline := time.Now().Add(2 * time.Second)
	for s.State() != StateActive && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if s.State() != StateActive {
		t.Fatalf("session 未进入激活态: %s", s.State())
	}
	return s
}

// TestCheckFunctionOpen_SilentServerFailsFast 验证服务端静默丢包、调用方又未设置
// deadline 时 (Routine 调度器现场)，CheckFunctionOpen 仍按自带短超时返回。
// 回归: 2026-11-10 叶公好龙因该探测无超时而冻结调度协程 24 分钟。
func TestCheckFunctionOpen_SilentServerFailsFast(t *testing.T) {
	s := newSilentPipeSession(t)

	start := time.Now()
	_, err := s.CheckFunctionOpen(context.Background(), protocol.FunctionIDPetAnimal)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("服务端静默无回包时应返回错误")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("CheckFunctionOpen 未按短超时退出, 耗时 %v", elapsed)
	}
}

// TestRoleSession_CallAppliesDefaultDeadline 验证调用方漏设 deadline 时，
// Call 仍会按默认上限返回，避免任何协议封装永久挂起调度协程。
func TestRoleSession_CallAppliesDefaultDeadline(t *testing.T) {
	prev := defaultCallTimeout
	defaultCallTimeout = 150 * time.Millisecond
	t.Cleanup(func() { defaultCallTimeout = prev })

	s := newSilentPipeSession(t)

	start := time.Now()
	_, err := s.Call(context.Background(),
		protocol.NewPacket(protocol.ActionIDPetAnimalInfo, nil),
		protocol.ActionIDPetAnimalInfo)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("无 deadline 的调用应被兜底超时中断, 实际 err=%v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Call 未按兜底超时退出, 耗时 %v", elapsed)
	}
}