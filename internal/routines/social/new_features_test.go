package social

import (
	"context"
	"encoding/binary"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
)

func newSocialMockSession(t *testing.T, handler func(net.Conn, *protocol.Packet)) *client.RoleSession {
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
			handler(serverConn, pkt)
		}
	}()
	sess := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role-social-new-test",
		RoleName:   "社交测试",
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
		ps.Coins = 1_000_000
	})
	return sess
}

func TestCollectionBookletsRoutine_ClaimAwardsAndApprove(t *testing.T) {
	var awardCalls, approveCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDCollectionBookletsPopularityInfo:
			_ = protocol.WritePacket(conn, protocol.BuildCollectionBookletsPopularityInfoResultPacket(
				protocol.CollectionBookletsPopularityInfo{Popularity: 160, ClaimedIDs: []int32{1, 2}}))
		case protocol.ActionIDCollectionBookletsGetPopularityAward:
			awardCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildCollectionBookletsActionCodePacket(
				protocol.ActionIDCollectionBookletsGetPopularityAward, protocol.CollectionBookletsResultSuccess))
		case protocol.ActionIDCollectionBookletsMyFriend:
			_ = protocol.WritePacket(conn, protocol.BuildCollectionBookletsMyFriendResultPacket(
				protocol.CollectionBookletsResultSuccess, []protocol.CollectionBookletsFriend{{
					RoleID: 1, PlayerID: 123, ServerName: "s0", Nickname: "好友", IsOpenVisit: 1,
				}}))
		case protocol.ActionIDCollectionBookletsApprove:
			approveCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildCollectionBookletsApproveResultPacket(
				protocol.CollectionBookletsApproveResult{Result: protocol.CollectionBookletsResultSuccess, LeftApproveTimes: 0}))
		}
	})
	defer sess.Close()

	if err := NewCollectionBookletsRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("collection booklets: %v", err)
	}
	if awardCalls.Load() != 2 {
		t.Fatalf("expected 2 popularity awards (levels 3/4), got %d", awardCalls.Load())
	}
	if approveCalls.Load() != 1 {
		t.Fatalf("expected 1 friend approval, got %d", approveCalls.Load())
	}
}

func TestCollectionBookletsRoutine_ApproveTimesLimitStops(t *testing.T) {
	var approveCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDCollectionBookletsPopularityInfo:
			_ = protocol.WritePacket(conn, protocol.BuildCollectionBookletsPopularityInfoResultPacket(
				protocol.CollectionBookletsPopularityInfo{Popularity: 0}))
		case protocol.ActionIDCollectionBookletsMyFriend:
			_ = protocol.WritePacket(conn, protocol.BuildCollectionBookletsMyFriendResultPacket(
				protocol.CollectionBookletsResultSuccess, []protocol.CollectionBookletsFriend{
					{RoleID: 1, PlayerID: 123, ServerName: "s0", IsOpenVisit: 1},
					{RoleID: 2, PlayerID: 124, ServerName: "s0", IsOpenVisit: 1},
				}))
		case protocol.ActionIDCollectionBookletsApprove:
			approveCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildCollectionBookletsApproveResultPacket(
				protocol.CollectionBookletsApproveResult{Result: protocol.CollectionBookletsResultNotEnoughApproveTimes}))
		}
	})
	defer sess.Close()

	if err := NewCollectionBookletsRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("collection booklets: %v", err)
	}
	if approveCalls.Load() != 1 {
		t.Fatalf("expected stop after approve limit, got %d", approveCalls.Load())
	}
}

func TestNpcFriendshipRoutine_SkipsMakeFriendBelowExpThreshold(t *testing.T) {
	var makeCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDNpcFriendshipInfo:
			_ = protocol.WritePacket(conn, protocol.BuildNpcFriendshipInfoResultPacket(protocol.NpcFriendshipSnapshot{
				Npcs: []protocol.NpcFriendshipInfo{{NpcID: 13, CurExp: 500, IsFriend: 0}},
			}))
		case protocol.ActionIDNpcFriendshipPresentInfo:
			_ = protocol.WritePacket(conn, protocol.BuildNpcFriendshipPresentInfoResultPacket(map[int32]int32{}, 0))
		case protocol.ActionIDNpcFriendshipMakeFriend:
			makeCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewNpcFriendshipRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("npc friendship: %v", err)
	}
	if got := makeCalls.Load(); got != 0 {
		t.Fatalf("好感度不足时不得发起结义, 实际 %d 次", got)
	}
}

func TestNpcFriendshipRoutine_GivesPresentToNonFriend(t *testing.T) {
	var makeCalls, giveCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDNpcFriendshipInfo:
			_ = protocol.WritePacket(conn, protocol.BuildNpcFriendshipInfoResultPacket(protocol.NpcFriendshipSnapshot{
				Npcs: []protocol.NpcFriendshipInfo{{NpcID: 13, CurExp: 1000, IsFriend: 0}},
			}))
		case protocol.ActionIDNpcFriendshipPresentInfo:
			_ = protocol.WritePacket(conn, protocol.BuildNpcFriendshipPresentInfoResultPacket(map[int32]int32{}, 1))
		case protocol.ActionIDNpcFriendshipMakeFriend:
			makeCalls.Add(1)
		case protocol.ActionIDNpcFriendshipGivePresent:
			giveCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildNpcFriendshipGiveResultPacket(protocol.NpcFriendshipGiveResult{
				Result: protocol.NpcFriendshipResultSuccess, NewExp: 1004, NpcID: 13,
			}))
		}
	})
	defer sess.Close()

	if err := NewNpcFriendshipRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("npc friendship: %v", err)
	}
	if got := makeCalls.Load(); got != 0 {
		t.Fatalf("好感度不足时不得发起结义, 实际 %d 次", got)
	}
	if got := giveCalls.Load(); got != 1 {
		t.Fatalf("未结义 NPC 也应收到伴手礼以提升好感度, 实际 %d 次", got)
	}
}

func TestNpcFriendshipRoutine_MakesFriendAboveExpThreshold(t *testing.T) {
	var makeCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDNpcFriendshipInfo:
			_ = protocol.WritePacket(conn, protocol.BuildNpcFriendshipInfoResultPacket(protocol.NpcFriendshipSnapshot{
				Npcs: []protocol.NpcFriendshipInfo{{NpcID: 13, CurExp: 1100, IsFriend: 0}},
			}))
		case protocol.ActionIDNpcFriendshipPresentInfo:
			_ = protocol.WritePacket(conn, protocol.BuildNpcFriendshipPresentInfoResultPacket(map[int32]int32{}, 0))
		case protocol.ActionIDNpcFriendshipMakeFriend:
			makeCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildNpcFriendshipFriendResultPacket(protocol.NpcFriendshipFriendResult{
				Result: protocol.NpcFriendshipResultSuccess, IsFriend: 1, NpcID: 13,
			}))
		}
	})
	defer sess.Close()

	if err := NewNpcFriendshipRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("npc friendship: %v", err)
	}
	if got := makeCalls.Load(); got != 1 {
		t.Fatalf("好感度达标时应发起结义, 实际 %d 次", got)
	}
}

func TestNpcFriendshipRoutine_GiveSouvenir(t *testing.T) {
	var giveCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDNpcFriendshipInfo:
			_ = protocol.WritePacket(conn, protocol.BuildNpcFriendshipInfoResultPacket(protocol.NpcFriendshipSnapshot{
				Npcs:        []protocol.NpcFriendshipInfo{{NpcID: 13, CurExp: 100, IsFriend: 1}},
				FriendCount: 1,
			}))
		case protocol.ActionIDNpcFriendshipPresentInfo:
			_ = protocol.WritePacket(conn, protocol.BuildNpcFriendshipPresentInfoResultPacket(map[int32]int32{}, 1))
		case protocol.ActionIDNpcFriendshipGivePresent:
			giveCalls.Add(1)
			if len(pkt.Payload) < 8 || int32(binary.BigEndian.Uint32(pkt.Payload[:4])) != protocol.NpcFriendshipPresentSouvenir {
				t.Errorf("unexpected give present payload: %x", pkt.Payload)
			}
			_ = protocol.WritePacket(conn, protocol.BuildNpcFriendshipGiveResultPacket(protocol.NpcFriendshipGiveResult{
				Result: protocol.NpcFriendshipResultSuccess, NewExp: 104, NpcID: 13,
			}))
		}
	})
	defer sess.Close()

	if err := NewNpcFriendshipRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("npc friendship: %v", err)
	}
	if giveCalls.Load() != 1 {
		t.Fatalf("expected 1 souvenir gift, got %d", giveCalls.Load())
	}
}

func TestNpcFriendshipRoutine_CountLimitStops(t *testing.T) {
	var giveCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDNpcFriendshipInfo:
			_ = protocol.WritePacket(conn, protocol.BuildNpcFriendshipInfoResultPacket(protocol.NpcFriendshipSnapshot{
				Npcs: []protocol.NpcFriendshipInfo{{NpcID: 13, CurExp: 100, IsFriend: 1}},
			}))
		case protocol.ActionIDNpcFriendshipPresentInfo:
			_ = protocol.WritePacket(conn, protocol.BuildNpcFriendshipPresentInfoResultPacket(map[int32]int32{}, 3))
		case protocol.ActionIDNpcFriendshipGivePresent:
			giveCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildNpcFriendshipGiveResultPacket(protocol.NpcFriendshipGiveResult{
				Result: protocol.NpcFriendshipResultCountLimit,
			}))
		}
	})
	defer sess.Close()

	if err := NewNpcFriendshipRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("npc friendship: %v", err)
	}
	if giveCalls.Load() != 1 {
		t.Fatalf("expected stop after count limit, got %d", giveCalls.Load())
	}
}
