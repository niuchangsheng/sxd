package dungeon

import (
	"context"
	"encoding/binary"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
)

// newTownResetTestSession 构造用于城镇复位测试的管道会话。
// handler 返回 respond=true 表示已回包；false 表示模拟静默丢弃。
func newTownResetTestSession(t *testing.T, handler func(pkt *protocol.Packet, conn net.Conn) bool) *client.RoleSession {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})
	go func() {
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			handler(pkt, serverConn)
		}
	}()

	sess := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role-town-reset-test",
		RoleName:   "城镇复位测试",
		ServerAddr: "mock",
		Dialer: func(context.Context, string, string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(*client.RoleSession) error { return nil },
	})
	if err := sess.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	t.Cleanup(sess.Close)
	deadline := time.Now().Add(2 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if sess.State() != client.StateActive {
		t.Fatalf("session not active, state=%v", sess.State())
	}
	return sess
}

// TestEnterQuestTownScene_FallsBackToPlayerInfoAnchor 验证以 Mod 4 锚点的 1/0
// 被服务端拒绝时，会用显式 0/2 锚点重试主城镇并成功。
//
// 2026-11-17 线上对照：0x00040000 锚点的 enter_town 静默丢弃；
// 0x00000002 锚点能回包，且随后 4/0 章节列表恢复正常。
func TestEnterQuestTownScene_FallsBackToPlayerInfoAnchor(t *testing.T) {
	var sawZeroAnchor atomic.Bool
	var zeroAnchorCalls atomic.Int32
	sess := newTownResetTestSession(t, func(pkt *protocol.Packet, conn net.Conn) bool {
		if pkt.ActionID != protocol.ActionIDTownEnter {
			return false
		}
		anchor := uint32(0)
		if len(pkt.Payload) >= 4 {
			anchor = binary.BigEndian.Uint32(pkt.Payload[len(pkt.Payload)-4:])
		}
		if anchor == protocol.ActionIDPlayerGetInfo {
			sawZeroAnchor.Store(true)
			zeroAnchorCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDTownEnter, []byte{0x00}))
			return true
		}
		// Mod 3/4 锚点：返回非 0 结果码模拟服务端拒绝。
		_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDTownEnter, []byte{0x01}))
		return true
	})

	sess.SetLastActionID(protocol.ActionIDMissionGetSections)
	sess.UpdatePlayerState(func(ps *client.PlayerState) { ps.TownMapID = 21 })

	id, ok := enterQuestTownScene(context.Background(), sess)
	if !ok || id != 21 {
		t.Fatalf("期望以 0/2 锚点重试成功进入城镇 21, 实际 id=%d ok=%v", id, ok)
	}
	if !sawZeroAnchor.Load() {
		t.Fatal("期望回退到 0/2 锚点重试")
	}
	if zeroAnchorCalls.Load() != 1 {
		t.Fatalf("0/2 锚点重试应仅尝试主城镇一次, 实际 %d 次", zeroAnchorCalls.Load())
	}
}

// TestEnterQuestTownScene_NoFallbackWhenFirstPassWorks 验证首轮成功时不额外重试。
func TestEnterQuestTownScene_NoFallbackWhenFirstPassWorks(t *testing.T) {
	var zeroAnchorCalls atomic.Int32
	sess := newTownResetTestSession(t, func(pkt *protocol.Packet, conn net.Conn) bool {
		if pkt.ActionID != protocol.ActionIDTownEnter {
			return false
		}
		if len(pkt.Payload) >= 4 &&
			binary.BigEndian.Uint32(pkt.Payload[len(pkt.Payload)-4:]) == protocol.ActionIDPlayerGetInfo {
			zeroAnchorCalls.Add(1)
		}
		_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDTownEnter, []byte{0x00}))
		return true
	})

	sess.SetLastActionID(protocol.ActionIDMissionGetSections)
	sess.UpdatePlayerState(func(ps *client.PlayerState) { ps.TownMapID = 21 })

	id, ok := enterQuestTownScene(context.Background(), sess)
	if !ok || id != 21 {
		t.Fatalf("期望首轮锚点直接成功, 实际 id=%d ok=%v", id, ok)
	}
	if zeroAnchorCalls.Load() != 0 {
		t.Fatalf("首轮成功时不应发起 0/2 锚点重试, 实际 %d 次", zeroAnchorCalls.Load())
	}
}
