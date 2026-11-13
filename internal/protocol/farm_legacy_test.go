package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestParseFarmRoleIDs(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteUint16(2)
	for _, id := range []int32{884402, 884192} {
		w.WriteInt32(id)
		w.WriteString("伙伴名")
		w.WriteString("sign")
		w.WriteInt32(94)
		w.WriteInt64(1000)
		w.WriteInt64(2000)
		w.WriteInt32(1)
		w.WriteInt32(2)
		w.WriteInt32(3)
		w.WriteUint8(1)
	}
	ids, err := protocol.ParseFarmRoleIDs(w.Bytes())
	if err != nil {
		t.Fatalf("ParseFarmRoleIDs: %v", err)
	}
	if len(ids) != 2 || ids[0] != 884402 || ids[1] != 884192 {
		t.Fatalf("ids = %v", ids)
	}
	if _, err := protocol.ParseFarmRoleIDs(nil); err == nil {
		t.Fatal("空载荷必须报错")
	}
}

func TestParseFarmCoinTreeInfo(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteInt32(0)   // count
	w.WriteInt32(3)   // remain buy
	w.WriteInt32(2)   // ingot
	w.WriteInt32(204) // recover cd
	info, err := protocol.ParseFarmCoinTreeInfo(w.Bytes())
	if err != nil {
		t.Fatalf("ParseFarmCoinTreeInfo: %v", err)
	}
	if info.Count != 0 || info.RemainBuyTimes != 3 || info.Ingot != 2 || info.RecoverCD != 204 {
		t.Fatalf("info = %+v", info)
	}
	if _, err := protocol.ParseFarmCoinTreeInfo([]byte{0, 0, 0}); err == nil {
		t.Fatal("截断载荷必须报错")
	}
}

func TestBuildFarmPlantHerbsPacket(t *testing.T) {
	pkt := protocol.BuildFarmPlantHerbsPacket(162236, 884402, protocol.FarmHerbTypeCoin, 5)
	if pkt.ActionID != protocol.ActionIDFarmPlantHerbs {
		t.Fatalf("ActionID = 0x%08X", pkt.ActionID)
	}
	// [Int32 land][Int32 role][Uint8 type][Int8 star] = 10 字节
	if len(pkt.Payload) != 10 {
		t.Fatalf("payload len = %d, want 10: %x", len(pkt.Payload), pkt.Payload)
	}
	if pkt.Payload[8] != protocol.FarmHerbTypeCoin || pkt.Payload[9] != 5 {
		t.Fatalf("type/star = %d/%d", pkt.Payload[8], pkt.Payload[9])
	}
}
