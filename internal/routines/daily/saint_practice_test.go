package daily

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func TestSaintPracticeRoutine_StatusSilentDropCooldown(t *testing.T) {
	sess := newDailyMockSession(t, func(net.Conn, *protocol.Packet) {
		// 圣域状态查询模拟服务端静默丢包。
	})
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := NewSaintPracticeRoutine().Execute(ctx, sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("圣域状态无回包应返回 CooldownError, 实际: %v", err)
	}
}

// TestSaintPracticeRoutine_FunctionClosedSkips 验证圣域功能未开放时直接跳过，
// 既不查询圣域状态，也不建立圣域会话、不产生冷却重试。
func TestSaintPracticeRoutine_FunctionClosedSkips(t *testing.T) {
	closed := make([]int32, 0, len(dailyAutomationFunctionIDs))
	for _, id := range dailyAutomationFunctionIDs {
		if id == protocol.FunctionIDSaintArea {
			continue
		}
		closed = append(closed, id)
	}

	var statusCalls int
	sess := newDailyMockSessionWithFunctions(t, closed, func(_ net.Conn, pkt *protocol.Packet) {
		if pkt.ActionID == protocol.ActionIDSaintAreaStatus {
			statusCalls++
		}
	})
	defer sess.Close()

	if err := NewSaintPracticeRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("功能未开放时应正常跳过, 实际: %v", err)
	}
	if statusCalls != 0 {
		t.Fatalf("功能未开放时不应查询圣域状态, 实际请求 %d 次", statusCalls)
	}
}
