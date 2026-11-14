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

// TestIncompleteDailyTasks 验证未完成每日任务按剩余活跃度降序汇总，
// 已完成任务、周常任务与无活跃度任务不列出。
func TestIncompleteDailyTasks(t *testing.T) {
	panel := &protocol.DailyActivityPanel{Tasks: []protocol.DailyActivityTask{
		{TaskID: 3, CurTimes: 1, MaxTimes: 1, PerActiveAdd: 5},    // 已完成
		{TaskID: 64, CurTimes: 0, MaxTimes: 1, PerActiveAdd: 30},  // 未完成
		{TaskID: 70, CurTimes: 1, MaxTimes: 3, PerActiveAdd: 20},  // 未完成 +40
		{TaskID: 84, CurTimes: 0, MaxTimes: 1, PerActiveAdd: 100}, // 周常
		{TaskID: 71, CurTimes: 0, MaxTimes: 2, PerActiveAdd: 0},   // 无活跃度
	}}
	got := incompleteDailyTasks(panel, 20)
	want := "70:1/3(+40),64:0/1(+30)"
	if got != want {
		t.Fatalf("incomplete tasks = %q, want %q", got, want)
	}
	if got := incompleteDailyTasks(panel, 1); got != "70:1/3(+40)" {
		t.Fatalf("limit=1 = %q", got)
	}
	if got := incompleteDailyTasks(nil, 20); got != "" {
		t.Fatalf("nil panel = %q", got)
	}
}
