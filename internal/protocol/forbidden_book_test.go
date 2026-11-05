// Package protocol_test 提供禁地之书协议 (Module 199) 的测试用例。
package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestForbiddenBookInfoResult(t *testing.T) {
	info, err := protocol.ParseForbiddenBookInfoResult(protocol.BuildForbiddenBookInfoResultPacket(protocol.ForbiddenBookInfo{
		Areas: []protocol.ForbiddenBookArea{{
			ForbiddenType: protocol.ForbiddenBookRegulus, ExploreType: protocol.ForbiddenBookOnce,
			CostCoin: 1000, CostIngot: 0, CurNumber: 2, TotalNumber: 5,
		}},
		ConsumScore: 10,
	}).Payload)
	if err != nil || len(info.Areas) != 1 || info.Areas[0].CurNumber != 2 || info.Areas[0].CostCoin != 1000 {
		t.Fatalf("禁地之书信息不一致: %+v err=%v", info, err)
	}
}

func TestForbiddenBookExploreResult(t *testing.T) {
	result, err := protocol.ParseForbiddenBookExploreResult(protocol.BuildForbiddenBookExploreResultPacket(protocol.AwakeResultSuccess).Payload)
	if err != nil || result != protocol.AwakeResultSuccess {
		t.Fatalf("期望探索成功, got %d err=%v", result, err)
	}
	// 失败码: 次数不足
	result, err = protocol.ParseForbiddenBookExploreResult(protocol.BuildForbiddenBookExploreResultPacket(protocol.AwakeResultNoEnoughNumber).Payload)
	if err != nil || result != protocol.AwakeResultNoEnoughNumber {
		t.Fatalf("期望次数不足码 %d, got %d err=%v", protocol.AwakeResultNoEnoughNumber, result, err)
	}
}

func TestForbiddenBookExchangeResult(t *testing.T) {
	result, err := protocol.ParseForbiddenBookExchangeResult(protocol.BuildForbiddenBookExchangeResultPacket(protocol.AwakeResultSuccess).Payload)
	if err != nil || result != protocol.AwakeResultSuccess {
		t.Fatalf("期望兑换成功, got %d err=%v", result, err)
	}
	// 失败码: 铜钱不足
	result, err = protocol.ParseForbiddenBookExchangeResult(protocol.BuildForbiddenBookExchangeResultPacket(protocol.AwakeResultNoEnoughCoin).Payload)
	if err != nil || result != protocol.AwakeResultNoEnoughCoin {
		t.Fatalf("期望铜钱不足码 %d, got %d err=%v", protocol.AwakeResultNoEnoughCoin, result, err)
	}
}
