package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestTownPractice_BuildPackets(t *testing.T) {
	prevAct := uint32(0x00000002)

	t.Run("StartPractice", func(t *testing.T) {
		pkt := protocol.BuildPlayerStartPracticePacket(prevAct)
		if pkt.ActionID != protocol.ActionIDPlayerStartPractice {
			t.Fatalf("expected ActionID 0x%08X, got 0x%08X", protocol.ActionIDPlayerStartPractice, pkt.ActionID)
		}
		if !pkt.HasPrevAct {
			t.Errorf("expected HasPrevAct to be true")
		}
		if len(pkt.Payload) != 4 {
			t.Errorf("expected payload length 4, got %d", len(pkt.Payload))
		}
	})

	t.Run("GetPracticeData", func(t *testing.T) {
		pkt := protocol.BuildPlayerGetPracticeDataPacket(prevAct)
		if pkt.ActionID != protocol.ActionIDPlayerGetPracticeData {
			t.Fatalf("expected ActionID 0x%08X, got 0x%08X", protocol.ActionIDPlayerGetPracticeData, pkt.ActionID)
		}
		if !pkt.HasPrevAct {
			t.Errorf("expected HasPrevAct to be true")
		}
	})

	t.Run("SignFinishedPractice", func(t *testing.T) {
		pkt := protocol.BuildPlayerSignFinishedPracticePacket(prevAct)
		if pkt.ActionID != protocol.ActionIDPlayerSignFinishedPractice {
			t.Fatalf("expected ActionID 0x%08X, got 0x%08X", protocol.ActionIDPlayerSignFinishedPractice, pkt.ActionID)
		}
		if !pkt.HasPrevAct {
			t.Errorf("expected HasPrevAct to be true")
		}
	})

	t.Run("CancelPractice", func(t *testing.T) {
		pkt := protocol.BuildPlayerCancelPracticePacket(prevAct)
		if pkt.ActionID != protocol.ActionIDPlayerCancelPractice {
			t.Fatalf("expected ActionID 0x%08X, got 0x%08X", protocol.ActionIDPlayerCancelPractice, pkt.ActionID)
		}
		if !pkt.HasPrevAct {
			t.Errorf("expected HasPrevAct to be true")
		}
	})
}

func TestTownPractice_DataResponseRoundtrip(t *testing.T) {
	expected := protocol.TownPracticeData{
		Param1:               1,
		Param2:               2,
		PracticeTime:         3600,
		PracticeWar:          0,
		PracticeExp:          999888777,
		PracticeSpeedNum:     24,
		PracticeExpOne:       15000,
		SuperTownPracticeExp: 3000,
	}

	pkt, err := protocol.BuildTownPracticeDataResponsePacket(expected)
	if err != nil {
		t.Fatalf("BuildTownPracticeDataResponsePacket failed: %v", err)
	}

	parsed, err := protocol.ParseTownPracticeDataResponse(pkt.Payload)
	if err != nil {
		t.Fatalf("ParseTownPracticeDataResponse failed: %v", err)
	}

	if *parsed != expected {
		t.Fatalf("parsed TownPracticeData mismatch:\ngot:  %+v\nwant: %+v", *parsed, expected)
	}
}

func TestTownPractice_DataResponseCompressed(t *testing.T) {
	expected := protocol.TownPracticeData{
		Param1:               0,
		Param2:               0,
		PracticeTime:         7200,
		PracticeWar:          1,
		PracticeExp:          12345678,
		PracticeSpeedNum:     10,
		PracticeExpOne:       5000,
		SuperTownPracticeExp: 1000,
	}

	pkt, _ := protocol.BuildTownPracticeDataResponsePacket(expected)
	compressed, err := protocol.Compress(pkt.Payload)
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}

	parsed, err := protocol.ParseTownPracticeDataResponse(compressed)
	if err != nil {
		t.Fatalf("ParseTownPracticeDataResponse on compressed failed: %v", err)
	}

	if *parsed != expected {
		t.Fatalf("parsed compressed TownPracticeData mismatch:\ngot:  %+v\nwant: %+v", *parsed, expected)
	}
}

func TestTownPractice_DataResponseTooShort(t *testing.T) {
	shortData := []byte{0x00, 0x01, 0x02}
	_, err := protocol.ParseTownPracticeDataResponse(shortData)
	if err == nil {
		t.Fatalf("expected error on short payload, got nil")
	}
}

func TestTownPractice_NotifyRoundtrip(t *testing.T) {
	testCases := []protocol.TownPracticeNotify{
		{NotifyType: protocol.PlayerPracticeNotifyStart, Exp: 0, SuperExp: 0},
		{NotifyType: protocol.PlayerPracticeNotifyUpdate, Exp: 50000, SuperExp: 10000},
		{NotifyType: protocol.PlayerPracticeNotifyFinished, Exp: 120000, SuperExp: 24000},
		{NotifyType: protocol.PlayerPracticeNotifyCancel, Exp: 0, SuperExp: 0},
	}

	for _, tc := range testCases {
		pkt, err := protocol.BuildTownPracticeNotifyPacket(tc)
		if err != nil {
			t.Fatalf("BuildTownPracticeNotifyPacket failed: %v", err)
		}

		parsed, err := protocol.ParseTownPracticeNotify(pkt.Payload)
		if err != nil {
			t.Fatalf("ParseTownPracticeNotify failed: %v", err)
		}

		if *parsed != tc {
			t.Fatalf("parsed TownPracticeNotify mismatch:\ngot:  %+v\nwant: %+v", *parsed, tc)
		}
	}
}

func TestTownPractice_NotifyCompressed(t *testing.T) {
	expected := protocol.TownPracticeNotify{
		NotifyType: protocol.PlayerPracticeNotifyUpdate,
		Exp:        88888,
		SuperExp:   9999,
	}

	w := protocol.NewWriter()
	w.WriteUint8(expected.NotifyType)
	w.WriteInt32(expected.Exp)
	w.WriteInt32(expected.SuperExp)

	compressed, _ := protocol.Compress(w.Bytes())
	parsed, err := protocol.ParseTownPracticeNotify(compressed)
	if err != nil {
		t.Fatalf("ParseTownPracticeNotify on compressed failed: %v", err)
	}

	if *parsed != expected {
		t.Fatalf("parsed compressed notify mismatch: got %+v, want %+v", *parsed, expected)
	}
}

func TestTownPractice_NotifyEmpty(t *testing.T) {
	_, err := protocol.ParseTownPracticeNotify([]byte{})
	if err == nil {
		t.Fatalf("expected error on empty notify payload")
	}
}
