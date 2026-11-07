// Package protocol 提供 NPC 结交任务/问答/猜拳协议编解码测试。
//
// @author Ateng
// @since 2026-11-08
package protocol

import (
	"encoding/hex"
	"testing"
)

func TestNpcFriendshipActionPackets(t *testing.T) {
	cases := []struct {
		name     string
		pkt      *Packet
		actionID uint32
		payload  string
	}{
		{"take", BuildNpcFriendshipTakeMissionPacket(13), ActionIDNpcFriendshipTakeMission, "0000000d"},
		{"answer", BuildNpcFriendshipAnswerQuestionPacket(1, 13, NpcFriendshipResultAnswerC), ActionIDNpcFriendshipAnswerQuestion, "000000010000000d0f"},
		{"complete", BuildNpcFriendshipCompleteMissionPacket(13, 3, 125), ActionIDNpcFriendshipCompleteMission, "0000000d000000030000007d"},
		{"finger", BuildNpcFriendshipFingerGuessPacket(13, 2, 125, 1), ActionIDNpcFriendshipFingerGuess, "0000000d000000020000007d00000001"},
		{"dice", BuildNpcFriendshipDiceGuessPacket(13, 13, 125), ActionIDNpcFriendshipDiceGuess, "0000000d0000000d0000007d"},
	}
	for _, tc := range cases {
		if tc.pkt.ActionID != tc.actionID {
			t.Errorf("%s action mismatch: 0x%08X", tc.name, tc.pkt.ActionID)
		}
		if got := hex.EncodeToString(tc.pkt.Payload); got != tc.payload {
			t.Errorf("%s payload mismatch: want=%s got=%s", tc.name, tc.payload, got)
		}
	}
}

func TestNpcFriendshipActionResultParsers(t *testing.T) {
	take, err := ParseNpcFriendshipTakeMissionResult(BuildNpcFriendshipTakeMissionResultPacket(
		NpcFriendshipTakeMissionResult{Result: NpcFriendshipResultSuccess, HasTakeMission: 1, NpcID: 13, MissionCount: 2}).Payload)
	if err != nil || take.Result != NpcFriendshipResultSuccess || take.HasTakeMission != 1 || take.NpcID != 13 || take.MissionCount != 2 {
		t.Fatalf("take parse: %+v err=%v", take, err)
	}

	answer, err := ParseNpcFriendshipAnswerResult(BuildNpcFriendshipAnswerResultPacket(
		NpcFriendshipAnswerResult{Result: NpcFriendshipResultSuccess, NewExp: 112, NpcID: 13}).Payload)
	if err != nil || answer.NewExp != 112 || answer.NpcID != 13 {
		t.Fatalf("answer parse: %+v err=%v", answer, err)
	}

	guess, err := ParseNpcFriendshipGuessResult(BuildNpcFriendshipGuessResultPacket(
		ActionIDNpcFriendshipFingerGuess,
		NpcFriendshipGuessResult{Result: NpcFriendshipResultWin, MyGuess: 1, NpcGuess: 0}).Payload)
	if err != nil || guess.Result != NpcFriendshipResultWin || guess.MyGuess != 1 || guess.NpcGuess != 0 {
		t.Fatalf("guess parse: %+v err=%v", guess, err)
	}
	if text := NpcFriendshipGuessResultText(NpcFriendshipResultLose); text != "失败" {
		t.Fatalf("guess text mismatch: %s", text)
	}

	award, err := ParseNpcFriendshipMissionAwardResult(BuildNpcFriendshipMissionAwardResultPacket(
		NpcFriendshipMissionAwardResult{Result: NpcFriendshipResultSuccess, IsReceive: 1, NewExp: 212, NpcID: 13}).Payload)
	if err != nil || award.IsReceive != 1 || award.NewExp != 212 || award.NpcID != 13 {
		t.Fatalf("award parse: %+v err=%v", award, err)
	}

	// 失败回包只有结果码时不应报错。
	fail, err := ParseNpcFriendshipCompleteResult(BuildNpcFriendshipCompleteResultPacket(
		NpcFriendshipCompleteResult{Result: NpcFriendshipResultHasNotComplete}).Payload)
	if err != nil || fail.Result != NpcFriendshipResultHasNotComplete {
		t.Fatalf("complete failure parse: %+v err=%v", fail, err)
	}
}

func TestNpcFriendshipBestAnswer(t *testing.T) {
	cases := []struct {
		questionID int32
		want       uint8
		award      int32
	}{
		{1, NpcFriendshipResultAnswerC, 12},
		{6, NpcFriendshipResultAnswerA, 12},
		{10, NpcFriendshipResultAnswerB, 12},
		{18, NpcFriendshipResultAnswerB, 10},
		{20, NpcFriendshipResultAnswerA, 12},
	}
	for _, tc := range cases {
		got, ok := NpcFriendshipBestAnswer(tc.questionID)
		if !ok || got.Answer != tc.want || got.Award != tc.award {
			t.Errorf("question %d: got=%+v ok=%v want answer=%d award=%d", tc.questionID, got, ok, tc.want, tc.award)
		}
	}
	if _, ok := NpcFriendshipBestAnswer(999); ok {
		t.Fatal("未知问题不应返回答案")
	}
}
