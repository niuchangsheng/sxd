// Package protocol_test 验证锁妖塔 (Module 705) 编解码与结果码语义。
//
// 面板字段顺序对齐 com.datas.RepressDemonTowerData.panel_info:
// [UByte result, Int×8, [[Int,Int,Int]], [[Int,Int]], Int×4]。
package protocol_test

import (
	"encoding/hex"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

// s0 实测 panel_info 原始回包 (2026-11-07)，用于锁定字段顺序与数组长度前缀。
const demonTowerPanelLiveHex = "00000000000000001f0001516700000001000000040000002f00000000" +
	"0000004800030000000100000028000001cf0000000200000028000000590000000e" +
	"000000010000001a0004000000040000000a000000030000000c000000020000000e" +
	"000000010000000b0000000000000008000000b800000000"

func TestParseDemonTowerPanelResult_LivePayload(t *testing.T) {
	payload, err := hex.DecodeString(demonTowerPanelLiveHex)
	if err != nil {
		t.Fatalf("测试数据解码失败: %v", err)
	}

	panel, err := protocol.ParseDemonTowerPanelResult(payload)
	if err != nil {
		t.Fatalf("解析锁妖塔面板失败: %v", err)
	}

	if panel.Result != protocol.DemonTowerResultSuccess {
		t.Fatalf("期望结果码 %d, 实际 %d", protocol.DemonTowerResultSuccess, panel.Result)
	}
	if panel.Season != 31 || panel.NowStep != 1 || panel.NowFinishLayer != 4 {
		t.Errorf("赛季/阶数/通关层不匹配: season=%d now_step=%d now_finish_layer=%d",
			panel.Season, panel.NowStep, panel.NowFinishLayer)
	}
	if panel.NowTicketNum != 8 || panel.BuyTicketNum != 0 {
		t.Errorf("挑战次数不匹配: now_ticket_num=%d buy_ticket_num=%d", panel.NowTicketNum, panel.BuyTicketNum)
	}
	if len(panel.StepList) != 3 {
		t.Fatalf("step_list 长度应为 3, 实际 %d", len(panel.StepList))
	}
	if panel.StepList[0] != (protocol.DemonTowerStep{StepUpID: 1, LayerNeed: 40, RoundNeed: 463}) {
		t.Errorf("step_list[0] 不匹配: %+v", panel.StepList[0])
	}
	if len(panel.LayerList) != 4 {
		t.Fatalf("layer_list 长度应为 4, 实际 %d", len(panel.LayerList))
	}
	if panel.LayerList[0] != (protocol.DemonTowerLayer{LayerID: 4, FastestRound: 10}) {
		t.Errorf("layer_list[0] 不匹配: %+v", panel.LayerList[0])
	}
	if got := panel.NextLayerID(); got != 4 {
		t.Errorf("当前可挑战层应为 4, 实际 %d", got)
	}
}

func TestParseDemonTowerPanelResult_FunctionNoOpen(t *testing.T) {
	panel, err := protocol.ParseDemonTowerPanelResult([]byte{protocol.DemonTowerResultFunctionNoOpen})
	if err != nil {
		t.Fatalf("未开放回包应可解析: %v", err)
	}
	if panel.Result != protocol.DemonTowerResultFunctionNoOpen {
		t.Fatalf("期望结果码 %d, 实际 %d", protocol.DemonTowerResultFunctionNoOpen, panel.Result)
	}
}

func TestParseDemonTowerPanelResult_Truncated(t *testing.T) {
	if _, err := protocol.ParseDemonTowerPanelResult(nil); err == nil {
		t.Fatal("空回包应返回错误")
	}
	if _, err := protocol.ParseDemonTowerPanelResult([]byte{0, 0, 0}); err == nil {
		t.Fatal("截断回包应返回错误")
	}
}

func TestDemonTowerPanel_NextLayerIDFallback(t *testing.T) {
	// layer_list 为空但已通关 4 层时，仍应从第 5 层继续。
	panel := &protocol.DemonTowerPanel{NowFinishLayer: 4}
	if got := panel.NextLayerID(); got != 4 {
		t.Errorf("当前可挑战层应为 4, 实际 %d", got)
	}
	// 新赛季无任何记录时从第 1 层开始。
	panel = &protocol.DemonTowerPanel{}
	if got := panel.NextLayerID(); got != 1 {
		t.Errorf("新赛季应从第 1 层开始, 实际 %d", got)
	}
}

func TestParseDemonTowerBaseInfoResult(t *testing.T) {
	inSeason, err := protocol.ParseDemonTowerBaseInfoResult([]byte{1})
	if err != nil || !inSeason {
		t.Errorf("赛季开放解析失败: inSeason=%v err=%v", inSeason, err)
	}
	inSeason, err = protocol.ParseDemonTowerBaseInfoResult([]byte{0})
	if err != nil || inSeason {
		t.Errorf("赛季关闭解析失败: inSeason=%v err=%v", inSeason, err)
	}
	if _, err := protocol.ParseDemonTowerBaseInfoResult(nil); err == nil {
		t.Error("空回包应返回错误")
	}
}

func TestParseDemonTowerRankSelfLayer(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteUint8(protocol.DemonTowerResultSuccess)
	w.WriteInt32(40)  // self_layer
	w.WriteInt32(522) // self_round
	w.WriteInt32(1793981573)
	w.WriteInt32(511)
	result, selfLayer, err := protocol.ParseDemonTowerRankSelfLayer(w.Bytes())
	if err != nil {
		t.Fatalf("解析排行榜自身层数失败: %v", err)
	}
	if result != protocol.DemonTowerResultSuccess || selfLayer != 40 {
		t.Fatalf("排行榜字段不匹配: result=%d self_layer=%d", result, selfLayer)
	}

	result, selfLayer, err = protocol.ParseDemonTowerRankSelfLayer([]byte{protocol.DemonTowerResultFunctionNoOpen})
	if err != nil || result != protocol.DemonTowerResultFunctionNoOpen || selfLayer != 0 {
		t.Fatalf("失败结果码处理不匹配: result=%d self_layer=%d err=%v", result, selfLayer, err)
	}
	if _, _, err := protocol.ParseDemonTowerRankSelfLayer(nil); err == nil {
		t.Error("空排行榜回包应返回错误")
	}
}

func TestParseDemonTowerChallengeResult(t *testing.T) {
	cases := []struct {
		name string
		code uint8
	}{
		{"success", protocol.DemonTowerResultSuccess},
		{"not_enough_ticket", protocol.DemonTowerResultNotEnoughTicket},
		{"layer_error", protocol.DemonTowerResultLayerError},
		{"item_pack_full", protocol.DemonTowerResultItemPackFull},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := protocol.ParseDemonTowerChallengeResult([]byte{tc.code})
			if err != nil {
				t.Fatalf("解析结果码失败: %v", err)
			}
			if got != tc.code {
				t.Fatalf("期望结果码 %d, 实际 %d", tc.code, got)
			}
		})
	}
	if _, err := protocol.ParseDemonTowerChallengeResult(nil); err == nil {
		t.Error("空挑战回包应返回错误")
	}
}
