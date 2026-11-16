package protocol

import "testing"

func TestParseSaUnionWorshipPanel(t *testing.T) {
	w := NewWriter()
	for _, v := range []int32{1, 2, 3, 1794324074, 5, 6, 7, 0, 9, 10, 11} {
		w.WriteInt32(v)
	}
	vals, err := ParseSaUnionWorshipPanel(w.Bytes())
	if err != nil {
		t.Fatalf("ParseSaUnionWorshipPanel: %v", err)
	}
	if len(vals) != 11 {
		t.Fatalf("字段数量 = %d, want 11", len(vals))
	}
	if vals[3] != 1794324074 {
		t.Fatalf("第 4 个字段 = %d, want 1794324074", vals[3])
	}
}

func TestParseSaUnionWorshipPanel_InvalidLength(t *testing.T) {
	if _, err := ParseSaUnionWorshipPanel([]byte{1, 2, 3}); err == nil {
		t.Fatal("长度不是 4 的倍数时应返回错误")
	}
	if _, err := ParseSaUnionWorshipPanel(nil); err == nil {
		t.Fatal("空载荷应返回错误")
	}
}

func TestParseSaUnionWorshipResult(t *testing.T) {
	code, err := ParseSaUnionWorshipResult([]byte{SaUnionWorshipResultFailed})
	if err != nil || code != SaUnionWorshipResultFailed {
		t.Fatalf("结果码 = %d, err = %v", code, err)
	}
	if _, err := ParseSaUnionWorshipResult(nil); err == nil {
		t.Fatal("空载荷应返回错误")
	}
}
