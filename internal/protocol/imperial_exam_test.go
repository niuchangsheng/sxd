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

func TestImperialExamQuestionResult(t *testing.T) {
	q, err := protocol.ParseImperialExamQuestionResult(protocol.BuildImperialExamStartResultPacket(protocol.ImperialExamQuestion{
		Result:           protocol.ImperialExamResultSuccess,
		CurAnswerSeq:     3,
		TotalTopicNumber: 10,
		QuestionID:       42,
		DecipherKey:      "testkey1",
	}).Payload)
	if err != nil || q.Result != protocol.ImperialExamResultSuccess || q.CurAnswerSeq != 3 || q.QuestionID != 42 || q.DecipherKey != "testkey1" {
		t.Fatalf("题目回包不一致: %+v err=%v", q, err)
	}
}

func TestImperialExamSubmitResult(t *testing.T) {
	// 成功路径
	res, err := protocol.ParseImperialExamSubmitResult(protocol.BuildImperialExamSubmitResultPacket(protocol.ImperialExamSubmitResult{
		Result: protocol.ImperialExamResultSuccess, ContinueRightNum: 2, RightScore: 5, RightSequence: protocol.ImperialExamSequenceA,
	}).Payload)
	if err != nil || res.Result != protocol.ImperialExamResultSuccess || res.RightSequence != protocol.ImperialExamSequenceA {
		t.Fatalf("提交回包不一致: %+v err=%v", res, err)
	}
	// 失败码: 提交超时
	res, err = protocol.ParseImperialExamSubmitResult(protocol.BuildImperialExamSubmitResultPacket(protocol.ImperialExamSubmitResult{
		Result: protocol.ImperialExamResultSubmitTimeOut,
	}).Payload)
	if err != nil || res.Result != protocol.ImperialExamResultSubmitTimeOut {
		t.Fatalf("期望提交超时码 %d, got %+v err=%v", protocol.ImperialExamResultSubmitTimeOut, res, err)
	}
}
