// Package protocol_test 验证悟法领域 (Module 658) 官方协议编解码。
//
// @author Ateng
// @since 2026-11-03
package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func buildWuFaPanelPayload() []byte {
	w := protocol.NewWriter()
	// 2 个秘馆，每个 9 个 Int。
	w.WriteUint16(2)
	w.WriteInt32(1) // field_type
	w.WriteInt32(3) // today_count
	w.WriteInt32(20)
	w.WriteInt32(1)
	w.WriteInt32(1) // left_free_one_count
	w.WriteInt32(0) // left_free_ten_count
	w.WriteInt32(0)
	w.WriteInt32(0)
	w.WriteInt32(5) // left_total_ten_count

	w.WriteInt32(2)
	w.WriteInt32(0)
	w.WriteInt32(0)
	w.WriteInt32(0)
	w.WriteInt32(0)
	w.WriteInt32(1) // left_free_ten_count
	w.WriteInt32(0)
	w.WriteInt32(0)
	w.WriteInt32(0)

	// 1 项资源，每项 3 个 Int。
	w.WriteUint16(1)
	w.WriteInt32(8364)
	w.WriteInt32(10)
	w.WriteInt32(0)

	// 公共字段：11 个 Int + 1 个 Byte。
	w.WriteInt32(100) // gift_start_time
	w.WriteInt32(200) // gift_end_time
	w.WriteInt32(1)   // have_privilege
	w.WriteInt32(0)   // book_store_is_open
	w.WriteInt32(1)   // ru_men_field_active
	w.WriteInt32(5)   // psychic_tbld_level
	w.WriteInt32(300) // psychic_res_num
	w.WriteInt32(0)   // is_lucky_day
	w.WriteInt32(4)   // rest_times
	w.WriteInt32(1)   // discount_num
	w.WriteInt32(123) // indexTime
	w.WriteInt8(1)    // is_skip
	return w.Bytes()
}

func TestParseWuFaPanel(t *testing.T) {
	panel, err := protocol.ParseWuFaPanel(buildWuFaPanelPayload())
	if err != nil {
		t.Fatalf("解析悟法面板失败: %v", err)
	}
	if len(panel.Fields) != 2 {
		t.Fatalf("期望 2 个秘馆, 实际 %d", len(panel.Fields))
	}
	if panel.Fields[0].FieldType != 1 || panel.Fields[0].LeftFreeOneCount != 1 {
		t.Errorf("秘馆 0 解析错误: %+v", panel.Fields[0])
	}
	if panel.Fields[1].FieldType != 2 || panel.Fields[1].LeftFreeTenCount != 1 {
		t.Errorf("秘馆 1 解析错误: %+v", panel.Fields[1])
	}
	if len(panel.Items) != 1 || panel.Items[0].ItemID != 8364 || panel.Items[0].ItemNum != 10 {
		t.Errorf("资源解析错误: %+v", panel.Items)
	}
	if panel.RestTimes != 4 || panel.IsSkip != 1 {
		t.Errorf("公共字段解析错误: rest_times=%d is_skip=%d", panel.RestTimes, panel.IsSkip)
	}
}

func TestParseWuFaPanel_Truncated(t *testing.T) {
	payload := buildWuFaPanelPayload()
	if _, err := protocol.ParseWuFaPanel(payload[:10]); err == nil {
		t.Fatal("截断的面板回包应返回错误")
	}
}

func TestParseWuFaDrawResult(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteUint8(protocol.WuFaResultSuccess)
	w.WriteUint16(1)
	w.WriteInt32(8364) // item_id
	w.WriteInt32(1)    // item_num
	w.WriteInt32(0)    // change_item_id
	w.WriteInt32(0)    // change_item_num
	w.WriteInt32(0)    // is_synthesize
	w.WriteInt32(0)    // is_up_level
	w.WriteInt32(0)    // psychic_add_item_num
	w.WriteInt32(0)    // lucky_item_num
	w.WriteInt32(4)    // rest_times

	result, err := protocol.ParseWuFaDrawResult(w.Bytes())
	if err != nil {
		t.Fatalf("解析悟法抽卡回包失败: %v", err)
	}
	if result.Result != protocol.WuFaResultSuccess {
		t.Errorf("期望成功码 %d, 实际 %d", protocol.WuFaResultSuccess, result.Result)
	}
	if len(result.Awards) != 1 || result.Awards[0].ItemID != 8364 || result.Awards[0].ItemNum != 1 {
		t.Errorf("奖励解析错误: %+v", result.Awards)
	}
	if result.RestTimes != 4 {
		t.Errorf("期望剩余次数 4, 实际 %d", result.RestTimes)
	}
}

func TestParseWuFaDrawResult_BusinessFailure(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteUint8(protocol.WuFaResultNoEnoughDrawTimes)
	w.WriteUint16(0)
	w.WriteInt32(0)

	result, err := protocol.ParseWuFaDrawResult(w.Bytes())
	if err != nil {
		t.Fatalf("解析失败码回包不应报错: %v", err)
	}
	if result.Result != protocol.WuFaResultNoEnoughDrawTimes {
		t.Errorf("期望结果码 %d, 实际 %d", protocol.WuFaResultNoEnoughDrawTimes, result.Result)
	}
	if len(result.Awards) != 0 {
		t.Errorf("失败回包不应包含奖励, 实际 %d 条", len(result.Awards))
	}
}

func TestParseWuFaDrawResult_Truncated(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteUint8(protocol.WuFaResultSuccess)
	w.WriteUint16(1)
	// 缺少完整的 8 个 Int 与 rest_times
	if _, err := protocol.ParseWuFaDrawResult(w.Bytes()); err == nil {
		t.Fatal("截断的抽卡回包应返回错误")
	}
}

func TestWuFaResultText(t *testing.T) {
	cases := map[uint8]string{
		protocol.WuFaResultSuccess:           "成功",
		protocol.WuFaResultNotEnoughCount:    "剩余次数不足",
		protocol.WuFaResultItemPackFull:      "背包已满",
		protocol.WuFaResultFieldTypeError:    "秘馆类型错误",
		protocol.WuFaResultNoEnoughDrawTimes: "感悟次数不足",
	}
	for code, want := range cases {
		if got := protocol.WuFaResultText(code); got != want {
			t.Errorf("WuFaResultText(%d) = %q, want %q", code, got, want)
		}
	}
	if got := protocol.WuFaResultText(200); got == "" {
		t.Error("未知结果码应返回非空诊断文本")
	}
}
