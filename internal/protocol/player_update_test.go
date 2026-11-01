// Package protocol_test 提供角色属性变更推送协议 (Module 0, Action 3) 的单元测试。
//
// @author Ateng
// @since 2026-11-01
package protocol_test

import (
	"encoding/hex"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

// TestParsePlayerUpdateData_GoldenPacket 锁定官方微端 0x00000003 黄金用例的批量布局:
// [Uint16 条数][Uint8 属性ID][Int64 数值]。
func TestParsePlayerUpdateData_GoldenPacket(t *testing.T) {
	payload, err := hex.DecodeString("00011b0000000000000000")
	if err != nil {
		t.Fatalf("解码黄金用例失败: %v", err)
	}

	updates, modern := protocol.ParsePlayerUpdateData(payload)
	if !modern {
		t.Fatalf("黄金用例应按 2026 批量布局解析")
	}
	if len(updates) != 1 || updates[0].Prop != 0x1b || updates[0].Value != 0 {
		t.Errorf("解析结果不匹配: %+v", updates)
	}
}

// TestParsePlayerUpdateData_LiveQuestReward 锁定真实推图会话中的经验/铜钱变更包。
// 完成任务 251 (奖励 240000 经验 / 80000 铜钱) 后服务端下发:
// 经验 63456+240000=303456 (prop 25), 铜钱 50355193+80000=50435193 (prop 20)。
func TestParsePlayerUpdateData_LiveQuestReward(t *testing.T) {
	payload, err := hex.DecodeString("000219000000000004a160140000000003019479")
	if err != nil {
		t.Fatalf("解码实测样本失败: %v", err)
	}

	updates, modern := protocol.ParsePlayerUpdateData(payload)
	if !modern {
		t.Fatalf("实测样本应按 2026 批量布局解析")
	}
	want := []protocol.PlayerPropUpdate{
		{Prop: protocol.PlayerPropExperience2026, Value: 303456},
		{Prop: protocol.PlayerPropCoins2026, Value: 50435193},
	}
	if len(updates) != len(want) {
		t.Fatalf("变更条数不匹配: 期望 %d, 实际 %d (%+v)", len(want), len(updates), updates)
	}
	for i := range want {
		if updates[i] != want[i] {
			t.Errorf("第 %d 条变更不匹配: 期望 %+v, 实际 %+v", i, want[i], updates[i])
		}
	}
}

// TestParsePlayerUpdateData_LiveMultiRecord 覆盖 29 字节三记录样本, 确保 9 字节步长正确。
func TestParsePlayerUpdateData_LiveMultiRecord(t *testing.T) {
	payload, err := hex.DecodeString("00032300000000000000011900000000000a1149140000000003032fbc")
	if err != nil {
		t.Fatalf("解码实测样本失败: %v", err)
	}

	updates, modern := protocol.ParsePlayerUpdateData(payload)
	if !modern {
		t.Fatalf("实测样本应按 2026 批量布局解析")
	}
	// 0x23 为尚未定值的同步字段, 此处仅验证按 9 字节步长对齐后其余字段不串位。
	want := []protocol.PlayerPropUpdate{
		{Prop: 0x23, Value: 1},
		{Prop: protocol.PlayerPropExperience2026, Value: 659785},
		{Prop: protocol.PlayerPropCoins2026, Value: 50540476},
	}
	if len(updates) != len(want) {
		t.Fatalf("变更条数不匹配: 期望 %d, 实际 %d (%+v)", len(want), len(updates), updates)
	}
	for i := range want {
		if updates[i] != want[i] {
			t.Errorf("第 %d 条变更不匹配: 期望 %+v, 实际 %+v", i, want[i], updates[i])
		}
	}
}

// TestParsePlayerUpdateData_LegacyLayout 验证旧版 [Uint8 属性ID][Int32 数值] 重复布局兼容分支。
func TestParsePlayerUpdateData_LegacyLayout(t *testing.T) {
	payload, err := hex.DecodeString("0f000000c9")
	if err != nil {
		t.Fatalf("解码旧版样本失败: %v", err)
	}

	updates, modern := protocol.ParsePlayerUpdateData(payload)
	if modern {
		t.Fatalf("5 字节载荷不应被识别为 2026 批量布局")
	}
	if len(updates) != 1 || updates[0].Prop != protocol.PlayerPropPower || updates[0].Value != 201 {
		t.Errorf("旧版布局解析结果不匹配: %+v", updates)
	}
}

// TestParsePlayerUpdateData_ShortPayload 验证不足 5 字节的载荷被安全忽略。
func TestParsePlayerUpdateData_ShortPayload(t *testing.T) {
	if updates, modern := protocol.ParsePlayerUpdateData([]byte{0x00, 0x01}); len(updates) != 0 || modern {
		t.Errorf("短载荷应返回空结果, 实际 updates=%+v modern=%v", updates, modern)
	}
}
