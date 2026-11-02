// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// @author Ateng
// @since 2026-11-02
package protocol

import (
	"encoding/hex"
	"testing"
)

func TestRollCakeCodec_BuildRequests(t *testing.T) {
	cases := []struct {
		name     string
		pkt      *Packet
		actionID uint32
	}{
		{"roll", BuildRollCakeRollPacket(ActionIDTownEnter), ActionIDRollCakeRoll},
		{"get_count", BuildRollCakeGetCountPacket(ActionIDRollCakeRoll), ActionIDRollCakeGetCount},
		{"reroll", BuildRollCakeRerollPacket(ActionIDRollCakeGetCount), ActionIDRollCakeReroll},
		{"get_award", BuildRollCakeGetAwardPacket(ActionIDRollCakeReroll), ActionIDRollCakeGetAward},
		{"get_state", BuildRollCakeGetStatePacket(ActionIDRollCakeGetAward), ActionIDRollCakeGetState},
		{"is_auto", BuildRollCakeIsAutoPacket(ActionIDRollCakeGetState), ActionIDRollCakeIsAuto},
	}

	for _, tc := range cases {
		if tc.pkt.ActionID != tc.actionID {
			t.Errorf("%s ActionID 不匹配: 期望 0x%08X, 实际 0x%08X", tc.name, tc.actionID, tc.pkt.ActionID)
		}
		if len(tc.pkt.Payload) != 4 {
			t.Errorf("%s 请求载荷应为 4 字节前序协议号, 实际 %d 字节", tc.name, len(tc.pkt.Payload))
		}
	}

	// 未指定前序协议时默认回落到进入城镇动作
	if got := hex.EncodeToString(BuildRollCakeRollPacket(0).Payload); got != "00010000" {
		t.Errorf("默认 prev_act 应为 town_enter, 实际: %s", got)
	}

	// set_auto 请求为 1 字节开关 + 4 字节前序协议号
	setAuto := BuildRollCakeSetAutoPacket(1, ActionIDRollCakeIsAuto)
	if setAuto.ActionID != ActionIDRollCakeSetAuto {
		t.Errorf("set_auto ActionID 不匹配: 0x%08X", setAuto.ActionID)
	}
	if got := hex.EncodeToString(setAuto.Payload); got != "0100260006" {
		t.Errorf("set_auto 载荷不匹配: %s", got)
	}
}

func TestRollCakeCodec_ParseCount(t *testing.T) {
	payload, err := hex.DecodeString(
		"0002" + // count = 2
			"03" + // free_reroll_times = 3
			"05" + // max_buy_reroll_times = 5
			"00000007", // roll_count = 7
	)
	if err != nil {
		t.Fatalf("构造测试载荷失败: %v", err)
	}

	info, err := ParseRollCakeCountResult(payload)
	if err != nil {
		t.Fatalf("解析次数面板失败: %v", err)
	}
	if info.Count != 2 || info.FreeRerollTimes != 3 || info.MaxBuyRerollTimes != 5 || info.RollCount != 7 {
		t.Errorf("次数面板字段不匹配: %+v", info)
	}

	if _, err := ParseRollCakeCountResult(nil); err == nil {
		t.Error("空载荷应返回解析错误")
	}
}

func TestRollCakeCodec_ParseRoll(t *testing.T) {
	payload, err := hex.DecodeString(
		"00" + // msg = 0 (SUCCESS)
			"06" + // type = 6
			"0000000a" + // reroll_ingot = 10
			"010203040506" + // dice = 1,2,3,4,5,6
			"00000008", // roll_count = 8
	)
	if err != nil {
		t.Fatalf("构造测试载荷失败: %v", err)
	}

	res, err := ParseRollCakeRollResult(payload)
	if err != nil {
		t.Fatalf("解析投掷回包失败: %v", err)
	}
	if res.Msg != RollCakeCodeSuccess {
		t.Errorf("结果码不匹配: %d", res.Msg)
	}
	if res.Type != 6 || res.RerollIngot != 10 || res.RollCount != 8 {
		t.Errorf("投掷字段不匹配: %+v", res)
	}
	if len(res.Dice) != 6 || res.Dice[0] != 1 || res.Dice[5] != 6 {
		t.Errorf("骰子点数不匹配: %v", res.Dice)
	}
}

func TestRollCakeCodec_ParseReroll(t *testing.T) {
	payload, err := hex.DecodeString(
		"00" + // msg = 0 (SUCCESS)
			"0b" + // type = 11
			"02" + // free_reroll_times = 2
			"00000014" + // reroll_ingot = 20
			"040404040404" + // dice = 4,4,4,4,4,4 (六吉)
			"00000009", // roll_count = 9
	)
	if err != nil {
		t.Fatalf("构造测试载荷失败: %v", err)
	}

	res, err := ParseRollCakeRerollResult(payload)
	if err != nil {
		t.Fatalf("解析逆天改运回包失败: %v", err)
	}
	if res.Msg != RollCakeCodeSuccess || res.Type != 11 {
		t.Errorf("结果码或奖励类型不匹配: %+v", res)
	}
	if res.FreeRerollTimes != 2 || res.RerollIngot != 20 || res.RollCount != 9 {
		t.Errorf("改运字段不匹配: %+v", res)
	}
	for i, num := range res.Dice {
		if num != 4 {
			t.Errorf("第 %d 颗骰子应为吉(4), 实际 %d", i+1, num)
		}
	}
}

func TestRollCakeCodec_ParseStateAndAward(t *testing.T) {
	statePayload, err := hex.DecodeString(
		"05" + // msg = 5 (HAVE_RECORD)
			"03" + // type = 3
			"0000000a" + // reroll_ingot = 10
			"060606060606", // dice
	)
	if err != nil {
		t.Fatalf("构造测试载荷失败: %v", err)
	}
	state, err := ParseRollCakeStateResult(statePayload)
	if err != nil {
		t.Fatalf("解析未领取记录失败: %v", err)
	}
	if state.Msg != RollCakeCodeHaveRecord || state.Type != 3 || len(state.Dice) != 6 {
		t.Errorf("未领取记录字段不匹配: %+v", state)
	}

	award, err := ParseRollCakeAwardResult([]byte{RollCakeCodeSuccess})
	if err != nil {
		t.Fatalf("解析领取奖励回包失败: %v", err)
	}
	if award.Msg != RollCakeCodeSuccess {
		t.Errorf("领取奖励结果码不匹配: %d", award.Msg)
	}

	auto, err := ParseRollCakeIsAutoResult([]byte{1})
	if err != nil || auto != 1 {
		t.Errorf("解析自动改运开关失败: auto=%d err=%v", auto, err)
	}
	msg, err := ParseRollCakeSetAutoResult([]byte{RollCakeCodeSuccess})
	if err != nil || msg != RollCakeCodeSuccess {
		t.Errorf("解析设置自动改运回包失败: msg=%d err=%v", msg, err)
	}
}

func TestRollCakeCodec_TruncatedDice(t *testing.T) {
	// 投掷回包声明到第 4 颗骰子即截断，必须报错而不是越界
	payload, _ := hex.DecodeString("00060000000a010203")
	if _, err := ParseRollCakeRollResult(payload); err == nil {
		t.Error("截断载荷应返回解析错误")
	}

	// 未领取记录回包同样必须校验 6 颗骰子
	statePayload, _ := hex.DecodeString("05030000000a060606")
	if _, err := ParseRollCakeStateResult(statePayload); err == nil {
		t.Error("截断的未领取记录应返回解析错误")
	}
}
