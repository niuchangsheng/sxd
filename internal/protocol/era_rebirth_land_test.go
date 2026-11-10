package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestParseEraRebirthFreeBackResult_RoundTrip(t *testing.T) {
	pkt := protocol.BuildEraRebirthFreeBackResultPacket(protocol.EraRebirthResultSuccess, []protocol.EraRebirthReward{
		{ItemID: 10994, Num: 72000},
		{ItemID: 10993, Num: 5760},
	})
	res, err := protocol.ParseEraRebirthFreeBackResult(pkt.Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Result != protocol.EraRebirthResultSuccess || len(res.Rewards) != 2 {
		t.Fatalf("res = %+v", res)
	}
	if res.Rewards[0].ItemID != 10994 || res.Rewards[0].Num != 72000 {
		t.Fatalf("reward[0] = %+v", res.Rewards[0])
	}
}

func TestParseEraRebirthFreeBackResult_NoGift(t *testing.T) {
	pkt := protocol.BuildEraRebirthFreeBackResultPacket(protocol.EraRebirthResultNoCanGetGift, nil)
	res, err := protocol.ParseEraRebirthFreeBackResult(pkt.Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Result != protocol.EraRebirthResultNoCanGetGift || len(res.Rewards) != 0 {
		t.Fatalf("res = %+v", res)
	}
	if _, err := protocol.ParseEraRebirthFreeBackResult(nil); err == nil {
		t.Fatal("expected error for empty payload")
	}
}

// 抓包样本 (2026-11-10): 渡业台抽取请求 = [Int type=0][PrevAct=852/12]。
func TestBuildEraRebirthLandDrawPacket(t *testing.T) {
	pkt := protocol.BuildEraRebirthLandDrawPacket(0, protocol.ActionIDEraRebirthLandPoolPanel)
	if pkt.ActionID != protocol.ActionIDEraRebirthLandDraw {
		t.Fatalf("ActionID = 0x%08X, 期望 0x%08X", pkt.ActionID, protocol.ActionIDEraRebirthLandDraw)
	}
	if !pkt.HasPrevAct {
		t.Fatal("HasPrevAct 未置位，PrevAct 会被心跳打断")
	}
	want := []byte{0x00, 0x00, 0x00, 0x00, 0x03, 0x54, 0x00, 0x0c}
	if len(pkt.Payload) != len(want) {
		t.Fatalf("载荷长度 = %d, 期望 %d", len(pkt.Payload), len(want))
	}
	for i := range want {
		if pkt.Payload[i] != want[i] {
			t.Fatalf("载荷 = % X, 期望 % X", pkt.Payload, want)
		}
	}
}

// 抓包样本回包: 00 00 00 00 01 00 00 00 04 -> result=0, first=1, second=4。
func TestParseEraRebirthDrawResult(t *testing.T) {
	res, err := protocol.ParseEraRebirthDrawResult([]byte{0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x04})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Result != protocol.EraRebirthResultSuccess || res.First != 1 || res.Second != 4 {
		t.Fatalf("res = %+v", res)
	}
	if _, err := protocol.ParseEraRebirthDrawResult([]byte{0x00}); err == nil {
		t.Fatal("截断回包应返回错误")
	}
}

// 官方抓包形态: 852/12 必须带 4 字节 PrevAct，空载荷不会登记 PrevAct 链。
func TestBuildEraRebirthLandPoolPanelPrevActPacket(t *testing.T) {
	pkt := protocol.BuildEraRebirthLandPoolPanelPrevActPacket(protocol.ActionIDTownEnter)
	if pkt.ActionID != protocol.ActionIDEraRebirthLandPoolPanel {
		t.Fatalf("ActionID = 0x%08X", pkt.ActionID)
	}
	if pkt.HasPrevAct {
		t.Fatal("面板锚点必须原样发送，不能被会话本地动作号覆盖")
	}
	if len(pkt.Payload) != 4 {
		t.Fatalf("载荷长度 = %d, 期望 4", len(pkt.Payload))
	}
}
