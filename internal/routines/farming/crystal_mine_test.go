package farming_test

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/farming"
)

// TestCrystalMineRoutine_LeavesRestingThenMines 验证矿山返回 RESTING(12) 时，
// 先按官方客户端关闭面板的行为发送 leave_mine_mountain，再重新查询进入并领取收益。
func TestCrystalMineRoutine_LeavesRestingThenMines(t *testing.T) {
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
				_ = protocol.WritePacket(c2, protocol.BuildStMineActionResultPacket(
					protocol.ActionIDStMineEnter, protocol.StMineResultSuccess))
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
