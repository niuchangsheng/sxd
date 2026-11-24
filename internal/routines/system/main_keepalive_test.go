package system

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
)

// TestMainKeepaliveRoutine_SendsBusinessRead 验证保活任务发送 0/6 业务读，
// 而不是建立跨服连接。
func TestMainKeepaliveRoutine_SendsBusinessRead(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	var calls atomic.Int32
	go func() {
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			if pkt.ActionID == protocol.ActionIDPlayerGetFunctions {
				calls.Add(1)
				_ = protocol.WritePacket(serverConn,
					protocol.BuildPlayerFunctionsResultPacket([]int32{165}))
			}
		}
	}()

	sess := client.NewRoleSession(client.SessionConfig{
		RoleID:        "role-keepalive",
		RoleName:      "保活测试",
		ServerAddr:    "mock-pipe",
		Dialer:        func(context.Context, string, string) (net.Conn, error) { return clientConn, nil },
		Authenticator: func(*client.RoleSession) error { return nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sess.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sess.Close()
	deadline := time.Now().Add(2 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		t.Fatalf("session not active: %s", sess.State())
	}

	if err := NewCrossKeepaliveRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("keepalive: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("保活应发送 1 次 0/6, got %d", calls.Load())
	}
	if sess.HasCrossSession() {
		t.Fatal("保活不应建立跨服常驻连接")
	}
}

func TestMainKeepaliveRoutine_RequiresActiveSession(t *testing.T) {
	sess := client.NewRoleSession(client.SessionConfig{RoleID: "role-keepalive-idle"})
	if err := NewCrossKeepaliveRoutine().Execute(context.Background(), sess, nil); err == nil {
		t.Fatal("未激活会话应返回错误")
	}
}
