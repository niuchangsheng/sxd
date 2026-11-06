package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestParseFateHunterPanel_RoundTrip(t *testing.T) {
	panel := protocol.FateHunterPanel{
		FreeAppointTimes: 1,
		Hunters: []protocol.FateHunter{
			{NpcID: 4, Status: 0, CanBuyTimer: 30, MaxBuyTimes: 30, BuyPrice: 20},
			{NpcID: 3, Status: 0},
			{NpcID: 1, Status: 1},
		},
		Tail: 100,
	}
	pkt := protocol.BuildFateHunterPanelResultPacket(panel)
	got, err := protocol.ParseFateHunterPanel(pkt.Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.FreeAppointTimes != 1 || got.Tail != 100 || len(got.Hunters) != 3 {
		t.Fatalf("panel = %+v", got)
	}
	if got.Hunters[0].NpcID != 4 || got.Hunters[0].BuyPrice != 20 {
		t.Fatalf("hunter[0] = %+v", got.Hunters[0])
	}
	if got.Hunters[2].Status != 1 {
		t.Fatalf("hunter[2] = %+v", got.Hunters[2])
	}
}

func TestParseFateAppointResult_SuccessAndInvalid(t *testing.T) {
	okPkt := protocol.BuildFateAppointResultPacket(protocol.AppointFateResult{
		Result:     protocol.FateHuntResultSuccess,
		FateID:     8,
		NpcID:      1,
		TempFateID: 123456789,
	})
	res, err := protocol.ParseFateAppointResult(okPkt.Payload)
	if err != nil {
		t.Fatalf("parse success: %v", err)
	}
	if res.Result != protocol.FateHuntResultSuccess || res.FateID != 8 || res.NpcID != 1 || res.TempFateID != 123456789 {
		t.Fatalf("result = %+v", res)
	}

	badPkt := protocol.BuildFateAppointResultPacket(protocol.AppointFateResult{
		Result: protocol.FateHuntResultInvalidFateNPCID,
		NpcID:  5,
	})
	bad, err := protocol.ParseFateAppointResult(badPkt.Payload)
	if err != nil {
		t.Fatalf("parse invalid: %v", err)
	}
	if bad.Result != protocol.FateHuntResultInvalidFateNPCID {
		t.Fatalf("bad result = %+v", bad)
	}
}

func TestParseFateMingGongInfo_RoundTrip(t *testing.T) {
	pkt := protocol.BuildFateMingGongInfoResultPacket(protocol.MingGongInfo{
		MingGong:      10,
		Scrap:         2628,
		ExchangeTimes: 500,
	})
	info, err := protocol.ParseFateMingGongInfo(pkt.Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if info.MingGong != 10 || info.Scrap != 2628 || info.ExchangeTimes != 500 {
		t.Fatalf("info = %+v", info)
	}
}

func TestParseFateMingGongExchangeResult_RoundTrip(t *testing.T) {
	pkt := protocol.BuildFateMingGongExchangeResultPacket(protocol.MingGongExchangeResult{
		Result: protocol.FateHuntResultSuccess,
		Scrap:  2629,
	})
	res, err := protocol.ParseFateMingGongExchangeResult(pkt.Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Result != protocol.FateHuntResultSuccess || res.Scrap != 2629 {
		t.Fatalf("res = %+v", res)
	}

	noCoin := protocol.BuildFateMingGongExchangeResultPacket(protocol.MingGongExchangeResult{
		Result: protocol.FateHuntResultNotEnoughCoin,
		Scrap:  10,
	})
	res, err = protocol.ParseFateMingGongExchangeResult(noCoin.Payload)
	if err != nil {
		t.Fatalf("parse no coin: %v", err)
	}
	if res.Result != protocol.FateHuntResultNotEnoughCoin {
		t.Fatalf("res = %+v", res)
	}
}

func TestFatePacketBuilders(t *testing.T) {
	npcPkt := protocol.BuildFateGetNPCPacket()
	if npcPkt.ActionID != protocol.ActionIDFateGetNPC || !npcPkt.HasPrevAct || len(npcPkt.Payload) != 4 {
		t.Fatalf("get npc packet = %+v", npcPkt)
	}
	apptPkt := protocol.BuildFateAppointNPCPacket(3)
	if apptPkt.ActionID != protocol.ActionIDFateAppointNPC || apptPkt.HasPrevAct || len(apptPkt.Payload) != 1 || apptPkt.Payload[0] != 3 {
		t.Fatalf("appoint packet = %+v", apptPkt)
	}
	exPkt := protocol.BuildFateExchangeMingGongScrapPacket(2)
	if exPkt.ActionID != protocol.ActionIDFateExchangeMingGongScrap || len(exPkt.Payload) != 4 || exPkt.Payload[3] != 2 {
		t.Fatalf("exchange packet = %+v", exPkt)
	}
}
