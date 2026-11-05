// Package protocol_test 提供金榜题名状态协议 (Module 237) 的测试用例。
package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestImperialExamStatusResult(t *testing.T) {
	status, err := protocol.ParseImperialExamStatusResult(protocol.BuildImperialExamStatusResultPacket(protocol.ImperialExamStatus{
		StartTime: 100, EndTime: 200, Status: protocol.ImperialExamActivityStart,
	}).Payload)
	if err != nil || status.StartTime != 100 || status.EndTime != 200 || status.Status != protocol.ImperialExamActivityStart {
		t.Fatalf("金榜题名状态不一致: %+v err=%v", status, err)
	}
	// 活动已结束
	status, err = protocol.ParseImperialExamStatusResult(protocol.BuildImperialExamStatusResultPacket(protocol.ImperialExamStatus{
		Status: protocol.ImperialExamActivityEnd,
	}).Payload)
	if err != nil || status.Status != protocol.ImperialExamActivityEnd {
		t.Fatalf("期望活动结束状态, got %+v err=%v", status, err)
	}
}
