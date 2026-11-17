package dungeon

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
)

// TestWaitForReconnect_LeavesActiveThenReturns 验证状态机尚未切入 Reconnecting 时
// 不会误判为已恢复：先等状态离开 Active，再等回到 Active。
func TestWaitForReconnect_LeavesActiveThenReturns(t *testing.T) {
	// 前 3 次查询仍是旧状态 Active（读循环尚未切换），之后 Reconnecting，
	// 再过 3 次回到 Active。
	var calls atomic.Int32
	state := func() client.SessionState {
		n := calls.Add(1)
		switch {
		case n <= 3:
			return client.StateActive
		case n <= 6:
			return client.StateReconnecting
		default:
			return client.StateActive
		}
	}

	if !waitForReconnect(context.Background(), state, 200*time.Millisecond, time.Second) {
		t.Fatal("期望重连在超时内恢复为 Active")
	}
	if calls.Load() < 7 {
		t.Fatalf("期望等待到状态迁移完成, 实际仅查询 %d 次", calls.Load())
	}
}

// TestWaitForReconnect_Timeout 验证会话始终未恢复时返回 false。
func TestWaitForReconnect_Timeout(t *testing.T) {
	state := func() client.SessionState { return client.StateReconnecting }
	if waitForReconnect(context.Background(), state, 100*time.Millisecond, 200*time.Millisecond) {
		t.Fatal("状态始终非 Active 时应返回 false")
	}
}

// TestWaitForReconnect_ContextCanceled 验证上下文取消后立即放弃等待。
func TestWaitForReconnect_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	state := func() client.SessionState { return client.StateReconnecting }
	start := time.Now()
	if waitForReconnect(ctx, state, time.Second, 5*time.Second) {
		t.Fatal("上下文已取消时应返回 false")
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("上下文取消后应立即返回, 实际耗时 %s", elapsed)
	}
}
