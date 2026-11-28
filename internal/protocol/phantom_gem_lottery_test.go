package protocol

import (
	"encoding/hex"
	"testing"
)

func TestBuildPhantomGemLotteryPackets(t *testing.T) {
	panelPkt := BuildPhantomGemLotteryPanelPacket()
	if panelPkt.ActionID != ActionIDPhantomGemLotteryPanel {
		t.Fatalf("panel ActionID = 0x%08X, want 0x%08X", panelPkt.ActionID, ActionIDPhantomGemLotteryPanel)
	}
	if panelPkt.PrevActMode != PrevActAutoBusiness {
		t.Fatalf("panel PrevActMode = %v, want PrevActAutoBusiness", panelPkt.PrevActMode)
	}
	if len(panelPkt.Payload) != 4 {
		t.Fatalf("panel payload len = %d, want 4 (prevAct placeholder)", len(panelPkt.Payload))
	}

	findPkt := BuildPhantomGemLotteryFindPacket(PhantomGemMountainLingYu, PhantomGemFindTypeNormal)
	if findPkt.ActionID != ActionIDPhantomGemLotteryFind {
		t.Fatalf("find ActionID = 0x%08X, want 0x%08X", findPkt.ActionID, ActionIDPhantomGemLotteryFind)
	}
	if findPkt.PrevActMode != PrevActAutoBusiness {
		t.Fatalf("find PrevActMode = %v, want PrevActAutoBusiness", findPkt.PrevActMode)
	}
	if len(findPkt.Payload) != 6 {
		t.Fatalf("find payload len = %d, want 6 (mountain_id + find_type + 4B prevAct)", len(findPkt.Payload))
	}
	if findPkt.Payload[0] != byte(PhantomGemMountainLingYu) || findPkt.Payload[1] != byte(PhantomGemFindTypeNormal) {
		t.Fatalf("find payload prefix = %x, want 0400", findPkt.Payload[:2])
	}
}

func TestParsePhantomGemLotteryFindResult_GoldenPcap(t *testing.T) {
	// 采自 duobaoshan.pcapng pkt 13134: S->C 694/1 (0x02B60001)
	raw, err := hex.DecodeString("000001000029ce0000000101000000000000")
	if err != nil {
		t.Fatalf("decode hex: %v", err)
	}
	res, err := ParsePhantomGemLotteryFindResult(raw)
	if err != nil {
		t.Fatalf("ParsePhantomGemLotteryFindResult: %v", err)
	}
	if res.Result != PhantomGemLotteryResultSuccess {
		t.Fatalf("Result = %d, want %d", res.Result, PhantomGemLotteryResultSuccess)
	}
	if len(res.Awards) != 1 {
		t.Fatalf("Awards len = %d, want 1", len(res.Awards))
	}
	wantAward := PhantomGemLotteryAward{ItemID: 10702, ItemNum: 1, IsHigh: 1}
	if res.Awards[0] != wantAward {
		t.Fatalf("Awards[0] = %+v, want %+v", res.Awards[0], wantAward)
	}
	if len(res.LuckyItems) != 0 {
		t.Fatalf("LuckyItems len = %d, want 0", len(res.LuckyItems))
	}
	if res.PlayerRestTime != 0 {
		t.Fatalf("PlayerRestTime = %d, want 0", res.PlayerRestTime)
	}
}

func TestParsePhantomGemLotteryFindResult_FailureTruncated(t *testing.T) {
	for _, code := range []uint8{
		PhantomGemLotteryResultFailed,
		PhantomGemLotteryResultNotEnoughTimes,
		PhantomGemLotteryResultNotEnoughIngot,
		PhantomGemLotteryResultNotEnoughDaoyuan,
		PhantomGemLotteryResultEudemonsFunNotOpened,
		PhantomGemLotteryResultItemPackFull,
	} {
		pkt := BuildPhantomGemLotteryFindResultPacket(PhantomGemLotteryFindResult{Result: code})
		res, err := ParsePhantomGemLotteryFindResult(pkt.Payload)
		if err != nil {
			t.Fatalf("code=%d unexpected err: %v", code, err)
		}
		if res.Result != code {
			t.Fatalf("Result = %d, want %d", res.Result, code)
		}
	}

	if _, err := ParsePhantomGemLotteryFindResult(nil); err == nil {
		t.Fatal("expected error on empty payload")
	}
}

func TestParsePhantomGemLotteryPanelResult_RoundTrip(t *testing.T) {
	want := PhantomGemLotteryPanel{
		Mountains: []PhantomGemLotteryMountain{
			{
				MountainID: PhantomGemMountainLingYu,
				NormalItems: []PhantomGemLotteryPoolItem{
					{ItemID: 10702, ItemNum: 1, Prob: 145, IsHigh: 1},
				},
				RemainNormalTimes: 1,
				HighItems: []PhantomGemLotteryPoolItem{
					{ItemID: 10703, ItemNum: 1, Prob: 110, IsHigh: 1},
				},
				RemainHighTimes:        102,
				RemainExtraNormalTimes: 3,
			},
			{
				MountainID:        PhantomGemMountainCangMing,
				RemainNormalTimes: 1,
				RemainHighTimes:   102,
			},
		},
		RequiteValue:                 4010,
		TicketNum:                    0,
		IsLuckyDay:                   0,
		PlayerRestTime:               0,
		DiscountNum:                  0,
		SectBuffExpireTime:           1752947518,
		IsCloudTreasuryPrivilegeFlag: 0,
	}

	pkt := BuildPhantomGemLotteryPanelResultPacket(want)
	got, err := ParsePhantomGemLotteryPanelResult(pkt.Payload)
	if err != nil {
		t.Fatalf("ParsePhantomGemLotteryPanelResult: %v", err)
	}
	if len(got.Mountains) != 2 {
		t.Fatalf("Mountains len = %d, want 2", len(got.Mountains))
	}
	m, ok := got.MountainByID(PhantomGemMountainLingYu)
	if !ok {
		t.Fatal("MountainByID(LingYu) not found")
	}
	if m.TotalRemainNormalTimes() != 4 {
		t.Fatalf("TotalRemainNormalTimes = %d, want 4", m.TotalRemainNormalTimes())
	}
	if got.RequiteValue != 4010 || got.SectBuffExpireTime != 1752947518 {
		t.Fatalf("unexpected tail fields: %+v", got)
	}
}

func TestParsePhantomGemMountain(t *testing.T) {
	cases := []struct {
		in   string
		want int8
		ok   bool
	}{
		{"", PhantomGemMountainLingYu, true},
		{"灵玉", PhantomGemMountainLingYu, true},
		{"灵玉宝山", PhantomGemMountainLingYu, true},
		{"lingyu", PhantomGemMountainLingYu, true},
		{"4", PhantomGemMountainLingYu, true},
		{"沧溟", PhantomGemMountainCangMing, true},
		{"沧溟宝山", PhantomGemMountainCangMing, true},
		{"cangming", PhantomGemMountainCangMing, true},
		{"1", PhantomGemMountainCangMing, true},
		{"玄晖", PhantomGemMountainXuanHui, true},
		{"xuanhui", PhantomGemMountainXuanHui, true},
		{"2", PhantomGemMountainXuanHui, true},
		{"翠青", PhantomGemMountainCuiQing, true},
		{"cuiqing", PhantomGemMountainCuiQing, true},
		{"3", PhantomGemMountainCuiQing, true},
		{"invalid", 0, false},
		{"5", 0, false},
	}
	for _, tc := range cases {
		got, ok := ParsePhantomGemMountain(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("ParsePhantomGemMountain(%q) = (%d, %v), want (%d, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}
