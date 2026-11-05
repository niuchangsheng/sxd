// Package protocol_test 提供精灵试炼协议 (Module 205) 的测试用例。
package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestStTrialsModeStatusResult(t *testing.T) {
	modes, err := protocol.ParseStTrialsModeStatusResult(protocol.BuildStTrialsModeStatusResultPacket([]protocol.StTrialsMode{
		{ModeType: 1, RemainChallengeTimes: 2, ModeStatus: 1},
	}).Payload)
	if err != nil || len(modes) != 1 || modes[0].RemainChallengeTimes != 2 {
		t.Fatalf("模式状态不一致: %+v err=%v", modes, err)
	}
}

func TestStTrialsModeInfoResult(t *testing.T) {
	info, err := protocol.ParseStTrialsModeInfoResult(protocol.BuildStTrialsModeInfoResultPacket(protocol.StTrialsModeInfo{
		Floor: 3, Pass: 1, AwardStatus: 0,
	}).Payload)
	if err != nil || info.Floor != 3 || info.Pass != 1 {
		t.Fatalf("模式详情不一致: %+v err=%v", info, err)
	}
}

func TestStTrialsAwardResult(t *testing.T) {
	result, _, err := protocol.ParseStTrialsAwardResult(protocol.BuildStTrialsAwardResultPacket(protocol.StTrialsResultSuccess).Payload)
	if err != nil || result != protocol.StTrialsResultSuccess {
		t.Fatalf("期望领奖成功, got %d err=%v", result, err)
	}
	// 失败码: 已领取
	result, _, err = protocol.ParseStTrialsAwardResult(protocol.BuildStTrialsAwardResultPacket(protocol.StTrialsResultIsGet).Payload)
	if err != nil || result != protocol.StTrialsResultIsGet {
		t.Fatalf("期望已领取码 %d, got %d err=%v", protocol.StTrialsResultIsGet, result, err)
	}
}

func TestStTrialsChallengeResult(t *testing.T) {
	result, err := protocol.ParseStTrialsChallengeResult(protocol.BuildStTrialsChallengeResultPacket(protocol.StTrialsResultSuccess).Payload)
	if err != nil || result != protocol.StTrialsResultSuccess {
		t.Fatalf("期望挑战成功, got %d err=%v", result, err)
	}
	if _, err := protocol.ParseStTrialsChallengeResult(nil); err == nil {
		t.Error("空挑战回包应返回解析错误")
	}
}
