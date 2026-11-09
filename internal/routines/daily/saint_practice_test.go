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