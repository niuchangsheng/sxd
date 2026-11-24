package protocol

import (
	"encoding/hex"
	"testing"
)

// liveSaSuperSportRaceInfoHex 为 2026-11-24 抓包 384/0 回包（解压后，294 字节），
// 含 5 名对手与剩余挑战次数 8。
const liveSaSuperSportRaceInfoHex = "000000000800000625000002da00000014000500000416050006e5bf83e58aa8000273300004313233320000000006b03d4000000640000000c9000002a30000012c0000000000000d10040006e5bf83e58aa8000273300009e5a4a9e98193e6b395000000000233d7460000060e000000c40000098e0000012c0000000000001451030006e5bf83e58aa8000273300006e8bf91e6999a00000000025767870000060e00000063000008d6000000e60000000000000795020006e5bf83e58aa8000273300009e8aeb8e4bba4e4b9890000000001c2fa850000060e0000006c000007630000012c000000000000117e010006e5bf83e58aa800027330000ce5a49ce5a49ce7ac99e6ad8c0000000002f3820d0000060e00000003000008750000012c00000000"

func TestParseSaSuperSportRaceInfo_LiveCapture(t *testing.T) {
	raw, err := hex.DecodeString(liveSaSuperSportRaceInfoHex)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	info, err := ParseSaSuperSportRaceInfo(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if info.RaceState != SaSuperSportRaceScore || info.LeftTimes != 8 ||
		info.Score != 1573 || info.Rank != 730 || info.Extra != 20 {
		t.Fatalf("头部字段不符: %+v", info)
	}
	if len(info.Opponents) != 5 {
		t.Fatalf("opponents = %d, want 5", len(info.Opponents))
	}
	got := info.Opponents[3]
	if got.PlayerID != 1941 || got.Slot != 2 || got.Nickname == "" ||
		got.WarPower != 29555333 || got.BattleID != 1891 || got.Level != 300 {
		t.Fatalf("opponent[3] = %+v", got)
	}
	// 抓包中第二次挑战请求即 [slot=2][battle_id=1891]。
	pkt := BuildSaSuperSportChallengePacket(got.Slot, got.BattleID)
	if pkt.ActionID != ActionIDSaSuperSportChallenge {
		t.Fatalf("action = 0x%08X", pkt.ActionID)
	}
	if len(pkt.Payload) != 9 || !pkt.HasPrevAct {
		t.Fatalf("challenge 载荷应为 [Byte][Int][PrevAct]: len=%d hasPrevAct=%v",
			len(pkt.Payload), pkt.HasPrevAct)
	}
	want := []byte{2, 0, 0, 0x07, 0x63}
	for i, v := range want {
		if pkt.Payload[i] != v {
			t.Fatalf("payload[%d] = 0x%02X, want 0x%02X", i, pkt.Payload[i], v)
		}
	}
}

func TestParseSaSuperSportRaceInfo_RoundTrip(t *testing.T) {
	info := &SaSuperSportRaceInfo{
		RaceState: SaSuperSportRaceRank,
		LeftTimes: 3,
		Score:     99,
		Rank:      12,
		Extra:     7,
		Opponents: []SaSuperSportOpponent{
			{PlayerID: 1, Slot: 5, Platform: "x", Server: "s0", Nickname: "甲",
				WarPower: 1000, A: 1, B: 2, BattleID: 11, Level: 300, E: 0},
			{PlayerID: 2, Slot: 4, Platform: "x", Server: "s0", Nickname: "乙",
				WarPower: 500, A: 3, B: 4, BattleID: 22, Level: 300, E: 9},
		},
	}
	parsed, err := ParseSaSuperSportRaceInfo(BuildSaSuperSportRaceInfoResultPacket(info).Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.RaceState != info.RaceState || parsed.LeftTimes != 3 ||
		parsed.Score != 99 || parsed.Rank != 12 || parsed.Extra != 7 {
		t.Fatalf("header = %+v", parsed)
	}
	if len(parsed.Opponents) != 2 || parsed.Opponents[1].BattleID != 22 ||
		parsed.Opponents[1].WarPower != 500 {
		t.Fatalf("opponents = %+v", parsed.Opponents)
	}
}

func TestParseSaSuperSportChallengeResult(t *testing.T) {
	for _, code := range []uint8{
		SaSuperSportResultSuccess, SaSuperSportResultError,
		SaSuperSportResultNoChallengeTimes, SaSuperSportResultNoShengHun,
		SaSuperSportResultPackFull, SaSuperSportResultTimesLimit,
	} {
		got, err := ParseSaSuperSportChallengeResult(BuildSaSuperSportChallengeResultPacket(code).Payload)
		if err != nil || got != code {
			t.Fatalf("code %d: got %d err %v", code, got, err)
		}
	}
	if _, err := ParseSaSuperSportChallengeResult(nil); err == nil {
		t.Fatal("空载荷应报错")
	}
}
