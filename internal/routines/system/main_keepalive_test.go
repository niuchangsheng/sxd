package system

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func buildMockStCrossInfoPayload() []byte {
	w := protocol.NewWriter()
	w.WriteString("cross-mock")
	w.WriteUint16(8219)
	w.WriteString("s0")
	w.WriteInt32(1700000000)
	w.WriteString("passcode123")
	w.WriteString("XianJie")
	return w.Bytes()
}

// TestCrossKeepaliveRoutine_MaintainsLoginOnlyCrossSession 验证保活任务建立并复用
// 仅登录、不进仙界城镇 (95/0) 的常驻跨服辅助会话。
func TestCrossKeepaliveRoutine_MaintainsLoginOnlyCrossSession(t *testing.T) {
	mainSrv, mainCli := net.Pipe()
	crossSrv, crossCli := net.Pipe()
	defer mainSrv.Close()
	defer mainCli.Close()
	defer crossSrv.Close()
	defer crossCli.Close()

	var credCalls, stLoginCalls, stTownEnterCalls, dialCrossCalls atomic.Int32

	go func() {
		for {
			pkt, err := protocol.ReadPacket(mainSrv)
			if err != nil {
				return
			}
			if pkt.ActionID == protocol.ActionIDStcLoginGetInfo {
				credCalls.Add(1)
				_ = protocol.WritePacket(mainSrv,
					protocol.NewPacket(protocol.ActionIDStcLoginGetInfo, buildMockStCrossInfoPayload()))
			}
		}
	}()

	go func() {
		for {
			pkt, err := protocol.ReadPacket(crossSrv)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDStLogin:
				stLoginCalls.Add(1)
				w := protocol.NewWriter()
				w.WriteUint8(0) // SUCCESS
				w.WriteInt32(106732)
				w.WriteInt32(1700000000)
				_ = protocol.WritePacket(crossSrv, protocol.NewPacket(protocol.ActionIDStLogin, w.Bytes()))
			case protocol.ActionIDStTownEnter:
				stTownEnterCalls.Add(1)
			}
		}
	}()

	sess := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role-keepalive",
		RoleName:   "保活测试",
		ServerAddr: "main-mock:8200",
		PlayerID:   106732,
		Dialer: func(_ context.Context, _, addr string) (net.Conn, error) {
			if addr == "cross-mock:8219" {
				dialCrossCalls.Add(1)
				return crossCli, nil
			}
			return mainCli, nil
		},
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

	r := NewCrossKeepaliveRoutine()
	if err := r.Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("keepalive first run: %v", err)
	}
	if !sess.HasCrossSession() {
		t.Fatal("保活应建立常驻跨服辅助连接")
	}
	if stLoginCalls.Load() != 1 {
		t.Fatalf("跨服登录应执行 1 次, got %d", stLoginCalls.Load())
	}
	if stTownEnterCalls.Load() != 0 {
		t.Fatalf("常驻保活严禁调用 95/0 enter_town, got %d", stTownEnterCalls.Load())
	}

	// 第二次巡检在常驻连接健康时应直接复用，不重复申请凭据或重连。
	if err := r.Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("keepalive second run: %v", err)
	}
	if credCalls.Load() != 1 || dialCrossCalls.Load() != 1 {
		t.Fatalf("健康常驻连接应被复用: credCalls=%d dialCross=%d",
			credCalls.Load(), dialCrossCalls.Load())
	}
}

// TestCrossKeepaliveRoutine_FailureReturnsCooldown 验证跨服凭据失败时返回短冷却退避。
func TestCrossKeepaliveRoutine_FailureReturnsCooldown(t *testing.T) {
	mainSrv, mainCli := net.Pipe()
	defer mainSrv.Close()
	defer mainCli.Close()

	go func() {
		for {
			pkt, err := protocol.ReadPacket(mainSrv)
			if err != nil {
				return
			}
			if pkt.ActionID == protocol.ActionIDStcLoginGetInfo {
				// 返回损坏/空凭据模拟失败分支。
				_ = protocol.WritePacket(mainSrv,
					protocol.NewPacket(protocol.ActionIDStcLoginGetInfo, []byte{0x00}))
			}
		}
	}()

	sess := client.NewRoleSession(client.SessionConfig{
		RoleID:        "role-keepalive-fail",
		RoleName:      "保活失败测试",
		ServerAddr:    "main-mock:8200",
		Dialer:        func(context.Context, string, string) (net.Conn, error) { return mainCli, nil },
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

	err := NewCrossKeepaliveRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("跨服保活建连失败应返回 CooldownError, got %v", err)
	}
}

func TestMainKeepaliveRoutine_RequiresActiveSession(t *testing.T) {
	sess := client.NewRoleSession(client.SessionConfig{RoleID: "role-keepalive-idle"})
	if err := NewCrossKeepaliveRoutine().Execute(context.Background(), sess, nil); err == nil {
		t.Fatal("未激活会话应返回错误")
	}
}
