// Package protocol_test 提供跨服PK协议 (Module 349) 的测试用例。
package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestWorldPkStatusResult(t *testing.T) {
	status, err := protocol.ParseWorldPkStatusResult(protocol.BuildWorldPkStatusResultPacket(protocol.WorldPkStatusOn).Payload)
	if err != nil || status.Status != protocol.WorldPkStatusOn {
		t.Fatalf("期望跨服PK开启状态, got %+v err=%v", status, err)
	}
	if _, err := protocol.ParseWorldPkStatusResult(nil); err == nil {
		t.Error("空跨服PK状态回包应返回解析错误")
	}
}

func TestWorldPkJoinResult(t *testing.T) {
	res, err := protocol.ParseWorldPkJoinResult(protocol.BuildWorldPkJoinResultPacket(protocol.WorldPkResultSuccess).Payload)
	if err != nil || res.Result != protocol.WorldPkResultSuccess {
		t.Fatalf("期望加入成功, got %+v err=%v", res, err)
	}
	// 失败码: 等级不足
	res, err = protocol.ParseWorldPkJoinResult(protocol.BuildWorldPkJoinResultPacket(protocol.WorldPkResultLowLevel).Payload)
	if err != nil || res.Result != protocol.WorldPkResultLowLevel {
		t.Fatalf("期望等级不足码 %d, got %+v err=%v", protocol.WorldPkResultLowLevel, res, err)
	}
}
