package daily

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// dailyAutomationFunctionIDs Mock 环境视为已开放的玩法功能编号。
var dailyAutomationFunctionIDs = []int32{
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

func newDailyMockSession(t *testing.T, handler func(net.Conn, *protocol.Packet)) *client.RoleSession {
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
				_ = protocol.WritePacket(serverConn, protocol.BuildPlayerFunctionsResultPacket(dailyAutomationFunctionIDs))
				continue
			}
			// 神龙龙珠一键合成默认成功，避免各例程单测等待超时；
			// 需要断言合并品质的用例请自建 mock 会话。
			if pkt.ActionID == protocol.ActionIDDragonballOneKeyMerge {
				_ = protocol.WritePacket(serverConn, protocol.BuildDragonballOneKeyMergeResultPacket(protocol.DragonballResultSuccess, nil))
				continue
			}
			handler(serverConn, pkt)
		}
	}()
	sess := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role-daily-quest-test",
		RoleName:   "日常测试",
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
	return sess
}

func TestDailyQuestRoutine_OneKeyClaimsAndSkipsEmptyTabs(t *testing.T) {
	var calls int
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		if pkt.ActionID != protocol.ActionIDAssistantOneKeyGetAward {
			return
		}
		calls++
		var code uint8 = protocol.AssistantResultSuccess
		var awards []protocol.AssistantAward
		if calls == 1 {
			awards = []protocol.AssistantAward{{ItemID: 1748, ItemNum: 350000}}
		} else {
			code = protocol.AssistantResultNotEnoughNum
		}
		_ = protocol.WritePacket(conn, protocol.BuildAssistantOneKeyResultPacket(code, awards))
	})
	defer sess.Close()

	if err := NewDailyQuestRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("daily quest: %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 tab attempts, got %d", calls)
	}
}

func TestDailyQuestRoutine_PackFullBranchesToCircuitBreaker(t *testing.T) {
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		if pkt.ActionID != protocol.ActionIDAssistantOneKeyGetAward {
			return
		}
		_ = protocol.WritePacket(conn, protocol.BuildAssistantOneKeyResultPacket(protocol.AssistantResultPackFull, nil))
	})
	defer sess.Close()

	err := NewDailyQuestRoutine().Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrBagFull) {
		t.Fatalf("expected ErrBagFull, got %v", err)
	}
}
