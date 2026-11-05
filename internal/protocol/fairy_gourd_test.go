// Package protocol_test 提供仙葫芦协议 (Module 220) 的测试用例。
package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestFairyGourdDateResult(t *testing.T) {
	d, err := protocol.ParseFairyGourdDateResult(protocol.BuildFairyGourdDateResultPacket(protocol.FairyGourdDate{
		StartTime: 100, EndTime: 200, GetTime: 150,
	}).Payload)
	if err != nil || d.StartTime != 100 || d.EndTime != 200 || d.GetTime != 150 {
		t.Fatalf("活动时间不一致: %+v err=%v", d, err)
	}
}

func TestFairyGourdListResult(t *testing.T) {
	entries, err := protocol.ParseFairyGourdListResult(protocol.BuildFairyGourdListResultPacket([]protocol.FairyGourdEntry{
		{Coin: 100000, Exp: 500, Date: 20261105},
	}).Payload)
	if err != nil || len(entries) != 1 || entries[0].Coin != 100000 || entries[0].Date != 20261105 {
		t.Fatalf("收益列表不一致: %+v err=%v", entries, err)
	}
}

func TestFairyGourdClaimResult(t *testing.T) {
	// 成功
	result, err := protocol.ParseFairyGourdClaimResult(protocol.BuildFairyGourdClaimResultPacket(protocol.FairyGourdResultSuccess).Payload)
	if err != nil || result != protocol.FairyGourdResultSuccess {
		t.Fatalf("期望成功码 0, got %d err=%v", result, err)
	}
	// 失败码: 已领取
	result, err = protocol.ParseFairyGourdClaimResult(protocol.BuildFairyGourdClaimResultPacket(protocol.FairyGourdResultAlreadyGet).Payload)
	if err != nil || result != protocol.FairyGourdResultAlreadyGet {
		t.Fatalf("期望已领取码 %d, got %d err=%v", protocol.FairyGourdResultAlreadyGet, result, err)
	}
	// 空回包报错
	if _, err := protocol.ParseFairyGourdClaimResult(nil); err == nil {
		t.Error("空仙葫芦回包应返回解析错误")
	}
}
