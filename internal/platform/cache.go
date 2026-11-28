// Package platform 提供多运营平台 Web 自动化认证、凭据换取与区服标识归一化驱动层。
//
// @author Ateng
// @since 2026-10-08
package platform

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// accountLoginGate 串行化同一平台账号（platform:username）的 Web 换票操作，
// 并记录最近一次换票时间以控制多角色共享账号时的最小换票间隔，避免互相顶号。
type accountLoginGate struct {
	mu        sync.Mutex
	lastLogin time.Time
}

// TicketCache 提供平台登录凭据的内存缓存、时效检测、账号级串行换票与静默刷新管理。
type TicketCache struct {
	mu                      sync.RWMutex
	tickets                 map[string]*Ticket
	authenticators          map[string]PlatformAuthenticator
	accountGates            map[string]*accountLoginGate
	defaultTTL              time.Duration
	minAccountLoginInterval time.Duration
}

// NewTicketCache 创建并初始化票据缓存容器。
func NewTicketCache(ttl time.Duration) *TicketCache {
	if ttl <= 0 {
		ttl = 90 * time.Minute // 默认 90 分钟主动静默续约
	}
	return &TicketCache{
		tickets:        make(map[string]*Ticket),
		authenticators: make(map[string]PlatformAuthenticator),
		accountGates:   make(map[string]*accountLoginGate),
		defaultTTL:     ttl,
	}
}

// SetMinAccountLoginInterval 配置同一平台账号两次真实 Web 换票之间的最小间隔。
// 用于多区服角色共享同一平台账号时，防止运行期并发换票互相顶掉登录态。
func (c *TicketCache) SetMinAccountLoginInterval(d time.Duration) {
	c.mu.Lock()
	c.minAccountLoginInterval = d
	c.mu.Unlock()
}

func (c *TicketCache) getAccountGate(accountKey string) (*accountLoginGate, time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	gate, ok := c.accountGates[accountKey]
	if !ok {
		gate = &accountLoginGate{}
		c.accountGates[accountKey] = gate
	}
	return gate, c.minAccountLoginInterval
}

// RegisterAuthenticator 注册指定运营平台的认证驱动。
func (c *TicketCache) RegisterAuthenticator(auth PlatformAuthenticator) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.authenticators[stringsLower(auth.Platform())] = auth
}

// GetOrFetch 获取有效的双轨凭据，若缓存未命中或已过期，将自动调用对应平台驱动进行换票。
// 同一平台账号（platform:username）下的所有区服角色共用一把账号级换票门闩：
//   - 不同账号之间并发换票互不阻塞；
//   - 同一账号的不同区服严格串行，并遵循 minAccountLoginInterval 间隔避免互相顶号。
func (c *TicketCache) GetOrFetch(ctx context.Context, platformName, username, password, serverID string) (*Ticket, error) {
	cacheKey := makeCacheKey(platformName, username, serverID)

	// 1. 先进行读锁快速判断
	c.mu.RLock()
	cached, ok := c.tickets[cacheKey]
	if ok && !cached.IsExpired() {
		c.mu.RUnlock()
		return cached, nil
	}
	auth, hasAuth := c.authenticators[stringsLower(platformName)]
	c.mu.RUnlock()

	if !hasAuth {
		return nil, fmt.Errorf("platform: authenticator for '%s' not registered", platformName)
	}

	// 2. 获取账号级门闩，串行化同一账号（跨区服）的 Web 换票，不持有全局 c.mu 进行网络 I/O
	accountKey := makeAccountKey(platformName, username)
	gate, minInterval := c.getAccountGate(accountKey)
	gate.mu.Lock()
	defer gate.mu.Unlock()

	// 二次检查缓存（可能刚被同区服并发调用刷新）
	c.mu.RLock()
	if cached, ok := c.tickets[cacheKey]; ok && !cached.IsExpired() {
		c.mu.RUnlock()
		return cached, nil
	}
	c.mu.RUnlock()

	// 同账号最小换票间隔保护：避免同账号多角色在极短时间内连续 Web 登录互顶。
	if minInterval > 0 && !gate.lastLogin.IsZero() {
		if wait := minInterval - time.Since(gate.lastLogin); wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
	}

	// 3. 执行平台认证换票
	ticket, err := auth.Login(ctx, username, password, serverID)
	gate.lastLogin = time.Now()
	if err != nil {
		return nil, fmt.Errorf("platform: login failed for user %s on %s: %w", username, serverID, err)
	}

	// 若未指定过期时间，填充默认 TTL
	if ticket.ExpiresAt.IsZero() {
		ticket.CreatedAt = time.Now()
		ticket.ExpiresAt = ticket.CreatedAt.Add(c.defaultTTL)
	}

	c.mu.Lock()
	c.tickets[cacheKey] = ticket
	c.mu.Unlock()
	return ticket, nil
}

// Invalidate 使特定账号与区服的票据失效 (通常在长连接收到鉴权失败状态时触发被动重登)。
func (c *TicketCache) Invalidate(platformName, username, serverID string) {
	cacheKey := makeCacheKey(platformName, username, serverID)
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.tickets, cacheKey)
}

func makeAccountKey(platformName, username string) string {
	return fmt.Sprintf("%s:%s", stringsLower(platformName), username)
}

func makeCacheKey(platformName, username, serverID string) string {
	return fmt.Sprintf("%s:%s:%s", stringsLower(platformName), username, stringsLower(serverID))
}

func stringsLower(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b = append(b, c)
	}
	return string(b)
}
