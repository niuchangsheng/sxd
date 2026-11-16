package social

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

func TestStUnionRitualRoutine_IncenseAndFights(t *testing.T) {
	var incenseCalls, fightCalls, buyCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDStUnionGodInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		case protocol.ActionIDStUnionGodIncense:
			incenseCalls.Add(1)
			if got := int32(uint32(pkt.Payload[0])<<24 | uint32(pkt.Payload[1])<<16 |
				uint32(pkt.Payload[2])<<8 | uint32(pkt.Payload[3])); got != 1 {
				t.Errorf("上香档位 = %d, want 1", got)
			}
			_ = protocol.WritePacket(conn, protocol.BuildStUnionIncenseResultPacket(
				protocol.StUnionIncenseSuccess, 34, 34, 0))
		case protocol.ActionIDStUnionTaskInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		case protocol.ActionIDStUnionTaskFight:
			n := fightCalls.Add(1)
			result := uint8(protocol.StUnionTaskSuccess)
			if n >= 3 {
				result = protocol.StUnionTaskTimesUsed
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{result, 0, 0, 0, 0, 0}))
		case 0x01510003: // buy_ticket：自动化禁止调用
			buyCalls.Add(1)
		}
	})
	restore := SetStUnionSessionOpenerForTest(func(_ context.Context, s *client.RoleSession) (*client.RoleSession, error) {
		return s, nil
	})
	defer restore()
	restoreSect := SetStUnionSectSessionOpenerForTest(func(_ context.Context, _ *client.RoleSession) (*client.RoleSession, error) {
		return nil, errors.New("skip sect session in test")
	})
	defer restoreSect()
	defer sess.Close()

	if err := NewStUnionRitualRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("st union ritual: %v", err)
	}
	if incenseCalls.Load() != 1 {
		t.Fatalf("应上香 1 次, calls = %d", incenseCalls.Load())
	}
	if fightCalls.Load() != 3 {
		t.Fatalf("应在第 3 次次数用完时停止, calls = %d", fightCalls.Load())
	}
	if buyCalls.Load() != 0 {
		t.Fatal("禁止调用 337/3 购买挑战券")
	}
}

func TestStUnionRitualRoutine_BuysTicketThenFights(t *testing.T) {
	var fightCalls, buyCalls atomic.Int32
	stUnionTicketBuyDisabled.Store(false)
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDStUnionTreeInfo, protocol.ActionIDStUnionGodInfo, protocol.ActionIDStUnionTaskInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		case protocol.ActionIDStUnionGodIncense:
			_ = protocol.WritePacket(conn, protocol.BuildStUnionIncenseResultPacket(
				protocol.StUnionIncenseNoCoin, 0, 0, 0))
		case protocol.ActionIDStUnionTaskFight:
			if fightCalls.Add(1) == 1 {
				// 第一次无券，购买后第二次成功。
				_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
					[]byte{protocol.StUnionTaskNoTicket, 0, 0, 0, 0, 0}))
				return
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.StUnionTaskSuccess, 0, 0, 0, 0, 0}))
		case protocol.ActionIDStUnionTaskBuyTicket:
			buyCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.StUnionTaskSuccess}))
		}
	})
	restore := SetStUnionSessionOpenerForTest(func(_ context.Context, s *client.RoleSession) (*client.RoleSession, error) {
		return s, nil
	})
	defer restore()
	restoreSect := SetStUnionSectSessionOpenerForTest(func(_ context.Context, _ *client.RoleSession) (*client.RoleSession, error) {
		return nil, errors.New("skip sect session in test")
	})
	defer restoreSect()
	defer sess.Close()

	if err := NewStUnionRitualRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("st union ritual: %v", err)
	}
	if buyCalls.Load() != 1 {
		t.Fatalf("无券时应购买 1 次挑战券, buys = %d", buyCalls.Load())
	}
	if fightCalls.Load() < 2 {
		t.Fatalf("购买后应再次挑战, fight calls = %d", fightCalls.Load())
	}
}

func TestStUnionRitualRoutine_IncenseFallsBackToSectTownSession(t *testing.T) {
	// 跨服会话：面板可读，但 179/11 被静默丢弃（不回包）。
	var sectTree, sectGod, sectIncense atomic.Int32
	cross := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDStUnionTreeInfo, protocol.ActionIDStUnionGodInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		case protocol.ActionIDStUnionGodIncense:
			// 跨服会话静默丢弃：不回任何包。
		case protocol.ActionIDStUnionTaskInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		case protocol.ActionIDStUnionTaskFight:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				[]byte{protocol.StUnionTaskNoTicket, 0, 0, 0, 0, 0}))
		}
	})
	town := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDStUnionTreeInfo:
			sectTree.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		case protocol.ActionIDStUnionGodInfo:
			sectGod.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		case protocol.ActionIDStUnionGodIncense:
			sectIncense.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildStUnionIncenseResultPacket(
				protocol.StUnionIncenseSuccess, 34, 34, 5))
		}
	})
	restore := SetStUnionSessionOpenerForTest(func(_ context.Context, s *client.RoleSession) (*client.RoleSession, error) {
		return s, nil
	})
	defer restore()
	restoreSect := SetStUnionSectSessionOpenerForTest(func(_ context.Context, _ *client.RoleSession) (*client.RoleSession, error) {
		return town, nil
	})
	defer restoreSect()
	defer cross.Close()
	defer town.Close()

	if err := NewStUnionRitualRoutine().Execute(context.Background(), cross, nil); err != nil {
		t.Fatalf("st union ritual: %v", err)
	}
	if sectTree.Load() != 1 || sectGod.Load() != 1 || sectIncense.Load() != 1 {
		t.Fatalf("城镇会话应依次发送 179/0、179/10、179/11，实际 tree=%d god=%d incense=%d",
			sectTree.Load(), sectGod.Load(), sectIncense.Load())
	}
}

func TestStUnionRitualRoutine_CrossSessionFailed(t *testing.T) {
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {})
	restore := SetStUnionSessionOpenerForTest(func(_ context.Context, _ *client.RoleSession) (*client.RoleSession, error) {
		return nil, errors.New("cross unavailable")
	})
	defer restore()
	restoreSect := SetStUnionSectSessionOpenerForTest(func(_ context.Context, _ *client.RoleSession) (*client.RoleSession, error) {
		return nil, errors.New("skip sect session in test")
	})
	defer restoreSect()
	defer sess.Close()

	err := NewStUnionRitualRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("跨服会话失败应返回 CooldownError, got %v", err)
	}
}
