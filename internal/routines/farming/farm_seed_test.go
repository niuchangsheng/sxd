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

func TestFarmPurchaseCandidates_CheapestCoinSeedFirst(t *testing.T) {
	seeds := []protocol.FarmSeedItem{
		{SeedID: 171, BuyNeedCoin: 1_000_000},
		{SeedID: 174, BuyNeedCoin: 500_000},
		{SeedID: 175, BuyNeedCoin: 200_000},
		{SeedID: 176, BuyNeedIngot: 30},
		{SeedID: 169, BuyNeedIngot: 20},
	}
	got := farmPurchaseCandidates(seeds)
	// 主力种子 176/169/171/174 优先（由调用方按元宝价跳过），
	// 其余未知种子按铜钱价格升序（175 在 174 之后）。
	want := []int32{176, 169, 171, 174, 175}
	rank := map[int32]int{}
	for i, id := range got {
		rank[id] = i
	}
	if rank[176] != 0 || rank[169] != 1 || rank[171] != 2 || rank[174] != 3 || rank[175] != 4 {
		t.Fatalf("candidates order = %v, want %v", got, want)
	}
}

func TestFarmCanAffordSeed_KeepsEqualReserve(t *testing.T) {
	cases := []struct {
		coins int64
		price int32
		want  bool
	}{
		{coins: 0, price: 1_000_000, want: true},          // 余额未知不拦截
		{coins: 1_900_000, price: 1_500_000, want: false}, // s0 实测：1.5M 占余额 79%
		{coins: 3_000_000, price: 1_500_000, want: true},
		{coins: 1_000_000, price: 500_000, want: true},
		{coins: 900_000, price: 500_000, want: false},
	}
	for _, c := range cases {
		if got := farmCanAffordSeed(c.coins, c.price); got != c.want {
			t.Errorf("farmCanAffordSeed(%d, %d) = %v, want %v", c.coins, c.price, got, c.want)
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
