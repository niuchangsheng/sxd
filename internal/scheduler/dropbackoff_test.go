package scheduler_test

import (
	"errors"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/scheduler"
)

// TestHandleSilentDrop_InactiveSessionReturnsNotConnected 验证会话未激活时
// 不累计丢包、不进入长冷却，交由调度器按 ErrNotConnected 重连重试。
func TestHandleSilentDrop_InactiveSessionReturnsNotConnected(t *testing.T) {
	sess := client.NewRoleSession(client.SessionConfig{RoleID: "role-drop-inactive"})
	err := scheduler.HandleSilentDrop(sess, "routine_x", 3, time.Minute, "test")
	if !errors.Is(err, client.ErrNotConnected) {
		t.Fatalf("未激活会话应返回 ErrNotConnected, got %v", err)
	}
	if n := scheduler.SilentDropCount(sess, "routine_x"); n != 0 {
		t.Fatalf("未激活会话不应累计丢包, got %d", n)
	}
}

// TestHandleSilentDrop_NilSessionReturnsNotConnected 验证 nil 会话同样安全。
func TestHandleSilentDrop_NilSessionReturnsNotConnected(t *testing.T) {
	if err := scheduler.HandleSilentDrop(nil, "routine_y", 3, time.Minute, "test"); !errors.Is(err, client.ErrNotConnected) {
		t.Fatalf("nil 会话应返回 ErrNotConnected, got %v", err)
	}
}
