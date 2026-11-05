// Package protocol_test 提供仙盟/圣盟/住宅状态探测协议的测试用例。
package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestSectOpenStatusResult(t *testing.T) {
	status, err := protocol.ParseSectOpenStatusResult(protocol.BuildSectOpenStatusResultPacket(1).Payload)
	if err != nil || status != 1 {
		t.Fatalf("期望仙盟开放状态 1, got %d err=%v", status, err)
	}
	if _, err := protocol.ParseSectOpenStatusResult(nil); err == nil {
		t.Error("空仙盟状态回包应返回解析错误")
	}
}

func TestSectAllianceSeasonResult(t *testing.T) {
	season, endTime, err := protocol.ParseSectAllianceSeasonResult(protocol.BuildSectAllianceSeasonResultPacket(3, 1800000000).Payload)
	if err != nil || season != 3 || endTime != 1800000000 {
		t.Fatalf("圣盟赛季不一致: season=%d end=%d err=%v", season, endTime, err)
	}
}

func TestMarryHomeSelfInfoResult(t *testing.T) {
	result, err := protocol.ParseMarryHomeSelfInfoResult(protocol.BuildMarryHomeSelfInfoResultPacket(protocol.MarryHomeResultNonMarry).Payload)
	if err != nil || result != protocol.MarryHomeResultNonMarry {
		t.Fatalf("期望未结婚码 %d, got %d err=%v", protocol.MarryHomeResultNonMarry, result, err)
	}
	if _, err := protocol.ParseMarryHomeSelfInfoResult(nil); err == nil {
		t.Error("空住宅状态回包应返回解析错误")
	}
}
