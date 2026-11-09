package minigame

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func TestAskFairyRoutine_InfoSilentDropCooldown(t *testing.T) {
	sess := newMinigameMockSession(t, func(net.Conn, *protocol.Packet) {
		// 问灵信息查询模拟服务端静默丢包：不回包。
	})
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := NewAskFairyRoutine().Execute(ctx, sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("问灵状态无回包应返回 CooldownError, 实际: %v", err)
	}
}