package daily

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func TestTacticsWaterRoutine_BuysUntilLimit(t *testing.T) {
	var buyCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGameAssistantInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: DailyTaskTactics, CurTimes: 0, MaxTimes: 3, PerActiveAdd: 5},
			}))
		case protocol.ActionIDTacticsBuyItem:
			n := buyCalls.Add(1)
			code := protocol.TacticsResultSucceed
			if n >= 3 {
				code = protocol.TacticsResultBuyLimit
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDTacticsBuyItem, []byte{code}))
		}
	})
	defer sess.Close()
	sess.UpdatePlayerState(func(ps *client.PlayerState) { ps.Coins = 100_000_000 })

	if err := NewTacticsWaterRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("tactics water: %v", err)
	}
	if got := buyCalls.Load(); got != 3 {
		t.Fatalf("buy calls = %d, want 3", got)
	}
}

func TestTacticsWaterRoutine_SkipsWhenTaskComplete(t *testing.T) {
	var buyCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGameAssistantInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: DailyTaskTactics, CurTimes: 25, MaxTimes: 25, PerActiveAdd: 5},
			}))
		case protocol.ActionIDTacticsBuyItem:
			buyCalls.Add(1)
		}
	})
	defer sess.Close()
	sess.UpdatePlayerState(func(ps *client.PlayerState) { ps.Coins = 100_000_000 })

	if err := NewTacticsWaterRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("tactics water: %v", err)
	}
	if got := buyCalls.Load(); got != 0 {
		t.Fatalf("buy calls = %d, want 0", got)
	}
}

func TestTacticsWaterRoutine_CoinGuard(t *testing.T) {
	var buyCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGameAssistantInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: DailyTaskTactics, CurTimes: 0, MaxTimes: 5, PerActiveAdd: 5},
			}))
		case protocol.ActionIDTacticsBuyItem:
			buyCalls.Add(1)
		}
	})
	defer sess.Close()
	sess.UpdatePlayerState(func(ps *client.PlayerState) { ps.Coins = 1_000_000 })

	if err := NewTacticsWaterRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("tactics water: %v", err)
	}
	if got := buyCalls.Load(); got != 0 {
		t.Fatalf("buy calls = %d, want 0 (coin guard)", got)
	}
}

func TestTacticsWaterRoutine_BuySilentDropCooldown(t *testing.T) {
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGameAssistantInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: DailyTaskTactics, CurTimes: 0, MaxTimes: 3, PerActiveAdd: 5},
			}))
		}
	})
	defer sess.Close()

	err := NewTacticsWaterRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("购买无回包应返回 CooldownError, 实际: %v", err)
	}
}
