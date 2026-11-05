// Package protocol_test 提供自动钓鱼协议 (Module 217) 的测试用例。
package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestFishHooksResult(t *testing.T) {
	n, err := protocol.ParseFishHooksResult(protocol.BuildFishHooksResultPacket(5).Payload)
	if err != nil || n != 5 {
		t.Fatalf("期望鱼钩数量 5, got %d err=%v", n, err)
	}
}

func TestFishDoResult(t *testing.T) {
	// 成功路径
	res, err := protocol.ParseFishDoResult(protocol.BuildFishDoResultPacket(protocol.FishResultSuccess, 5715, 1).Payload)
	if err != nil {
		t.Fatalf("解析钓鱼回包失败: %v", err)
	}
	if res.Result != protocol.FishResultSuccess || res.ItemID != 5715 || res.Number != 1 {
		t.Errorf("钓鱼回包不一致: %+v", res)
	}
	// 失败码路径: 鱼钩不足必须能被识别
	res, err = protocol.ParseFishDoResult(protocol.BuildFishDoResultPacket(protocol.FishResultNotEnoughFishhook, 0, 0).Payload)
	if err != nil || res.Result != protocol.FishResultNotEnoughFishhook {
		t.Fatalf("期望鱼钩不足码 %d, got %+v err=%v", protocol.FishResultNotEnoughFishhook, res, err)
	}
	// 空回包必须报错
	if _, err := protocol.ParseFishDoResult(nil); err == nil {
		t.Error("空钓鱼回包应返回解析错误")
	}
}
