// Package protocol_test 提供冰窟探险协议 (Module 393) 的测试用例。
package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestIceCaveInfoResult(t *testing.T) {
	info, err := protocol.ParseIceCaveInfoResult(protocol.BuildIceCaveInfoResultPacket(protocol.IceCaveInfo{
		Slots:    []protocol.IceCaveSlot{{Ice: 1, State: 0, IsIron: 0}, {Ice: 2, State: 1, IsIron: 1}},
		Location: 3, ActiveNum: 50, ActiveLimit: 10, HammerNum: 2,
		HammerToday: 1, HammerLimit: 5, HammerIngot: 30, GoldNum: 8,
		GiftItem: 5715, GiftGold: 5, GiftCount: 0, GiftLimit: 2,
	}).Payload)
	if err != nil {
		t.Fatalf("解析冰窟面板失败: %v", err)
	}
	if len(info.Slots) != 2 || info.Slots[0].Ice != 1 || info.Slots[1].IsIron != 1 || info.HammerNum != 2 || info.GiftGold != 5 {
		t.Errorf("冰窟面板不一致: %+v", info)
	}
}

func TestIceCaveHitResult(t *testing.T) {
	res, err := protocol.ParseIceCaveHitResult(protocol.BuildIceCaveHitResultPacket(protocol.IceCaveResultSuccess).Payload)
	if err != nil || res.Result != protocol.IceCaveResultSuccess {
		t.Fatalf("冰窟敲击成功回包不一致: %+v err=%v", res, err)
	}
	// 失败码: 活跃度不足
	res, err = protocol.ParseIceCaveHitResult(protocol.BuildIceCaveHitResultPacket(protocol.IceCaveResultNotEnoughActive).Payload)
	if err != nil || res.Result != protocol.IceCaveResultNotEnoughActive {
		t.Fatalf("期望活跃度不足码 %d, got %+v err=%v", protocol.IceCaveResultNotEnoughActive, res, err)
	}
	if _, err := protocol.ParseIceCaveHitResult(nil); err == nil {
		t.Error("空冰窟回包应返回解析错误")
	}
}
