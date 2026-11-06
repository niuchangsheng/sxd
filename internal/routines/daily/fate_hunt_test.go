package daily

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestFateHuntRoutine_AppointsUnlockedHunterAndExchanges(t *testing.T) {
	var appointCalls, exchangeCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGameAssistantInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: DailyTaskFreeFate, CurTimes: 0, MaxTimes: 1, PerActiveAdd: 5},
				{TaskID: DailyTaskBuyMingGongScrap, CurTimes: 0, MaxTimes: 1, PerActiveAdd: 5},
			}))
		case protocol.ActionIDFateGetNPC:
			_ = protocol.WritePacket(conn, protocol.BuildFateHunterPanelResultPacket(protocol.FateHunterPanel{
				FreeAppointTimes: 1,
				Hunters: []protocol.FateHunter{
					{NpcID: 4, Status: 0},
					{NpcID: 1, Status: 1},
				},
				Tail: 100,
			}))
		case protocol.ActionIDFateAppointNPC:
			appointCalls.Add(1)
			npcID := pkt.Payload[0]
			code := protocol.FateHuntResultInvalidFateNPCID
			if npcID == 1 {
				code = protocol.FateHuntResultSuccess
			}
			_ = protocol.WritePacket(conn, protocol.BuildFateAppointResultPacket(protocol.AppointFateResult{
				Result: code,
				FateID: 8,
				NpcID:  npcID,
			}))
		case protocol.ActionIDFateMingGongInfo:
			_ = protocol.WritePacket(conn, protocol.BuildFateMingGongInfoResultPacket(protocol.MingGongInfo{
				MingGong:      10,
				Scrap:         2628,
				ExchangeTimes: 500,
			}))
		case protocol.ActionIDFateExchangeMingGongScrap:
			exchangeCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildFateMingGongExchangeResultPacket(protocol.MingGongExchangeResult{
				Result: protocol.FateHuntResultSuccess,
				Scrap:  2629,
			}))
		}
	})
	defer sess.Close()

	if err := NewFateHuntRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("fate hunt: %v", err)
	}
	// 已激活的猎命师优先，因此应先尝试 npc 1 并立即成功，只调用一次。
	if got := appointCalls.Load(); got != 1 {
		t.Fatalf("appoint calls = %d, want 1", got)
	}
	if got := exchangeCalls.Load(); got != 1 {
		t.Fatalf("exchange calls = %d, want 1", got)
	}
}

func TestFateHuntRoutine_SkipsWhenTasksComplete(t *testing.T) {
	var appointCalls, exchangeCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGameAssistantInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: DailyTaskFreeFate, CurTimes: 1, MaxTimes: 1, PerActiveAdd: 5},
				{TaskID: DailyTaskBuyMingGongScrap, CurTimes: 1, MaxTimes: 1, PerActiveAdd: 5},
			}))
		case protocol.ActionIDFateAppointNPC:
			appointCalls.Add(1)
		case protocol.ActionIDFateExchangeMingGongScrap:
			exchangeCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewFateHuntRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("fate hunt: %v", err)
	}
	if appointCalls.Load() != 0 || exchangeCalls.Load() != 0 {
		t.Fatalf("expected no business calls, appoint=%d exchange=%d",
			appointCalls.Load(), exchangeCalls.Load())
	}
}

func TestFateHuntRoutine_StopsWhenNotEnoughCoin(t *testing.T) {
	var exchangeCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGameAssistantInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: DailyTaskBuyMingGongScrap, CurTimes: 0, MaxTimes: 1, PerActiveAdd: 5},
			}))
		case protocol.ActionIDFateMingGongInfo:
			_ = protocol.WritePacket(conn, protocol.BuildFateMingGongInfoResultPacket(protocol.MingGongInfo{
				ExchangeTimes: 10,
			}))
		case protocol.ActionIDFateExchangeMingGongScrap:
			exchangeCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildFateMingGongExchangeResultPacket(protocol.MingGongExchangeResult{
				Result: protocol.FateHuntResultNotEnoughCoin,
			}))
		}
	})
	defer sess.Close()

	if err := NewFateHuntRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("fate hunt: %v", err)
	}
	if got := exchangeCalls.Load(); got != 1 {
		t.Fatalf("exchange calls = %d, want 1", got)
	}
}
