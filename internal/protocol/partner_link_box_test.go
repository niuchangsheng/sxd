package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestParsePartnerLinkShopResult_RoundTrip(t *testing.T) {
	pkt := protocol.BuildPartnerLinkShopResultPacket([]protocol.PartnerLinkBox{
		{BoxType: 7, OnceNum: 12, Times: 5, TotalTimes: 5, MoneyType: protocol.PartnerLinkMoneyIngot, MoneyNum: 100},
		{BoxType: 6, OnceNum: 6, Times: 3, TotalTimes: 3, MoneyType: protocol.PartnerLinkMoneyDaoYuan, MoneyNum: 200},
	})
	boxes, err := protocol.ParsePartnerLinkShopResult(pkt.Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(boxes) != 2 || boxes[1].BoxType != 6 || boxes[1].Times != 3 ||
		boxes[1].MoneyType != protocol.PartnerLinkMoneyDaoYuan || boxes[1].MoneyNum != 200 {
		t.Fatalf("boxes = %+v", boxes)
	}
	if _, err := protocol.ParsePartnerLinkShopResult(nil); err == nil {
		t.Fatal("expected error for empty payload")
	}
}

func TestBuildPartnerLinkBuyPacket(t *testing.T) {
	pkt := protocol.BuildPartnerLinkBuyPacket(6)
	if pkt.ActionID != protocol.ActionIDPartnerLinkBuy || len(pkt.Payload) != 1 || pkt.Payload[0] != 6 {
		t.Fatalf("packet = %+v", pkt)
	}
}
