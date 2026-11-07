package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestBuildDragonballOneKeyMergePacket(t *testing.T) {
	pkt := protocol.BuildDragonballOneKeyMergePacket([]int32{
		protocol.DragonballQualityGreen,
		protocol.DragonballQualityBlue,
		protocol.DragonballQualityPurple,
	})
	if pkt.ActionID != protocol.ActionIDDragonballOneKeyMerge || !pkt.HasPrevAct {
		t.Fatalf("packet = %+v", pkt)
	}
	// Uint16 count + 3×Int32 + 4 字节 PrevAct 占位
	if len(pkt.Payload) != 2+12+4 {
		t.Fatalf("payload len = %d", len(pkt.Payload))
	}
	r := protocol.NewReader(pkt.Payload)
	n, _ := r.ReadUint16()
	if n != 3 {
		t.Fatalf("count = %d", n)
	}
	for i, want := range []int32{1, 2, 3} {
		got, _ := r.ReadInt32()
		if got != want {
			t.Fatalf("quality[%d] = %d, want %d", i, got, want)
		}
	}
}

func TestParseDragonballOneKeyMergeResult(t *testing.T) {
	pkt := protocol.BuildDragonballOneKeyMergeResultPacket(protocol.DragonballResultSuccess, []int32{101, 102})
	result, ids, err := protocol.ParseDragonballOneKeyMergeResult(pkt.Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if result != protocol.DragonballResultSuccess || len(ids) != 2 || ids[1] != 102 {
		t.Fatalf("result=%d ids=%v", result, ids)
	}
	if _, _, err := protocol.ParseDragonballOneKeyMergeResult(nil); err == nil {
		t.Fatal("expected error for empty payload")
	}
}
