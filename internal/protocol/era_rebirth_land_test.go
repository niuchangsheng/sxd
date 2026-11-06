package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestParseEraRebirthFreeBackResult_RoundTrip(t *testing.T) {
	pkt := protocol.BuildEraRebirthFreeBackResultPacket(protocol.EraRebirthResultSuccess, []protocol.EraRebirthReward{
		{ItemID: 10994, Num: 72000},
		{ItemID: 10993, Num: 5760},
	})
	res, err := protocol.ParseEraRebirthFreeBackResult(pkt.Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Result != protocol.EraRebirthResultSuccess || len(res.Rewards) != 2 {
		t.Fatalf("res = %+v", res)
	}
	if res.Rewards[0].ItemID != 10994 || res.Rewards[0].Num != 72000 {
		t.Fatalf("reward[0] = %+v", res.Rewards[0])
	}
}

func TestParseEraRebirthFreeBackResult_NoGift(t *testing.T) {
	pkt := protocol.BuildEraRebirthFreeBackResultPacket(protocol.EraRebirthResultNoCanGetGift, nil)
	res, err := protocol.ParseEraRebirthFreeBackResult(pkt.Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Result != protocol.EraRebirthResultNoCanGetGift || len(res.Rewards) != 0 {
		t.Fatalf("res = %+v", res)
	}
	if _, err := protocol.ParseEraRebirthFreeBackResult(nil); err == nil {
		t.Fatal("expected error for empty payload")
	}
}
