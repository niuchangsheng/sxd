package dungeon_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/dungeon"
	"sxd-pie-ng/internal/scheduler"
)

func TestTongTianTowerRoutine_BagFullBranchesToCircuitBreaker(t *testing.T) {
	sess, srvConn := createTestSession(t, 5000)
	defer sess.Close()
	defer srvConn.Close()

	var panelCalls atomic.Int32
	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionIDTongTianTowerPanel:
			n := panelCalls.Add(1)
			panel := protocol.TongTianTowerPanel{
				Heaven: 1, Floor: 1, ChallengeTimes: 1, CurHeaven: 1, CurFloor: 1,
			}
			if n > 1 {
				panel.ChallengeTimes = 0
			}
			_ = protocol.WritePacket(srvConn, protocol.BuildTongTianTowerPanelResultPacket(panel))
			return true
		case protocol.ActionIDTongTianTowerChallenge:
			_ = protocol.WritePacket(srvConn, protocol.BuildTongTianTowerChallengeResultPacket(
				protocol.TongTianTowerChallengeResult{Result: protocol.TongTianTowerResultBagFull}))
			return true
		}
		return false
	})

	err := dungeon.NewTongTianTowerRoutine().Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrBagFull) {
		t.Fatalf("expected ErrBagFull, got %v", err)
	}
}

func TestTongTianTowerRoutine_ChallengeSuccessAdvances(t *testing.T) {
	sess, srvConn := createTestSession(t, 5000)
	defer sess.Close()
	defer srvConn.Close()

	var panelCalls, challengeCalls atomic.Int32
	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionIDTongTianTowerPanel:
			n := panelCalls.Add(1)
			panel := protocol.TongTianTowerPanel{
				Heaven: 1, Floor: 1, ChallengeTimes: 1, CurHeaven: 1, CurFloor: 1,
			}
			if n > 1 {
				panel.Floor = 2
				panel.ChallengeTimes = 0
			}
			_ = protocol.WritePacket(srvConn, protocol.BuildTongTianTowerPanelResultPacket(panel))
			return true
		case protocol.ActionIDTongTianTowerChallenge:
			challengeCalls.Add(1)
			_ = protocol.WritePacket(srvConn, protocol.BuildTongTianTowerChallengeResultPacket(
				protocol.TongTianTowerChallengeResult{
					Result:      protocol.TongTianTowerResultSuccess,
					DeployType:  3,
					WinPlayerID: sess.PlayerID(),
				}))
			return true
		}
		return false
	})

	if err := dungeon.NewTongTianTowerRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("执行通天塔失败: %v", err)
	}
	if challengeCalls.Load() != 1 {
		t.Fatalf("expected 1 challenge, got %d", challengeCalls.Load())
	}
	if panelCalls.Load() < 2 {
		t.Fatalf("expected panel refreshed after challenge, got %d calls", panelCalls.Load())
	}
}

func TestTongTianTowerRoutine_NotEnoughPowerBranchesToCircuitBreaker(t *testing.T) {
	sess, srvConn := createTestSession(t, 5000)
	defer sess.Close()
	defer srvConn.Close()

	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionIDTongTianTowerPanel:
			_ = protocol.WritePacket(srvConn, protocol.BuildTongTianTowerPanelResultPacket(protocol.TongTianTowerPanel{
				Heaven: 1, Floor: 1, ChallengeTimes: 3, CurHeaven: 1, CurFloor: 1,
			}))
			return true
		case protocol.ActionIDTongTianTowerChallenge:
			_ = protocol.WritePacket(srvConn, protocol.BuildTongTianTowerChallengeResultPacket(
				protocol.TongTianTowerChallengeResult{Result: protocol.TongTianTowerResultNotEnoughPower}))
			return true
		}
		return false
	})

	err := dungeon.NewTongTianTowerRoutine().Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrStaminaDepleted) {
		t.Fatalf("expected ErrStaminaDepleted, got %v", err)
	}
}

// TestTongTianTowerRoutine_SkipsWhenFunctionClosed 验证功能开放列表未包含通天塔所属
// 山海九界时不发送 panel 请求，而是直接安全跳过。
func TestTongTianTowerRoutine_SkipsWhenFunctionClosed(t *testing.T) {
	sess, srvConn := createTestSession(t, 5000)
	defer sess.Close()
	defer srvConn.Close()

	var panelCalls atomic.Int32
	startMockDungeonServerWithFunctions(srvConn, nil, func(pkt *protocol.Packet) bool {
		if pkt.ActionID == protocol.ActionIDTongTianTowerPanel {
			panelCalls.Add(1)
		}
		return false
	})

	if err := dungeon.NewTongTianTowerRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("功能未开放时应安全跳过, got %v", err)
	}
	if got := panelCalls.Load(); got != 0 {
		t.Fatalf("功能未开放时不应发送 panel 请求, 实际 %d 次", got)
	}
}
