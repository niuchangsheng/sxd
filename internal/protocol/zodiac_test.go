// Package protocol_test 提供生肖挑战协议 (Module 44) 的测试用例。
package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestZodiacInfoResult(t *testing.T) {
	pkt := protocol.BuildZodiacInfoResultPacket(protocol.ZodiacInfo{
		NowZodiacLevel: 2, NowBarrier: 5, NextZodiacLevel: 2, NextBarrier: 6,
		RequireLevel: 120, GoldOilID: 88, Heaven: 3,
	})
	info, err := protocol.ParseZodiacInfoResult(pkt.Payload)
	if err != nil {
		t.Fatalf("解析生肖信息失败: %v", err)
	}
	if info.NowZodiacLevel != 2 || info.NowBarrier != 5 || info.RequireLevel != 120 || info.GoldOilID != 88 || info.Heaven != 3 {
		t.Errorf("生肖信息不一致: %+v", info)
	}
}

func TestZodiacChallengeResult(t *testing.T) {
	// 成功路径
	res, err := protocol.ParseZodiacChallengeResult(
		protocol.BuildZodiacChallengeResultPacket(protocol.ZodiacResultSuccess, 2, 6, 88).Payload)
	if err != nil || res.Result != protocol.ZodiacResultSuccess || res.Barrier != 6 {
		t.Fatalf("生肖挑战成功回包不一致: %+v err=%v", res, err)
	}
	// 失败码路径: 冷却中必须能被识别
	res, err = protocol.ParseZodiacChallengeResult(
		protocol.BuildZodiacChallengeResultPacket(protocol.ZodiacResultCDTime, 0, 0, 0).Payload)
	if err != nil || res.Result != protocol.ZodiacResultCDTime {
		t.Fatalf("期望冷却码 %d, got %+v err=%v", protocol.ZodiacResultCDTime, res, err)
	}
	// 空回包报错
	if _, err := protocol.ParseZodiacChallengeResult(nil); err == nil {
		t.Error("空生肖回包应返回解析错误")
	}
}
