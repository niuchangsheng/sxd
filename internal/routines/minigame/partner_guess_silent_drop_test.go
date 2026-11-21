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

// TestPartnerGuessRoutine_StartSilentDropBacksOff 验证伙伴猜猜看开局被静默丢弃时
// 按连续丢包退避，而不是 break + return nil 把剩余次数丢到下一个 cron 时段。
func TestPartnerGuessRoutine_StartSilentDropBacksOff(t *testing.T) {
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGuessPartnerInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGuessPartnerInfoResultPacket(1))
		case protocol.ActionIDGuessPartnerStart:
			// 静默丢弃：不回包。
		}
	})
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := NewPartnerGuessRoutine().Execute(ctx, sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("开局无应答应返回 CooldownError, got %v", err)
	}
	if got := scheduler.SilentDropCount(sess, "partner_guess"); got < 1 {
		t.Fatalf("应记录伙伴猜猜看静默丢包，got %d", got)
	}
}
