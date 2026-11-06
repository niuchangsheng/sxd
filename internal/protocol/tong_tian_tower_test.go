package protocol_test

import (
	"encoding/hex"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

// 线上 s0 实测 panel_info 回包 (65 字节)。
const tongTianTowerPanelLiveHex = "0000000100000037000000aa000003e80000000b00000000000003e800000000000000000000000100000036000000180000000000000000000000000000000007"

func TestParseTongTianTowerPanelResult_Live(t *testing.T) {
	raw, err := hex.DecodeString(tongTianTowerPanelLiveHex)
	if err != nil {
		t.Fatalf("hex decode: %v", err)
	}
	panel, err := protocol.ParseTongTianTowerPanelResult(raw)
	if err != nil {
		t.Fatalf("ParseTongTianTowerPanelResult: %v", err)
	}
	if panel.Heaven != 1 || panel.Floor != 55 {
		t.Errorf("unexpected heaven/floor: %+v", panel)
	}
	if panel.ChallengeTimes != 11 {
		t.Errorf("expected 11 challenge times, got %d", panel.ChallengeTimes)
	}
	if panel.IsBreakUp != 0 || panel.IsWeekendDiscount != 0 {
		t.Errorf("unexpected flags: %+v", panel)
	}
	if panel.CurHeaven != 1 || panel.CurFloor != 54 || panel.MaterialDopNum != 24 {
		t.Errorf("unexpected cur/heaven material fields: %+v", panel)
	}
	if panel.SkyPalaceLevel != 7 {
		t.Errorf("expected sky palace level 7, got %d", panel.SkyPalaceLevel)
	}
}

func TestTongTianTowerMonsterTeamID(t *testing.T) {
	if got := protocol.TongTianTowerMonsterTeamID(1, 1); got != 10637 {
		t.Errorf("heaven 1 floor 1: got %d", got)
	}
	if got := protocol.TongTianTowerMonsterTeamID(1, 55); got != 10691 {
		t.Errorf("heaven 1 floor 55: got %d", got)
	}
	if got := protocol.TongTianTowerMonsterTeamID(9, 100); got != 12187 {
		t.Errorf("heaven 9 floor 100: got %d", got)
	}
	if got := protocol.TongTianTowerMonsterTeamID(0, 1); got != 0 {
		t.Errorf("invalid heaven should return 0, got %d", got)
	}
	if got := protocol.TongTianTowerMonsterTeamID(10, 1); got != 0 {
		t.Errorf("out of range heaven should return 0, got %d", got)
	}
}

func TestTongTianTowerChallengePacketLayout(t *testing.T) {
	pkt := protocol.BuildTongTianTowerChallengePacket([]protocol.TongTianTowerTeam{
		{TeamType: protocol.TongTianTowerMainTeam, MonsterTeamID: 10691},
	})
	if pkt.ActionID != protocol.ActionIDTongTianTowerChallenge {
		t.Fatalf("unexpected action: 0x%08X", pkt.ActionID)
	}
	// Uint16 count + Uint8 team_type + Int32 monster_team_id
	want := []byte{0x00, 0x01, 0x00, 0x00, 0x00, 0x29, 0xC3}
	if string(pkt.Payload) != string(want) {
		t.Fatalf("payload mismatch:\n got %x\nwant %x", pkt.Payload, want)
	}
}

func TestParseTongTianTowerChallengeResult_Success(t *testing.T) {
	payload := protocol.BuildTongTianTowerChallengeResultPacket(protocol.TongTianTowerChallengeResult{
		Result:        protocol.TongTianTowerResultSuccess,
		AwardItemIDs:  []int32{933, 937},
		AwardItemNums: []int32{1, 2},
		DeployType:    3,
		WinPlayerID:   106732,
	}).Payload
	got, err := protocol.ParseTongTianTowerChallengeResult(payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Result != protocol.TongTianTowerResultSuccess || !got.IsWin(106732) {
		t.Fatalf("unexpected result: %+v", got)
	}
	if len(got.AwardItemIDs) != 2 || got.AwardItemIDs[0] != 933 || got.AwardItemNums[1] != 2 {
		t.Fatalf("unexpected awards: %+v", got)
	}
	if got.DeployType != 3 {
		t.Fatalf("unexpected deploy type: %d", got.DeployType)
	}
}

func TestParseTongTianTowerChallengeResult_FailureCodes(t *testing.T) {
	for _, code := range []uint8{
		protocol.TongTianTowerResultBagFull,
		protocol.TongTianTowerResultNotEnoughPower,
		protocol.TongTianTowerResultChallengeTimesLimit,
		protocol.TongTianTowerResultNeedBreakUp,
		protocol.TongTianTowerResultMonsterTeamError,
	} {
		payload := protocol.BuildTongTianTowerChallengeResultPacket(
			protocol.TongTianTowerChallengeResult{Result: code}).Payload
		got, err := protocol.ParseTongTianTowerChallengeResult(payload)
		if err != nil {
			t.Fatalf("code %d parse: %v", code, err)
		}
		if got.Result != code || got.Success() {
			t.Fatalf("code %d: unexpected result %+v", code, got)
		}
	}
}

func TestParseTongTianTowerPracticeResult(t *testing.T) {
	ok := protocol.BuildTongTianTowerPracticeResultPacket(protocol.TongTianTowerPracticeResult{
		Result:       protocol.TongTianTowerResultSuccess,
		PracticeTime: 30,
		Power:        4017,
	}).Payload
	res, err := protocol.ParseTongTianTowerPracticeResult(ok)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Result != protocol.TongTianTowerResultSuccess || res.PracticeTime != 30 || res.Power != 4017 {
		t.Fatalf("unexpected practice result: %+v", res)
	}

	fail := protocol.BuildTongTianTowerPracticeResultPacket(protocol.TongTianTowerPracticeResult{
		Result: protocol.TongTianTowerResultNotEnoughPower,
	}).Payload
	res, err = protocol.ParseTongTianTowerPracticeResult(fail)
	if err != nil {
		t.Fatalf("parse fail: %v", err)
	}
	if res.Result != protocol.TongTianTowerResultNotEnoughPower {
		t.Fatalf("unexpected fail result: %+v", res)
	}
}