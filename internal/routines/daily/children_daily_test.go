package daily

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestChildrenDailyRoutine_ClaimsTaskAndAttendsSchool(t *testing.T) {
	var awardCalls, attendCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDChildrenInfo:
			_ = protocol.WritePacket(conn, protocol.BuildChildrenPanelResultPacket(
				protocol.ChildrenResultSuccess, []int32{1812}))
		case protocol.ActionIDChildrenStudyInfo:
			// 子面板会话链预热：只要回包即可，内容由后续面板解析。
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDChildrenStudyInfo, []byte{0}))
		case protocol.ActionIDChildrenTaskInfo:
			_ = protocol.WritePacket(conn, protocol.BuildChildrenTaskInfoResultPacket([]protocol.ChildrenTask{
				{TaskID: 4, Cur: 0, Max: 1},
				{TaskID: 26, Cur: 1, Max: 1},
			}))
		case protocol.ActionIDChildrenTaskAward:
			awardCalls.Add(1)
			code := protocol.ChildrenTaskResultNotComplete
			if len(pkt.Payload) >= 8 && pkt.Payload[7] == 26 {
				code = protocol.ChildrenTaskResultSuccess
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDChildrenTaskAward, []byte{code}))
		case protocol.ActionIDChildrenSchoolInfo:
			_ = protocol.WritePacket(conn, protocol.BuildChildrenSchoolInfoResultPacket([]protocol.ChildrenSchool{
				{SchoolID: 22, State: protocol.ChildrenResultUndo},
			}))
		case protocol.ActionIDChildrenAttendSchool:
			attendCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDChildrenAttendSchool,
				[]byte{protocol.ChildrenResultSuccess}))
		case protocol.ActionIDGameAssistantInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: DailyTaskChildrenSchool, CurTimes: 1, MaxTimes: 1, PerActiveAdd: 5},
				{TaskID: DailyTaskChildrenMission, CurTimes: 1, MaxTimes: 1, PerActiveAdd: 5},
			}))
		}
	})
	defer sess.Close()

	if err := NewChildrenDailyRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("children daily: %v", err)
	}
	if got := awardCalls.Load(); got != 2 {
		t.Fatalf("award calls = %d, want 2", got)
	}
	if got := attendCalls.Load(); got != 1 {
		t.Fatalf("attend calls = %d, want 1", got)
	}
}

// TestChildrenDailyRoutine_ActivityPanelReadLast 验证活跃度面板在所有孩子操作之后
// 才被读取：线上实测 Module 0 Act 42 会污染同会话后续的 312/315 子面板请求。
func TestChildrenDailyRoutine_ActivityPanelReadLast(t *testing.T) {
	var mu sync.Mutex
	var order []uint32
	record := func(action uint32) {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, action)
	}
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDChildrenInfo:
			record(pkt.ActionID)
			_ = protocol.WritePacket(conn, protocol.BuildChildrenPanelResultPacket(
				protocol.ChildrenResultSuccess, []int32{1812}))
		case protocol.ActionIDChildrenStudyInfo:
			record(pkt.ActionID)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDChildrenStudyInfo, []byte{0}))
		case protocol.ActionIDChildrenTaskInfo:
			record(pkt.ActionID)
			_ = protocol.WritePacket(conn, protocol.BuildChildrenTaskInfoResultPacket(nil))
		case protocol.ActionIDChildrenSchoolInfo:
			record(pkt.ActionID)
			_ = protocol.WritePacket(conn, protocol.BuildChildrenSchoolInfoResultPacket(nil))
		case protocol.ActionIDGameAssistantInfo:
			record(pkt.ActionID)
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket(nil))
		}
	})
	defer sess.Close()

	if err := NewChildrenDailyRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("children daily: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) == 0 || order[len(order)-1] != protocol.ActionIDGameAssistantInfo {
		t.Fatalf("expected activity panel to be read last, order = %v", order)
	}
}

func TestChildrenDailyRoutine_NoChildrenSkips(t *testing.T) {
	var awardCalls, attendCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDChildrenInfo:
			_ = protocol.WritePacket(conn, protocol.BuildChildrenPanelResultPacket(
				protocol.ChildrenResultSuccess, nil))
		case protocol.ActionIDChildrenTaskAward:
			awardCalls.Add(1)
		case protocol.ActionIDChildrenAttendSchool:
			attendCalls.Add(1)
		case protocol.ActionIDGameAssistantInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket(nil))
		}
	})
	defer sess.Close()

	if err := NewChildrenDailyRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("children daily: %v", err)
	}
	if awardCalls.Load() != 0 || attendCalls.Load() != 0 {
		t.Fatalf("expected no business calls, award=%d attend=%d",
			awardCalls.Load(), attendCalls.Load())
	}
}

func TestChildrenDailyRoutine_PrimerDropSkipsSubPanels(t *testing.T) {
	var schoolCalls, taskCalls, summaryCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDChildrenInfo:
			_ = protocol.WritePacket(conn, protocol.BuildChildrenPanelResultPacket(
				protocol.ChildrenResultSuccess, []int32{1812}))
		case protocol.ActionIDChildrenStudyInfo:
			// 模拟服务端子面板链不可达：不回包。
		case protocol.ActionIDChildrenSchoolInfo:
			schoolCalls.Add(1)
		case protocol.ActionIDChildrenTaskInfo:
			taskCalls.Add(1)
		case protocol.ActionIDGameAssistantInfo:
			summaryCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket(nil))
		}
	})
	defer sess.Close()

	if err := NewChildrenDailyRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("children daily: %v", err)
	}
	if got := schoolCalls.Load(); got != 0 {
		t.Fatalf("预热失败后不应再请求学堂子面板, 实际 %d 次", got)
	}
	if got := taskCalls.Load(); got != 0 {
		t.Fatalf("预热失败后不应再请求孩子任务子面板, 实际 %d 次", got)
	}
	if got := summaryCalls.Load(); got != 1 {
		t.Fatalf("活跃度汇总仍应执行 1 次, 实际 %d 次", got)
	}
}

func TestChildrenDailyRoutine_SkipsDoneSchool(t *testing.T) {
	var attendCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDChildrenInfo:
			_ = protocol.WritePacket(conn, protocol.BuildChildrenPanelResultPacket(
				protocol.ChildrenResultSuccess, []int32{1812}))
		case protocol.ActionIDChildrenStudyInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDChildrenStudyInfo, []byte{0}))
		case protocol.ActionIDChildrenTaskInfo:
			_ = protocol.WritePacket(conn, protocol.BuildChildrenTaskInfoResultPacket(nil))
		case protocol.ActionIDChildrenSchoolInfo:
			_ = protocol.WritePacket(conn, protocol.BuildChildrenSchoolInfoResultPacket([]protocol.ChildrenSchool{
				{SchoolID: 22, State: protocol.ChildrenResultDone},
			}))
		case protocol.ActionIDChildrenAttendSchool:
			attendCalls.Add(1)
		case protocol.ActionIDGameAssistantInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket(nil))
		}
	})
	defer sess.Close()

	if err := NewChildrenDailyRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("children daily: %v", err)
	}
	if got := attendCalls.Load(); got != 0 {
		t.Fatalf("expected no attend for DONE school, got %d", got)
	}
}
