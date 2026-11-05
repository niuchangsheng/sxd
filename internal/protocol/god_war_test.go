// Package protocol_test 提供神魔大战协议 (Module 118) 的测试用例。
package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestGodWarStatusResult(t *testing.T) {
	status, err := protocol.ParseGodWarStatusResult(protocol.BuildGodWarStatusResultPacket(protocol.GodWarStatusSignUp, 0).Payload)
	if err != nil || status.Status != protocol.GodWarStatusSignUp {
		t.Fatalf("期望报名期状态, got %+v err=%v", status, err)
	}
}

func TestGodWarSignUpResult(t *testing.T) {
	result, err := protocol.ParseGodWarSignUpResult(protocol.BuildGodWarSignUpResultPacket(protocol.GodWarResultSuccess).Payload)
	if err != nil || result != protocol.GodWarResultSuccess {
		t.Fatalf("期望成功码 %d, got %d err=%v", protocol.GodWarResultSuccess, result, err)
	}
	// 失败码路径: 非报名期/已报名等返回其他码
	result, err = protocol.ParseGodWarSignUpResult(protocol.BuildGodWarSignUpResultPacket(2).Payload)
	if err != nil || result == protocol.GodWarResultSuccess {
		t.Fatalf("期望非成功码, got %d err=%v", result, err)
	}
	if _, err := protocol.ParseGodWarSignUpResult(nil); err == nil {
		t.Error("空报名回包应返回解析错误")
	}
}
