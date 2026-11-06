package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestParseGameAssistantInfo_RoundTrip(t *testing.T) {
	tasks := []protocol.DailyActivityTask{
		{TaskID: 4, CurTimes: 0, MaxTimes: 1, PerActiveAdd: 5},
		{TaskID: 56, CurTimes: 0, MaxTimes: 1, PerActiveAdd: 5},
		{TaskID: 69, CurTimes: 1, MaxTimes: 20, PerActiveAdd: 10},
		{TaskID: 85, CurTimes: 2, MaxTimes: 5, PerActiveAdd: 20},
	}
	pkt := protocol.BuildGameAssistantInfoResultPacket(tasks)
	panel, err := protocol.ParseGameAssistantInfo(pkt.Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(panel.Tasks) != len(tasks) {
		t.Fatalf("task count = %d, want %d", len(panel.Tasks), len(tasks))
	}
	// 每日合计只统计 task_id <= 83: 0 + 0 + 10 + 周常不计 = 10
	if got := panel.DailyTotal(); got != 10 {
		t.Fatalf("daily total = %d, want 10", got)
	}
	if got := panel.RemainingTimesFor(69); got != 19 {
		t.Fatalf("remaining for 69 = %d, want 19", got)
	}
	if got := panel.RemainingTimesFor(9999); got != 0 {
		t.Fatalf("remaining for unknown = %d, want 0", got)
	}
	task, ok := panel.FindTask(4)
	if !ok || task.RemainingActive() != 5 {
		t.Fatalf("task 4 = %+v ok=%v, want remaining active 5", task, ok)
	}
}

func TestParseGameAssistantInfo_EmptyList(t *testing.T) {
	pkt := protocol.BuildGameAssistantInfoResultPacket(nil)
	panel, err := protocol.ParseGameAssistantInfo(pkt.Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(panel.Tasks) != 0 || panel.DailyTotal() != 0 {
		t.Fatalf("expected empty panel, got %+v total=%d", panel.Tasks, panel.DailyTotal())
	}
}

func TestParseGameAssistantInfo_Truncated(t *testing.T) {
	pkt := protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
		{TaskID: 1, CurTimes: 1, MaxTimes: 1, PerActiveAdd: 5},
	})
	if _, err := protocol.ParseGameAssistantInfo(pkt.Payload[:len(pkt.Payload)-3]); err == nil {
		t.Fatal("expected error for truncated payload")
	}
}
