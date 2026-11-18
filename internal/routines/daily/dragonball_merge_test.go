package daily

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
)

// TestDragonballOfferRoutine_MergesNormalAndFireSeparately 验证例程启动时先拾取，
// 再分别以「紫及以下」与「火龙珠」两组品质发起一键合成。
func TestDragonballOfferRoutine_MergesNormalAndFireSeparately(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})
	var mu sync.Mutex
	var batches [][]int32
	go func() {
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDPlayerGetFunctions:
				_ = protocol.WritePacket(serverConn, protocol.BuildPlayerFunctionsResultPacket(dailyAutomationFunctionIDs))
			case protocol.ActionIDDragonballTmpInfo:
				_ = protocol.WritePacket(serverConn, protocol.BuildDragonballTmpInfoResultPacket(
					[]protocol.DragonballTmpBall{{PlayerDBID: 1001, Level: 5, Quality: 3}}))
			case protocol.ActionIDDragonballCollect:
				_ = protocol.WritePacket(serverConn, protocol.BuildDragonballCollectResultPacket(
					protocol.DragonballResultSuccess, []int32{1001}))
			case protocol.ActionIDDragonballOneKeyMerge:
				r := protocol.NewReader(pkt.Payload)
				n, _ := r.ReadUint16()
				quals := make([]int32, 0, n)
				for i := 0; i < int(n); i++ {
					q, _ := r.ReadInt32()
					quals = append(quals, q)
				}
				mu.Lock()
				batches = append(batches, quals)
				mu.Unlock()
				_ = protocol.WritePacket(serverConn, protocol.BuildDragonballOneKeyMergeResultPacket(
					protocol.DragonballResultSuccess, nil))
			case protocol.ActionIDDragonballSacrifice:
				_ = protocol.WritePacket(serverConn, protocol.BuildDragonballSacrificeResultPacket(
					protocol.DragonballSacrificeResult{Result: protocol.DragonballResultTimesUseUp}))
			}
		}
	}()
	sess := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role-dragonball-merge-test",
		RoleName:   "龙珠合成测试",
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

	if err := NewDragonballOfferRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("dragonball offer: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(batches) < 2 {
		t.Fatalf("expected at least 2 merge batches, got %v", batches)
	}
	first := batches[0]
	if len(first) != 3 || first[0] != 1 || first[1] != 2 || first[2] != 3 {
		t.Fatalf("normal merge qualities = %v, want [1 2 3]", first)
	}
	foundFire := false
	for _, b := range batches[1:] {
		if len(b) == 1 && b[0] == protocol.DragonballQualityFire {
			foundFire = true
			break
		}
	}
	if !foundFire {
		t.Fatalf("expected a separate fire merge batch, got %v", batches)
	}
}

// TestDragonballOfferRoutine_ClosedInFunctionListStillTries 验证 get_player_function
// 未下发 89 时不直接跳过，而是继续按业务回包判断（2026-11-18 用户确认 s1 仍可培养）。
func TestDragonballOfferRoutine_ClosedInFunctionListStillTries(t *testing.T) {
	withoutDragonball := make([]int32, 0, len(dailyAutomationFunctionIDs))
	for _, id := range dailyAutomationFunctionIDs {
		if id == protocol.FunctionIDDragonball {
			continue
		}
		withoutDragonball = append(withoutDragonball, id)
	}

	var tmpCalls int
	var mu sync.Mutex
	sess := newDailyMockSessionWithFunctions(t, withoutDragonball, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDDragonballTmpInfo:
			mu.Lock()
			tmpCalls++
			mu.Unlock()
			_ = protocol.WritePacket(conn, protocol.BuildDragonballTmpInfoDetailResultPacket(protocol.DragonballTmpInfo{
				RemainNormalCount: 0, // 次数已用尽，快速结束本轮
			}))
		}
	})
	defer sess.Close()

	if err := NewDragonballOfferRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	mu.Lock()
	calls := tmpCalls
	mu.Unlock()
	if calls == 0 {
		t.Fatal("功能列表缺少 89 时仍应查询神龙临时格（业务兜底）")
	}
}
