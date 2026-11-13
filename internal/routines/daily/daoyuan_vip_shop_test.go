package daily

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func TestDaoyuanVipShopRoutine_ListSilentDropCooldown(t *testing.T) {
	sess := newDailyMockSession(t, func(net.Conn, *protocol.Packet) {
		// 列表查询模拟服务端静默丢包：不回包。
	})
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := NewDaoyuanVipShopRoutine().Execute(ctx, sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("列表无回包应返回 CooldownError, 实际: %v", err)
	}
}

func TestDaoyuanVipShopRoutine_PackFullStops(t *testing.T) {
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDDaoyuanVipShopList:
			if len(pkt.Payload) != 1 || pkt.Payload[0] != protocol.DaoyuanVipShopTabFive {
				t.Errorf("VIP5 请求载荷 = %v, want [%d] (不能附带 PrevAct)",
					pkt.Payload, protocol.DaoyuanVipShopTabFive)
			}
			w := protocol.NewWriter()
			w.WriteUint16(1)
			w.WriteInt8(1)
			w.WriteInt32(itemShenJiaJingShi)
			w.WriteInt16(100)
			w.WriteInt16(2)
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDDaoyuanVipShopList, w.Bytes()))
		case protocol.ActionIDDaoyuanShopBuy:
			_ = protocol.WritePacket(conn, protocol.NewPacket(protocol.ActionIDDaoyuanShopBuy,
				[]byte{protocol.DaoyuanShopResultPackFull, 0, 0}))
		}
	})
	defer sess.Close()

	err := NewDaoyuanVipShopRoutine().Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrBagFull) {
		t.Fatalf("背包已满应返回 ErrBagFull, 实际: %v", err)
	}
}

// TestDaoyuanVipShopRoutine_SaintAreaClosedSkips 验证圣域功能未开放时直接跳过，
// 不再回退主服产生必然的 132 静默丢包。
func TestDaoyuanVipShopRoutine_SaintAreaClosedSkips(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})
	var shopCalls int32
	go func() {
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDPlayerGetFunctions:
				// 功能列表为空：圣域未开放。
				_ = protocol.WritePacket(serverConn,
					protocol.BuildPlayerFunctionsResultPacket(nil))
			case protocol.ActionIDDaoyuanVipShopList, protocol.ActionIDDaoyuanShopBuy:
				atomic.AddInt32(&shopCalls, 1)
			}
		}
	}()

	sess := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role-daoyuan-sa-closed",
		RoleName:   "圣域未开放测试",
		ServerAddr: "mock-pipe",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(*client.RoleSession) error { return nil },
	})
	if err := sess.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer sess.Close()
	deadline := time.Now().Add(2 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	if err := NewDaoyuanVipShopRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := atomic.LoadInt32(&shopCalls); got != 0 {
		t.Fatalf("圣域未开放时不应发送 132 业务请求, 实际 %d 次", got)
	}
}
