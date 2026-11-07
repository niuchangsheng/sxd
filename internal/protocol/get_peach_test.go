// Package protocol 提供摘仙桃协议编解码测试。
//
// @author Ateng
// @since 2026-11-07
package protocol

import (
	"encoding/hex"
	"testing"
)

func TestGetPeachPanel_LiveSample(t *testing.T) {
	// 2026-11-07 s1 线上样本: peachLv=2, peachNum=0, is_can_batch=0, is_can_call_peach=0。
	payload, err := hex.DecodeString("02000000")
	if err != nil {
		t.Fatalf("decode sample: %v", err)
	}
	panel, err := ParseGetPeachPanelResult(payload)
	if err != nil {
		t.Fatalf("parse panel: %v", err)
	}
	if panel.PeachLv != 2 || panel.PeachNum != 0 || panel.CanBatch || panel.CanCall {
		t.Fatalf("面板字段不符: %+v", panel)
	}

	roundTrip, err := ParseGetPeachPanelResult(BuildGetPeachPanelResultPacket(*panel).Payload)
	if err != nil {
		t.Fatalf("round trip parse: %v", err)
	}
	if *roundTrip != *panel {
		t.Fatalf("面板回包不幂等: want=%+v got=%+v", panel, roundTrip)
	}
}

func TestGetPeachPlayerInfo_LiveSample(t *testing.T) {
	// 2026-11-07 s1 线上样本:
	// playerCount=0, haveMonkeyNum=0, canBuyCount=5, call_coin_times=0,
	// call_ingot_times=0, call_need_coin=10000000, call_need_ingot=100。
	payload, err := hex.DecodeString("00000500000098968000000064")
	if err != nil {
		t.Fatalf("decode sample: %v", err)
	}
	info, err := ParseGetPeachPlayerInfoResult(payload)
	if err != nil {
		t.Fatalf("parse player info: %v", err)
	}
	want := GetPeachPlayerInfo{
		PlayerCount:    0,
		HaveMonkeyNum:  0,
		CanBuyCount:    5,
		CallCoinTimes:  0,
		CallIngotTimes: 0,
		CallNeedCoin:   10000000,
		CallNeedIngot:  100,
	}
	if *info != want {
		t.Fatalf("玩家信息字段不符: want=%+v got=%+v", want, *info)
	}

	roundTrip, err := ParseGetPeachPlayerInfoResult(BuildGetPeachPlayerInfoResultPacket(want).Payload)
	if err != nil {
		t.Fatalf("round trip parse: %v", err)
	}
	if *roundTrip != want {
		t.Fatalf("玩家信息回包不幂等: want=%+v got=%+v", want, *roundTrip)
	}
}

func TestGetPeachBuilders(t *testing.T) {
	info := BuildGetPeachInfoPacket(0x00280004)
	if info.ActionID != ActionIDGetPeachInfo || !info.HasPrevAct {
		t.Fatalf("面板封包类型不符: %+v", info)
	}
	if got := hex.EncodeToString(info.Payload); got != "00280004" {
		t.Fatalf("面板封包 PrevAct 不符: %s", got)
	}

	player := BuildGetPeachPlayerInfoPacket(0x00280003)
	if player.ActionID != ActionIDGetPeachPlayerInfo || !player.HasPrevAct {
		t.Fatalf("玩家信息封包类型不符: %+v", player)
	}
	if got := hex.EncodeToString(player.Payload); got != "00280003" {
		t.Fatalf("玩家信息封包 PrevAct 不符: %s", got)
	}

	harvest := BuildGetPeachHarvestPacket(0x00280004)
	if harvest.ActionID != ActionIDGetPeachHarvest || !harvest.HasPrevAct {
		t.Fatalf("摘取封包类型不符: %+v", harvest)
	}

	call := BuildGetPeachCallPacket()
	if call.ActionID != ActionIDGetPeachCall || call.HasPrevAct || len(call.Payload) != 0 {
		t.Fatalf("召唤封包应为空载荷: %+v", call)
	}
}

func TestGetPeachCallResult(t *testing.T) {
	res, err := ParseGetPeachCallResult(BuildGetPeachCallResultPacket(GetPeachResultSuccess).Payload)
	if err != nil || res.Result != GetPeachResultSuccess {
		t.Fatalf("召唤成功解析失败: res=%+v err=%v", res, err)
	}
	if got := GetPeachResultText(GetPeachResultFailure); got != "失败" {
		t.Fatalf("结果码文本不符: %s", got)
	}
	if got := GetPeachResultText(GetPeachResultNoEnoughCoin); got != "铜钱不足" {
		t.Fatalf("结果码文本不符: %s", got)
	}
}

func TestGetPeachHarvestResult(t *testing.T) {
	res, err := ParseGetPeachHarvestResult(BuildGetPeachHarvestResultPacket(GetPeachResultSuccess, 123456).Payload)
	if err != nil {
		t.Fatalf("parse success: %v", err)
	}
	if res.Result != GetPeachResultSuccess || res.WarExp != 123456 {
		t.Fatalf("摘取成功字段不符: %+v", res)
	}

	// 失败回包可能只有结果码，不应因缺少 war_exp 而报错。
	res, err = ParseGetPeachHarvestResult(BuildGetPeachHarvestResultPacket(GetPeachResultNotEnoughPeachCount, 0).Payload)
	if err != nil {
		t.Fatalf("parse failure: %v", err)
	}
	if res.Result != GetPeachResultNotEnoughPeachCount || res.WarExp != 0 {
		t.Fatalf("摘取失败字段不符: %+v", res)
	}
}
