// Package protocol_test 提供点金石协议 (Module 551) 的测试用例。
package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestGoldenTouchStoneStatusResult(t *testing.T) {
	status, err := protocol.ParseGoldenTouchStoneStatusResult(protocol.BuildGoldenTouchStoneStatusResultPacket(protocol.GoldenTouchStoneStatusTouch).Payload)
	if err != nil || status != protocol.GoldenTouchStoneStatusTouch {
		t.Fatalf("期望可点化状态, got %d err=%v", status, err)
	}
	if _, err := protocol.ParseGoldenTouchStoneStatusResult(nil); err == nil {
		t.Error("空状态回包应返回解析错误")
	}
}

func TestGoldenTouchStoneTouchPanelResult(t *testing.T) {
	panel, err := protocol.ParseGoldenTouchStoneTouchPanelResult(protocol.BuildGoldenTouchStoneTouchPanelResultPacket(protocol.GoldenTouchStonePanel{
		Level: 2, Exp: 100, ThisWeekLuckValue: 8, CurActivity: 3, NextWeekLuckValue: 9, TodayTouchNum: 4,
	}).Payload)
	if err != nil || panel.Level != 2 || panel.TodayTouchNum != 4 {
		t.Fatalf("点金石面板不一致: %+v err=%v", panel, err)
	}
}

func TestGoldenTouchStoneTouchResult(t *testing.T) {
	result, exp, luck, err := protocol.ParseGoldenTouchStoneTouchResult(
		protocol.BuildGoldenTouchStoneTouchResultPacket(protocol.GoldenTouchStoneResultSuccess, 12, 6).Payload)
	if err != nil || result != protocol.GoldenTouchStoneResultSuccess || exp != 12 || luck != 6 {
		t.Fatalf("点化回包不一致: result=%d exp=%d luck=%d err=%v", result, exp, luck, err)
	}
	// 失败码: 帮助不足
	result, _, _, err = protocol.ParseGoldenTouchStoneTouchResult(
		protocol.BuildGoldenTouchStoneTouchResultPacket(protocol.GoldenTouchStoneResultNotEnoughHelp, 0, 0).Payload)
	if err != nil || result != protocol.GoldenTouchStoneResultNotEnoughHelp {
		t.Fatalf("期望帮助不足码 %d, got %d err=%v", protocol.GoldenTouchStoneResultNotEnoughHelp, result, err)
	}
}
