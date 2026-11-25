package dungeon

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
)

// TestWaitForReconnect_LeavesActiveThenReturns 验证状态机尚未切入 Reconnecting 时
// 不会误判为已恢复：先等状态离开 Active，再等回到 Active。
func TestWaitForReconnect_LeavesActiveThenReturns(t *testing.T) {
	// 前 3 次查询仍是旧状态 Active（读循环尚未切换），之后 Reconnecting，
	// 再过 3 次回到 Active。
	var calls atomic.Int32
	state := func() client.SessionState {
		n := calls.Add(1)
		switch {
		case n <= 3:
			return client.StateActive
		case n <= 6:
			return client.StateReconnecting
		default:
			return client.StateActive
		}
	}

	if !waitForReconnect(context.Background(), state, 200*time.Millisecond, time.Second) {
		t.Fatal("期望重连在超时内恢复为 Active")
	}
	if calls.Load() < 7 {
		t.Fatalf("期望等待到状态迁移完成, 实际仅查询 %d 次", calls.Load())
	}
}

// TestWaitForReconnect_Timeout 验证会话始终未恢复时返回 false。
func TestWaitForReconnect_Timeout(t *testing.T) {
	state := func() client.SessionState { return client.StateReconnecting }
	if waitForReconnect(context.Background(), state, 100*time.Millisecond, 200*time.Millisecond) {
		t.Fatal("状态始终非 Active 时应返回 false")
	}
}

// TestWaitForReconnect_ContextCanceled 验证上下文取消后立即放弃等待。
func TestWaitForReconnect_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	state := func() client.SessionState { return client.StateReconnecting }
	start := time.Now()
	if waitForReconnect(ctx, state, time.Second, 5*time.Second) {
		t.Fatal("上下文已取消时应返回 false")
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("上下文取消后应立即返回, 实际耗时 %s", elapsed)
	}
}

// TestAllowQuestMidCycleReconnect_Throttle 验证轮内重连按间隔节流，
// 避免每关都重连造成重连风暴。
func TestAllowQuestMidCycleReconnect_Throttle(t *testing.T) {
	sess := &client.RoleSession{}
	old := questMidReconnectInterval
	questMidReconnectInterval = 20 * time.Millisecond
	t.Cleanup(func() {
		questMidReconnectInterval = old
		questMidReconnectAt.Delete(sess)
	})

	if !allowQuestMidCycleReconnect(sess) {
		t.Fatal("首次应允许轮内重连")
	}
	if allowQuestMidCycleReconnect(sess) {
		t.Fatal("节流间隔内不应再次允许重连")
	}
	time.Sleep(30 * time.Millisecond)
	if !allowQuestMidCycleReconnect(sess) {
		t.Fatal("超过节流间隔后应重新允许重连")
	}
}

// TestReconnectQuestSession_RefreshesTicketAndAdvancesGen 验证推图主动重连前
// 会先刷新一次性票据并等待新一代连接进入 Active，取消上下文时返回 false。
func TestReconnectQuestSession_RefreshesTicketAndAdvancesGen(t *testing.T) {
	var refreshed, dials atomic.Int32
	sess := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role-quest-reconn",
		ServerAddr: "mock-pipe",
		Dialer: func(context.Context, string, string) (net.Conn, error) {
			dials.Add(1)
			srv, cli := net.Pipe()
			t.Cleanup(func() {
				_ = srv.Close()
				_ = cli.Close()
			})
			go func() {
				buf := make([]byte, 64)
				for {
					if _, err := srv.Read(buf); err != nil {
						return
					}
				}
			}()
			return cli, nil
		},
		Authenticator: func(*client.RoleSession) error { return nil },
		RefreshCredentials: func(_ context.Context, cfg *client.SessionConfig) error {
			refreshed.Add(1)
			cfg.Hash = "fresh-ticket"
			return nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := sess.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sess.Close()
	deadline := time.Now().Add(2 * time.Second)
	for sess.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	gen0 := sess.ConnectionGeneration()

	if !reconnectQuestSession(ctx, sess) {
		t.Fatal("reconnectQuestSession 应成功恢复会话")
	}
	if refreshed.Load() != 1 {
		t.Fatalf("主动重连前应换票 1 次, got %d", refreshed.Load())
	}
	if sess.ConnectionGeneration() <= gen0 {
		t.Fatalf("连接代数应递增: before=%d after=%d", gen0, sess.ConnectionGeneration())
	}

	if reconnectQuestSession(ctx, nil) {
		t.Fatal("nil session 应返回 false")
	}
}
