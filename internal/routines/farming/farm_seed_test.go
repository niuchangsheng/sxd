package farming

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestFarmPurchaseCandidates_PreferredThenOthers(t *testing.T) {
	seeds := []protocol.FarmSeedItem{
		{SeedID: 175},
		{SeedID: 169},
		{SeedID: 176},
		{SeedID: 171},
		{SeedID: 180},
		{SeedID: 169}, // 重复项只保留一次
	}
	got := farmPurchaseCandidates(seeds)
	want := []int32{176, 169, 171, 175, 180}
	if len(got) != len(want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidates = %v, want %v", got, want)
		}
	}
}

func TestFarmPlantCandidates_OnlyOwnedPreferredFirst(t *testing.T) {
	owned := map[int32]int32{
		176: 0,
		169: 2,
		171: 0,
		175: 1,
		180: 3,
	}
	got := farmPlantCandidates(owned)
	want := []int32{169, 175, 180}
	if len(got) != len(want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidates = %v, want %v", got, want)
		}
	}
}

func TestFarmPlantCandidates_Empty(t *testing.T) {
	if got := farmPlantCandidates(map[int32]int32{176: 0, 169: 0}); len(got) != 0 {
		t.Fatalf("expected no candidates, got %v", got)
	}
}
