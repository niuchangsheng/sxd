// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// @author Ateng
// @since 2026-10-09
package protocol

import (
	"encoding/hex"
	"testing"
)

// 以下 Hex 均为 2026 官方微端 Mod_TravelEvent_Base 线上实机回包样本。
const (
	goldenFateInfoPayload = "000000000000000a00000000000000000000000000000005"

	goldenFateQuestionPayload = "0000001a0063e6ada4e59cb0e587bae78eb0e5a5b3e5a8b2e5a4a7e7a59ee980a0e4babae589a9e4b88be79a84e4b883e5bda9e7a59ee59c9fefbc8ce4bca0e997bbe4bdbfe794a8e7a59ee59c9fe58fafe5889be980a0e7acace4b889e7a78de4babae7b1bbe380820002000000070001410033e5889be980a0e7acace4b889e4babae7b1bbefbc8ce887aae8afa9e4b8bae7acace4b889e4babae7b1bbe5a78be7a596e3808200000008000142003be981b5e5ae88e5a4a9e59cb0e6b395e58899efbc8ce5b081e58db0e7a59ee59c9fefbc8ce4bba5e5858de4babae997b4e5a4a7e4b9b1e380820d0a02000000000000000500000000"

	goldenFateAnswerPayload = "001de4bda0e88eb7e5be973530e5a3b0e69c9b2c3330e99885e58e86efbc8100050300000000040000000005000000320600000000070000001e000000010000000a00000000000000000000000000000005"
)

func TestFateCodec_BuildAndParse(t *testing.T) {
	// 1. 测试 FateGetInfoRequest 序列化与反序列化
	getInfoReq := FateGetInfoRequest{PrevAct: 0x00010000}
	pkt, err := BuildFateGetInfoPacket(getInfoReq)
	if err != nil {
		t.Fatalf("BuildFateGetInfoPacket 失败: %v", err)
	}
	if pkt.ActionID != ActionIDFateGetInfo {
		t.Fatalf("ActionID 不匹配: 期望 0x%08X, 实际 0x%08X", ActionIDFateGetInfo, pkt.ActionID)
	}

	parsedGetInfo, err := ParseFateGetInfoRequest(pkt.Payload)
	if err != nil {
		t.Fatalf("ParseFateGetInfoRequest 失败: %v", err)
	}
	if parsedGetInfo.PrevAct != 0x00010000 {
		t.Errorf("PrevAct 不匹配: 期望 0x00010000, 实际 0x%08X", parsedGetInfo.PrevAct)
	}

	// 2. 测试 FateQuestionRequest 序列化与反序列化
	questionReq := FateQuestionRequest{PrevAct: ActionIDFateGetInfo}
	pktQuestion, err := BuildFateQuestionPacket(questionReq)
	if err != nil {
		t.Fatalf("BuildFateQuestionPacket 失败: %v", err)
	}
	if pktQuestion.ActionID != ActionIDFateGetQuestion {
		t.Fatalf("ActionID 不匹配: 期望 0x%08X, 实际 0x%08X", ActionIDFateGetQuestion, pktQuestion.ActionID)
	}

	parsedQuestion, err := ParseFateQuestionRequest(pktQuestion.Payload)
	if err != nil {
		t.Fatalf("ParseFateQuestionRequest 失败: %v", err)
	}
	if parsedQuestion.PrevAct != ActionIDFateGetInfo {
		t.Errorf("PrevAct 不匹配: 期望 0x%08X, 实际 0x%08X", ActionIDFateGetInfo, parsedQuestion.PrevAct)
	}

	// 3. 答案请求必须严格为 8 字节 [eventId, answerId]。
	// 线上实测: 追加 PrevAct 后服务端会静默丢弃该请求，因此不得再带第 3 个字段。
	answerReq := FateAnswerRequest{EventID: 26, AnswerID: 7}
	pktAnswer, err := BuildFateAnswerPacket(answerReq)
	if err != nil {
		t.Fatalf("BuildFateAnswerPacket 失败: %v", err)
	}
	if pktAnswer.ActionID != ActionIDFateAnswer {
		t.Fatalf("ActionID 不匹配: 期望 0x%08X, 实际 0x%08X", ActionIDFateAnswer, pktAnswer.ActionID)
	}

	expectedHex := "0000001a00000007"
	actualHex := hex.EncodeToString(pktAnswer.Payload)
	if actualHex != expectedHex {
		t.Errorf("载荷 Hex 不符合线上协议: 期望 %s, 实际 %s", expectedHex, actualHex)
	}

	parsedAnswer, err := ParseFateAnswerRequest(pktAnswer.Payload)
	if err != nil {
		t.Fatalf("ParseFateAnswerRequest 失败: %v", err)
	}
	if parsedAnswer.EventID != 26 || parsedAnswer.AnswerID != 7 {
		t.Errorf("解析结果与原值不符: %+v", parsedAnswer)
	}
}

func TestFateCodec_ParseGoldenGetInfo(t *testing.T) {
	payload, err := hex.DecodeString(goldenFateInfoPayload)
	if err != nil {
		t.Fatal(err)
	}
	res, err := ParseFateGetInfoResult(payload)
	if err != nil {
		t.Fatalf("ParseFateGetInfoResult 失败: %v", err)
	}
	if res.JoinCount != 0 || res.TotalCount != 10 || res.CdTimer != 0 || res.CdIngot != 0 {
		t.Errorf("活动状态解析不符: %+v", res)
	}
	if res.RemainingTimes() != 10 {
		t.Errorf("剩余次数期望 10, 实际 %d", res.RemainingTimes())
	}
}

func TestFateCodec_ParseGoldenQuestion(t *testing.T) {
	payload, err := hex.DecodeString(goldenFateQuestionPayload)
	if err != nil {
		t.Fatal(err)
	}
	res, err := ParseFateQuestionResult(payload)
	if err != nil {
		t.Fatalf("ParseFateQuestionResult 失败: %v", err)
	}
	if res.EventID != 26 {
		t.Errorf("事件编号期望 26, 实际 %d", res.EventID)
	}
	if res.Msg != FateResultSuccess {
		t.Errorf("业务结果码期望 %d, 实际 %d", FateResultSuccess, res.Msg)
	}
	if res.Story != "此地出现女娲大神造人剩下的七彩神土，传闻使用神土可创造第三种人类。" {
		t.Errorf("故事文本不符: %q", res.Story)
	}
	if len(res.Options) != 2 {
		t.Fatalf("选项数量期望 2, 实际 %d", len(res.Options))
	}
	if res.Options[0].AnswerID != 7 || res.Options[0].Sign != "A" {
		t.Errorf("选项 1 不符: %+v", res.Options[0])
	}
	if res.Options[1].AnswerID != 8 || res.Options[1].Sign != "B" {
		t.Errorf("选项 2 不符: %+v", res.Options[1])
	}
}

func TestFateCodec_ParseGoldenAnswer(t *testing.T) {
	payload, err := hex.DecodeString(goldenFateAnswerPayload)
	if err != nil {
		t.Fatal(err)
	}
	res, err := ParseFateAnswerResult(payload)
	if err != nil {
		t.Fatalf("ParseFateAnswerResult 失败: %v", err)
	}
	if res.Message != "你获得50声望,30阅历！" {
		t.Errorf("结算文本不符: %q", res.Message)
	}
	wantAwards := map[uint8]int32{
		FateAwardCoin:  0,
		FateAwardExp:   0,
		FateAwardFame:  50,
		FateAwardPower: 0,
		FateAwardSkill: 30,
	}
	if len(res.Awards) != len(wantAwards) {
		t.Fatalf("奖励条目期望 %d, 实际 %d: %+v", len(wantAwards), len(res.Awards), res.Awards)
	}
	for _, award := range res.Awards {
		if want, ok := wantAwards[award.Type]; !ok || want != award.Value {
			t.Errorf("奖励条目不符: type=%d value=%d", award.Type, award.Value)
		}
	}
	if res.Info.JoinCount != 1 || res.Info.TotalCount != 10 {
		t.Errorf("结算后活动状态不符: %+v", res.Info)
	}
}

func TestFateCodec_ResultPacketsRoundTrip(t *testing.T) {
	info := FateGetInfoResult{JoinCount: 2, TotalCount: 10, CdTimer: 30, CdIngot: 6}
	infoPkt := BuildFateGetInfoResultPacket(info)
	parsedInfo, err := ParseFateGetInfoResult(infoPkt.Payload)
	if err != nil || *parsedInfo != info {
		t.Fatalf("状态回包往返失败: %+v, err=%v", parsedInfo, err)
	}

	question := FateQuestionResult{
		EventID: 26,
		Story:   "测试故事",
		Msg:     FateResultSuccess,
		Options: []FateOption{
			{AnswerID: 7, Sign: "A", Text: "选项一"},
			{AnswerID: 8, Sign: "B", Text: "选项二"},
		},
	}
	qPkt := BuildFateQuestionResultPacket(question)
	parsedQ, err := ParseFateQuestionResult(qPkt.Payload)
	if err != nil {
		t.Fatalf("题目回包往返失败: %v", err)
	}
	if parsedQ.EventID != question.EventID || parsedQ.Story != question.Story || parsedQ.Msg != question.Msg || len(parsedQ.Options) != 2 {
		t.Errorf("题目回包往返不符: %+v", parsedQ)
	}

	answer := FateAnswerResult{
		Message: "测试结算",
		Awards:  []FateAward{{Type: FateAwardFame, Value: 50}, {Type: FateAwardSkill, Value: 30}},
		Info:    FateGetInfoResult{JoinCount: 3, TotalCount: 10},
	}
	aPkt := BuildFateAnswerResultPacket(answer)
	parsedA, err := ParseFateAnswerResult(aPkt.Payload)
	if err != nil {
		t.Fatalf("结算回包往返失败: %v", err)
	}
	if parsedA.Message != answer.Message || len(parsedA.Awards) != 2 || parsedA.Info.JoinCount != 3 {
		t.Errorf("结算回包往返不符: %+v", parsedA)
	}
}
