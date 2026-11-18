package social

import (
	"os"
	"testing"
)

// TestJieYuanBoxSkipState_ScopedByRole 验证跳过标记按「角色 + 箱型」隔离：
// s0 的不可开启标记不能错误影响 s1（2026-11-18 实测 s1 被 s0 的 3937 挡住）。
func TestJieYuanBoxSkipState_ScopedByRole(t *testing.T) {
	t.Cleanup(func() {
		_ = os.Remove(jieyuanSkipStatePath)
		_ = os.Remove("../data/runtime")
	})

	markJiYuanBoxSkipped("s0-niuchangsheng", 3937)

	if !jiYuanBoxSkippedToday("s0-niuchangsheng", 3937) {
		t.Fatal("s0 标记后应处于跳过期")
	}
	if jiYuanBoxSkippedToday("s1-niuchangsheng", 3937) {
		t.Fatal("s1 不应继承 s0 的跳过标记")
	}
	if _, ok := jiYuanBoxSkipExpiry("s1-niuchangsheng", 3937); ok {
		t.Fatal("s1 不应继承 s0 的跳过到期时间")
	}
}
