package dungeon

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"sync"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/protocol"
)

func newScrollTestSession(t *testing.T, roleID string, handler func(net.Conn, *protocol.Packet)) *client.RoleSession {
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
			handler(serverConn, pkt)
		}
	}()
	sess := client.NewRoleSession(client.SessionConfig{
		RoleID:     roleID,
		RoleName:   "扫荡出售测试",
		ServerAddr: "scroll-pipe",
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
	return sess
}

func scrollPackPayload(items []protocol.ItemRecord) []byte {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.BigEndian, int16(60))
	_ = binary.Write(buf, binary.BigEndian, int32(0))
	_ = binary.Write(buf, binary.BigEndian, uint16(len(items)))
	for _, it := range items {
		_ = protocol.WriteItemRecord(buf, it)
	}
	_ = binary.Write(buf, binary.BigEndian, int16(10))
	_ = binary.Write(buf, binary.BigEndian, uint16(0))
	return buf.Bytes()
}

// TestSellEquipmentScrollsAfterEliteSweep_S0Only 验证精英扫荡兜底只卖
// s0 的装备制作卷 (1002)，不卖 1-5 品丹药卷 (1001)，也不卖其他道具。
func TestSellEquipmentScrollsAfterEliteSweep_S0Only(t *testing.T) {
	dictRepo, err := dictionary.NewRepository("")
	if err != nil {
		t.Fatalf("load dictionary: %v", err)
	}
	defer dictRepo.Close()

	var mu sync.Mutex
	var sold []int16
	sess := newScrollTestSession(t, "s0-test-role", func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDItemGetPackList:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, scrollPackPayload([]protocol.ItemRecord{
				{PlayerItemID: 1, ItemID: 940, GridID: 5, Field5: 1},                       // 逍遥戟制作卷 (1002)
				{PlayerItemID: 2, ItemID: 382, GridID: 6, Field5: 1},                       // 一品武力丹制作卷 (1001)：不卖
				{PlayerItemID: 3, ItemID: protocol.ItemIDSteamedBun, GridID: 7, Field5: 9}, // 包子：不卖
			})))
		case protocol.ActionIDItemSell:
			grid := int16(binary.BigEndian.Uint16(pkt.Payload[:2]))
			mu.Lock()
			sold = append(sold, grid)
			mu.Unlock()
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.ItemActionSuccess}))
		}
	})

	sellEquipmentScrollsAfterEliteSweep(context.Background(), sess, dictRepo)
	mu.Lock()
	defer mu.Unlock()
	if len(sold) != 1 || sold[0] != 5 {
		t.Fatalf("应只出售装备制作卷格位 5, 实际 %v", sold)
	}
}

// TestSellEquipmentScrollsAfterEliteSweep_S1SellsEquipmentScrolls 验证
// s1 也会在精英扫荡后出售装备制作卷 (1002)，同时保留 1-5 品丹药卷 (1001)
// 与普通道具。2026-11-28 之前 s1 被排除在兜底出售外，背包沉积 11 叠制作卷。
func TestSellEquipmentScrollsAfterEliteSweep_S1SellsEquipmentScrolls(t *testing.T) {
	dictRepo, err := dictionary.NewRepository("")
	if err != nil {
		t.Fatalf("load dictionary: %v", err)
	}
	defer dictRepo.Close()

	var mu sync.Mutex
	var sold []int16
	sess := newScrollTestSession(t, "s1-test-role", func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDItemGetPackList:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, scrollPackPayload([]protocol.ItemRecord{
				{PlayerItemID: 1, ItemID: 940, GridID: 5, Field5: 1},                       // 逍遥戟制作卷 (1002)
				{PlayerItemID: 4, ItemID: 980, GridID: 9, Field5: 2},                       // 倚天战魂制作卷 (1002)
				{PlayerItemID: 2, ItemID: 382, GridID: 6, Field5: 1},                       // 一品武力丹制作卷 (1001)：不卖
				{PlayerItemID: 3, ItemID: protocol.ItemIDSteamedBun, GridID: 7, Field5: 9}, // 包子：不卖
			})))
		case protocol.ActionIDItemSell:
			grid := int16(binary.BigEndian.Uint16(pkt.Payload[:2]))
			mu.Lock()
			sold = append(sold, grid)
			mu.Unlock()
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.ItemActionSuccess}))
		}
	})

	sellEquipmentScrollsAfterEliteSweep(context.Background(), sess, dictRepo)
	mu.Lock()
	defer mu.Unlock()
	if len(sold) != 2 {
		t.Fatalf("应出售 2 叠装备制作卷, 实际 %v", sold)
	}
	for _, g := range sold {
		if g != 5 && g != 9 {
			t.Fatalf("误卖非装备制作卷格位 %d (实际出售 %v)", g, sold)
		}
	}
}

// TestSellEquipmentScrollsAfterEliteSweep_SellRejected 验证服务端拒绝出售
// (结果码非成功) 时只记录失败、不重试、不崩溃，且不影响后续其它叠出售。
func TestSellEquipmentScrollsAfterEliteSweep_SellRejected(t *testing.T) {
	dictRepo, err := dictionary.NewRepository("")
	if err != nil {
		t.Fatalf("load dictionary: %v", err)
	}
	defer dictRepo.Close()

	var mu sync.Mutex
	var sellCalls []int16
	sess := newScrollTestSession(t, "s1-test-role", func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDItemGetPackList:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, scrollPackPayload([]protocol.ItemRecord{
				{PlayerItemID: 1, ItemID: 940, GridID: 5, Field5: 1}, // 服务端拒绝
				{PlayerItemID: 4, ItemID: 980, GridID: 9, Field5: 2}, // 正常出售
			})))
		case protocol.ActionIDItemSell:
			grid := int16(binary.BigEndian.Uint16(pkt.Payload[:2]))
			mu.Lock()
			sellCalls = append(sellCalls, grid)
			mu.Unlock()
			code := byte(protocol.ItemActionSuccess)
			if grid == 5 {
				code = protocol.PlayerActionFailed // 服务端明确拒绝
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{code}))
		}
	})

	sellEquipmentScrollsAfterEliteSweep(context.Background(), sess, dictRepo)
	mu.Lock()
	defer mu.Unlock()
	// 被拒绝的格位只尝试一次，不无限重试。
	attempts := 0
	for _, g := range sellCalls {
		if g == 5 {
			attempts++
		}
	}
	if attempts != 1 {
		t.Fatalf("被拒绝的格位应只尝试 1 次, 实际 %d 次 (全部调用 %v)", attempts, sellCalls)
	}
}
