package social

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func TestFriendshipRoutine_PanelSilentDropCooldown(t *testing.T) {
	sess := newSocialMockSession(t, func(net.Conn, *protocol.Packet) {
		// 好友来信面板模拟服务端静默丢包。
	})
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := NewFriendshipRoutine().Execute(ctx, sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("好友来信面板无回包应返回 CooldownError, 实际: %v", err)
	}
}
