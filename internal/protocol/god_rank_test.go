package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

// TestBuildGodRankWorshipPacket 验证 500/1 为 [] + 4 字节 PrevAct 尾链。
func TestBuildGodRankWorshipPacket(t *testing.T) {
	pkt := protocol.BuildGodRankWorshipPacket()
	if pkt.ActionID != protocol.ActionIDGodRankWorship {
		t.Fatalf("action = 0x%08X", pkt.ActionID)
	}
	if !pkt.PrevActFromLastSent || len(pkt.Payload) != 4 {
		t.Fatalf("应为 4 字节 PrevAct 占位, got %+v", pkt)
	}
}
