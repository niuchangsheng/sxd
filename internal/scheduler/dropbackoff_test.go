package scheduler_test

import (
	"errors"
	"testing"
	"time"

	"sxd-pie-ng/internal/scheduler"
)

// TestHandleSilentDrop_BacksOffToDailyReset 验证连续静默丢包：未达阈值按短冷却
// 重试，达到阈值后挂起到次日 00:05。
func TestHandleSilentDrop_BacksOffToDailyReset(t *testing.T) {
	const routine = "silent_drop_test_routine"
	scheduler.ResetSilentDrops(nil, routine)
	defer scheduler.ResetSilentDrops(nil, routine)

	for i := 1; i <= 2; i++ {
		err := scheduler.HandleSilentDrop(nil, routine, 3, 10*time.Minute, "测试无回包")
		var cd *scheduler.CooldownError
		if !errors.As(err, &cd) {
			t.Fatalf("第 %d 次应返回 CooldownError, got %v", i, err)
		}
		if got := scheduler.SilentDropCount(nil, routine); got != i {
			t.Fatalf("连续次数 = %d, want %d", got, i)
		}
		// 未达阈值：应落在短冷却范围内（<= 10 分钟 + 少量误差）。
		if d := time.Until(cd.Until); d > 11*time.Minute || d <= 0 {
			t.Fatalf("第 %d 次冷却 = %v, want ~10m", i, d)
		}
	}

	err := scheduler.HandleSilentDrop(nil, routine, 3, 10*time.Minute, "测试无回包")
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("达到阈值应返回 CooldownError, got %v", err)
	}
	want := scheduler.NextDailyReset(time.Now())
	if cap := time.Now().Add(6 * time.Hour); want.After(cap) {
		want = cap
	}
	if diff := cd.Until.Sub(want); diff > time.Second || diff < -time.Second {
		t.Fatalf("达到阈值应挂起到 %v, got %v", want, cd.Until)
	}
	if d := time.Until(cd.Until); d > 6*time.Hour+time.Second {
		t.Fatalf("最长挂起不应超过 6 小时, got %v", d)
	}

	scheduler.ResetSilentDrops(nil, routine)
	if got := scheduler.SilentDropCount(nil, routine); got != 0 {
		t.Fatalf("重置后连续次数 = %d, want 0", got)
	}
}

// TestNextDailyReset 验证 00:05 重置时刻的边界。
func TestNextDailyReset(t *testing.T) {
	loc := time.Local
	before := time.Date(2026, 11, 15, 0, 4, 59, 0, loc)
	if got := scheduler.NextDailyReset(before); !got.Equal(time.Date(2026, 11, 15, 0, 5, 0, 0, loc)) {
		t.Fatalf("00:04:59 的下一次重置 = %v", got)
	}
	after := time.Date(2026, 11, 15, 0, 5, 0, 0, loc)
	if got := scheduler.NextDailyReset(after); !got.Equal(time.Date(2026, 11, 16, 0, 5, 0, 0, loc)) {
		t.Fatalf("00:05:00 的下一次重置 = %v", got)
	}
}
