package social

import (
	"testing"
	"time"

	"sxd-pie-ng/internal/protocol"
)

// TestNextSoulHuntActivityTime 验证按 step_pairs 选择下一轮开始时间：
// 未来最近的两个时间戳取后一个（结束→下一轮开始），只有一个时取该时刻。
func TestNextSoulHuntActivityTime(t *testing.T) {
	now := time.Date(2026, 11, 22, 17, 0, 0, 0, time.Local)
	panel := &protocol.SoulHuntMainPanel{
		StepPairs: [][2]int32{
			{114, int32(time.Date(2026, 11, 22, 23, 59, 30, 0, time.Local).Unix())},
			{115, int32(time.Date(2026, 11, 23, 0, 0, 0, 0, time.Local).Unix())},
			{116, int32(time.Date(2026, 12, 6, 23, 59, 30, 0, time.Local).Unix())},
		},
	}
	got := nextSoulHuntActivityTime(panel, now)
	want := time.Date(2026, 11, 23, 0, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("next open = %v, want %v", got, want)
	}

	// 只有一个未来时间戳时取该时刻。
	panel.StepPairs = panel.StepPairs[:1]
	if got := nextSoulHuntActivityTime(panel, now); !got.Equal(time.Date(2026, 11, 22, 23, 59, 30, 0, time.Local)) {
		t.Fatalf("single next = %v", got)
	}
	// 全部已过期时返回零值。
	if got := nextSoulHuntActivityTime(panel, time.Date(2027, 1, 1, 0, 0, 0, 0, time.Local)); !got.IsZero() {
		t.Fatalf("expired = %v, want zero", got)
	}
}
