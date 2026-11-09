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

func TestFactionBlessingRoutine_BothSilentDropCooldownNotPermanent(t *testing.T) {
	sess := newSocialMockSession(t, func(net.Conn, *protocol.Packet) {
		// 神像面板与白檀香探测均模拟静默丢包。
	})
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := NewFactionBlessingRoutine(DefaultFactionBlessingConfig()).Execute(ctx, sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("双丢包应返回 CooldownError 而非永久禁用, 实际: %v", err)
	}
	if sess.IsFactionFeatureUnavailable() {
		t.Fatal("瞬断不应标记帮派模块永久不可用")
	}
}
