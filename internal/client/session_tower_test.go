package client_test

import (
	"context"
	"net"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
)

func TestRoleSession_TowerFlow(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	// 模拟服务端响应处理
	go func() {
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDTowerGetInfo:
				tw := protocol.NewWriter()
				tw.WriteUint8(25) // SequenceMax
				tw.WriteUint8(1)  // Layer
				tw.WriteUint8(1)  // SequenceWar
				tw.WriteUint8(2)  // BagNum
				tw.WriteUint8(1)  // ResetNum
				tw.WriteUint16(0) // ResetIngot
				tw.WriteUint8(0)  // HideID
				tw.WriteInt32(50) // HighStone
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDTowerGetInfo, tw.Bytes()))

			case protocol.ActionIDTowerStartChallenge:
				cw := protocol.NewWriter()
				cw.WriteUint8(protocol.TowerResultSuccess) // Result
				cw.WriteInt32(1001)                       // WarSoulID
				cw.WriteInt32(300000)                     // WarExp
				cw.WriteInt32(300)                        // WarFame
				cw.WriteInt32(50000)                      // WarCoin
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDTowerStartChallenge, cw.Bytes()))

			case protocol.ActionIDTowerResetLayer:
				rw := protocol.NewWriter()
				rw.WriteUint8(protocol.TowerResultSuccess) // Result
				rw.WriteUint8(1)                          // Layer
				rw.WriteUint16(50)                         // ResetIngot
				rw.WriteUint8(0)                          // ResetNum
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDTowerResetLayer, rw.Bytes()))

			case protocol.ActionIDTowerStartAutoChallenge:
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDTowerStartAutoChallenge, []byte{protocol.TowerResultSuccess}))

			case protocol.ActionIDTowerQuicklyAutoChallenge:
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDTowerQuicklyAutoChallenge, []byte{protocol.TowerResultSuccess}))

			case protocol.ActionIDTowerStopAutoChallenge:
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDTowerStopAutoChallenge, []byte{}))
			}
		}
	}()

	cfg := client.SessionConfig{
		RoleID:            "role-tower-test",
		RoleName:          "六道测试员",
		ServerAddr:        "mock-pipe",
		HeartbeatInterval: 1 * time.Second,
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(s *client.RoleSession) error {
			return nil
		},
	}

	session := client.NewRoleSession(cfg)
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("session.Start failed: %v", err)
	}
	defer session.Close()

	deadline := time.Now().Add(2 * time.Second)
	for session.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	ctx := context.Background()

	// 1. 查询六道轮回塔面板
	info, err := session.GetTowerInfo(ctx)
	if err != nil {
		t.Fatalf("GetTowerInfo failed: %v", err)
	}
	if info.SequenceMax != 25 || info.Layer != 1 || info.SequenceWar != 1 || info.HighStone != 50 {
		t.Fatalf("unexpected tower info: %+v", info)
	}

	// 2. 发起单层挑战
	res, err := session.ChallengeTower(ctx, 1, true, false)
	if err != nil {
		t.Fatalf("ChallengeTower failed: %v", err)
	}
	if res.Result != 0 || res.WarSoulID != 1001 || res.WarExp != 300000 {
		t.Fatalf("unexpected challenge result: %+v", res)
	}

	// 3. 重置塔
	resetRes, err := session.ResetTowerLayer(ctx)
	if err != nil {
		t.Fatalf("ResetTowerLayer failed: %v", err)
	}
	if resetRes.Result != 0 || resetRes.Layer != 1 {
		t.Fatalf("unexpected reset result: %+v", resetRes)
	}

	// 4. 自动爬塔与加速
	if err := session.StartTowerAutoChallenge(ctx, true, false); err != nil {
		t.Fatalf("StartTowerAutoChallenge failed: %v", err)
	}
	if err := session.QuicklyTowerAutoChallenge(ctx, true, false); err != nil {
		t.Fatalf("QuicklyTowerAutoChallenge failed: %v", err)
	}
	if err := session.StopTowerAutoChallenge(ctx); err != nil {
		t.Fatalf("StopTowerAutoChallenge failed: %v", err)
	}
}
