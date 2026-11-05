// Package protocol_test 提供仙界矿山协议 (Module 264) 的测试用例。
package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestStMineInfoResult(t *testing.T) {
	info, err := protocol.ParseStMineInfoResult(protocol.BuildStMineInfoResultPacket(protocol.StMineInfo{
		Result: protocol.StMineResultSuccess, Level: 2, CurMountain: 3,
	}).Payload)
	if err != nil || info.Result != protocol.StMineResultSuccess || info.Level != 2 || info.CurMountain != 3 {
		t.Fatalf("矿山信息不一致: %+v err=%v", info, err)
	}
}

func TestStMineActionResult(t *testing.T) {
	result, err := protocol.ParseStMineActionResult(protocol.BuildStMineActionResultPacket(protocol.ActionIDStMineEnter, protocol.StMineResultSuccess).Payload)
	if err != nil || result != protocol.StMineResultSuccess {
		t.Fatalf("期望成功码 9, got %d err=%v", result, err)
	}
	// 失败码: 功能未开放
	result, err = protocol.ParseStMineActionResult(protocol.BuildStMineActionResultPacket(protocol.ActionIDStMineEnter, protocol.StMineResultFunctionUnopen).Payload)
	if err != nil || result != protocol.StMineResultFunctionUnopen {
		t.Fatalf("期望未开放码 %d, got %d err=%v", protocol.StMineResultFunctionUnopen, result, err)
	}
	if _, err := protocol.ParseStMineActionResult(nil); err == nil {
		t.Error("空矿山回包应返回解析错误")
	}
}
