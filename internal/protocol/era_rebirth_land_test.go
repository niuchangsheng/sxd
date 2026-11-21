package protocol_test

import (
	"encoding/hex"
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
		t.Fatal("852/16 必须由会话层用最近发送动作覆盖 PrevAct")
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

func TestBuildEraRebirthLandTodayAwardPrevActPacket(t *testing.T) {
	pkt := protocol.BuildEraRebirthLandTodayAwardPrevActPacket(0x0354000D)
	if pkt.ActionID != protocol.ActionIDEraRebirthLandTodayAward || pkt.HasPrevAct {
		t.Fatalf("852/14 PrevAct 变体封包不符: %+v", pkt)
	}
	if len(pkt.Payload) != 4 || pkt.Payload[3] != 0x0D {
		t.Fatalf("852/14 PrevAct 载荷不符: %x", pkt.Payload)
	}
}

// 抓包样本 (2026-11-21): 快速挂机请求 852/8 只有 4 字节 PrevAct。
func TestBuildEraRebirthLandQuickAutoPacket(t *testing.T) {
	pkt := protocol.BuildEraRebirthLandQuickAutoPacket(protocol.ActionIDStateRecordGetState)
	if pkt.ActionID != protocol.ActionIDEraRebirthLandQuickAuto {
		t.Fatalf("ActionID = 0x%08X, 期望 0x%08X", pkt.ActionID, protocol.ActionIDEraRebirthLandQuickAuto)
	}
	if !pkt.HasPrevAct {
		t.Fatal("852/8 必须由会话层用最近收到的包覆盖 PrevAct")
	}
	if len(pkt.Payload) != 4 {
		t.Fatalf("载荷长度 = %d, 期望 4 字节 PrevAct", len(pkt.Payload))
	}
}

// 抓包样本 (2026-11-21): 701/0 get_state 请求 [Int key=8] + PrevAct。
func TestBuildStateRecordGetStatePacket(t *testing.T) {
	pkt := protocol.BuildStateRecordGetStatePacket(8)
	if pkt.ActionID != protocol.ActionIDStateRecordGetState {
		t.Fatalf("ActionID = 0x%08X", pkt.ActionID)
	}
	if !pkt.HasPrevAct {
		t.Fatal("701/0 必须由会话层填充 PrevAct")
	}
	want := []byte{0x00, 0x00, 0x00, 0x08}
	if len(pkt.Payload) != 4 || pkt.Payload[3] != want[3] {
		t.Fatalf("载荷 = % X, 期望 % X", pkt.Payload, want)
	}
	key, value, err := protocol.ParseStateRecordGetStateResult([]byte{0x00, 0x00, 0x00, 0x08, 0x00, 0x00, 0x00, 0x00})
	if err != nil || key != 8 || value != 0 {
		t.Fatalf("解析 701/0 回包失败: key=%d value=%d err=%v", key, value, err)
	}
}

// 抓包样本 (2026-11-21) 852/9 回包 43 字节:
// result=0, profit=87105, 3 条奖励 (10993/155033, 10992/6802260, 10994/1700565)。
func TestParseEraRebirthAutoProfit_Capture(t *testing.T) {
	payload := []byte{
		0x00,
		0x00, 0x01, 0x54, 0x41,
		0x00, 0x03,
		0x00, 0x00, 0x2a, 0xf1, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0x5d, 0x99,
		0x00, 0x00, 0x2a, 0xf0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x67, 0xcb, 0x54,
		0x00, 0x00, 0x2a, 0xf2, 0x00, 0x00, 0x00, 0x00, 0x00, 0x19, 0xf2, 0xd5,
	}
	res, err := protocol.ParseEraRebirthAutoProfit(payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Result != protocol.EraRebirthResultSuccess || res.Profit != 87105 {
		t.Fatalf("res = %+v", res)
	}
	want := []protocol.EraRebirthReward{
		{ItemID: 10993, Num: 155033},
		{ItemID: 10992, Num: 6802260},
		{ItemID: 10994, Num: 1700565},
	}
	if len(res.Rewards) != len(want) {
		t.Fatalf("rewards = %+v", res.Rewards)
	}
	for i := range want {
		if res.Rewards[i] != want[i] {
			t.Fatalf("reward[%d] = %+v, want %+v", i, res.Rewards[i], want[i])
		}
	}
	if _, err := protocol.ParseEraRebirthAutoProfit(payload[:5]); err == nil {
		t.Fatal("截断回包应返回错误")
	}
}

// 抓包样本 (2026-11-21) 852/8 回包 39 字节: result=0, 3 条奖励。
func TestParseEraRebirthQuickAutoResult_Capture(t *testing.T) {
	payload := []byte{
		0x00,
		0x00, 0x03,
		0x00, 0x00, 0x2a, 0xf0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x04, 0x59, 0xc0,
		0x00, 0x00, 0x2a, 0xf1, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x19, 0x50,
		0x00, 0x00, 0x2a, 0xf2, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x16, 0x70,
	}
	res, err := protocol.ParseEraRebirthFreeBackResult(payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Result != protocol.EraRebirthResultSuccess || len(res.Rewards) != 3 {
		t.Fatalf("res = %+v", res)
	}
	if res.Rewards[0] != (protocol.EraRebirthReward{ItemID: 10992, Num: 285120}) {
		t.Fatalf("reward[0] = %+v", res.Rewards[0])
	}
}

// 抓包样本 (2026-11-21): 852/1 面板 329 字节，键值对 28 条。
// 尾部 [10]=当日已用免费快速挂机次数，[11]=已用付费次数。
func TestParseEraRebirthLandPanel_Capture(t *testing.T) {
	const panelFreeUnused = "00000004001c0000005a000000010000006f0000000000000060000000000000005f0000000000000068000000000000006c00000000000000580000000100000062000000000000006600000000000000560000000100000069000000000000007000000000000000550000000100000057000000010000005d000000000000006e00000000000000610000000000000067000000000000005c000000000000005e0000000000000064000000000000005b00000000000000630000000000000065000000000000006a000000000000006b0000000000000059000000010000006d000000000000008c0000008e0000008b00000001000154470002a300000000000000005a000000000000000000000000000000000001000fe58fb8e7a9bae6b58be8af95e58fb7000273300000006d0000000000001bee0000018c000000000000000000000001"
	const panelOneFreeUsed = "00000004001c0000005a000000010000006f0000000000000060000000000000005f0000000000000068000000000000006c00000000000000580000000100000062000000000000006600000000000000560000000100000069000000000000007000000000000000550000000100000057000000010000005d000000000000006e00000000000000610000000000000067000000000000005c000000000000005e0000000000000064000000000000005b00000000000000630000000000000065000000000000006a000000000000006b0000000000000059000000010000006d000000000000008c0000008e0000008e00000001000000150002a300000000000000005a000000000000000000000001000000000001000fe58fb8e7a9bae6b58be8af95e58fb7000273300000006d0000000000001bee0000018c000000000000000000000001"
	for _, tc := range []struct {
		name     string
		hexBody  string
		freeUsed int32
		paidUsed int32
		remain   int32
	}{
		{"未使用", panelFreeUnused, 0, 0, 2},
		{"已用一次", panelOneFreeUsed, 1, 0, 1},
	} {
		b, err := hex.DecodeString(tc.hexBody)
		if err != nil {
			t.Fatalf("%s: hex: %v", tc.name, err)
		}
		p, err := protocol.ParseEraRebirthLandPanel(b)
		if err != nil {
			t.Fatalf("%s: parse: %v", tc.name, err)
		}
		if p.Level != 4 || p.Entries != 28 || p.IdleCapSeconds != 172800 {
			t.Fatalf("%s: 面板基础字段不符: %+v", tc.name, p)
		}
		if p.QuickFreeUsed != tc.freeUsed || p.QuickPaidUsed != tc.paidUsed {
			t.Fatalf("%s: free_used=%d paid_used=%d, want %d/%d",
				tc.name, p.QuickFreeUsed, p.QuickPaidUsed, tc.freeUsed, tc.paidUsed)
		}
		if got := p.QuickFreeRemaining(); got != tc.remain {
			t.Fatalf("%s: remaining=%d, want %d", tc.name, got, tc.remain)
		}
	}
	if _, err := protocol.ParseEraRebirthLandPanel([]byte{0, 0, 0, 4}); err == nil {
		t.Fatal("截断面板应返回错误")
	}
}

// 线上样本 (2026-11-21): 3 次快速挂机后 free_used=2、paid_used=1 (第 3 次扣 10 元宝)。
func TestParseEraRebirthLandPanel_LiveAfterPaid(t *testing.T) {
	const panel = "00000004001c0000005a000000000000006f0000000000000060000000000000005f0000000000000068000000000000006c00000000000000580000000100000062000000000000006600000000000000560000000100000069000000000000007000000000000000550000000100000057000000010000005d000000000000006e00000000000000610000000000000067000000000000005c000000000000005e0000000000000064000000000000005b00000000000000630000000000000065000000000000006a000000000000006b0000000000000059000000010000006d000000000000006e000000760000007600000001000000450002a300000000000000005a000000000000000000000002000000010001000fe58fb8e7a9bae6b58be8af95e58fb7000273300000006d0000000000001bee00000190000000000000000000000001"
	b, err := hex.DecodeString(panel)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	p, err := protocol.ParseEraRebirthLandPanel(b)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.QuickFreeUsed != 2 || p.QuickPaidUsed != 1 || p.QuickFreeRemaining() != 0 {
		t.Fatalf("free_used=%d paid_used=%d remaining=%d", p.QuickFreeUsed, p.QuickPaidUsed, p.QuickFreeRemaining())
	}
}
