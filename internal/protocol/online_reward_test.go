// Package protocol_test 提供在线奖励协议 (Module 712) 的测试用例。
package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestOnlineRewardPanelResult(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteInt32(1700000000)
	w.WriteInt32(1800000000)
	w.WriteUint8(3)
	w.WriteUint16(2)
	// 档位 1: 元宝 x100
	w.WriteInt32(15)
	w.WriteUint16(1)
	w.WriteInt32(3345)
	w.WriteInt64(100)
	w.WriteUint8(1)
	w.WriteUint8(0)
	w.WriteInt32(300)
	// 档位 2: 铜钱 x200000
	w.WriteInt32(16)
	w.WriteUint16(1)
	w.WriteInt32(1747)
	w.WriteInt64(200000)
	w.WriteUint8(0)
	w.WriteUint8(1)
	w.WriteInt32(600)
	w.WriteInt32(400)

	panel, err := protocol.ParseOnlineRewardPanelResult(w.Bytes())
	if err != nil {
		t.Fatalf("解析在线奖励面板失败: %v", err)
	}
	if panel.Day != 3 || panel.OnlineTime != 400 || len(panel.Tiers) != 2 {
		t.Fatalf("在线奖励面板不一致: %+v", panel)
	}
	if panel.Tiers[0].AwardID != 15 || panel.Tiers[0].IsGet != 0 || panel.Tiers[0].NeedOnlineTime != 300 {
		t.Errorf("档位 1 不一致: %+v", panel.Tiers[0])
	}
	if panel.Tiers[0].Items[0].ItemID != 3345 || panel.Tiers[0].Items[0].Num != 100 || panel.Tiers[0].Items[0].IsBigAward != 1 {
		t.Errorf("档位 1 奖励不一致: %+v", panel.Tiers[0].Items[0])
	}
}

func TestOnlineRewardOneKeyResult(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteUint8(protocol.OnlineRewardResultSuccess)
	w.WriteUint16(2)
	w.WriteInt32(15)
	w.WriteInt32(16)
	result, awards, err := protocol.ParseOnlineRewardOneKeyResult(w.Bytes())
	if err != nil {
		t.Fatalf("解析一键领取回包失败: %v", err)
	}
	if result != protocol.OnlineRewardResultSuccess || len(awards) != 2 || awards[0] != 15 || awards[1] != 16 {
		t.Errorf("一键领取回包不一致: result=%d awards=%v", result, awards)
	}

	// 失败码路径: 背包已满必须能被识别 (回包解析铁律)
	w = protocol.NewWriter()
	w.WriteUint8(protocol.OnlineRewardResultPackFull)
	w.WriteUint16(0)
	result, awards, err = protocol.ParseOnlineRewardOneKeyResult(w.Bytes())
	if err != nil {
		t.Fatalf("解析背包已满回包失败: %v", err)
	}
	if result != protocol.OnlineRewardResultPackFull || len(awards) != 0 {
		t.Errorf("期望背包已满码 %d, got result=%d awards=%v", protocol.OnlineRewardResultPackFull, result, awards)
	}
}
