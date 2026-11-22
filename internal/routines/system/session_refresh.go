// 本文件实现「定期重建会话上下文」维护任务。
//
// 背景（2026-11-22 实测）：
//   - 同一账号登录后 0-8 分钟内，所有业务请求正常回包；
//   - 会话持续约 8 分钟后，即使 PrevAct 完全正确，成批业务请求也会被服务端
//     静默丢弃；重新登录后立即恢复。
//
// 该任务以固定间隔在调度空隙中主动断开并重连，把长会话切成多个「健康窗口」。
// 调度器单协程顺序执行，因此不会打断正在进行的玩法；重连期间其他任务会收到
// ErrNotConnected 并按既有 5 分钟策略重试。
package system

import (
	"context"
	"log/slog"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/scheduler"
)

// sessionRefreshInterval 会话重建检查周期。
// 每次检查仅在连接存活超过 minSessionAge 时才真正重连。
const sessionRefreshInterval = 2 * time.Minute

// minSessionAge 触发主动重建的最小连接存活时长。
//
// 线上静默丢弃约在登录后 8 分钟出现；连接满 4 分钟后、下一次 2 分钟检查
// （即登录后 4-6 分钟）立即重建，留出安全余量。刚被外部顶号重连的会话
// 不会被再次重建，避免连续登录。
var minSessionAge = 4 * time.Minute

// sessionRefreshTimeout 等待重连重新激活的上限。
const sessionRefreshTimeout = 90 * time.Second

// NewSessionRefreshRoutine 构造会话上下文刷新任务。
func NewSessionRefreshRoutine() scheduler.ActivityRoutine {
	return scheduler.NewBaseRoutine(
		"session_refresh",
		"system",
		scheduler.ScheduleLoop,
		// 高于推图/宝库：到期后优先在下一个调度空隙执行。
		scheduler.PhasePriority(scheduler.PhaseSystem, 0),
		sessionRefreshInterval,
		func(ctx context.Context, session *client.RoleSession, jitter *scheduler.Jitter) error {
			if session == nil {
				return client.ErrNotConnected
			}
			if session.State() != client.StateActive {
				// 已在重连中：交给会话状态机，不重复触发。
				return nil
			}
			if since := session.ConnectionSince(); !since.IsZero() &&
				time.Since(since) < minSessionAge {
				// 会话仍处在登录后的健康窗口，等待下一次检查。
				return nil
			}
			if jitter != nil {
				_ = jitter.Wait(ctx)
			}
			slog.Info("定期刷新会话上下文，规避长会话静默丢包",
				"role_id", session.RoleID(),
				"last_business", session.LastBusinessActionID(),
				"last_rx", session.LastRxActionID())
			gen := session.ConnectionGeneration()
			session.ReconnectNow()

			deadline := time.Now().Add(sessionRefreshTimeout)
			for {
				// 必须等到「新一代连接」建立（仅看 State 会命中关闭前的瞬时 Active）。
				if session.ConnectionGeneration() > gen && session.State() == client.StateActive {
					slog.Info("会话上下文刷新完成", "role_id", session.RoleID(),
						"generation", session.ConnectionGeneration())
					return nil
				}
				if session.State() == client.StateClosed {
					return client.ErrNotConnected
				}
				if time.Now().After(deadline) {
					slog.Warn("会话上下文刷新超时，交给调度器稍后重试",
						"role_id", session.RoleID(), "state", session.State())
					return client.ErrNotConnected
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(200 * time.Millisecond):
				}
			}
		},
	)
}
