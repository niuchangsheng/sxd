package farming_test

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/farming"
	"sxd-pie-ng/internal/scheduler"
)

// TestCrystalMineRoutine_LeavesRestingThenMines 验证矿山返回 RESTING(12) 时，
// 先按官方客户端关闭面板的行为发送 leave_mine_mountain，再重新查询进入并领取收益。
func TestCrystalMineRoutine_LeavesRestingThenMines(t *testing.T) {
	restore := farming.SetCrystalMineSessionOpenerForTest(func(_ context.Context, s *client.RoleSession) (*client.RoleSession, error) {
		return s, nil
	})
	defer restore()
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	var infoCalls, leaveCalls, enterCalls, awardCalls atomic.Int32
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			pkt, err := protocol.ReadPacket(c2)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDPlayerLogin:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerLogin, []byte{
					0x00, 0x00, 0x00, 0x00, 0x04, 0x0a, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
					0x00, 0x00, 0x01, 0x00, 0x0b, 0x00, 0x00, 0x10, 0x85, 0x01,
				}))
			case protocol.ActionIDPlayerInitStep1:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerInitStep1,
					[]byte{0x00, 0x00, 0x13, 0x0f, 0x00, 0x02, 0x00, 0x00, 0x13, 0x0f, 0x00, 0x00, 0x13, 0x12}))
			case protocol.ActionIDPlayerInitStep2:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerInitStep2, []byte{0x00}))
			case protocol.ActionIDPlayerInitStep3:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerInitStep3, make([]byte, 8)))
			case protocol.ActionIDPlayerGetInfo:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerGetInfo, []byte{
					0x00, 0x09, 0xe6, 0xa2, 0xa6, 0xe4, 0xb8, 0x80, 0xe5, 0x9c, 0xba,
					0x00, 0x00, 0x01, 0x2c,
					0x00, 0x00, 0x00, 0x64,
					0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00,
					0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
					0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
					0x00, 0x00, 0x01, 0x2c,
				}))
			case protocol.ActionHeartbeat:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionHeartbeat, []byte{0x06, 0x00, 0x0a, 0x28}))
			case protocol.ActionIDPlayerGetFunctions:
				_ = protocol.WritePacket(c2, protocol.BuildPlayerFunctionsResultPacket(
					[]int32{protocol.FunctionIDStMine}))
			case protocol.ActionIDStMineInfo:
				n := infoCalls.Add(1)
				result := uint8(protocol.StMineResultResting)
				if n > 1 {
					result = protocol.StMineResultSuccess
				}
				resp := protocol.BuildStMineInfoResultPacket(protocol.StMineInfo{
					Result: result, Level: 1, CurMountain: 1,
				})
				_ = protocol.WritePacket(c2, resp)
			case protocol.ActionIDStMineLeave:
				leaveCalls.Add(1)
				_ = protocol.WritePacket(c2, protocol.BuildStMineActionResultPacket(
					protocol.ActionIDStMineLeave, protocol.StMineResultSuccess))
			case protocol.ActionIDStMineEnter:
				enterCalls.Add(1)
				_ = protocol.WritePacket(c2, protocol.BuildStMineEnterResultPacket(protocol.StMineHolesInfo{
					Result: protocol.StMineResultSuccess,
					PlayerHole: protocol.StMinePlayerHole{
						Status:       33, // MINE_START
						HoleID:       1,
						ReceiveCount: 5,
					},
				}))
			case protocol.ActionIDStMineReceiveAward:
				awardCalls.Add(1)
				_ = protocol.WritePacket(c2, protocol.BuildStMineActionResultPacket(
					protocol.ActionIDStMineReceiveAward, protocol.StMineResultSuccess))
			}
		}
	}()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID: "role-mine-resting", RoleName: "矿山休息测试",
		ServerAddr: "mine-resting-pipe",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return c1, nil
		},
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("session.Start 失败: %v", err)
	}
	defer session.Close()
	deadline := time.Now().Add(2 * time.Second)
	for session.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if session.State() != client.StateActive {
		t.Fatalf("会话未进入激活态: %s", session.State())
	}

	if err := farming.NewCrystalMineRoutine().Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("矿山任务执行失败: %v", err)
	}
	if infoCalls.Load() != 2 {
		t.Fatalf("休息后应重新查询矿山, info calls = %d", infoCalls.Load())
	}
	// 休息中先 leave 一次；进入矿脉后的 defer 清理还会再 leave 一次。
	if leaveCalls.Load() != 2 {
		t.Fatalf("leave calls = %d, want 2", leaveCalls.Load())
	}
	if enterCalls.Load() != 1 || awardCalls.Load() != 1 {
		t.Fatalf("应进入并领取收益, enter=%d award=%d", enterCalls.Load(), awardCalls.Load())
	}

	session.Close()
	c1.Close()
	c2.Close()
	wg.Wait()
}

// newMineMock 启动最小登录握手，并把矿山业务包交给 handler。
func newMineMock(t *testing.T, handler func(conn net.Conn, pkt *protocol.Packet), functionIDs ...int32) *client.RoleSession {
	t.Helper()
	if len(functionIDs) == 0 {
		functionIDs = []int32{protocol.FunctionIDStMine}
	}
	c1, c2 := net.Pipe()
	go func() {
		defer c2.Close()
		for {
			pkt, err := protocol.ReadPacket(c2)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDPlayerLogin:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerLogin, []byte{
					0x00, 0x00, 0x00, 0x00, 0x04, 0x0a, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
					0x00, 0x00, 0x01, 0x00, 0x0b, 0x00, 0x00, 0x10, 0x85, 0x01,
				}))
			case protocol.ActionIDPlayerInitStep1:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerInitStep1,
					[]byte{0x00, 0x00, 0x13, 0x0f, 0x00, 0x02, 0x00, 0x00, 0x13, 0x0f, 0x00, 0x00, 0x13, 0x12}))
			case protocol.ActionIDPlayerInitStep2:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerInitStep2, []byte{0x00}))
			case protocol.ActionIDPlayerInitStep3:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerInitStep3, make([]byte, 8)))
			case protocol.ActionIDPlayerGetInfo:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerGetInfo, []byte{
					0x00, 0x09, 0xe6, 0xa2, 0xa6, 0xe4, 0xb8, 0x80, 0xe5, 0x9c, 0xba,
					0x00, 0x00, 0x01, 0x2c,
					0x00, 0x00, 0x00, 0x64,
					0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00,
					0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
					0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
					0x00, 0x00, 0x01, 0x2c,
				}))
			case protocol.ActionHeartbeat:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionHeartbeat, []byte{0x06, 0x00, 0x0a, 0x28}))
			case protocol.ActionIDPlayerGetFunctions:
				_ = protocol.WritePacket(c2, protocol.BuildPlayerFunctionsResultPacket(functionIDs))
			default:
				handler(c2, pkt)
			}
		}
	}()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID: "role-mine-mock", RoleName: "矿山单测", ServerAddr: "mine-mock-pipe",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return c1, nil
		},
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("session.Start 失败: %v", err)
	}
	t.Cleanup(session.Close)
	deadline := time.Now().Add(2 * time.Second)
	for session.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if session.State() != client.StateActive {
		t.Fatalf("会话未进入激活态: %s", session.State())
	}
	return session
}

func mineInfoSuccess() *protocol.Packet {
	return protocol.BuildStMineInfoResultPacket(protocol.StMineInfo{
		Result: protocol.StMineResultSuccess, Level: 1, CurMountain: 1,
	})
}

// TestCrystalMineRoutine_RobsEmptyHole 验证未占据矿洞时优先占领空矿洞并领取收益。
func TestCrystalMineRoutine_RobsEmptyHole(t *testing.T) {
	restore := farming.SetCrystalMineSessionOpenerForTest(func(_ context.Context, s *client.RoleSession) (*client.RoleSession, error) {
		return s, nil
	})
	defer restore()
	var robCalls, awardCalls atomic.Int32
	var robPayload []byte
	session := newMineMock(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDStMineInfo:
			_ = protocol.WritePacket(conn, mineInfoSuccess())
		case protocol.ActionIDStMineEnter:
			_ = protocol.WritePacket(conn, protocol.BuildStMineEnterResultPacket(protocol.StMineHolesInfo{
				Result:         protocol.StMineResultSuccess,
				RemainRobCount: 3,
				Holes: []protocol.StMineHole{
					{HoleID: 7, Status: protocol.StMineHoleEmpty},
					{HoleID: 9, Status: protocol.StMineHoleRob, PlayerID: 888, PlayerLevel: 120},
				},
			}))
		case protocol.ActionIDStMineRobHole:
			robCalls.Add(1)
			robPayload = append([]byte(nil), pkt.Payload...)
			_ = protocol.WritePacket(conn, protocol.BuildStMineRobHoleResultPacket(protocol.StMineResultSuccess))
		case protocol.ActionIDStMineReceiveAward:
			awardCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildStMineActionResultPacket(
				protocol.ActionIDStMineReceiveAward, protocol.StMineResultSuccess))
		case protocol.ActionIDStMineLeave:
			_ = protocol.WritePacket(conn, protocol.BuildStMineActionResultPacket(
				protocol.ActionIDStMineLeave, protocol.StMineResultSuccess))
		}
	})

	if err := farming.NewCrystalMineRoutine().Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("矿山任务执行失败: %v", err)
	}
	if robCalls.Load() != 1 {
		t.Fatalf("应占领空矿洞, rob calls = %d", robCalls.Load())
	}
	if len(robPayload) < 6 {
		t.Fatalf("264/3 载荷过短: %x", robPayload)
	}
	if got := int(robPayload[0])<<8 | int(robPayload[1]); got != 7 {
		t.Fatalf("应选择空矿洞 7, 实际 %d", got)
	}
	if awardCalls.Load() != 1 {
		t.Fatalf("占领成功后应领取收益, award calls = %d", awardCalls.Load())
	}
}

// TestCrystalMineRoutine_RobCooldown 验证抢夺冷却中不发送 264/3，并按冷却唤醒。
func TestCrystalMineRoutine_RobCooldown(t *testing.T) {
	restore := farming.SetCrystalMineSessionOpenerForTest(func(_ context.Context, s *client.RoleSession) (*client.RoleSession, error) {
		return s, nil
	})
	defer restore()
	var robCalls atomic.Int32
	session := newMineMock(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDStMineInfo:
			_ = protocol.WritePacket(conn, mineInfoSuccess())
		case protocol.ActionIDStMineEnter:
			_ = protocol.WritePacket(conn, protocol.BuildStMineEnterResultPacket(protocol.StMineHolesInfo{
				Result:           protocol.StMineResultSuccess,
				RemainRobCount:   3,
				NextRobTimestamp: int32(time.Now().Add(10 * time.Minute).Unix()),
				Holes: []protocol.StMineHole{
					{HoleID: 7, Status: protocol.StMineHoleEmpty},
				},
			}))
		case protocol.ActionIDStMineRobHole:
			robCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildStMineRobHoleResultPacket(protocol.StMineResultSuccess))
		case protocol.ActionIDStMineLeave:
			_ = protocol.WritePacket(conn, protocol.BuildStMineActionResultPacket(
				protocol.ActionIDStMineLeave, protocol.StMineResultSuccess))
		}
	})

	err := farming.NewCrystalMineRoutine().Execute(context.Background(), session, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("抢夺冷却时应返回 CooldownError, 实际: %v", err)
	}
	if robCalls.Load() != 0 {
		t.Fatalf("冷却中不得发送抢夺, rob calls = %d", robCalls.Load())
	}
}

// TestCrystalMineRoutine_FunctionClosedSkips 验证功能未开放时不发送任何矿山业务包。
func TestCrystalMineRoutine_FunctionClosedSkips(t *testing.T) {
	restore := farming.SetCrystalMineSessionOpenerForTest(func(_ context.Context, s *client.RoleSession) (*client.RoleSession, error) {
		return s, nil
	})
	defer restore()
	var infoCalls atomic.Int32
	session := newMineMock(t, func(conn net.Conn, pkt *protocol.Packet) {
		if pkt.ActionID == protocol.ActionIDStMineInfo {
			infoCalls.Add(1)
		}
	}, 999) // 已开放功能列表不含 157 (StMine)

	if err := farming.NewCrystalMineRoutine().Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("功能未开放时应正常跳过: %v", err)
	}
	if infoCalls.Load() != 0 {
		t.Fatalf("功能未开放时不应查询矿山, info calls = %d", infoCalls.Load())
	}
}
