// Package protocol_test 提供 VIP 每日礼包协议 (Module 18) 的测试用例。
package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestVIPInfoResult(t *testing.T) {
	pkt := protocol.BuildVIPInfoResultPacket(protocol.VIPInfo{
		VIPLevel:         2,
		MoneyToNextLevel: 1000,
		Gifts: []protocol.VIPGiftState{
			{Level: 1, SendGiftGot: 1},
			{Level: 2, SendGiftGot: 0},
			{Level: 3, SendGiftGot: 0, BuyGiftGot: 1},
		},
	})
	info, err := protocol.ParseVIPInfoResult(pkt.Payload)
	if err != nil {
		t.Fatalf("解析 VIP 信息失败: %v", err)
	}
	if info.VIPLevel != 2 || info.MoneyToNextLevel != 1000 || len(info.Gifts) != 3 {
		t.Fatalf("VIP 信息不一致: %+v", info)
	}
	if info.Gifts[0].SendGiftClaimable() {
		t.Error("等级 1 已领取，不应判定为可领")
	}
	if !info.Gifts[1].SendGiftClaimable() {
		t.Error("等级 2 未领取，应判定为可领")
	}
}

func TestVIPGiftResult(t *testing.T) {
	// 成功路径
	result, err := protocol.ParseVIPGiftResult(protocol.BuildVIPGiftResultPacket(protocol.VIPResultSuccess).Payload)
	if err != nil || result != protocol.VIPResultSuccess {
		t.Fatalf("期望成功码 0, got result=%d err=%v", result, err)
	}
	// 失败码路径: 背包已满 (回包解析铁律)
	result, err = protocol.ParseVIPGiftResult(protocol.BuildVIPGiftResultPacket(protocol.VIPResultPackFull).Payload)
	if err != nil || result != protocol.VIPResultPackFull {
		t.Fatalf("期望背包已满码 %d, got result=%d err=%v", protocol.VIPResultPackFull, result, err)
	}
	// 空回包必须报错而不是当作成功
	if _, err := protocol.ParseVIPGiftResult(nil); err == nil {
		t.Error("空 VIP 礼包回包应返回解析错误")
	}
}

func TestVIPSendGiftPacket(t *testing.T) {
	pkt := protocol.BuildVIPSendGiftPacket(12)
	if pkt.ActionID != protocol.ActionIDVIPSendGift {
		t.Fatalf("ActionID 不匹配: 0x%08X", pkt.ActionID)
	}
	if len(pkt.Payload) != 1 || pkt.Payload[0] != 12 {
		t.Errorf("VIP 礼包请求应为单字节 12, got %v", pkt.Payload)
	}
}
