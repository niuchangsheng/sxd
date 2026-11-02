// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// @author Ateng
// @since 2026-11-02
package protocol

import (
	"encoding/hex"
	"testing"
)

func TestFindImmortalCodec_BuildRequests(t *testing.T) {
	cases := []struct {
		name     string
		pkt      *Packet
		actionID uint32
	}{
		{"open", BuildFindImmortalOpenPacket(ActionIDTownEnter), ActionIDFindImmortalOpen},
		{"start", BuildFindImmortalStartPacket(ActionIDFindImmortalOpen), ActionIDFindImmortalStart},
		{"goad", BuildFindImmortalGoadPacket(ActionIDFindImmortalStart), ActionIDFindImmortalGoad},
		{"pickup", BuildFindImmortalPickupAwardPacket(ActionIDFindImmortalGoad), ActionIDFindImmortalPickupAward},
		{"tokens", BuildFindImmortalGetXunXianLingPacket(ActionIDFindImmortalPickupAward), ActionIDFindImmortalGetXunXianLing},
		{"five_open", BuildFindImmortalOpenFiveBlessingsPacket(ActionIDFindImmortalGetXunXianLing), ActionIDFindImmortalOpenFiveBlessings},
		{"bless_start", BuildFindImmortalStartBlessPacket(ActionIDFindImmortalOpenFiveBlessings), ActionIDFindImmortalStartBless},
		{"bless_continue", BuildFindImmortalContinueBlessPacket(ActionIDFindImmortalStartBless), ActionIDFindImmortalContinueBless},
		{"bless_end", BuildFindImmortalEndBlessPacket(ActionIDFindImmortalContinueBless), ActionIDFindImmortalEndBless},
		{"bless_flag", BuildFindImmortalIsFiveBlessingsOpenPacket(ActionIDFindImmortalEndBless), ActionIDFindImmortalIsFiveBlessingsOpen},
		{"change_lucky", BuildFindImmortalChangeLuckyPacket(ActionIDFindImmortalIsFiveBlessingsOpen), ActionIDFindImmortalChangeLucky},
	}

	for _, tc := range cases {
		if tc.pkt.ActionID != tc.actionID {
			t.Errorf("%s ActionID 不匹配: 期望 0x%08X, 实际 0x%08X", tc.name, tc.actionID, tc.pkt.ActionID)
		}
		if len(tc.pkt.Payload) != 4 {
			t.Errorf("%s 请求载荷应为 4 字节前序协议号, 实际 %d 字节", tc.name, len(tc.pkt.Payload))
		}
	}

	if got := hex.EncodeToString(BuildFindImmortalOpenPacket(0).Payload); got != "00010000" {
		t.Errorf("默认 prev_act 应为 town_enter, 实际: %s", got)
	}

	exchange := BuildFindImmortalExchangeXianLingPacket(3, ActionIDFindImmortalGetXunXianLing)
	if exchange.ActionID != ActionIDFindImmortalExchangeXianLing {
		t.Errorf("兑换 ActionID 不匹配: 0x%08X", exchange.ActionID)
	}
	// int32 兑换数量 + uint32 前序协议号
	if got := hex.EncodeToString(exchange.Payload); got != "0000000300440005" {
		t.Errorf("兑换请求载荷不匹配: %s", got)
	}
}

func TestFindImmortalCodec_ParsePanel(t *testing.T) {
	payload, err := hex.DecodeString(
		"0002" + // can_buy_number = 2
			"0064" + // fame = 100
			"000001f4" + // skill = 500
			"000a" + // cultivation = 10
			"0005" + // state_point = 5
			"0001" + // yu_pai = 1
			"03" + // state = 3
			"01" + // god_id = 1
			"000003e8" + // cultivation total = 1000
			"00000005", // total_count = 5
	)
	if err != nil {
		t.Fatalf("构造测试载荷失败: %v", err)
	}

	panel, err := ParseFindImmortalPanelResult(payload)
	if err != nil {
		t.Fatalf("解析喜从天降面板失败: %v", err)
	}
	if panel.CanBuyNumber != 2 || panel.State != 3 || panel.GodID != 1 {
		t.Errorf("面板基础字段不匹配: %+v", panel)
	}
	if panel.Award.Fame != 100 || panel.Award.Skill != 500 || panel.Award.Cultivation != 10 ||
		panel.Award.StatePoint != 5 || panel.Award.YuPai != 1 {
		t.Errorf("面板奖励不匹配: %+v", panel.Award)
	}
	if panel.Cultivation != 1000 || panel.TotalCount != 5 {
		t.Errorf("面板累计字段不匹配: cultivation=%d total=%d", panel.Cultivation, panel.TotalCount)
	}

	if _, err := ParseFindImmortalPanelResult([]byte{0x00}); err == nil {
		t.Error("截断载荷应返回解析错误")
	}
}

func TestFindImmortalCodec_ParseStartGoadPickup(t *testing.T) {
	awardHex := "0064" + "000001f4" + "000a" + "0005" + "0001"

	startPayload, err := hex.DecodeString(
		"00" + awardHex + "00" + "01" + "0002" + "00000005",
	)
	if err != nil {
		t.Fatalf("构造召唤载荷失败: %v", err)
	}
	start, err := ParseFindImmortalStartResult(startPayload)
	if err != nil {
		t.Fatalf("解析召唤龙鱼失败: %v", err)
	}
	if start.Result != FindImmortalResultSuccess || start.State != 0 || start.GodID != 1 {
		t.Errorf("召唤结果不匹配: %+v", start)
	}
	if start.RemainNumber != 2 || start.TotalCount != 5 {
		t.Errorf("召唤剩余次数不匹配: remain=%d total=%d", start.RemainNumber, start.TotalCount)
	}

	goadPayload, err := hex.DecodeString("05" + awardHex + "04" + "01" + "00000005")
	if err != nil {
		t.Fatalf("构造吹鱼载荷失败: %v", err)
	}
	goad, err := ParseFindImmortalGoadResult(goadPayload)
	if err != nil {
		t.Fatalf("解析吹龙鱼失败: %v", err)
	}
	if goad.Result != FindImmortalResultFailed || goad.State != 4 {
		t.Errorf("吹鱼结果不匹配: %+v", goad)
	}

	pickupPayload, err := hex.DecodeString("00" + awardHex + "0000044c")
	if err != nil {
		t.Fatalf("构造收奖载荷失败: %v", err)
	}
	pickup, err := ParseFindImmortalPickupResult(pickupPayload)
	if err != nil {
		t.Fatalf("解析收取龙鱼奖励失败: %v", err)
	}
	if pickup.Result != FindImmortalResultSuccess || pickup.Cultivation != 1100 {
		t.Errorf("收奖结果不匹配: %+v", pickup)
	}
	if pickup.Award.Skill != 500 {
		t.Errorf("收奖奖励不匹配: %+v", pickup.Award)
	}
}

func TestFindImmortalCodec_TokensAndExchange(t *testing.T) {
	tokens, err := ParseFindImmortalXunXianLingResult([]byte{0x00, 0x07})
	if err != nil {
		t.Fatalf("解析龙鱼令失败: %v", err)
	}
	if tokens != 7 {
		t.Errorf("期望 7 枚龙鱼令, 实际 %d", tokens)
	}

	result, err := ParseFindImmortalExchangeResult([]byte{FindImmortalResultNoEnoughFame})
	if err != nil {
		t.Fatalf("解析兑换结果失败: %v", err)
	}
	if result != FindImmortalResultNoEnoughFame {
		t.Errorf("期望声望不足结果码, 实际 %d", result)
	}

	open, err := ParseFiveBlessingsOpenFlagResult([]byte{FindImmortalResultYes})
	if err != nil || !open {
		t.Errorf("期望五福活动已开启, open=%v err=%v", open, err)
	}
}

func TestFindImmortalCodec_ParseFiveBlessings(t *testing.T) {
	awardHex := "01" + "00000064" + "0032" + "0005" + "0001"

	infoPayload, err := hex.DecodeString(
		"0002" + // bless_number = 2
			"01" + // stage = 1
			"0001" + awardHex + // 1 张奖励卡
			"03", // total_count = 0
	)
	if err != nil {
		t.Fatalf("构造五福面板载荷失败: %v", err)
	}
	info, err := ParseFiveBlessingsInfoResult(infoPayload)
	if err != nil {
		t.Fatalf("解析五福面板失败: %v", err)
	}
	if info.BlessNumber != 2 || info.Stage != 1 || info.TotalCount != 3 || len(info.Awards) != 1 {
		t.Errorf("五福面板字段不匹配: %+v", info)
	}
	if info.Awards[0].ImageID != 1 || info.Awards[0].Skill != 100 ||
		info.Awards[0].XianLing != 50 || info.Awards[0].StarSoul != 5 ||
		info.Awards[0].JewelScrap != 1 {
		t.Errorf("五福奖励不匹配: %+v", info.Awards[0])
	}

	actionPayload, err := hex.DecodeString("00" + "02" + "0001" + awardHex)
	if err != nil {
		t.Fatalf("构造祈福动作载荷失败: %v", err)
	}
	action, err := ParseFiveBlessingsActionResult(actionPayload)
	if err != nil {
		t.Fatalf("解析祈福动作失败: %v", err)
	}
	if action.Result != FindImmortalResultSuccess || action.Stage != 2 || len(action.Awards) != 1 {
		t.Errorf("祈福动作字段不匹配: %+v", action)
	}

	endPayload, err := hex.DecodeString("00" + "0001" + awardHex)
	if err != nil {
		t.Fatalf("构造祈福结算载荷失败: %v", err)
	}
	end, err := ParseFiveBlessingsEndResult(endPayload)
	if err != nil {
		t.Fatalf("解析祈福结算失败: %v", err)
	}
	if end.Result != FindImmortalResultSuccess || len(end.Awards) != 1 {
		t.Errorf("祈福结算字段不匹配: %+v", end)
	}
}
