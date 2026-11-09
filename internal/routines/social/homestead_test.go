package social

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestHomesteadRoutine_ClaimsFurnitureEffects(t *testing.T) {
	var effectCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDMarryHomeSelfInfo:
			_ = protocol.WritePacket(conn, protocol.BuildMarryHomeSelfInfoResultPacket(
				protocol.MarryHomeResultSuccess))
		case protocol.ActionIDMarryHomeBatchFurnitureEffect:
			effectCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildMarryHomeBatchFurnitureEffectResultPacket(
				[]protocol.MarryHomeFurnitureEffect{{ItemID: 6001, Effect: 12}}))
		}
	})
	defer sess.Close()

	if err := NewHomesteadRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("homestead: %v", err)
	}
	if got := effectCalls.Load(); got != 1 {
		t.Fatalf("应领取 1 次家具效果, 实际 %d 次", got)
	}
}
