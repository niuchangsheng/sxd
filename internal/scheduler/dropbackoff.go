// 本文件提供「连续静默丢包」退避：服务端对按日状态已结束的玩法会静默丢弃
// 请求（不回任何结果码），例程若按 10~30 分钟反复重试会整晚空等。
// 达到阈值后直接挂起到 min(次日 00:05, 6 小时)，与「N 次失败才退出」
// 及「有冷却时间的应在冷却结束后再唤醒」保持一致。
package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"sxd-pie-ng/internal/client"
)

// dailyResetHour/Minute 每日重置时刻（与调度器的定点唤醒一致）。
const (
	dailyResetHour   = 0
	dailyResetMinute = 5
)

// maxSilentDropBackoff 达到阈值后的最长等待。即使按日状态已结束，也保留
// 每天约 4 次低频探测，避免错过「晚间才开启」的活动窗口。
const maxSilentDropBackoff = 6 * time.Hour

// defaultSilentDropThreshold 默认连续静默丢包熔断阈值。
const defaultSilentDropThreshold = 3

// defaultSilentDropRetry 默认单次静默丢包短重试间隔。
const defaultSilentDropRetry = 30 * time.Minute

// DropTracker 维护隔离作用域内的连续静默丢包计数（按 角色+玩法 维度），
// 由 RoleScheduler 实例持有并通过 context 透传，避免进程级全局状态在多调度器
// 与并行单元测试之间互相污染。
type DropTracker struct {
	mu       sync.Mutex
	counters map[string]int
}

// NewDropTracker 创建新的隔离静默丢包计数器。
func NewDropTracker() *DropTracker {
	return &DropTracker{
		counters: make(map[string]int),
	}
}

func dropKey(roleID, routine string) string {
	return routine + "|" + roleID
}

func sessionRoleID(session *client.RoleSession) string {
	if session == nil {
		return ""
	}
	return session.RoleID()
}

// Note 记录一次连续静默丢包并返回累计次数。
func (t *DropTracker) Note(roleID, routine string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.counters == nil {
		t.counters = make(map[string]int)
	}
	key := dropKey(roleID, routine)
	t.counters[key]++
	return t.counters[key]
}

// Reset 清零指定角色与玩法的连续静默丢包计数。
func (t *DropTracker) Reset(roleID, routine string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.counters, dropKey(roleID, routine))
}

// Count 查询指定角色与玩法当前的连续静默丢包次数。
func (t *DropTracker) Count(roleID, routine string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.counters[dropKey(roleID, routine)]
}

// BackoffUntil 根据当前连续丢包次数与阈值计算下一次唤醒时间与是否已触发长挂起。
func (t *DropTracker) BackoffUntil(now time.Time, count, threshold int, retry time.Duration) (until time.Time, escalated bool) {
	if threshold <= 0 {
		threshold = defaultSilentDropThreshold
	}
	capUntil := NextDailyReset(now)
	if maxCap := now.Add(maxSilentDropBackoff); capUntil.After(maxCap) {
		capUntil = maxCap
	}
	if count >= threshold {
		return capUntil, true
	}
	if retry <= 0 {
		retry = defaultSilentDropRetry
	}
	if d := capUntil.Sub(now); d > 0 && retry > d {
		retry = d
	}
	if retry < time.Second {
		retry = time.Second
	}
	return now.Add(retry), false
}

// Handle 按连续丢包次数返回对应冷却错误；会话未激活时直接返回 client.ErrNotConnected 且不累计计数。
func (t *DropTracker) Handle(session *client.RoleSession, routine string, threshold int, retry time.Duration, reason string) error {
	if session == nil || session.State() != client.StateActive {
		return client.ErrNotConnected
	}
	if threshold <= 0 {
		threshold = defaultSilentDropThreshold
	}
	n := t.Note(sessionRoleID(session), routine)
	until, escalated := t.BackoffUntil(time.Now(), n, threshold, retry)
	if escalated {
		return NewCooldownUntil(until, fmt.Sprintf("%s（连续静默丢包 %d 次，最长挂起 %s）",
			reason, n, maxSilentDropBackoff))
	}
	return NewCooldownUntil(until, fmt.Sprintf("%s（连续静默丢包 %d 次）", reason, n))
}

type dropTrackerCtxKey struct{}

// WithDropTracker 将隔离的 DropTracker 注入上下文。RoleScheduler 在拉起 Routine 时自动注入。
func WithDropTracker(ctx context.Context, dt *DropTracker) context.Context {
	if dt == nil {
		return ctx
	}
	return context.WithValue(ctx, dropTrackerCtxKey{}, dt)
}

// DropTrackerFromContext 从上下文获取 DropTracker；若上下文未注入则返回 nil。
func DropTrackerFromContext(ctx context.Context) *DropTracker {
	if ctx == nil {
		return nil
	}
	dt, _ := ctx.Value(dropTrackerCtxKey{}).(*DropTracker)
	return dt
}

// defaultDropTracker 仅用于未通过 RoleScheduler/ctx 注入 DropTracker 的存量直接调用路径。
var defaultDropTracker = NewDropTracker()

func resolveTracker(ctx context.Context) *DropTracker {
	if dt := DropTrackerFromContext(ctx); dt != nil {
		return dt
	}
	return defaultDropTracker
}

// NoteSilentDrop 记录一次静默丢包，返回当前连续次数（存量全局路径）。
func NoteSilentDrop(session *client.RoleSession, routine string) int {
	return defaultDropTracker.Note(sessionRoleID(session), routine)
}

// ResetSilentDrops 清零连续静默丢包计数（存量全局路径）。
func ResetSilentDrops(session *client.RoleSession, routine string) {
	defaultDropTracker.Reset(sessionRoleID(session), routine)
}

// SilentDropCount 返回当前连续静默丢包次数（存量全局路径）。
func SilentDropCount(session *client.RoleSession, routine string) int {
	return defaultDropTracker.Count(sessionRoleID(session), routine)
}

// HandleSilentDropCtx 优先使用上下文绑定的隔离 DropTracker 处理连续静默丢包。
func HandleSilentDropCtx(ctx context.Context, session *client.RoleSession, routine string, threshold int, retry time.Duration, reason string) error {
	return resolveTracker(ctx).Handle(session, routine, threshold, retry, reason)
}

// NoteSilentDropCtx 优先使用上下文绑定的隔离 DropTracker 记录一次连续静默丢包。
func NoteSilentDropCtx(ctx context.Context, session *client.RoleSession, routine string) int {
	return resolveTracker(ctx).Note(sessionRoleID(session), routine)
}

// SilentDropCountCtx 优先使用上下文绑定的隔离 DropTracker 查询当前连续静默丢包次数。
func SilentDropCountCtx(ctx context.Context, session *client.RoleSession, routine string) int {
	return resolveTracker(ctx).Count(sessionRoleID(session), routine)
}

// ResetSilentDropsCtx 优先使用上下文绑定的隔离 DropTracker 清零连续静默丢包计数。
func ResetSilentDropsCtx(ctx context.Context, session *client.RoleSession, routine string) {
	resolveTracker(ctx).Reset(sessionRoleID(session), routine)
}

// NextDailyReset 返回下一次每日重置时刻（00:05）。
func NextDailyReset(now time.Time) time.Time {
	reset := time.Date(now.Year(), now.Month(), now.Day(), dailyResetHour, dailyResetMinute, 0, 0, now.Location())
	if !now.Before(reset) {
		reset = reset.AddDate(0, 0, 1)
	}
	return reset
}

// HandleSilentDrop 按连续丢包次数返回冷却（存量全局路径）：
//   - 未达阈值：按 retry 重试（且不超过每日重置时刻）；
//   - 达到阈值：挂起到 min(次日 00:05, 6 小时)，避免整晚反复请求必被丢弃的玩法。
func HandleSilentDrop(session *client.RoleSession, routine string, threshold int, retry time.Duration, reason string) error {
	return defaultDropTracker.Handle(session, routine, threshold, retry, reason)
}
