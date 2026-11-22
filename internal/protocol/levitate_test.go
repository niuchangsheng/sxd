package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

// TestParseLevitateInfo 验证 357/0 面板字段（阶段、材料、经验、神炼石）解析。
func TestParseLevitateInfo(t *testing.T) {
	want := &protocol.LevitateInfo{
		Step:             3,
		AvailableLingZhi: 7,
		MaxLingZhi:       10,
		Materials: []protocol.LevitateMaterial{
			{ItemID: protocol.LevitateMaterialExpItemID, Available: 12345, Need: 50000},
			{ItemID: protocol.LevitateMaterialCoinItemID, Available: 800000, Need: 200000},
			{ItemID: 9001, Available: 2, Need: 4},
		},
		LevelLimitExp:    999,
		PerfusionableExp: 45678,
		ExpItems: []protocol.LevitateExpItem{
			{ItemID: 5644, Num: 5},
			{ItemID: 5645, Num: 1},
		},
		ShenLianShiNum: 6,
	}
	pkt := protocol.BuildLevitateInfoResultPacket(want)

	got, err := protocol.ParseLevitateInfo(pkt.Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Step != want.Step || got.AvailableLingZhi != want.AvailableLingZhi ||
		got.MaxLingZhi != want.MaxLingZhi || got.LevelLimitExp != want.LevelLimitExp ||
		got.PerfusionableExp != want.PerfusionableExp || got.ShenLianShiNum != want.ShenLianShiNum {
		t.Fatalf("标量字段不一致: got %+v want %+v", got, want)
	}
	if len(got.Materials) != len(want.Materials) {
		t.Fatalf("材料数量 = %d, want %d", len(got.Materials), len(want.Materials))
	}
	for i, m := range want.Materials {
		if got.Materials[i] != m {
			t.Fatalf("材料[%d] = %+v, want %+v", i, got.Materials[i], m)
		}
	}
	if len(got.ExpItems) != len(want.ExpItems) {
		t.Fatalf("经验丹数量 = %d, want %d", len(got.ExpItems), len(want.ExpItems))
	}
	for i, it := range want.ExpItems {
		if got.ExpItems[i] != it {
			t.Fatalf("经验丹[%d] = %+v, want %+v", i, got.ExpItems[i], it)
		}
	}
}

// TestParseLevitateInfo_Truncated 验证残缺回包返回错误而不是越界。
func TestParseLevitateInfo_Truncated(t *testing.T) {
	if _, err := protocol.ParseLevitateInfo([]byte{0, 1}); err == nil {
		t.Fatal("残缺面板应返回解析错误")
	}
}

// TestParseLevitateActionResult 验证 357/2 飞升回包解析。
func TestParseLevitateActionResult(t *testing.T) {
	pkt := protocol.BuildLevitateActionResultPacket(protocol.LevitateSuccess, 7)
	got, err := protocol.ParseLevitateActionResult(pkt.Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Result != protocol.LevitateSuccess || got.Value != 7 {
		t.Fatalf("got %+v", got)
	}
}

// TestBuildLevitatePacket 验证 357/2 请求为 [UByte type, Int is_use_shen_lian_zhu, 4B PrevAct]。
func TestBuildLevitatePacket(t *testing.T) {
	pkt := protocol.BuildLevitatePacket(protocol.LevitateTypeMainRole, 1)
	if pkt.ActionID != protocol.ActionIDLevitate {
		t.Fatalf("action = 0x%08X", pkt.ActionID)
	}
	if !pkt.PrevActFromLastSent {
		t.Fatal("应使用最后业务动作作 PrevAct")
	}
	if len(pkt.Payload) != 9 {
		t.Fatalf("payload len = %d, want 9", len(pkt.Payload))
	}
	if pkt.Payload[0] != protocol.LevitateTypeMainRole || pkt.Payload[4] != 1 {
		t.Fatalf("payload = %X", pkt.Payload)
	}
}
