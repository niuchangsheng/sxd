package protocol_test

import (
	"fmt"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestParseChildrenPanel_RoundTrip(t *testing.T) {
	pkt := protocol.BuildChildrenPanelResultPacket(protocol.ChildrenResultSuccess, []int32{1812, 2000})
	panel, err := protocol.ParseChildrenPanel(pkt.Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if panel.Result != protocol.ChildrenResultSuccess {
		t.Fatalf("result = %d", panel.Result)
	}
	if len(panel.ChildIDs) != 2 || panel.ChildIDs[0] != 1812 || panel.ChildIDs[1] != 2000 {
		t.Fatalf("child ids = %v", panel.ChildIDs)
	}
}

func TestParseChildrenPanel_NonSuccess(t *testing.T) {
	pkt := protocol.BuildChildrenPanelResultPacket(protocol.ChildrenResultUndo, nil)
	panel, err := protocol.ParseChildrenPanel(pkt.Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if panel.Result != protocol.ChildrenResultUndo || len(panel.ChildIDs) != 0 {
		t.Fatalf("panel = %+v", panel)
	}
}

func TestParseChildrenSchoolInfo_RoundTrip(t *testing.T) {
	pkt := protocol.BuildChildrenSchoolInfoResultPacket([]protocol.ChildrenSchool{
		{SchoolID: 22, State: protocol.ChildrenResultUndo},
		{SchoolID: 23, State: protocol.ChildrenResultDone},
	})
	schools, err := protocol.ParseChildrenSchoolInfo(pkt.Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(schools) != 2 || schools[0].SchoolID != 22 || schools[0].State != protocol.ChildrenResultUndo ||
		schools[1].SchoolID != 23 || schools[1].State != protocol.ChildrenResultDone {
		t.Fatalf("schools = %+v", schools)
	}
}

func TestParseChildrenTaskInfo_RoundTrip(t *testing.T) {
	pkt := protocol.BuildChildrenTaskInfoResultPacket([]protocol.ChildrenTask{
		{TaskID: 4, Cur: 0, Max: 1},
		{TaskID: 26, Cur: 1, Max: 1},
	})
	tasks, err := protocol.ParseChildrenTaskInfo(pkt.Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tasks) != 2 || tasks[0].TaskID != 4 || tasks[1].Cur != 1 || tasks[1].Max != 1 {
		t.Fatalf("tasks = %+v", tasks)
	}
}

func TestParseChildrenResultCode(t *testing.T) {
	if code, err := protocol.ParseChildrenResultCode([]byte{protocol.ChildrenTaskResultSuccess}); err != nil || code != 2 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if _, err := protocol.ParseChildrenResultCode(nil); err == nil {
		t.Fatal("expected error for empty payload")
	}
}

func TestChildrenPacketBuilders(t *testing.T) {
	info := protocol.BuildChildrenInfoPacket()
	if info.ActionID != protocol.ActionIDChildrenInfo || !info.HasPrevAct || len(info.Payload) != 4 {
		t.Fatalf("info packet = %+v", info)
	}
	attend := protocol.BuildChildrenAttendSchoolPacket(1812, 22)
	if attend.ActionID != protocol.ActionIDChildrenAttendSchool || len(attend.Payload) != 5 ||
		attend.Payload[4] != 22 {
		t.Fatalf("attend packet = %+v", attend)
	}
	study := protocol.BuildChildrenStudyInfoPacket(1812)
	if study.ActionID != protocol.ActionIDChildrenStudyInfo || !study.HasPrevAct || len(study.Payload) != 8 {
		t.Fatalf("study packet = %+v", study)
	}
	award := protocol.BuildChildrenTaskAwardPacket(1812, 26)
	if award.ActionID != protocol.ActionIDChildrenTaskAward || len(award.Payload) != 8 {
		t.Fatalf("award packet = %+v", award)
	}
}

// TestParseChildrenPanel_LivePayload 使用 2026-11-06 线上真实回包校验解析器，
// 覆盖孩子记录中 UByte,Int,UByte,UByte,UByte 五字段尾部布局。
func TestParseChildrenPanel_LivePayload(t *testing.T) {
	const hexPayload = "0700251363000500320001000007140001a0ec02040006e59ba2e59ba20006e98181e4b8800006e5908ce5b098000506000002e200000320040000003200000032070000765c0000765c05000000500000005001000000c80000003200000000001400050a000000ff00000b0000003c0000080000cf08000009000011ad03340c0000012c000000030706090005081110040c0000000000050605016501f800ab00000000000100"
	payload := make([]byte, len(hexPayload)/2)
	for i := 0; i < len(payload); i++ {
		var v byte
		_, err := fmt.Sscanf(hexPayload[i*2:i*2+2], "%02x", &v)
		if err != nil {
			t.Fatalf("hex decode at %d: %v", i, err)
		}
		payload[i] = v
	}
	panel, err := protocol.ParseChildrenPanel(payload)
	if err != nil {
		t.Fatalf("parse live payload: %v", err)
	}
	if panel.Result != protocol.ChildrenResultSuccess {
		t.Fatalf("result = %d", panel.Result)
	}
	if len(panel.ChildIDs) != 1 || panel.ChildIDs[0] != 1812 {
		t.Fatalf("child ids = %v", panel.ChildIDs)
	}
}
