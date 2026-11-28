package protocol

import (
	"encoding/hex"
	"errors"
	"testing"
)

func TestParseMemoryZoneMainPanel_GoldenPcapng(t *testing.T) {
	// 采自 huiyizhijing.pcapng Frame 16 (526/1, 55B)
	raw, err := hex.DecodeString("000003000000010000000600000006000000010000000200000005000000050000000100000003000000010000000300000001000000c8")
	if err != nil {
		t.Fatalf("decode hex: %v", err)
	}
	got, err := ParseMemoryZoneMainPanel(raw)
	if err != nil {
		t.Fatalf("ParseMemoryZoneMainPanel: %v", err)
	}
	if got.Result != MemoryZoneResultSuccess {
		t.Fatalf("Result = %d, want 0", got.Result)
	}
	if got.AwardDaoYuan != 200 {
		t.Fatalf("AwardDaoYuan = %d, want 200", got.AwardDaoYuan)
	}
	if len(got.Categories) != 3 {
		t.Fatalf("len(Categories) = %d, want 3", len(got.Categories))
	}
	want := []MemoryZoneCategory{
		{Type: 1, HaveNum: 6, MaxNum: 6, GetAward: 1},
		{Type: 2, HaveNum: 5, MaxNum: 5, GetAward: 1},
		{Type: 3, HaveNum: 1, MaxNum: 3, GetAward: 1},
	}
	for i := range want {
		if got.Categories[i] != want[i] {
			t.Fatalf("Categories[%d] = %+v, want %+v", i, got.Categories[i], want[i])
		}
	}
}

func TestParseMemoryZoneBackPanel_GoldenPcapng(t *testing.T) {
	// 采自 huiyizhijing.pcapng Frame 1747 (526/2 type=3, 55B)
	raw, err := hex.DecodeString("000003000001e6000001f4000001f4000001e70000000b000001f4000001e800000000000001f400000000000000010000000900000000")
	if err != nil {
		t.Fatalf("decode hex: %v", err)
	}
	got, err := ParseMemoryZoneBackPanel(raw)
	if err != nil {
		t.Fatalf("ParseMemoryZoneBackPanel: %v", err)
	}
	if got.Result != MemoryZoneResultSuccess {
		t.Fatalf("Result = %d, want 0", got.Result)
	}
	if got.TodayBackTimes != 0 || got.DailyFreeTimes != 1 || got.DailyIngotTimes != 9 || got.CostIngot != 0 {
		t.Fatalf("counters = %+v, want today=0 free=1 ingot=9 cost=0", got)
	}
	if !got.HasFreeBacktrack() {
		t.Fatalf("HasFreeBacktrack() = false, want true")
	}
	wantPartners := []MemoryZonePartner{
		{RoleID: 486, HaveScrap: 500, NeedScrap: 500},
		{RoleID: 487, HaveScrap: 11, NeedScrap: 500},
		{RoleID: 488, HaveScrap: 0, NeedScrap: 500},
	}
	if len(got.Partners) != len(wantPartners) {
		t.Fatalf("len(Partners) = %d, want %d", len(got.Partners), len(wantPartners))
	}
	for i := range wantPartners {
		if got.Partners[i] != wantPartners[i] {
			t.Fatalf("Partners[%d] = %+v, want %+v", i, got.Partners[i], wantPartners[i])
		}
	}
	if !got.Partners[0].Owned() || got.Partners[1].Owned() || got.Partners[2].Owned() {
		t.Fatalf("Owned flags = [%v, %v, %v], want [true, false, false]",
			got.Partners[0].Owned(), got.Partners[1].Owned(), got.Partners[2].Owned())
	}
}

func TestParseMemoryZoneBacktrack_GoldenPcapng(t *testing.T) {
	// 采自 huiyizhijing.pcapng Frame 2552 (526/4 role_id=487, 33B)
	raw, err := hex.DecodeString("00000100002051000000010001000001e70000000c000001f40000000100000014")
	if err != nil {
		t.Fatalf("decode hex: %v", err)
	}
	got, err := ParseMemoryZoneBacktrack(raw)
	if err != nil {
		t.Fatalf("ParseMemoryZoneBacktrack: %v", err)
	}
	if got.Result != MemoryZoneResultSuccess {
		t.Fatalf("Result = %d, want 0", got.Result)
	}
	if len(got.Awards) != 1 || got.Awards[0] != (MemoryZoneAward{ItemID: 8273, ItemNum: 1}) {
		t.Fatalf("Awards = %+v, want [{8273 1}]", got.Awards)
	}
	if len(got.Partners) != 1 || got.Partners[0] != (MemoryZonePartner{RoleID: 487, HaveScrap: 12, NeedScrap: 500}) {
		t.Fatalf("Partners = %+v, want [{487 12 500}]", got.Partners)
	}
	if got.TodayBackTimes != 1 || got.CostIngot != 20 {
		t.Fatalf("today=%d cost=%d, want today=1 cost=20", got.TodayBackTimes, got.CostIngot)
	}
}

func TestParseMemoryZone_FailureAndTruncated(t *testing.T) {
	// 失败短回包（仅 1 字节 result_code）不应报错，也不应尝试读取后续字段。
	if main, err := ParseMemoryZoneMainPanel([]byte{MemoryZoneResultFailed}); err != nil || main.Result != MemoryZoneResultFailed {
		t.Fatalf("main failed short payload: got (%+v, %v)", main, err)
	}
	if back, err := ParseMemoryZoneBackPanel([]byte{MemoryZoneResultMaxTimes}); err != nil || back.Result != MemoryZoneResultMaxTimes {
		t.Fatalf("back failed short payload: got (%+v, %v)", back, err)
	}
	if bt, err := ParseMemoryZoneBacktrack([]byte{MemoryZoneResultNoIngot}); err != nil || bt.Result != MemoryZoneResultNoIngot {
		t.Fatalf("backtrack failed short payload: got (%+v, %v)", bt, err)
	}

	// 空载荷返回 ErrPayloadTruncated。
	if _, err := ParseMemoryZoneMainPanel(nil); !errors.Is(err, ErrPayloadTruncated) {
		t.Fatalf("empty main payload err = %v, want ErrPayloadTruncated", err)
	}
	if _, err := ParseMemoryZoneBackPanel(nil); !errors.Is(err, ErrPayloadTruncated) {
		t.Fatalf("empty back payload err = %v, want ErrPayloadTruncated", err)
	}
	if _, err := ParseMemoryZoneBacktrack(nil); !errors.Is(err, ErrPayloadTruncated) {
		t.Fatalf("empty backtrack payload err = %v, want ErrPayloadTruncated", err)
	}
}

func TestBuildMemoryZonePackets_PrevActLayout(t *testing.T) {
	p1 := BuildMemoryZoneMainPanelPacket()
	if p1.ActionID != ActionIDMemoryZoneMainPanel || len(p1.Payload) != 4 || p1.PrevActMode != PrevActAutoBusiness {
		t.Fatalf("main_panel pkt = %+v", p1)
	}
	p2 := BuildMemoryZoneBackPanelPacket(3)
	if p2.ActionID != ActionIDMemoryZoneBackPanel || len(p2.Payload) != 8 || p2.PrevActMode != PrevActAutoBusiness {
		t.Fatalf("back_panel pkt = %+v", p2)
	}
	p4 := BuildMemoryZoneBacktrackPacket(487)
	if p4.ActionID != ActionIDMemoryZoneBacktrack || len(p4.Payload) != 8 || p4.PrevActMode != PrevActAutoBusiness {
		t.Fatalf("backtrack pkt = %+v", p4)
	}
}
