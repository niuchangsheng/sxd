package daily

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestDailyActivityReportRoutine_ReadsPanel(t *testing.T) {
	var calls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		if pkt.ActionID == protocol.ActionIDGameAssistantInfo {
			calls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: 3, CurTimes: 1, MaxTimes: 1, PerActiveAdd: 5},
				{TaskID: 84, CurTimes: 1, MaxTimes: 1, PerActiveAdd: 20},
			}))
		}
	})
	defer sess.Close()

	if err := NewDailyActivityReportRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("activity report: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("panel calls = %d, want 1", got)
	}
}

func TestDailyActivityReportRoutine_NoResponseIsNonFatal(t *testing.T) {
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {})
	defer sess.Close()

	if err := NewDailyActivityReportRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("activity report should degrade gracefully: %v", err)
	}
}
