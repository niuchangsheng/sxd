package client

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/protocol"
)

// TestRoleSession_IsFunctionOpen_CachesFunctionList 验证已开放功能列表只查询一次并命中缓存。
func TestRoleSession_IsFunctionOpen_CachesFunctionList(t *testing.T) {
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
			if pkt.ActionID != protocol.ActionIDPlayerGetFunctions {
				continue
			}
			calls.Add(1)
			w := protocol.NewWriter()
			w.WriteUint16(2)
			w.WriteInt32(protocol.FunctionIDBeelzebubTrials)
			w.WriteUint8(1)
			w.WriteInt32(protocol.FunctionIDTreasureHouse)
			w.WriteUint8(0)
			_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDPlayerGetFunctions, w.Bytes()))
		}
	}()

	s := NewRoleSession(SessionConfig{
		ServerAddr: "sandbox",
		RoleID:     "1001",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(s *RoleSession) error {
			return nil
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("session start failed: %v", err)
	}
	defer s.Close()

	deadline := time.Now().Add(2 * time.Second)
	for s.State() != StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	open, err := s.IsFunctionOpen(ctx, protocol.FunctionIDBeelzebubTrials)
	if err != nil {
		t.Fatalf("IsFunctionOpen failed: %v", err)
	}
	if !open {
		t.Fatal("魔王试炼应判定为已开放")
	}

	open, err = s.IsFunctionOpen(ctx, protocol.FunctionIDZodiac)
	if err != nil {
		t.Fatalf("IsFunctionOpen failed: %v", err)
	}
	if open {
		t.Fatal("未出现在开放列表中的功能应判定为未开放")
	}

	if got := calls.Load(); got != 1 {
		t.Fatalf("功能列表应只查询一次并命中缓存, 实际查询 %d 次", got)
	}
}
