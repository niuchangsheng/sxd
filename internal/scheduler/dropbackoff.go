// 本文件提供「连续静默丢包」退避：服务端对按日状态已结束的玩法会静默丢弃
// 请求（不回任何结果码），例程若按 10~30 分钟反复重试会整晚空等。
// 达到阈值后直接挂起到次日每日重置 (00:05)，与用户要求的「N 次失败才退出」
// 及「有冷却时间的应在冷却结束后再唤醒」保持一致。
package scheduler

import (
	"fmt"
	"sync"
	"sync/atomic"
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

// silentDropCounter 连续静默丢包计数（按 例程+角色 维度）。
var silentDropCounters sync.Map // string -> *atomic.Int32

func silentDropKey(session *client.RoleSession, routine string) string {
	roleID := ""
	if session != nil {
		roleID = session.RoleID()
	}
	return routine + "|" + roleID
}

// NoteSilentDrop 记录一次静默丢包，返回当前连续次数。
func NoteSilentDrop(session *client.RoleSession, routine string) int {
	v, _ := silentDropCounters.LoadOrStore(silentDropKey(session, routine), new(atomic.Int32))
	return int(v.(*atomic.Int32).Add(1))
}

// ResetSilentDrops 清零连续静默丢包计数（请求恢复后调用）。
func ResetSilentDrops(session *client.RoleSession, routine string) {
	silentDropCounters.Delete(silentDropKey(session, routine))
}

// SilentDropCount 返回当前连续静默丢包次数（用于日志/断言）。
func SilentDropCount(session *client.RoleSession, routine string) int {
	if v, ok := silentDropCounters.Load(silentDropKey(session, routine)); ok {
		return int(v.(*atomic.Int32).Load())
	}
	return 0
}

// NextDailyReset 返回下一次每日重置时刻（00:05）。
func NextDailyReset(now time.Time) time.Time {
	reset := time.Date(now.Year(), now.Month(), now.Day(), dailyResetHour, dailyResetMinute, 0, 0, now.Location())
	if !now.Before(reset) {
		reset = reset.AddDate(0, 0, 1)
	}
	return reset
}

// HandleSilentDrop 按连续丢包次数返回冷却：
//   - 未达阈值：按 retry 重试（且不超过每日重置时刻）；
//   - 达到阈值：挂起到次日 00:05，避免整晚反复请求必被丢弃的玩法。
func HandleSilentDrop(session *client.RoleSession, routine string, threshold int, retry time.Duration, reason string) error {
	// 会话已断开时，请求超时不是「服务端静默丢包」，而是网络中断。此时不应
	// 累计丢包次数或进入长冷却，交给调度器的 ErrNotConnected 分支 5 分钟后
	// 重连重试（2026-11-22 实测：断线期间无尽洞天被误判为静默丢包而挂起 30 分钟）。
	if session == nil || session.State() != client.StateActive {
		return client.ErrNotConnected
	}
	if threshold <= 0 {
		threshold = 3
	}
	n := NoteSilentDrop(session, routine)
	now := time.Now()
	until := NextDailyReset(now)
	if cap := now.Add(maxSilentDropBackoff); until.After(cap) {
		until = cap
	}
	if n >= threshold {
		return NewCooldownUntil(until, fmt.Sprintf("%s（连续静默丢包 %d 次，最长挂起 %s）",
			reason, n, maxSilentDropBackoff))
	}
	if retry <= 0 {
		retry = 30 * time.Minute
	}
	if d := time.Until(until); d > 0 && retry > d {
		retry = d
	}
	return NewCooldown(retry, fmt.Sprintf("%s（连续静默丢包 %d 次）", reason, n))
}
