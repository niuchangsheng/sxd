package protocol_test

import (
	"encoding/hex"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

const wuxingPanelHex = "000000001800050404000024a80000001f000016be0000000100000000000003260304000024a900000038000016be0000000100000000000002500204000024aa00000027000016be00000001000000000000025e0504000024ab00000019000016be0000000100000000000002c50104000024ac0000000e000016be00000001000000000000027b006400000101"

func TestWuxing_ParsePanel(t *testing.T) {
	raw, err := hex.DecodeString(wuxingPanelHex)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	entries, err := protocol.ParseWuxingPanel(raw)
	if err != nil {
		t.Fatalf("ParseWuxingPanel: %v", err)
	}
	if len(entries) != 5 {
		t.Fatalf("期望 5 个五行条目, 实际 %d", len(entries))
	}
	wantElements := []uint8{4, 3, 2, 5, 1}
	wantIDs := []int32{0x24a8, 0x24a9, 0x24aa, 0x24ab, 0x24ac}
	for i, e := range entries {
		if e.Element != wantElements[i] || e.State != 4 || e.ID != wantIDs[i] {
			t.Fatalf("条目 %d = %+v, want element=%d state=4 id=0x%X",
				i, e, wantElements[i], wantIDs[i])
		}
	}
}

func TestWuxing_Builders(t *testing.T) {
	p := protocol.BuildWuxingDeployPacket(0x00037A67, 9, protocol.ActionIDWuxingLineup)
	if p.ActionID != protocol.ActionIDWuxingDeploy || !p.HasPrevAct || len(p.Payload) != 9 {
		t.Fatalf("453/2 封包不符: %+v", p)
	}
	if got := hex.EncodeToString(p.Payload[:5]); got != "00037a6709" {
		t.Fatalf("453/2 载荷不符: %s", got)
	}
	f := protocol.BuildWuxingFightPacket(protocol.WuxingFightModeAll, protocol.ActionIDWuxingPanel)
	if f.ActionID != protocol.ActionIDWuxingFight || !f.HasPrevAct || len(f.Payload) != 5 || f.Payload[0] != 1 {
		t.Fatalf("453/3 封包不符: %+v", f)
	}
}

func TestWuxing_ParseLineup(t *testing.T) {
	// [Int role_id][Short len][UTF-8 name] 连续排列。
	build := func(roleID int32, name string) []byte {
		out := []byte{byte(roleID >> 24), byte(roleID >> 16), byte(roleID >> 8), byte(roleID), 8}
		out = append(out, make([]byte, 8)...) // 属性字段填充
		out = append(out, byte(len(name)>>8), byte(len(name)))
		return append(out, []byte(name)...)
	}
	raw := append(build(1080352, "遁一"), build(1006659, "李逍遥")...)
	raw = append(raw, build(768553, "佛玲珑")...)
	roles, err := protocol.ParseWuxingLineup(raw)
	if err != nil {
		t.Fatalf("ParseWuxingLineup: %v", err)
	}
	if len(roles) != 3 {
		t.Fatalf("期望 3 个伙伴, 实际 %d: %+v", len(roles), roles)
	}
	if roles[1].RoleID != 1006659 || roles[1].Name != "李逍遥" {
		t.Fatalf("李逍遥映射不符: %+v", roles[1])
	}
	if roles[2].RoleID != 768553 || roles[2].Name != "佛玲珑" {
		t.Fatalf("玲珑映射不符: %+v", roles[2])
	}
}
