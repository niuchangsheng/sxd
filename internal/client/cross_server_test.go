// Package client_test 验证仙界跨服会话建立流程 (Mod 94 登录 + Mod 95 进入仙界城镇)。
//
// @author Ateng
// @since 2026-11-02
package client_test

import (
	"context"
	"net"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
)

func TestOpenCrossServerSessionWithInfo(t *testing.T) {
	serverConn, dialConn := net.Pipe()
	defer serverConn.Close()

	var gotLogin *protocol.StLoginRequest
	var gotTownID int32
	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDStLogin:
				req, pErr := protocol.ParseStLoginRequest(pkt.Payload)
				if pErr == nil {
					gotLogin = req
				}
				w := protocol.NewWriter()
				w.WriteUint8(0)     // SUCCESS
				w.WriteInt32(65536) // playerID
				w.WriteInt32(1793549704)
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDStLogin, w.Bytes()))

			case protocol.ActionIDStTownEnter:
				r := protocol.NewReader(pkt.Payload)
				gotTownID, _ = r.ReadInt32()
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDStTownEnter, []byte{protocol.StTownResultSuccess}))

			case protocol.ActionHeartbeat:
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionHeartbeat, []byte{0x06, 0x00, 0x0a, 0x28}))
			}
		}
	}()

	main := client.NewRoleSession(client.SessionConfig{
		RoleID:     "s1-role",
		RoleName:   "曹植始",
		ServerAddr: "main-gateway",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialConn, nil
		},
	})
	main.UpdatePlayerState(func(ps *client.PlayerState) {
		ps.PlayerID = 65536
		ps.OriginNickName = "曹植始.s1"
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cred := &protocol.StCrossServerInfo{
		Host:       "9x703.sxdweb.xd.com",
		Port:       8480,
		ServerName: "s703",
		Time:       1793549704,
		PassCode:   "mock_pass_code",
		TownSign:   "XianJie",
	}
	cross, err := main.OpenCrossServerSessionWithInfo(ctx, cred)
	if err != nil {
		t.Fatalf("OpenCrossServerSessionWithInfo failed: %v", err)
	}
	defer cross.Close()

	if cross.State() != client.StateActive {
		t.Fatalf("expected cross session active, got %s", cross.State())
	}
	if cross.PlayerID() != 65536 {
		t.Errorf("expected cross player id 65536, got %d", cross.PlayerID())
	}
	if gotLogin == nil {
		t.Fatal("cross login packet was not received")
	}
	if gotLogin.ServerName != "s703" || gotLogin.PlayerID != 65536 {
		t.Errorf("unexpected login request: %+v", gotLogin)
	}
	if gotLogin.OriginNickName != "曹植始.s1" {
		t.Errorf("expected origin nick name 曹植始.s1, got %q", gotLogin.OriginNickName)
	}
	if gotLogin.Time != 1793549704 || gotLogin.PassCode != "mock_pass_code" {
		t.Errorf("unexpected login credentials: %+v", gotLogin)
	}
	if gotTownID != protocol.TownIDXianJie {
		t.Errorf("expected town id 41, got %d", gotTownID)
	}

	dialConn.Close()
	<-done
}

func TestOpenCrossServerSessionWithInfo_Rejected(t *testing.T) {
	serverConn, dialConn := net.Pipe()
	defer serverConn.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			if pkt.ActionID == protocol.ActionIDStLogin {
				w := protocol.NewWriter()
				w.WriteUint8(1) // FAILED
				w.WriteInt32(0)
				w.WriteInt32(0)
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDStLogin, w.Bytes()))
			}
		}
	}()

	main := client.NewRoleSession(client.SessionConfig{
		RoleID:     "s1-role",
		RoleName:   "曹植始",
		ServerAddr: "main-gateway",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialConn, nil
		},
	})
	main.UpdatePlayerState(func(ps *client.PlayerState) {
		ps.PlayerID = 65536
		ps.OriginNickName = "曹植始.s1"
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cred := &protocol.StCrossServerInfo{
		Host:       "9x703.sxdweb.xd.com",
		Port:       8480,
		ServerName: "s703",
		Time:       1793549704,
		PassCode:   "mock_pass_code",
	}
	if _, err := main.OpenCrossServerSessionWithInfo(ctx, cred); err == nil {
		t.Fatal("expected error when cross login is rejected")
	}

	dialConn.Close()
	<-done
}
