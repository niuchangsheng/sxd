package client

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"sxd-pie-ng/internal/protocol"
)

// TestFetchSaintAreaCredentials_MissingCredentialIsSentinel 验证服务端返回空凭据时
// 返回可判定的 ErrSaintAreaCredentialMissing，供 Routine 按「未开放」回退而不是告警重试。
//
// 2026-11-26 实机：s1 区服圣域未开放，状态探测返回开放但凭据 host/server 为空。
func TestFetchSaintAreaCredentials_MissingCredentialIsSentinel(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})
	go func() {
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			if pkt.ActionID != protocol.ActionIDSaintAreaLoginInfo {
				continue
			}
			w := protocol.NewWriter()
			w.WriteString("") // host
			w.WriteUint16(0)  // port
			w.WriteString("") // server
			w.WriteInt32(0)   // time
			w.WriteString("") // passcode
			w.WriteString("") // town
			w.WriteInt32(0)   // dragon clear
			_ = protocol.WritePacket(serverConn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		}
	}()

	s := NewRoleSession(SessionConfig{
		ServerAddr: "mock",
		RoleID:     "role-sa-test",
		Dialer: func(context.Context, string, string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(*RoleSession) error { return nil },
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(s.Close)
	deadline := time.Now().Add(2 * time.Second)
	for s.State() != StateActive && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if s.State() != StateActive {
		t.Fatalf("state = %s", s.State())
	}

	_, err := s.FetchSaintAreaCredentials(ctx)
	if !errors.Is(err, ErrSaintAreaCredentialMissing) {
		t.Fatalf("err = %v, want ErrSaintAreaCredentialMissing", err)
	}
}
