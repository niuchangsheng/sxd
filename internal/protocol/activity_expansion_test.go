package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestBuildFairylandMainPanelWithPrevActPacket(t *testing.T) {
	pkt := protocol.BuildFairylandMainPanelWithPrevActPacket(0x0000002A)
	if pkt.ActionID != protocol.ActionIDFairylandMainPanel || pkt.HasPrevAct {
		t.Fatalf("714/1 PrevAct 变体封包不符: %+v", pkt)
	}
	if len(pkt.Payload) != 4 || pkt.Payload[3] != 0x2A {
		t.Fatalf("714/1 PrevAct 载荷不符: %x", pkt.Payload)
	}
}
