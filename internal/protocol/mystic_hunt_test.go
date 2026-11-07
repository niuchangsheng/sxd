// Package protocol 提供仙幽猎境协议编解码测试。
//
// @author Ateng
// @since 2026-11-07
package protocol

import (
	"encoding/hex"
	"testing"
)

func TestMysticHuntPackets(t *testing.T) {
	entrance := BuildMysticHuntEntrancePacket()
	if entrance.ActionID != ActionIDMysticHuntEntranceOpen || entrance.HasPrevAct || len(entrance.Payload) != 0 {
		t.Fatalf("入口状态封包不符: %+v", entrance)
	}
	enter := BuildMysticHuntEnterPacket()
	if enter.ActionID != ActionIDMysticHuntEnter || enter.HasPrevAct || len(enter.Payload) != 0 {
		t.Fatalf("进入封包不符: %+v", enter)
	}
}

func TestMysticHuntResults(t *testing.T) {
	// 线上样本: 入口关闭 = 0
	open, err := ParseMysticHuntEntranceResult([]byte{0, 0, 0, 0})
	if err != nil || open != 0 {
		t.Fatalf("入口关闭解析不符: open=%d err=%v", open, err)
	}
	openPkt := BuildMysticHuntEntranceResultPacket(1)
	if open, err = ParseMysticHuntEntranceResult(openPkt.Payload); err != nil || open != 1 {
		t.Fatalf("入口开放解析不符: open=%d err=%v", open, err)
	}
	if _, err := ParseMysticHuntEntranceResult(nil); err == nil {
		t.Fatal("空载荷应返回错误")
	}

	// 成功与失败码透传
	if code, err := ParseMysticHuntEnterResult([]byte{MysticHuntResultSuccess}); err != nil || code != MysticHuntResultSuccess {
		t.Fatalf("成功码解析不符: code=%d err=%v", code, err)
	}
	if code, err := ParseMysticHuntEnterResult([]byte{MysticHuntResultNoTimes}); err != nil || code != MysticHuntResultNoTimes {
		t.Fatalf("失败码解析不符: code=%d err=%v", code, err)
	}
	if _, err := ParseMysticHuntEnterResult(nil); err == nil {
		t.Fatal("空载荷应返回错误")
	}
	if got := hex.EncodeToString(BuildMysticHuntEnterResultPacket(0).Payload); got != "00" {
		t.Fatalf("进入回包编码不符: %s", got)
	}
}