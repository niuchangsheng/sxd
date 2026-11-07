package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestBuildTacticsBuyItemPacket(t *testing.T) {
	pkt := protocol.BuildTacticsBuyItemPacket(protocol.TacticsWaterXiao)
	if pkt.ActionID != protocol.ActionIDTacticsBuyItem || len(pkt.Payload) != 1 ||
		pkt.Payload[0] != protocol.TacticsWaterXiao {
		t.Fatalf("packet = %+v", pkt)
	}
}

func TestParseTacticsResultCode(t *testing.T) {
	if code, err := protocol.ParseTacticsResultCode([]byte{protocol.TacticsResultSucceed}); err != nil || code != 7 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if _, err := protocol.ParseTacticsResultCode(nil); err == nil {
		t.Fatal("expected error for empty payload")
	}
}
