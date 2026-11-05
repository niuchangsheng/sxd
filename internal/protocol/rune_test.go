// Package protocol_test 提供符文批量信息协议 (Module 16) 的测试用例。
package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestRuneBatchListResult(t *testing.T) {
	info, err := protocol.ParseRuneBatchListResult(protocol.BuildRuneBatchListResultPacket(protocol.RuneBatchInfo{
		Field1: 600, Field2: 10, Field3: 98, Field4: 24050000,
	}).Payload)
	if err != nil || info.Field1 != 600 || info.Field2 != 10 || info.Field3 != 98 || info.Field4 != 24050000 {
		t.Fatalf("符文批量信息不一致: %+v err=%v", info, err)
	}
}
