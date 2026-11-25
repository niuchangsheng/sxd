package client

import (
	"context"
	"io"
	"testing"
	"time"
)

// TestNextReconnectDelay 验证连续重连的退避策略：
// 前 10 次上限 30 秒，长时间连续失败后放宽到 5 分钟，避免高频登录触发风控。
func TestNextReconnectDelay(t *testing.T) {
	base := 2 * time.Second
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 2 * time.Second},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 8 * time.Second},
		{4, 16 * time.Second},
		{5, 30 * time.Second},
		{9, 30 * time.Second},
		{10, 5 * time.Minute},
		{50, 5 * time.Minute},
	}
	for _, c := range cases {
		if got := nextReconnectDelay(c.attempt, base); got != c.want {
			t.Errorf("nextReconnectDelay(%d) = %s, want %s", c.attempt, got, c.want)
		}
	}
}

// TestNextReconnectDelay_ZeroBase 验证未配置 ReconnectInterval 时的兜底值。
func TestNextReconnectDelay_ZeroBase(t *testing.T) {
	if got := nextReconnectDelay(1, 0); got != 2*time.Second {
		t.Fatalf("zero base should fall back to 2s, got %s", got)
	}
}

// TestShouldRefreshCredentials 验证换票节流：
// 普通 EOF 复用现有票据，连续失败达到阈值或服务端明确拒绝认证时才换票；
// 且两次换票之间受最小间隔保护，避免同账号多角色互相顶号。
func TestShouldRefreshCredentials(t *testing.T) {
	s := NewRoleSession(SessionConfig{RoleID: "t", ServerAddr: "mock"})

	// 刚启动：处于换票保护期，任何失败都不立即换票。
	s.credRefreshMu.Lock()
	s.lastCredRefresh = time.Now()
	s.lastConnectErr = ErrAuthRejected
	s.credRefreshMu.Unlock()
	if s.shouldRefreshCredentials() {
		t.Fatal("保护期内不应换票")
	}

	// 超过保护期后，认证被拒应立即换票。
	s.credRefreshMu.Lock()
	s.lastCredRefresh = time.Now().Add(-3 * time.Minute)
	s.credRefreshMu.Unlock()
	if !s.shouldRefreshCredentials() {
		t.Fatal("认证被拒后应换票")
	}

	// 普通断线：前两次复用票据，第三次才换票。
	s.credRefreshMu.Lock()
	s.lastConnectErr = io.EOF
	s.failuresSinceRefresh = 2
	s.credRefreshMu.Unlock()
	if s.shouldRefreshCredentials() {
		t.Fatal("普通断线连续失败 <3 次不应换票")
	}
	s.credRefreshMu.Lock()
	s.failuresSinceRefresh = 3
	s.credRefreshMu.Unlock()
	if !s.shouldRefreshCredentials() {
		t.Fatal("普通断线连续失败 >=3 次应换票")
	}

	// 换票成功后计数与失败原因清零。
	s.markCredentialRefreshed()
	if s.shouldRefreshCredentials() {
		t.Fatal("刚换票成功后不应再次换票")
	}
}

// TestShouldRefreshCredentials_ConsumedTicket 验证一次性登录票据已被网关认证消费后，
// 再次断线只需过 15 秒防抖即触发重新换票，避免连续 3 次 15 秒握手超时。
func TestShouldRefreshCredentials_ConsumedTicket(t *testing.T) {
	s := NewRoleSession(SessionConfig{RoleID: "t-consumed", ServerAddr: "mock"})
	s.markTicketConsumed()

	// 15 秒防抖内不重复换票。
	s.credRefreshMu.Lock()
	s.lastCredRefresh = time.Now().Add(-5 * time.Second)
	s.lastConnectErr = io.EOF
	s.failuresSinceRefresh = 1
	s.credRefreshMu.Unlock()
	if s.shouldRefreshCredentials() {
		t.Fatal("15s 防抖内不应重复换票")
	}

	// 超过 15 秒防抖后，即便仅第 1 次 EOF，已消费票据也应立即换新票。
	s.credRefreshMu.Lock()
	s.lastCredRefresh = time.Now().Add(-20 * time.Second)
	s.credRefreshMu.Unlock()
	if !s.shouldRefreshCredentials() {
		t.Fatal("已消费过的一次性票据断线后应立即重新换票")
	}
}

// TestReconnectWait_HandshakeTimeout 验证登录握手超时后至少等待旧会话释放窗口，
// 避免断线后立刻重连导致连续多次 15 秒握手超时。
func TestReconnectWait_HandshakeTimeout(t *testing.T) {
	if got := reconnectWait(context.DeadlineExceeded, 1, 2*time.Second); got != sessionReleaseWait {
		t.Fatalf("握手超时首次重连应等待 %s, got %s", sessionReleaseWait, got)
	}
	if got := reconnectWait(io.EOF, 1, 2*time.Second); got != 2*time.Second {
		t.Fatalf("普通 EOF 首次重连应保持 2s, got %s", got)
	}
	// 指数退避已超过释放窗口时保持原值。
	if got := reconnectWait(context.DeadlineExceeded, 5, 2*time.Second); got != 30*time.Second {
		t.Fatalf("长退避不应被缩短, got %s", got)
	}
}
