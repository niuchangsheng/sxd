package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestParseCaravanRobResult_Success(t *testing.T) {
	pkt := protocol.BuildCaravanRobResultPacket(protocol.CaravanResultSuccess, [2]int32{3, 120000})
	res, err := protocol.ParseCaravanRobResult(pkt.Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Result != protocol.CaravanResultSuccess || res.Award[0] != 3 || res.Award[1] != 120000 {
		t.Fatalf("result = %+v", res)
	}
}

func TestParseCaravanRobResult_Failure(t *testing.T) {
	pkt := protocol.BuildCaravanRobResultPacket(protocol.CaravanResultFailure, [2]int32{})
	res, err := protocol.ParseCaravanRobResult(pkt.Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Result != protocol.CaravanResultFailure {
		t.Fatalf("result = %+v", res)
	}
	if _, err := protocol.ParseCaravanRobResult(nil); err == nil {
		t.Fatal("expected error for empty payload")
	}
}

func TestParseCaravanInfoResultCode(t *testing.T) {
	pkt := protocol.BuildCaravanInfoResultPacket(protocol.CaravanResultNotActivityTime)
	code, err := protocol.ParseCaravanInfoResultCode(pkt.Payload)
	if err != nil || code != protocol.CaravanResultNotActivityTime {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if _, err := protocol.ParseCaravanInfoResultCode(nil); err == nil {
		t.Fatal("expected error for empty payload")
	}
}
