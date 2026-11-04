package protocol_test

import (
	"encoding/hex"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestFunctionEnd_PacketBuilders(t *testing.T) {
	if p := protocol.BuildIsGameFunctionEndPacket(); p.ActionID != protocol.ActionIDIsGameFunctionEnd || len(p.Payload) != 0 {
		t.Fatalf("is_game_function_end 封包异常: action=0x%08X payload=%x", p.ActionID, p.Payload)
	}
	if p := protocol.BuildFunctionEndGiftListPacket(); p.ActionID != protocol.ActionIDGameFunctionEndGift || len(p.Payload) != 0 {
		t.Fatalf("game_function_end_gift 封包异常: action=0x%08X payload=%x", p.ActionID, p.Payload)
	}
	if p := protocol.BuildGetFunctionEndGiftPacket(16); p.ActionID != protocol.ActionIDGetGameFunctionEndGift ||
		hex.EncodeToString(p.Payload) != "0010" {
		t.Fatalf("get_game_function_end_gift 封包异常: action=0x%08X payload=%x", p.ActionID, p.Payload)
	}
	if p := protocol.BuildFunctionEndRandomAwardPacket(4); p.ActionID != protocol.ActionIDFunctionEndRandomAward ||
		hex.EncodeToString(p.Payload) != "0004" {
		t.Fatalf("random_award 封包异常: action=0x%08X payload=%x", p.ActionID, p.Payload)
	}
}

func TestParseIsGameFunctionEndResult_LiveGolden(t *testing.T) {
	// 实机 s0 回包: 9 个已终结功能, 每个 (int16 function_id, uint8 ended)
	payload, _ := hex.DecodeString("0009003601004601005501003701006a01007001004401002701009301")
	got, err := protocol.ParseIsGameFunctionEndResult(payload)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(got) != 9 {
		t.Fatalf("期望 9 个功能, 实际 %d", len(got))
	}
	if got[0].FunctionID != 0x36 || !got[0].Ended {
		t.Errorf("首条记录异常: %+v", got[0])
	}
	if got[8].FunctionID != 0x93 || !got[8].Ended {
		t.Errorf("末条记录异常: %+v", got[8])
	}
}

func TestParseFunctionEndGiftListResult_LiveGolden(t *testing.T) {
	// 实机 s0 回包: 6 个礼包 (gift 4 为吉星高照, 元宝消耗 50)
	payload, _ := hex.DecodeString("000600040000000138800000001e000001400000000000000000000000320400000000001100000000000000000000000000000000000000000000000000000200000000001000000000000000000000000000000000000000000000000000000200000000000700000000000000000000000000000000000000000000000000000200000000000d00000000000000000000000000000000000000000000000000000a00000000000100000000000000000000000000000000000000000000000000000200000000")
	got, err := protocol.ParseFunctionEndGiftListResult(payload)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(got) != 6 {
		t.Fatalf("期望 6 个礼包, 实际 %d", len(got))
	}
	wantIDs := []int16{4, 17, 16, 7, 13, 1}
	for i, want := range wantIDs {
		if got[i].GiftID != want {
			t.Errorf("第 %d 个礼包 ID 期望 %d, 实际 %d", i, want, got[i].GiftID)
		}
	}
	if got[0].IngotCost != 50 || got[0].Bei != 4 {
		t.Errorf("吉星高照礼包字段异常: %+v", got[0])
	}
	for _, g := range got[1:] {
		if g.IngotCost != 0 {
			t.Errorf("礼包 %d 期望免费, 实际消耗 %d 元宝", g.GiftID, g.IngotCost)
		}
	}
}

func TestParseGetGameFunctionEndGiftResult_Success(t *testing.T) {
	// 实机 s0 领取吉星高照礼包回包 (result=0, is_have_next=0)
	payload, _ := hex.DecodeString("0000000004e200000000780000050000000000000000000000000000")
	got, err := protocol.ParseGetGameFunctionEndGiftResult(payload)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got.Result != protocol.FunctionEndSuccess {
		t.Errorf("结果码期望 0, 实际 %d", got.Result)
	}
	if got.IsHaveNext {
		t.Error("期望 is_have_next=false")
	}
	if got.Skill != 320000 || got.XianLing != 120 || got.Fame != 1280 {
		t.Errorf("奖励字段异常: %+v", got)
	}
}

func TestParseGetGameFunctionEndGiftResult_Failures(t *testing.T) {
	cases := []struct {
		name   string
		result uint8
	}{
		{"元宝不足", protocol.FunctionEndNoIngot},
		{"VIP等级不足", protocol.FunctionEndNoVIPLevel},
		{"背包已满", protocol.FunctionEndItemPackFull},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// result + int16 + 4×int32 + 2×int16 + is_have_next + int32
			payload := make([]byte, 28)
			payload[0] = tc.result
			got, err := protocol.ParseGetGameFunctionEndGiftResult(payload)
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if got.Result != tc.result {
				t.Errorf("结果码期望 %d, 实际 %d", tc.result, got.Result)
			}
			if protocol.FunctionEndClaimResultText(tc.result) == "未知结果码" {
				t.Errorf("结果码 %d 缺少中文语义", tc.result)
			}
		})
	}
}

func TestParseFunctionEndRandomAwardResult_LiveGolden(t *testing.T) {
	// 实机 s0 吉星高照礼包预览回包
	payload, _ := hex.DecodeString("0000000138800000001e0000014000000000000000000000003200000000")
	got, err := protocol.ParseFunctionEndRandomAwardResult(payload)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got.Skill != 80000 || got.XianLing != 30 || got.Fame != 320 || got.Ingot != 50 {
		t.Errorf("预览字段异常: %+v", got)
	}
}

func TestParseFunctionEndGiftListResult_Truncated(t *testing.T) {
	payload, _ := hex.DecodeString("00010004")
	if _, err := protocol.ParseFunctionEndGiftListResult(payload); err == nil {
		t.Fatal("截断回包期望返回错误")
	}
}
