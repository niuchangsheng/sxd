package pvp

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

// pvpAutomationFunctionIDs Mock 环境视为已开放的玩法功能编号。
var pvpAutomationFunctionIDs = []int32{
	protocol.FunctionIDZodiac, protocol.FunctionIDBeelzebubTrials,
	protocol.FunctionIDMiracleFighters, protocol.FunctionIDRepressDemonTower,
	protocol.FunctionIDShanhaiWorld, protocol.FunctionIDEudemons,
	protocol.FunctionIDDestinySword, protocol.FunctionIDLinkFate,
	protocol.FunctionIDPartnerLink, protocol.FunctionIDSaintArea,
	protocol.FunctionIDFaction, protocol.FunctionIDTreasureHouse,
	protocol.FunctionIDStTrials, protocol.FunctionIDStPracticeRoom,
	protocol.FunctionIDTravelEvent, protocol.FunctionIDRollCake,
	protocol.FunctionIDFindImmortal, protocol.FunctionIDFishing,
}

func newPvPMockSession(t *testing.T, handler func(net.Conn, *protocol.Packet)) *client.RoleSession {
	t.Helper()
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
			// 参与自动化功能门槛的玩法在 Mock 下一律视为已开放。
			if pkt.ActionID == protocol.ActionIDPlayerGetFunctions {
				_ = protocol.WritePacket(serverConn, protocol.BuildPlayerFunctionsResultPacket(pvpAutomationFunctionIDs))
				continue
			}
			// 圣域竞技场先查圣域开启状态，Mock 默认返回"已开启、无关闭窗口"。
			if pkt.ActionID == protocol.ActionIDSaintAreaStatus {
				w := protocol.NewWriter()
				w.WriteUint8(protocol.SaintAreaStatusOpen)
				w.WriteUint16(0)
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(protocol.ActionIDSaintAreaStatus, w.Bytes()))
				continue
			}
			handler(serverConn, pkt)
		}
	}()
	sess := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role-holy-arena-test",
		RoleName:   "圣域测试",
		ServerAddr: "mock-pipe",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(*client.RoleSession) error { return nil },
	})
	if err := sess.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	sess.UpdatePlayerState(func(ps *client.PlayerState) {
		ps.PlayerID = 999
	})
	return sess
}

// stubSaintAreaSession 让圣域会话建立返回主会话本身，便于单测验证协议流程。
func stubSaintAreaSession(t *testing.T) {
	t.Helper()
	prev := openSaintAreaSession
	openSaintAreaSession = func(ctx context.Context, s *client.RoleSession) (*client.RoleSession, error) {
		return s, nil
	}
	t.Cleanup(func() { openSaintAreaSession = prev })
}

func TestHolyArenaRoutine_ClaimsEmptySeat(t *testing.T) {
	stubSaintAreaSession(t)
	var panelCalls, robCalls atomic.Int32
	sess := newPvPMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDSaArenaGetInfo:
			n := panelCalls.Add(1)
			info := protocol.SaArenaInfo{
				Seats:          []protocol.SaArenaSeat{{Seq: 1}},
				AvailableCount: 0,
			}
			if n == 1 {
				info.AvailableCount = 1
			}
			_ = protocol.WritePacket(conn, protocol.BuildSaArenaGetInfoResultPacket(info))
		case protocol.ActionIDSaArenaRobSeat:
			robCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildSaArenaRobResultPacket(protocol.SaArenaRobResult{
				Result: protocol.SaArenaResultSuccessFree,
			}))
		}
	})
	defer sess.Close()

	if err := NewHolyArenaRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("holy arena: %v", err)
	}
	if robCalls.Load() != 1 {
		t.Fatalf("expected 1 rob, got %d", robCalls.Load())
	}
}

func TestHolyArenaRoutine_ActivityCloseStops(t *testing.T) {
	stubSaintAreaSession(t)
	var robCalls atomic.Int32
	sess := newPvPMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDSaArenaGetInfo:
			_ = protocol.WritePacket(conn, protocol.BuildSaArenaGetInfoResultPacket(protocol.SaArenaInfo{
				Seats:          []protocol.SaArenaSeat{{Seq: 1}},
				AvailableCount: 3,
			}))
		case protocol.ActionIDSaArenaRobSeat:
			robCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildSaArenaRobResultPacket(protocol.SaArenaRobResult{
				Result: protocol.SaArenaResultActivityClose,
			}))
		}
	})
	defer sess.Close()

	if err := NewHolyArenaRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("holy arena: %v", err)
	}
	if robCalls.Load() != 1 {
		t.Fatalf("expected stop after ACTIVITY_CLOSE, got %d robs", robCalls.Load())
	}
}

// TestHolyArenaRoutine_SaintSessionUnavailableSkips 验证：圣域连接无法建立时
// 不应回退到主服发起抢占（主服 rob 会固定返回 ACTIVITY_CLOSE）。
func TestHolyArenaRoutine_SaintSessionUnavailableSkips(t *testing.T) {
	var robCalls, panelCalls atomic.Int32
	prev := openSaintAreaSession
	openSaintAreaSession = func(ctx context.Context, s *client.RoleSession) (*client.RoleSession, error) {
		return nil, context.DeadlineExceeded
	}
	t.Cleanup(func() { openSaintAreaSession = prev })

	sess := newPvPMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDSaArenaGetInfo:
			panelCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildSaArenaGetInfoResultPacket(protocol.SaArenaInfo{
				Seats:          []protocol.SaArenaSeat{{Seq: 1}},
				AvailableCount: 1,
			}))
		case protocol.ActionIDSaArenaRobSeat:
			robCalls.Add(1)
		}
	})
	defer sess.Close()

	err := NewHolyArenaRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("圣域会话建立失败应返回 CooldownError, got %v", err)
	}
	if panelCalls.Load() != 0 || robCalls.Load() != 0 {
		t.Fatalf("expected skip when saint session unavailable, panel=%d rob=%d",
			panelCalls.Load(), robCalls.Load())
	}
}

// TestHolyArenaRoutine_PanelSilentDropBacksOff 验证面板被静默丢弃时
// 不再 return nil，而是按连续丢包退避，剩余抢占次数稍后继续消费。
func TestHolyArenaRoutine_PanelSilentDropBacksOff(t *testing.T) {
	stubSaintAreaSession(t)
	sess := newPvPMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		// 静默丢弃 SaArenaGetInfo：不回包。
	})
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := NewHolyArenaRoutine().Execute(ctx, sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("面板无回包应返回 CooldownError, got %v", err)
	}
	if got := scheduler.SilentDropCount(sess, "holy_arena"); got < 1 {
		t.Fatalf("应记录圣域竞技场静默丢包，got %d", got)
	}
}

// TestSaintAreaReopenTime_UsesCurrentCloseWindow 验证关闭窗口包含当前时间时，
// 精确到窗口结束后 1 分钟唤醒。
func TestSaintAreaReopenTime_UsesCurrentCloseWindow(t *testing.T) {
	// 计算逻辑由 scheduler.SaintAreaReopenTime 统一提供。
	now := time.Now()
	status := &protocol.SaintAreaStatus{
		Status: protocol.SaintAreaStatusClose,
		CloseTimes: []protocol.SaintAreaCloseTime{{
			StartTime: int32(now.Add(-10 * time.Minute).Unix()),
			EndTime:   int32(now.Add(5 * time.Minute).Unix()),
		}},
	}
	got := scheduler.SaintAreaReopenTime(status, now)
	want := now.Add(6 * time.Minute)
	if d := got.Sub(want); d > 5*time.Second || d < -5*time.Second {
		t.Fatalf("唤醒时间 = %s, want %s", got, want)
	}
}

// TestSaintAreaReopenTime_FallbackHour 验证窗口不包含当前时间时回退 1 小时探测。
func TestSaintAreaReopenTime_FallbackHour(t *testing.T) {
	now := time.Now()
	got := scheduler.SaintAreaReopenTime(&protocol.SaintAreaStatus{
		CloseTimes: []protocol.SaintAreaCloseTime{{StartTime: 1, EndTime: 2}},
	}, now)
	if d := got.Sub(now); d < 55*time.Minute || d > 65*time.Minute {
		t.Fatalf("兜底唤醒时间应约为 1 小时，实际 %s", d)
	}
}
