// Package protocol_test 提供尊像膜拜协议 (Module 41) 的测试用例。
package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestWorshipMarsInfoResult(t *testing.T) {
	pkt := protocol.BuildWorshipMarsInfoResultPacket(protocol.WorshipMarsInfo{
		MarsLevel: 3, MarsExp: 100, RequireExp: 500, IncenseCount: 1, TotalCount: 3, IsCover: 0,
	})
	info, err := protocol.ParseWorshipMarsInfoResult(pkt.Payload)
	if err != nil {
		t.Fatalf("解析膜拜信息失败: %v", err)
	}
	if info.MarsLevel != 3 || info.IncenseCount != 1 || info.TotalCount != 3 {
		t.Errorf("膜拜信息不一致: %+v", info)
	}
}

func TestWorshipMarsIncenseResult(t *testing.T) {
	// 成功
	result, err := protocol.ParseWorshipMarsIncenseResult(protocol.BuildWorshipMarsIncenseResultPacket(protocol.WorshipMarsResultSuccess).Payload)
	if err != nil || result != protocol.WorshipMarsResultSuccess {
		t.Fatalf("期望成功码 0, got result=%d err=%v", result, err)
	}
	// 失败码: 次数已用尽
	result, err = protocol.ParseWorshipMarsIncenseResult(protocol.BuildWorshipMarsIncenseResultPacket(protocol.WorshipMarsResultCountUsed).Payload)
	if err != nil || result != protocol.WorshipMarsResultCountUsed {
		t.Fatalf("期望次数用尽码 %d, got result=%d err=%v", protocol.WorshipMarsResultCountUsed, result, err)
	}
	// 空回包报错
	if _, err := protocol.ParseWorshipMarsIncenseResult(nil); err == nil {
		t.Error("空膜拜回包应返回解析错误")
	}
}

func TestWorshipMarsRemainResult(t *testing.T) {
	n, err := protocol.ParseWorshipMarsRemainResult(protocol.BuildWorshipMarsRemainResultPacket(2).Payload)
	if err != nil || n != 2 {
		t.Fatalf("期望剩余次数 2, got %d err=%v", n, err)
	}
}
