package protocol

import (
	"encoding/hex"
	"testing"
)

func TestNewPractice_BuildPackets(t *testing.T) {
	pktGetInfo := BuildNewPracticeGetInfoPacket()
	if pktGetInfo.ActionID != ActionIDNewPracticeGetInfo {
		t.Fatalf("expected ActionID 0x%08X, got 0x%08X", ActionIDNewPracticeGetInfo, pktGetInfo.ActionID)
	}
	if len(pktGetInfo.Payload) != 0 {
		t.Fatalf("expected empty payload, got %d", len(pktGetInfo.Payload))
	}

	pktCollect := BuildNewPracticeCollectAllPacket()
	if pktCollect.ActionID != ActionIDNewPracticeCollectAll {
		t.Fatalf("expected ActionID 0x%08X, got 0x%08X", ActionIDNewPracticeCollectAll, pktCollect.ActionID)
	}
	if len(pktCollect.Payload) != 0 {
		t.Fatalf("expected empty payload, got %d", len(pktCollect.Payload))
	}
}

func TestNewPractice_ParseGetInfoResponse(t *testing.T) {
	// Golden response from live server:
	// payload = 0000000000bcc00003f480000000000044ced8000000010000000000000000
	rawHex := "0000000000bcc00003f480000000000044ced8000000010000000000000000"
	payload, err := hex.DecodeString(rawHex)
	if err != nil {
		t.Fatalf("failed to decode hex: %v", err)
	}

	info, err := ParseNewPracticeGetInfoResponse(payload)
	if err != nil {
		t.Fatalf("failed to parse get_info response: %v", err)
	}

	if info.Result != NewPracticeSuccess {
		t.Errorf("expected result %d, got %d", NewPracticeSuccess, info.Result)
	}
	if len(info.AirList) != 0 {
		t.Errorf("expected 0 air items, got %d", len(info.AirList))
	}
	if info.GatherTime != 48320 {
		t.Errorf("expected gather_time 48320, got %d", info.GatherTime)
	}
	if info.MaxGatherTime != 259200 {
		t.Errorf("expected max_gather_time 259200, got %d", info.MaxGatherTime)
	}
	if info.PracticeExp != 4509400 {
		t.Errorf("expected practice_exp 4509400, got %d", info.PracticeExp)
	}
	if info.RouteLevel != 1 {
		t.Errorf("expected route_level 1, got %d", info.RouteLevel)
	}
}

func TestNewPractice_ParseCollectAllResponse(t *testing.T) {
	// Golden response from live server:
	// payload = 000000000000000044ced8
	rawHex := "000000000000000044ced8"
	payload, err := hex.DecodeString(rawHex)
	if err != nil {
		t.Fatalf("failed to decode hex: %v", err)
	}

	res, err := ParseNewPracticeCollectAllResponse(payload)
	if err != nil {
		t.Fatalf("failed to parse collect_all response: %v", err)
	}

	if res.Result != NewPracticeSuccess {
		t.Errorf("expected result %d, got %d", NewPracticeSuccess, res.Result)
	}
	if len(res.AirList) != 0 {
		t.Errorf("expected 0 air items, got %d", len(res.AirList))
	}
	if res.GainExp != 4509400 {
		t.Errorf("expected gain_exp 4509400, got %d", res.GainExp)
	}
}
