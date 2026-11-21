package client_test

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
)

func TestRoleSession_TownPracticeFlow(t *testing.T) {
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
			case protocol.ActionIDPlayerStartPractice:
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDPlayerStartPractice, []byte{}))
			case protocol.ActionIDPlayerGetPracticeData:
				resp, _ := protocol.BuildTownPracticeDataResponsePacket(protocol.TownPracticeData{
					PracticeTime:         1200,
					PracticeExp:          150000,
					PracticeSpeedNum:     24,
					PracticeExpOne:       8000,
					SuperTownPracticeExp: 2000,
				})
				_ = protocol.WritePacket(serverConn, resp)
			case protocol.ActionIDPlayerCancelPractice:
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDPlayerCancelPractice, []byte{}))
			case protocol.ActionIDPlayerSignFinishedPractice:
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDPlayerSignFinishedPractice, []byte{}))
			}
		}
	}()

	cfg := client.SessionConfig{
		RoleID:            "role-practice-test",
		RoleName:          "打坐测试员",
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := session.Start(ctx); err != nil {
		t.Fatalf("session.Start failed: %v", err)
	}
	defer session.Close()

	// 等待连接进入就绪状态
	time.Sleep(50 * time.Millisecond)

	// 1. 开始城镇打坐练功
	if err := session.StartTownPractice(ctx); err != nil {
		t.Fatalf("StartTownPractice failed: %v", err)
	}
	if !session.GetPlayerState().IsPracticing {
		t.Errorf("expected IsPracticing to be true")
	}

	// 2. 获取城镇打坐练功数据
	data, err := session.GetTownPracticeData(ctx)
	if err != nil {
		t.Fatalf("GetTownPracticeData failed: %v", err)
	}
	if data.PracticeExp != 150000 || data.PracticeTime != 1200 {
		t.Errorf("unexpected practice data: %+v", data)
	}
	if session.GetPlayerState().PracticeExp != 150000 {
		t.Errorf("expected player state PracticeExp to be 150000, got %d", session.GetPlayerState().PracticeExp)
	}

	// 3. 模拟服务端下发经验跳动通知 (UPDATE_PRACTICE 89)
	notifyPkt, _ := protocol.BuildTownPracticeNotifyPacket(protocol.TownPracticeNotify{
		NotifyType: protocol.PlayerPracticeNotifyUpdate,
		Exp:        180000,
		SuperExp:   3000,
	})
	_ = protocol.WritePacket(serverConn, notifyPkt)
	time.Sleep(30 * time.Millisecond)

	if session.GetPlayerState().PracticeExp != 180000 {
		t.Errorf("expected player state PracticeExp to update to 180000, got %d", session.GetPlayerState().PracticeExp)
	}

	// 4. 一键结算并领取练功经验 (Cancel + Sign)
	if err := session.ClaimTownPracticeExp(ctx); err != nil {
		t.Fatalf("ClaimTownPracticeExp failed: %v", err)
	}
	if session.GetPlayerState().IsPracticing {
		t.Errorf("expected IsPracticing to be false after claim")
	}
	if session.GetPlayerState().PracticeExp != 0 {
		t.Errorf("expected PracticeExp to reset to 0 after sign finished, got %d", session.GetPlayerState().PracticeExp)
	}
}

// TestRoleSession_ClaimNewPracticeExpFailureKeepsExp 验证新版打坐结算返回失败码时
// 不清零本地累积经验（否则会出现「资源没变化但本地记为零」）。
func TestRoleSession_ClaimNewPracticeExpFailureKeepsExp(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	var succeed atomic.Bool
	go func() {
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			if pkt.ActionID != protocol.ActionIDNewPracticeCollectAll {
				continue
			}
			if !succeed.Load() {
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(pkt.ActionID, []byte{protocol.NewPracticeFailed}))
				continue
			}
			w := protocol.NewWriter()
			w.WriteUint8(protocol.NewPracticeSuccess)
			w.WriteUint16(0)  // air_list 空
			w.WriteInt64(100) // gain_exp
			_ = protocol.WritePacket(serverConn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		}
	}()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role-new-practice-fail",
		RoleName:   "打坐失败测试",
		ServerAddr: "sandbox",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(s *client.RoleSession) error { return nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := session.Start(ctx); err != nil {
		t.Fatalf("session.Start failed: %v", err)
	}
	defer session.Close()
	deadline := time.Now().Add(2 * time.Second)
	for session.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	session.UpdatePlayerState(func(ps *client.PlayerState) { ps.PracticeExp = 12345 })

	res, err := session.ClaimNewPracticeExp(ctx)
	if err != nil {
		t.Fatalf("ClaimNewPracticeExp: %v", err)
	}
	if res == nil || res.Result != protocol.NewPracticeFailed {
		t.Fatalf("result = %+v, want FAILED", res)
	}
	if got := session.GetPlayerState().PracticeExp; got != 12345 {
		t.Fatalf("失败回包不应清零 PracticeExp, got %d", got)
	}

	// 成功后必须清零。
	succeed.Store(true)
	if _, err := session.ClaimNewPracticeExp(ctx); err != nil {
		t.Fatalf("ClaimNewPracticeExp success: %v", err)
	}
	if got := session.GetPlayerState().PracticeExp; got != 0 {
		t.Fatalf("成功回包后应清零 PracticeExp, got %d", got)
	}
}
