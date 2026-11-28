package platform

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type coordMockAuth struct {
	plat       string
	delay      time.Duration
	onLogin    func(username, serverID string)
	inFlight   atomic.Int32
	maxFlight  atomic.Int32
	loginTimes sync.Map
}

func (m *coordMockAuth) Platform() string { return m.plat }

func (m *coordMockAuth) Login(ctx context.Context, username, password, serverID string) (*Ticket, error) {
	cur := m.inFlight.Add(1)
	defer m.inFlight.Add(-1)
	for {
		old := m.maxFlight.Load()
		if cur <= old || m.maxFlight.CompareAndSwap(old, cur) {
			break
		}
	}
	if m.onLogin != nil {
		m.onLogin(username, serverID)
	}
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	m.loginTimes.Store(username+":"+serverID, time.Now())
	return &Ticket{
		Platform: m.plat,
		ServerID: serverID,
		RoleName: username + "." + serverID,
	}, nil
}

// TestTicketCache_AccountLevelSerializationAndInterval 验证同一平台账号的不同区服角色
// 并发换票时会按账号维度严格串行，且遵守最小换票间隔，避免运行期互相顶号。
func TestTicketCache_AccountLevelSerializationAndInterval(t *testing.T) {
	cache := NewTicketCache(10 * time.Minute)
	const minInterval = 80 * time.Millisecond
	cache.SetMinAccountLoginInterval(minInterval)

	auth := &coordMockAuth{plat: "xindong", delay: 10 * time.Millisecond}
	cache.RegisterAuthenticator(auth)

	var wg sync.WaitGroup
	wg.Add(2)
	for _, srv := range []string{"s0", "s1"} {
		srv := srv
		go func() {
			defer wg.Done()
			if _, err := cache.GetOrFetch(context.Background(), "xindong", "shared_user", "pwd", srv); err != nil {
				t.Errorf("GetOrFetch(%s) failed: %v", srv, err)
			}
		}()
	}
	wg.Wait()

	if got := auth.maxFlight.Load(); got != 1 {
		t.Fatalf("同账号多区服换票必须串行，期望 maxFlight=1, got %d", got)
	}
	t0v, ok0 := auth.loginTimes.Load("shared_user:s0")
	t1v, ok1 := auth.loginTimes.Load("shared_user:s1")
	if !ok0 || !ok1 {
		t.Fatalf("缺少登录时间记录: ok0=%v ok1=%v", ok0, ok1)
	}
	diff := t1v.(time.Time).Sub(t0v.(time.Time))
	if diff < 0 {
		diff = -diff
	}
	if diff < minInterval-10*time.Millisecond {
		t.Fatalf("同账号两次换票间隔过短: got %v, want >= %v", diff, minInterval)
	}
}

// TestTicketCache_DifferentAccountsRunInParallel 验证不同账号的换票不会互相阻塞。
func TestTicketCache_DifferentAccountsRunInParallel(t *testing.T) {
	cache := NewTicketCache(10 * time.Minute)
	cache.SetMinAccountLoginInterval(200 * time.Millisecond)

	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	auth := &coordMockAuth{
		plat: "xindong",
		onLogin: func(username, serverID string) {
			entered <- struct{}{}
			<-release
		},
	}
	cache.RegisterAuthenticator(auth)

	var wg sync.WaitGroup
	wg.Add(2)
	for _, user := range []string{"user_a", "user_b"} {
		user := user
		go func() {
			defer wg.Done()
			if _, err := cache.GetOrFetch(context.Background(), "xindong", user, "pwd", "s0"); err != nil {
				t.Errorf("GetOrFetch(%s) failed: %v", user, err)
			}
		}()
	}

	// 两个不同账号应同时进入 Login，而不会互相等待。
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("不同账号的换票不应互相阻塞")
		}
	}
	close(release)
	wg.Wait()

	if got := auth.maxFlight.Load(); got != 2 {
		t.Fatalf("不同账号应并发换票，期望 maxFlight=2, got %d", got)
	}
}
